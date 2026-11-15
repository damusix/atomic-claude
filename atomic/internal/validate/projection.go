// The projection gate re-renders the canonical corpus through the real adapters
// and audits each projection against the capability record, duplicating no adapter logic.
package validate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
	"github.com/pelletier/go-toml/v2"
)

// Rule IDs for the projection gate; see docs/spec/atomic-validate.md. P0 reports
// an adapter that cannot render at all instead of silently skipping it.
const (
	ruleDependency    = "P1"
	ruleIdentity      = "P2"
	ruleMetadata      = "P3"
	ruleDeterminism   = "P4"
	ruleTier          = "P5"
	ruleWireToken     = "P6"
	ruleRenderFailure = "P0"
)

// projGate accumulates projection findings for one corpus pass.
type projGate struct {
	root string
	cat  *artifacts.Catalog
	ids  map[string]bool
	// supports overrides capability role proof, so a test can exercise a tier
	// the shipped matrix cannot prove; nil means the shipped matrix.
	supports func(artifacts.Target, harness.Role) bool
	findings []Finding
}

// RunProjectionRules re-renders and audits the canonical corpus for every adapter.
// It requires the atomic-claude corpus, so callers gate on repoDev.
func RunProjectionRules(repoRoot string) ([]Finding, error) {
	_, findings, err := projectionRules(repoRoot)
	return findings, err
}

// projectionRules also returns the canonical sources it audited, keyed by
// context-relative Source so summarize can count the ones no finding names as PASS.
func projectionRules(repoRoot string) ([]string, []Finding, error) {
	cat, err := artifacts.Load(repoRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate corpus: %w", err)
	}

	g := &projGate{root: repoRoot, cat: cat, ids: make(map[string]bool, len(cat.Artifacts))}
	inspected := make([]string, 0, len(cat.Artifacts))
	for _, a := range cat.Artifacts {
		g.ids[a.ID] = true
		inspected = append(inspected, a.Source)
	}

	g.checkCorpus()
	g.checkAgents()
	reports := g.skillReports()
	g.checkSkills(reports)
	g.checkCommands()
	g.checkRules()

	sortFindings(g.findings)
	return inspected, g.findings, nil
}

// runProjections discovers the repo root and renders it for the audit.
func runProjections(jsonOut, suggest bool, w io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(w, "atomic validate projections: cannot get working directory: %v\n", err)
		return 2
	}
	return runProjectionsAt(findRepoRoot(cwd), jsonOut, suggest, w)
}

// runProjectionsAt audits repoRoot's canonical corpus, skipping with exit 0
// outside the atomic-claude dev repo.
func runProjectionsAt(repoRoot string, jsonOut, suggest bool, w io.Writer) int {
	if !repoDev(repoRoot) {
		if jsonOut {
			printJSON(w, nil, summary{})
		} else {
			printHeader(w, "projections", "canonical corpus projection audit")
			fmt.Fprintln(w, "SKIP — not in atomic-claude repo (no canonical corpus to render)")
		}
		return 0
	}

	inspected, findings, err := projectionRules(repoRoot)
	if err != nil {
		fmt.Fprintf(w, "atomic validate projections: %v\n", err)
		return 2
	}

	s := summarize(inspected, findings)
	if jsonOut {
		printJSON(w, findings, s)
	} else {
		printHeader(w, "projections", "canonical corpus projection audit")
		printHuman(w, findings, s, suggest)
	}
	return exitCode(s)
}

// runProjectionsCollect returns findings without printing, so runWholeRepo can
// aggregate before emitting its own block.
func runProjectionsCollect(repoRoot string) ([]Finding, summary, int) {
	inspected, findings, err := projectionRules(repoRoot)
	if err != nil {
		return nil, summary{}, 2
	}
	return findings, summarize(inspected, findings), 0
}

func (g *projGate) fail(rule, path, format string, args ...any) {
	g.findings = append(g.findings, Finding{
		Severity: "FAIL",
		Rule:     rule,
		Path:     path,
		Message:  fmt.Sprintf(format, args...),
	})
}

