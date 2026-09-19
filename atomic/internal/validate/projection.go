// This file owns the projection gate: it re-renders the complete canonical
// corpus through the real target adapters and audits every projection against
// the CP0 capability record. It renders nothing itself and duplicates no
// adapter logic — a failure means the corpus or an adapter broke an invariant
// the projection contract promises.
package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
	"github.com/pelletier/go-toml/v2"
)

// Rule IDs for the projection gate, in the order the contract lists the
// classes:
//
//	P1 dependency resolution   — declared dependencies resolve by stable identity
//	P2 canonical identity      — projection carries its artifact ID; source digest unchanged
//	P3 native metadata safety  — no model/effort/tool policy leaks into native docs
//	P4 projection determinism  — re-render is byte-identical; digest matches the bytes
//	P5 enforcement tier        — no tier stronger than every CP0 role it stands on proves; drops reported
//	P6 wire-token portability  — no unclassified Claude-only wire token in projected bytes
//
// P0 is the audit's own render-failure surface: an adapter that cannot produce
// a projection at all names the artifact and the reason instead of being
// silently skipped.
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
	// supports overrides CP0 role proof, so a test can exercise a tier the
	// shipped matrix cannot prove; nil means the shipped matrix.
	supports func(artifacts.Target, harness.Role) bool
	findings []Finding
}

// RunProjectionRules re-renders the canonical corpus for every adapter and
// audits the results. It requires the atomic-claude corpus (context/), so
// callers gate on repoDev before invoking it.
func RunProjectionRules(repoRoot string) ([]Finding, error) {
	cat, err := artifacts.Load(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("enumerate corpus: %w", err)
	}

	g := &projGate{root: repoRoot, cat: cat, ids: make(map[string]bool, len(cat.Artifacts))}
	for _, a := range cat.Artifacts {
		g.ids[a.ID] = true
	}

	g.checkCorpus()
	g.checkAgents()
	reports := g.skillReports()
	g.checkSkills(reports)
	g.checkCommands()
	g.checkRules()

	sortFindings(g.findings)
	return g.findings, nil
}

// parseProjectionsFlags reads flags placed after the subcommand, so
// `atomic validate projections --json` behaves like the other subcommands.
func parseProjectionsFlags(args []string, w io.Writer) (jsonOut, suggest, ok bool) {
	fs := flag.NewFlagSet("validate projections", flag.ContinueOnError)
	fs.SetOutput(w)
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output")
	fs.BoolVar(&suggest, "suggest", false, "print structural templates for content-FAIL rules")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return false, false, true
		}
		return false, false, false
	}
	return jsonOut, suggest, true
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

// runProjectionsAt audits repoRoot's canonical corpus. Projections only exist
// in the atomic-claude dev repo, which owns context/; elsewhere the check skips
// cleanly with exit 0 like bundle parity.
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

	findings, err := RunProjectionRules(repoRoot)
	if err != nil {
		fmt.Fprintf(w, "atomic validate projections: %v\n", err)
		return 2
	}

	s := summarize(findings)
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
	findings, err := RunProjectionRules(repoRoot)
	if err != nil {
		return nil, summary{}, 2
	}
	return findings, summarize(findings), 0
}

func (g *projGate) fail(rule, path, format string, args ...any) {
	g.findings = append(g.findings, Finding{
		Severity: "FAIL",
		Rule:     rule,
		Path:     path,
		Message:  fmt.Sprintf(format, args...),
	})
}

// checkCorpus audits the shared corpus invariants: every canonical ID is
// unique, every declared dependency resolves by stable identity, and every
// source digest still matches the authored bytes on disk.
func (g *projGate) checkCorpus() {
	seen := make(map[string]bool, len(g.cat.Artifacts))
	for _, a := range g.cat.Artifacts {
		if seen[a.ID] {
			g.fail(ruleIdentity, a.Source, "duplicate canonical identity %s", a.ID)
		}
		seen[a.ID] = true

		srcPath := filepath.Join(bundlespec.SourceRoot(g.root), filepath.FromSlash(a.Source))
		data, err := os.ReadFile(srcPath)
		if err != nil {
			g.fail(ruleIdentity, a.Source, "%s: cannot read authored source: %v", a.ID, err)
			continue
		}
		if got := managedfile.Digest(data); got != a.SourceDigest {
			g.fail(ruleIdentity, a.Source, "%s: source digest %s does not match authored bytes %s", a.ID, a.SourceDigest, got)
		}
		for _, req := range a.Semantics.Requires {
			if !g.ids[req] {
				g.fail(ruleDependency, a.Source, "%s requires %s, which the corpus does not carry", a.ID, req)
			}
		}
	}
}