// warn records a gap the gate cannot judge. Claude Code accepts frontmatter YAML
// rejects, such as an unquoted colon, so an unparseable document warns, not fails.
func (g *projGate) warn(rule, path, format string, args ...any) {
	g.findings = append(g.findings, Finding{
		Severity: "WARN",
		Rule:     rule,
		Path:     path,
		Message:  fmt.Sprintf(format, args...),
	})
}

// checkCorpus audits unique IDs and resolved dependencies. SourceDigest is not
// re-verified: artifacts.Load stamps it from the same bytes, so it cannot disagree.
func (g *projGate) checkCorpus() {
	seen := make(map[string]bool, len(g.cat.Artifacts))
	for _, a := range g.cat.Artifacts {
		if seen[a.ID] {
			g.fail(ruleIdentity, a.Source, "duplicate canonical identity %s", a.ID)
		}
		seen[a.ID] = true

		for _, req := range a.Semantics.Requires {
			if !g.ids[req] {
				g.fail(ruleDependency, a.Source, "%s requires %s, which the corpus does not carry", a.ID, req)
			}
		}

		if a.Kind == artifacts.KindSteering {
			g.checkSteeringNeutral(a)
		}
	}
}

// checkSteeringNeutral rejects a Claude-only wire token in canonical steering:
// every harness renders it, and only an adapter's vocabulary paragraph names tools.
func (g *projGate) checkSteeringNeutral(a artifacts.Artifact) {
	for _, u := range harness.HarnessRuntimeUses(a.Body) {
		if u.Wire {
			g.fail(ruleWireToken, a.Source, "%s: canonical steering carries Claude-only wire token %q", a.ID, u.Name)
		}
	}
}

// audit renders one artifact for one target twice and checks the projection
// invariants. reported names the harness markers the adapter already classifies.
func (g *projGate) audit(a artifacts.Artifact, target artifacts.Target, render func(artifacts.Artifact) (artifacts.Projection, error), reported map[string]bool) {
	p, err := render(a)
	if err != nil {
		g.fail(ruleRenderFailure, a.Source, "%s: %s projection failed: %v", a.ID, target, err)
		return
	}

	if p.Artifact != a.ID || !g.ids[p.Artifact] {
		g.fail(ruleIdentity, a.Source, "%s: %s projection carries identity %q, which does not resolve to its artifact", a.ID, target, p.Artifact)
	}
	if p.Digest == "" || p.Digest != artifacts.ProjectionDigest(p.Bytes) {
		g.fail(ruleDeterminism, a.Source, "%s: %s projection digest %q does not match its bytes", a.ID, target, p.Digest)
	}

	again, err := render(a)
	if err != nil {
		g.fail(ruleRenderFailure, a.Source, "%s: %s projection re-render failed: %v", a.ID, target, err)
		return
	}
	if string(again.Bytes) != string(p.Bytes) || again.Digest != p.Digest {
		g.fail(ruleDeterminism, a.Source, "%s: %s projection is not deterministic (%s != %s)", a.ID, target, p.Digest, again.Digest)
	}

	g.checkNativeMetadata(a, target, p.Bytes)
	g.checkTier(a, target, p)
	g.checkWireTokens(a, target, p.Bytes, reported)
}

// matrixFor resolves the capability record a target's projections are
// audited against.
func matrixFor(target artifacts.Target) harness.CapabilityMatrix {
	switch target {
	case artifacts.TargetClaude:
		return harness.ClaudeCapabilities()
	case artifacts.TargetOMP:
		return harness.OMPCapabilities()
	default:
		return harness.CodexCapabilities()
	}
}

// tierRoles is the role set each enforcement tier stands on. Every role must be
// proven, or a target proving half of hook-required would pass an overclaim.
var tierRoles = map[artifacts.EnforcementTier][]harness.Role{
	artifacts.EnforcementNativeScope:  {harness.RoleStaticScope},
	artifacts.EnforcementHookRequired: {harness.RolePreOperationTargets, harness.RoleContextReturn},
	artifacts.EnforcementDenyEnforced: {harness.RoleDeterministicDeny},
}

// unprovenTierRole returns the first role of tier's required set the target
// does not prove; known is false when tier is not a known enforcement tier.
func (g *projGate) unprovenTierRole(tier artifacts.EnforcementTier, target artifacts.Target) (role harness.Role, known bool) {
	roles, ok := tierRoles[tier]
	if !ok {
		return "", false
	}
	for _, role := range roles {
		if !g.proves(target, role) {
			return role, true
		}
	}
	return "", true
}

// proves reports whether target's capability record supports role, unless a
// test injected its own predicate.
func (g *projGate) proves(target artifacts.Target, role harness.Role) bool {
	if g.supports != nil {
		return g.supports(target, role)
	}
	return matrixFor(target).Supports(role)
}

// checkTier rejects a projection that claims a stronger native guarantee than
// every capability role backing it proves.
func (g *projGate) checkTier(a artifacts.Artifact, target artifacts.Target, p artifacts.Projection) {
	if p.Enforcement == artifacts.EnforcementUnsupported {
		return
	}
	role, known := g.unprovenTierRole(p.Enforcement, target)
	if !known {
		g.fail(ruleTier, a.Source, "%s: %s projection claims unknown enforcement tier %q", a.ID, target, p.Enforcement)
		return
	}
	if role != "" {
		g.fail(ruleTier, a.Source, "%s: %s projection claims %q but CP0 role %q is not supported", a.ID, target, p.Enforcement, role)
	}
}

// checkNativeMetadata rejects a model, effort, or tool key in a projected native
// document; user configuration owns those fields, so one is a leak.
func (g *projGate) checkNativeMetadata(a artifacts.Artifact, target artifacts.Target, projected []byte) {
	keys, err := nativeMetadataKeys(a.Kind, target, projected)
	if err != nil {
		g.warn(ruleMetadata, a.Source, "%s: %s projection metadata is unreadable: %v", a.ID, target, err)
		return
	}
	for _, key := range keys {
		if isUserPolicyKey(key) {
			g.fail(ruleMetadata, a.Source, "%s: %s projection carries native metadata key %q, which user model policy owns", a.ID, target, key)
		}
	}
}

// checkRuleMetadata rejects a user-policy key in a projected rule document and
// reports unparseable frontmatter rather than skipping it.
func (g *projGate) checkRuleMetadata(path, recordID string, target artifacts.Target, projected []byte) {
	keys, err := nativeMetadataKeys(artifacts.KindRule, target, projected)
	if err != nil {
		g.warn(ruleMetadata, path, "%s: %s rule projection metadata is unreadable: %v", recordID, target, err)
		return
	}
	for _, key := range keys {
		if isUserPolicyKey(key) {
			g.fail(ruleMetadata, path, "%s: rule projection carries native metadata key %q, which user model policy owns", recordID, key)
		}
	}
}

// checkWireTokens rejects a Claude-only wire token in another target's projected
// bytes, unless the adapter already reports that marker.
func (g *projGate) checkWireTokens(a artifacts.Artifact, target artifacts.Target, projected []byte, reported map[string]bool) {
	if target == artifacts.TargetClaude {
		return
	}
	for _, u := range harness.HarnessRuntimeUses(projected) {
		if !u.Wire || reported[u.Name] {
			continue
		}
		g.fail(ruleWireToken, a.Source, "%s: %s projection leaks Claude-only wire token %q", a.ID, target, u.Name)
	}
}

// checkAgents projects every canonical agent for Claude, OMP, and Codex.
func (g *projGate) checkAgents() {
	for _, a := range g.cat.OfKind(artifacts.KindAgent) {
		g.audit(a, artifacts.TargetClaude, func(a artifacts.Artifact) (artifacts.Projection, error) {
			return harness.ClaudeAgent(g.cat, a)
		}, nil)
		g.audit(a, artifacts.TargetOMP, func(a artifacts.Artifact) (artifacts.Projection, error) {
			return harness.OMPAgent(g.cat, a)
		}, nil)
		g.audit(a, artifacts.TargetCodex, func(a artifacts.Artifact) (artifacts.Projection, error) {
			return harness.CodexAgent(g.cat, a)
		}, nil)
		g.checkAgentDrops(a)
	}
}