// audit renders one artifact for one target twice and checks the projection
// invariants. reported names the harness markers the adapter already classifies
// for this artifact (empty when the projection has no classification channel).
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

// matrixFor resolves the CP0 capability record a target's projections are
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

// tierRoles is the ordered CP0 role set each non-unsupported enforcement tier
// stands on. Every listed role must be proven: rules.Select awards
// hook-required only when the target proves pre-operation targets AND context
// return, so a check that read a single role would pass an overclaim on a
// target that proves half the tier.
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

// proves reports whether target's CP0 record supports role. A gate built by a
// test may inject its own predicate to exercise a tier the shipped matrix
// cannot prove.
func (g *projGate) proves(target artifacts.Target, role harness.Role) bool {
	if g.supports != nil {
		return g.supports(target, role)
	}
	return matrixFor(target).Supports(role)
}

// checkTier rejects a projection that claims a stronger native guarantee than
// every CP0 role backing it proves.
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

// checkNativeMetadata rejects any model, effort, or tool restriction key that
// reached a projected native document. The user's own configuration owns those
// fields, so a projection that writes one is a leak, not a default.
func (g *projGate) checkNativeMetadata(a artifacts.Artifact, target artifacts.Target, projected []byte) {
	for _, key := range nativeMetadataKeys(target, projected) {
		if isUserPolicyKey(key) {
			g.fail(ruleMetadata, a.Source, "%s: %s projection carries native metadata key %q, which user model policy owns", a.ID, target, key)
		}
	}
}

// checkWireTokens rejects a Claude-only wire token that reached another
// target's projected bytes without being classified. A marker the adapter
// already reports is not a leak; a re-classified surface (the state-root
// default, a background-delivery instruction) is portable and never a token.
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

// checkAgentDrops requires an agent projection to report every canonical field
// it could not carry natively. A dropped dependency that ships unlabeled reads
// as support the target never proved.
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

// skillReports projects the skill corpus for each target and indexes the
// harness markers each shipped file already classifies, so the wire-token audit
// can tell a reported gap from a silent leak.
func (g *projGate) skillReports() map[artifacts.Target]map[string]map[string]bool {
	reports := make(map[artifacts.Target]map[string]map[string]bool, 2)
	for _, target := range []artifacts.Target{artifacts.TargetClaude, artifacts.TargetOMP} {
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
		g.checkSkillDrops(a)
	}
}

// checkSkillDrops requires an OMP skill manifest to report every canonical
// frontmatter key it cannot carry natively. A referenced file has no metadata
// contract of its own, so only a manifest is checked.
func (g *projGate) checkSkillDrops(a artifacts.Artifact) {
	if !harness.SkillManifest(a) {
		return
	}
	p, err := harness.OMPSkill(a)
	if err != nil {
		return // already reported by audit
	}
	kvs, _, err := frontmatter.ParseOrdered(string(a.Body))
	if err != nil {
		return
	}
	for _, kv := range kvs {
		if kv.Key == "name" || kv.Key == "description" {
			continue
		}
		if !containsString(p.Unsupported, kv.Key) {
			g.fail(ruleTier, a.Source, "%s: OMP projection drops canonical metadata key %q without reporting it unsupported", a.ID, kv.Key)
		}
	}
}

// checkCommands projects every canonical command into Claude's native command
// tree. Claude commands render directly: the authored bytes are the native
// bytes, so the audit covers identity, metadata, and digest without a command
// adapter.
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

// checkRules projects the authored rule corpus into Claude's native rule tree
// twice and audits identity, digests, metadata, and tier. The projection
// verifies each record's source digest itself; the gate pins determinism and
// the CP0 tier ceiling.
func (g *projGate) checkRules() {
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
		for _, key := range nativeMetadataKeys(artifacts.TargetClaude, r.Bytes) {
			if isUserPolicyKey(key) {
				g.fail(ruleMetadata, path, "%s: rule projection carries native metadata key %q, which user model policy owns", r.RecordID, key)
			}
		}
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

// nativeMetadataKeys returns the top-level metadata keys a projected native
// document declares: TOML keys for Codex, frontmatter keys for Markdown. A
// document with no parseable metadata contributes none.
func nativeMetadataKeys(target artifacts.Target, projected []byte) []string {
	if target == artifacts.TargetCodex {
		var doc map[string]any
		if err := toml.Unmarshal(projected, &doc); err != nil {
			return nil
		}
		keys := make([]string, 0, len(doc))
		for k := range doc {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	kvs, _, err := frontmatter.ParseOrdered(string(projected))
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		keys = append(keys, kv.Key)
	}
	return keys
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