// checkAgentDrops requires an agent projection to report every field it could not
// carry, since an unlabeled drop reads as support the target never proved.
func (g *projGate) checkAgentDrops(a artifacts.Artifact) {
	if len(a.Semantics.Requires) == 0 {
		return
	}
	for _, target := range []artifacts.Target{artifacts.TargetOMP, artifacts.TargetCodex} {
		p, err := agentProjector(target)(g.cat, a)
		if err != nil {
			continue // already reported by audit
		}
		if !containsString(p.Unsupported, "skills") {
			g.fail(ruleTier, a.Source, "%s: %s projection drops the canonical skills dependency without reporting it unsupported", a.ID, target)
		}
	}
}

// agentProjector resolves the per-target agent projection function.
func agentProjector(target artifacts.Target) func(*artifacts.Catalog, artifacts.Artifact) (artifacts.Projection, error) {
	switch target {
	case artifacts.TargetOMP:
		return harness.OMPAgent
	case artifacts.TargetCodex:
		return harness.CodexAgent
	default:
		return harness.ClaudeAgent
	}
}

// skillReports indexes the harness markers each projected skill file classifies,
// so the wire-token audit can tell a reported gap from a silent leak.
func (g *projGate) skillReports() map[artifacts.Target]map[string]map[string]bool {
	reports := make(map[artifacts.Target]map[string]map[string]bool, 2)
	for _, target := range []artifacts.Target{artifacts.TargetClaude, artifacts.TargetOMP, artifacts.TargetCodex} {
		report, err := harness.ProjectSkills(g.cat, target, harness.SkillPolicy{}, matrixFor(target))
		if err != nil {
			g.fail(ruleRenderFailure, "skills", "project %s skill corpus: %v", target, err)
			continue
		}
		byArtifact := make(map[string]map[string]bool, len(report.Files))
		for _, use := range report.Runtime {
			if use.Surface != harness.SkillRuntimeHarness {
				continue
			}
			id := string(artifacts.KindSkill) + ":" + use.File
			if byArtifact[id] == nil {
				byArtifact[id] = map[string]bool{}
			}
			byArtifact[id][use.Name] = true
		}
		reports[target] = byArtifact
	}
	return reports
}

// checkSkills projects every canonical skill file into Claude's and OMP's
// native trees and audits the result.
func (g *projGate) checkSkills(reports map[artifacts.Target]map[string]map[string]bool) {
	for _, a := range g.cat.OfKind(artifacts.KindSkill) {
		g.audit(a, artifacts.TargetClaude, harness.ClaudeSkill, reports[artifacts.TargetClaude][a.ID])
		g.audit(a, artifacts.TargetOMP, harness.OMPSkill, reports[artifacts.TargetOMP][a.ID])
		g.audit(a, artifacts.TargetCodex, harness.CodexSkill, reports[artifacts.TargetCodex][a.ID])
		g.checkSkillDrops(a)
	}
}

// checkSkillDrops requires an OMP or Codex skill manifest to report every
// frontmatter key it cannot carry; referenced files carry no metadata contract.
func (g *projGate) checkSkillDrops(a artifacts.Artifact) {
	if !harness.SkillManifest(a) {
		return
	}
	kvs, _, err := frontmatter.ParseOrdered(string(a.Body))
	if err != nil {
		return
	}
	for _, tc := range []struct {
		project func(artifacts.Artifact) (artifacts.Projection, error)
		label   string
	}{
		{harness.OMPSkill, "OMP"},
		{harness.CodexSkill, "Codex"},
	} {
		p, err := tc.project(a)
		if err != nil {
			continue // already reported by audit
		}
		for _, kv := range kvs {
			if kv.Key == "name" || kv.Key == "description" {
				continue
			}
			if !containsString(p.Unsupported, kv.Key) {
				g.fail(ruleTier, a.Source, "%s: %s projection drops canonical metadata key %q without reporting it unsupported", a.ID, tc.label, kv.Key)
			}
		}
	}
}

// checkCommands audits every canonical command as a Claude native command, whose
// bytes are the authored bytes, so no command adapter is involved.
func (g *projGate) checkCommands() {
	for _, a := range g.cat.OfKind(artifacts.KindCommand) {
		g.audit(a, artifacts.TargetClaude, commandProjection, nil)
	}
}

// commandProjection renders a canonical command into Claude's native command
// file: the authored bytes install verbatim.
func commandProjection(a artifacts.Artifact) (artifacts.Projection, error) {
	if a.Kind != artifacts.KindCommand {
		return artifacts.Projection{}, fmt.Errorf("validate: %s is not a command", a.ID)
	}
	return artifacts.Projection{
		Artifact:    a.ID,
		Target:      artifacts.TargetClaude,
		Path:        a.Source,
		Bytes:       a.Body,
		Delivery:    artifacts.DeliveryDirect,
		Enforcement: artifacts.EnforcementUnsupported,
		Digest:      artifacts.ProjectionDigest(a.Body),
	}, nil
}

// checkRules projects the rule corpus into each target twice, pinning
// determinism and the capability tier ceiling.
func (g *projGate) checkRules() {
	g.checkClaudeRules()
	g.checkOMPRules()
	g.checkOMPRuntime()
}

// checkOMPRuntime fails an OMP extension event wired without a proven capability
// row, or a rule claiming more than instruction-only delivery, before a session.
func (g *projGate) checkOMPRuntime() {
	sources, err := shippedRuleSources(g.root)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "load shipped rules: %v", err)
		return
	}
	if len(sources) == 0 {
		g.fail(ruleRenderFailure, "context/rules", "the shipped rule corpus is empty")
		return
	}
	matrix := harness.OMPCapabilities()
	delivery, err := omp.BuildSessionDelivery(sources, matrix, nil)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "plan OMP runtime delivery: %v", err)
		return
	}
	again, err := omp.BuildSessionDelivery(sources, matrix, nil)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "re-plan OMP runtime delivery: %v", err)
		return
	}
	module, err := delivery.RenderExtension()
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "render OMP runtime module: %v", err)
		return
	}
	moduleAgain, err := again.RenderExtension()
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "re-render OMP runtime module: %v", err)
		return
	}
	if string(module) != string(moduleAgain) {
		g.fail(ruleDeterminism, omp.SkeletonPath, "the OMP runtime module is not deterministic")
	}
	// A template handler for an unproven role is invisible to delivery.Events,
	// so the artifact's own registrations are compared.
	if planned, registered := delivery.WiredEvents(), omp.RegisteredEvents(module); !sameEventSet(planned, registered) {
		g.fail(ruleMetadata, omp.SkeletonPath, "the rendered module registers events %v, but the delivery plans %v", registered, planned)
	}

	for _, event := range delivery.Events {
		if !event.Observation && event.Status != harness.StatusSupported {
			g.fail(ruleTier, omp.SkeletonPath, "runtime event %s rests on role %s, which CP0 reports %s", event.Event, event.Role, event.Status)
		}
		if event.Evidence == "" {
			g.fail(ruleMetadata, omp.SkeletonPath, "runtime event %s carries no CP0 evidence", event.Event)
		}
	}
	digests := make(map[string]string, len(sources))
	tiers := make(map[string]artifacts.EnforcementTier, len(sources))
	paths := make(map[string]string, len(sources))
	for _, source := range sources {
		digests[source.Record.ID] = source.Record.SourceDigest
		tiers[source.Record.ID] = rules.Select(source.Record, artifacts.TargetOMP, harness.RuleEvidence(matrix)).Tier
		paths[source.Record.ID] = source.Record.Source
	}
	for _, entry := range delivery.Index {
		path, ok := paths[entry.RecordID]
		if !ok {
			path = "context/rules"
		}
		if entry.SourceDigest != digests[entry.RecordID] {
			g.fail(ruleIdentity, path, "%s: runtime index digest %s does not match the authored source digest %s", entry.RecordID, entry.SourceDigest, digests[entry.RecordID])
		}
		if entry.Tier != tiers[entry.RecordID] {
			g.fail(ruleTier, path, "%s: runtime index tier %q does not match the projected tier %q", entry.RecordID, entry.Tier, tiers[entry.RecordID])
		}
		for _, pattern := range entry.Patterns {
			if pattern.Regex == "" {
				g.fail(ruleMetadata, path, "%s: runtime index pattern %q carries no expression", entry.RecordID, pattern.Glob)
			}
		}
	}
	for _, rule := range delivery.Undeliverable {
		g.fail(ruleMetadata, paths[rule.RecordID], "%s: rule patterns are outside the runtime dialect: %s", rule.RecordID, rule.Reason)
	}
	if delivery.Suppressed {
		g.fail(ruleMetadata, omp.SkeletonPath, "the runtime rule index exceeds the %d-unit bound at %d units (UTF-16 code units, the unit the delivered module measures)", delivery.Bound, delivery.IndexUnits)
	}
}

// sameEventSet reports whether two event-name lists name the same events,
// ignoring order and duplicates.
func sameEventSet(a, b []string) bool {
	as := make(map[string]bool, len(a))
	for _, name := range a {
		as[name] = true
	}
	bs := make(map[string]bool, len(b))
	for _, name := range b {
		bs[name] = true
	}
	if len(as) != len(bs) {
		return false
	}
	for name := range as {
		if !bs[name] {
			return false
		}
	}
	return true
}

// checkClaudeRules audits the Claude-native rule projection.
func (g *projGate) checkClaudeRules() {
	sources, err := shippedRuleSources(g.root)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "load shipped rules: %v", err)
		return
	}
	if len(sources) == 0 {
		g.fail(ruleRenderFailure, "context/rules", "the shipped rule corpus is empty")
		return
	}

	matrix := harness.ClaudeCapabilities()
	report, err := harness.ProjectClaudeRules(sources, matrix)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "project Claude rules: %v", err)
		return
	}
	again, err := harness.ProjectClaudeRules(sources, matrix)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "re-project Claude rules: %v", err)
		return
	}
	if len(again.Rules) != len(report.Rules) {
		g.fail(ruleDeterminism, "context/rules", "Claude rule projection is not deterministic (%d != %d rules)", len(report.Rules), len(again.Rules))
	}

	for i, r := range report.Rules {
		path := r.Source
		if got := managedfile.Digest(r.Bytes); got != r.SourceDigest {
			g.fail(ruleIdentity, path, "%s: projected bytes digest %s does not match source digest %s", r.RecordID, got, r.SourceDigest)
		}
		if r.Digest == "" || r.Digest != artifacts.ProjectionDigest(r.Bytes) {
			g.fail(ruleDeterminism, path, "%s: projection digest %q does not match its bytes", r.RecordID, r.Digest)
		}
		if i < len(again.Rules) && (again.Rules[i].Digest != r.Digest || string(again.Rules[i].Bytes) != string(r.Bytes)) {
			g.fail(ruleDeterminism, path, "%s: Claude rule projection is not deterministic", r.RecordID)
		}
		g.checkRuleMetadata(path, r.RecordID, artifacts.TargetClaude, r.Bytes)
		if r.Tier != artifacts.EnforcementUnsupported {
			role, known := g.unprovenTierRole(r.Tier, artifacts.TargetClaude)
			if !known {
				g.fail(ruleTier, path, "%s: rule projection claims unknown enforcement tier %q", r.RecordID, r.Tier)
			} else if role != "" {
				g.fail(ruleTier, path, "%s: rule projection claims %q but CP0 role %q is not supported", r.RecordID, r.Tier, role)
			}
		}
	}
}

// checkOMPRules audits the OMP-native rule projection. OMP 18.1.18 proved no
// context return, so no record may claim native scope or hook-required.
func (g *projGate) checkOMPRules() {
	matrix := harness.OMPCapabilities()
	report, err := omp.ProjectShippedRules(g.cat, matrix)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "project OMP rules: %v", err)
		return
	}
	again, err := omp.ProjectShippedRules(g.cat, matrix)
	if err != nil {
		g.fail(ruleRenderFailure, "context/rules", "re-project OMP rules: %v", err)
		return
	}
	if len(report.Rules) == 0 {
		g.fail(ruleRenderFailure, "context/rules", "the shipped rule corpus is empty")
		return
	}
	if len(again.Rules) != len(report.Rules) {
		g.fail(ruleDeterminism, "context/rules", "OMP rule projection is not deterministic (%d != %d rules)", len(report.Rules), len(again.Rules))
	}

	for i, r := range report.Rules {
		path := r.Source
		if got := managedfile.Digest(r.Bytes); got != r.SourceDigest {
			g.fail(ruleIdentity, path, "%s: projected bytes digest %s does not match source digest %s", r.RecordID, got, r.SourceDigest)
		}
		if r.Digest == "" || r.Digest != artifacts.ProjectionDigest(r.Bytes) {
			g.fail(ruleDeterminism, path, "%s: projection digest %q does not match its bytes", r.RecordID, r.Digest)
		}
		if i < len(again.Rules) && (again.Rules[i].Digest != r.Digest || string(again.Rules[i].Bytes) != string(r.Bytes)) {
			g.fail(ruleDeterminism, path, "%s: OMP rule projection is not deterministic", r.RecordID)
		}
		g.checkRuleMetadata(path, r.RecordID, artifacts.TargetOMP, r.Bytes)
		if r.Tier != artifacts.EnforcementUnsupported {
			role, known := g.unprovenTierRole(r.Tier, artifacts.TargetOMP)
			if !known {
				g.fail(ruleTier, path, "%s: rule projection claims unknown enforcement tier %q", r.RecordID, r.Tier)
			} else if role != "" {
				g.fail(ruleTier, path, "%s: rule projection claims %q but CP0 role %q is not supported", r.RecordID, r.Tier, role)
			}
		}
		if r.Tier == artifacts.EnforcementUnsupported && (len(r.Scope) != 0 || r.Delivery != rules.RuntimeNone) {
			g.fail(ruleTier, path, "%s: unsupported rule projection still claims scope %v / delivery %q", r.RecordID, r.Scope, r.Delivery)
		}
	}
}

// shippedRuleSources loads every authored rule record with its source bytes,
// the exact pair a rule projection consumes.
func shippedRuleSources(repoRoot string) ([]harness.RuleSource, error) {
	rulesDir := filepath.Join(bundlespec.SourceRoot(repoRoot), "rules")
	records, err := rules.LoadShipped(rulesDir)
	if err != nil {
		return nil, err
	}
	out := make([]harness.RuleSource, 0, len(records))
	for _, r := range records {
		data, err := os.ReadFile(filepath.Join(bundlespec.SourceRoot(repoRoot), filepath.FromSlash(r.Source)))
		if err != nil {
			return nil, err
		}
		out = append(out, harness.RuleSource{Record: r, Bytes: data})
	}
	return out, nil
}

// nativeMetadataKeys parses by document shape, not target: Codex agents are TOML
// but Codex skills are Markdown, which a target-keyed parser skipped silently.
func nativeMetadataKeys(kind artifacts.Kind, target artifacts.Target, projected []byte) ([]string, error) {
	if hasFrontmatter(projected) {
		kvs, _, err := frontmatter.ParseOrdered(string(projected))
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(kvs))
		for _, kv := range kvs {
			keys = append(keys, kv.Key)
		}
		sort.Strings(keys)
		return keys, nil
	}
	// A Codex agent document is TOML with no frontmatter fence. A file that is
	// neither form carries no metadata.
	if target != artifacts.TargetCodex || kind != artifacts.KindAgent {
		return nil, nil
	}
	var doc map[string]any
	if err := toml.Unmarshal(projected, &doc); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// hasFrontmatter reports whether a projected document opens with a Markdown
// frontmatter fence.
func hasFrontmatter(projected []byte) bool {
	text := strings.TrimPrefix(string(projected), "\ufeff")
	return strings.HasPrefix(text, "---\n") || strings.HasPrefix(text, "---\r\n")
}

// isUserPolicyKey reports whether key selects a model, a reasoning effort, or a
// tool surface, the fields user configuration owns.
func isUserPolicyKey(key string) bool {
	for _, k := range harness.UserPolicyKeys {
		if key == k {
			return true
		}
	}
	return false
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
