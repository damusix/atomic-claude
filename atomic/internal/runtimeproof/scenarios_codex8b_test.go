// CP8B Codex-only scenarios: the end-to-end proof of Milestone B over isolated
// CODEX_HOME roots, driven through the production adapter, plugin generator,
// install engine, and doctor categories.
//
// The retained CP0 record is the whole capability boundary: Codex CLI 0.147.0
// proved registration and list visibility inside a homed CODEX_HOME, and the
// isolated runtime had the configured model rejected before any hook event ran.
// Every guidance, trust, disablement, coverage, payload, resume, and child row
// therefore stays uncovered and is recorded as a loud skip, never as a pass on
// absence. The deterministic rows run: the plugin package publishes, no hook is
// registered, no rule body reaches the matcher module, and an unproven
// operation receives no matched body.
package runtimeproof

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/embeddedcorpus"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// codexShippedPlugin builds the production plugin from the embedded corpus, so
// every assertion observes the generation the selected binary would publish.
func codexShippedPlugin(t *testing.T) codex.Plugin {
	t.Helper()
	cat, err := embeddedcorpus.Load()
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	plugin, err := codex.BuildPlugin(cat, harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build plugin: %v", err)
	}
	return plugin
}

// codexPluginFile returns one plugin file by its plugin-relative path.
func codexPluginFile(p codex.Plugin, path string) (codex.PluginFile, bool) {
	for _, f := range p.Files {
		if f.Path == path {
			return f, true
		}
	}
	return codex.PluginFile{}, false
}

// codexWithheldRoles is every rule-delivery role CP0 leaves unproven for Codex
// 0.147.0. The plugin must name all of them, so the absence of a hook reads as
// a reported gap rather than a deliberate no-op.
var codexWithheldRoles = []harness.Role{
	harness.RoleStaticScope,
	harness.RoleSessionBaseline,
	harness.RolePreOperationTargets,
	harness.RoleContextReturn,
	harness.RoleDeterministicDeny,
}

// codexMatcherCorpus builds a small canonical corpus with overlapping rules
// through the production catalog: two rules scope docs/spec/**, so a matcher
// that short-circuited on the first match or ignored canonical ordering would
// fail the overlap assertion.
func codexMatcherCorpus(t *testing.T) *artifacts.Catalog {
	t.Helper()
	rule := func(source, body string, include ...string) artifacts.Artifact {
		var b strings.Builder
		b.WriteString("---\npaths:\n")
		for _, glob := range include {
			b.WriteString("  - \"" + glob + "\"\n")
		}
		b.WriteString("---\n\n" + body)
		return artifacts.Artifact{
			ID:     "rule:" + source,
			Kind:   artifacts.KindRule,
			Source: source,
			Body:   []byte(b.String()),
		}
	}
	cat, err := artifacts.NewCatalog([]artifacts.Artifact{
		rule("rules/ts/style.md", "# TypeScript\n", "**/*.ts"),
		rule("rules/docs/spec.md", "# Specs\n", "docs/spec/**/*.md"),
		rule("rules/markdown/style.md", "# Markdown\n", "**/*.md"),
	})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return cat
}

// codexMatcherDriver imports the generated matcher and writes one result
// document, so the module under proof is imported rather than reimplemented.
const codexMatcherDriver = `import { readFileSync, writeFileSync } from "node:fs";
import matcher from "./matcher.mjs";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
const paths = {};
for (const [name, candidate] of Object.entries(spec.paths)) {
	paths[name] = matcher.matchPath(candidate);
}
const normalized = {};
for (const [name, candidate] of Object.entries(spec.normalize)) {
	normalized[name] = matcher.normalize(candidate);
}
const operations = spec.operations.map((operation) => matcher.matchOperation(operation));
writeFileSync(process.argv[3], JSON.stringify({ paths, normalized, operations }, null, 2));
`

// codexMatcherSpec is the synthetic operation sequence one Bun run consumes.
type codexMatcherSpec struct {
	Paths      map[string]string `json:"paths"`
	Normalize  map[string]string `json:"normalize"`
	Operations []map[string]any  `json:"operations"`
}

// codexMatcherResult is one driver run, decoded from the output document.
type codexMatcherResult struct {
	Paths      map[string][]string `json:"paths"`
	Normalized map[string]any      `json:"normalized"`
	Operations []map[string]any    `json:"operations"`
}

// runCodexMatcher drives the generated matcher under Bun once and returns the
// decoded result plus the raw document, so a second run is byte-comparable.
func runCodexMatcher(t *testing.T, p codex.Plugin, spec codexMatcherSpec) (codexMatcherResult, string) {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skipf("bun is unavailable: %v", err)
	}
	matcher, ok := codexPluginFile(p, codex.MatcherPath)
	if !ok {
		t.Fatalf("plugin carries no matcher module")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "matcher.mjs"), matcher.Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "driver.ts"), []byte(codexMatcherDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.json")
	cmd := exec.Command(bun, "run", "driver.ts", "spec.json", out)
	cmd.Dir = dir
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bun matcher driver failed: %v\n%s", err, combined)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got codexMatcherResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode matcher output: %v", err)
	}
	return got, string(raw)
}

// TestCP8BCodexGlobalInstall proves the Codex global install path over an
// isolated CODEX_HOME: one explicitly named home enrolls, the plugin package
// lands at the home-anchored path, no hook is registered, every withheld
// rule-delivery role is named, and no runtime surface is claimed.
//
// Criterion: Codex projection is an Atomic plugin whose registration surface
// is the only observed row; trust, disablement, coverage, payload, and child
// rows stay unsupported and are reported with their evidence.
func TestCP8BCodexGlobalInstall(t *testing.T) {
	home := isolatedHome(t)
	codexRoot := codexHomeRoot(t, home)
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/global-install",
		Group:     "codex",
		Engine:    "install + harness/codex",
		Criterion: "global install publishes the plugin package, registers no hook, names every withheld role, and claims no runtime surface",
		Command:   "install.Steps.Converge (codex, enroll)",
		Paths:     []string{home, codexRoot},
	}

	reports, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}, Enroll: true})
	if err != nil {
		t.Fatalf("install codex: %v", err)
	}
	if len(reports) != 1 || reports[0].Status != harness.StatusConverged || len(reports[0].Blockers) > 0 {
		t.Fatalf("reports = %+v, want one converged Codex target", reports)
	}
	if !enrolledTarget(t, home, harness.KindCodex, codexRoot) {
		t.Fatalf("the Codex target was not enrolled")
	}
	packageRoot := codex.PackageResource(home)
	for _, path := range []string{codex.MarketplaceManifestPath, codex.PluginManifestPath, codex.MatcherPath, codex.RuleIndexPath} {
		if !fileExists(filepath.Join(packageRoot, filepath.FromSlash(path))) {
			t.Errorf("the published package is missing %s", path)
		}
	}
	if fileExists(codex.CachePath(codexRoot)) {
		t.Errorf("enrollment wrote the native cache tree; registration is a separate observed surface")
	}

	plugin := codexShippedPlugin(t)
	// No hook is registered: the plugin manifest carries no hooks key and no
	// hook configuration file is emitted.
	if plugin.Hooks.ConfigFile != "" {
		t.Errorf("plugin publishes a hook config file %q although no hook role is proven", plugin.Hooks.ConfigFile)
	}
	if len(plugin.Hooks.Registered) != 0 {
		t.Errorf("plugin registered events %+v although no hook role is proven", plugin.Hooks.Registered)
	}
	withheld := map[harness.Role]bool{}
	for _, c := range plugin.Hooks.Withheld {
		withheld[c.Role] = true
	}
	for _, role := range codexWithheldRoles {
		if !withheld[role] {
			t.Errorf("withheld hook roles omit %s", role)
		}
	}
	manifest, ok := codexPluginFile(plugin, codex.PluginManifestPath)
	if !ok {
		t.Fatalf("plugin carries no manifest")
	}
	if strings.Contains(string(manifest.Bytes), `"hooks"`) {
		t.Errorf("the plugin manifest wires a hooks key although no hook role is proven")
	}

	adapter := codexAdapter()
	for _, row := range append(adapter.RuntimeState(codexRoot), adapter.RegistrationStateRow(codexRoot)) {
		if row.Evidence == "" {
			t.Errorf("runtime surface %q carries no evidence", row.Surface)
		}
		if row.Status != harness.StatusUnsupported {
			t.Errorf("runtime surface %q reports %s; Codex 0.147.0 proved no runtime row", row.Surface, row.Status)
		}
	}
	// Trust and disablement specifically: reported unproven, never fabricated.
	surfaces, statuses := codexRuntimeSurfaces(t, codexRoot)
	for _, want := range []string{"plugin-hook trust", "deliberate hook disablement"} {
		surface, ok := surfaceMatching(surfaces, want)
		if !ok {
			t.Errorf("no runtime surface names %q", want)
			continue
		}
		if statuses[surface] != harness.StatusUnsupported || surfaces[surface] == "" {
			t.Errorf("surface %q = %s (%q), want unsupported with evidence", surface, statuses[surface], surfaces[surface])
		}
	}

	ev.Outcome = "codex target converged; plugin package published; no hook registered; withheld roles named; every runtime surface unsupported with evidence"
	recordScenario(t, ev)
}

// TestCP8BCodexWikiAutoConvergence proves repository wiki refresh stays on the
// generic producer path while only the already-enrolled Codex target
// reconverges: the canonical pointer cards land under the selected state root,
// rules sync re-converges the enrolled target, and Codex fabricates no steering
// claim because CP0 proved no instruction surface.
//
// Criterion: repository wiki refresh writes canonical pointer cards first and
// auto-converges only already-enrolled targets; Codex materializes no
// instruction file.
func TestCP8BCodexWikiAutoConvergence(t *testing.T) {
	home := isolatedHome(t)
	codexRoot := codexHomeRoot(t, home)
	stateRoot := filepath.Join(home, ".claude")
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/wiki-auto-convergence",
		Group:     "codex",
		Engine:    "wiki + install + harness/codex",
		Criterion: "wiki refresh writes canonical pointer cards and auto-converges only the enrolled Codex target, which claims no steering surface",
		Command:   "wiki.Refresh → install.Steps.RulesSync",
		Paths:     []string{home, codexRoot, stateRoot},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}, Enroll: true}); err != nil {
		t.Fatalf("enroll codex: %v", err)
	}

	records, err := wiki.Refresh(stateRoot, "cp8b-key", wiki.RefreshRepo, []wiki.PointerCard{{
		Domain:  "typescript",
		Include: []string{"**/*.ts"},
		Body:    "Domain: typescript.\n\nMap:\n  - src/\n",
	}})
	if err != nil {
		t.Fatalf("wiki refresh: %v", err)
	}
	if len(records) != 1 || records[0].ID != "wiki:cp8b-key:typescript" {
		t.Fatalf("wiki records = %+v, want one producer-qualified card", records)
	}
	cardPath := filepath.Join(stateRoot, "rules", "wiki", "typescript.md")
	if !fileExists(cardPath) {
		t.Errorf("the canonical pointer card was not written at %s", cardPath)
	}

	reports, err := steps.RulesSync()
	if err != nil {
		t.Fatalf("rules sync: %v", err)
	}
	if len(reports) != 1 || reports[0].Target.Kind != harness.KindCodex || len(reports[0].Blockers) > 0 {
		t.Fatalf("rules sync reports = %+v, want the one enrolled Codex target reconverged", reports)
	}

	// No steering claim: the adapter's only claim is the shared package, and no
	// instruction file appeared anywhere under the Codex home.
	claims, err := codexAdapter().Claims(home)
	if err != nil {
		t.Fatalf("claims: %v", err)
	}
	if len(claims) != 1 || claims[0].ID != codex.PackageResource(home) {
		t.Fatalf("claims = %+v, want only the shared package claim", claims)
	}
	if fileExists(filepath.Join(codexRoot, "AGENTS.md")) {
		t.Errorf("enrollment materialized an instruction file Codex does not prove")
	}
	for _, name := range codexDirEntries(t, codexRoot) {
		if name == "AGENTS.md" {
			t.Errorf("the Codex home gained %s", name)
		}
	}

	ev.Outcome = "pointer card written at the canonical path; enrolled Codex target reconverged; no steering file or claim created"
	recordScenario(t, ev)
}

// TestCP8BCodexTwoReposCwdIndependent proves the published plugin targets the
// enrolled CODEX_HOME independent of the working directory: two repository
// checkouts and one linked worktree publish one identical generation at the
// same home-anchored package path, the enrolled instance is the CODEX_HOME, and
// no working directory gains a Codex artifact.
//
// Criterion: the Codex plugin targets the enrolled home; two repositories and a
// worktree share one generation and no cwd is a write target.
func TestCP8BCodexTwoReposCwdIndependent(t *testing.T) {
	root := isolatedHome(t)
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	codexRoot := codexHomeRoot(t, root)
	main, worktree := codexWorktree(t, root)
	second := filepath.Join(root, "second-repo")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, second, map[string]string{"README.md": "second checkout\n"})
	guidanceGit(t, second, "init", "-q", "-b", "main")

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/two-repos-cwd-independent",
		Group:     "codex",
		Engine:    "harness/codex",
		Criterion: "two repositories and a linked worktree produce one generation at the home-anchored package path; no cwd gains a plugin artifact",
		Command:   "codex.Enroll with cwd = repo | worktree | repo",
		Paths:     []string{home, codexRoot, main, worktree, second},
	}

	a := codexAdapter()
	var generations []string
	for _, cwd := range []struct{ dir, id string }{
		{main, "cp8b-two-main"},
		{worktree, "cp8b-two-worktree"},
		{second, "cp8b-two-second"},
	} {
		before := codexDirEntries(t, cwd.dir)
		t.Chdir(cwd.dir)
		result, err := a.Enroll(codex.EnrollRequest{Home: home, Root: codexRoot, OperationID: cwd.id})
		if err != nil {
			t.Fatalf("enroll from %s: %v", cwd.dir, err)
		}
		if result.PackageRoot != codex.PackageResource(home) {
			t.Errorf("enroll from %s published to %s, want %s", cwd.dir, result.PackageRoot, codex.PackageResource(home))
		}
		if result.Target.Instance != codexRoot {
			t.Errorf("enroll from %s targeted %s, want the enrolled CODEX_HOME %s", cwd.dir, result.Target.Instance, codexRoot)
		}
		generations = append(generations, result.Generation)
		if got := codexDirEntries(t, cwd.dir); strings.Join(got, ",") != strings.Join(before, ",") {
			t.Errorf("enrolling from %s changed the working directory: %v → %v", cwd.dir, before, got)
		}
	}
	for i := 1; i < len(generations); i++ {
		if generations[i] != generations[0] {
			t.Errorf("generation depends on cwd: %v", generations)
		}
	}

	ev.Outcome = "three cwds produced one generation at the CODEX_HOME-anchored package path; no cwd was written"
	recordScenario(t, ev)
}

// TestCP8BCodexMatcherOffline proves the offline matcher contract over the
// generated module: match yields its identity, overlap yields every identity in
// canonical order, nonmatch yields none, absolute and escaping candidates are
// refused, and an operation without a proven structured input is uncovered and
// receives no matched body.
//
// Criterion: Codex matching rules resolve exact paths and supply every
// overlapping identity, while shell and uncovered operations receive no matched
// body.
func TestCP8BCodexMatcherOffline(t *testing.T) {
	plugin, err := codex.BuildPlugin(codexMatcherCorpus(t), harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build plugin: %v", err)
	}
	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/matcher-offline",
		Group:     "codex",
		Engine:    "harness/codex + bun",
		Criterion: "match/nonmatch/overlap resolve exact paths in canonical order and uncovered operations receive no matched body",
		Command:   "bun run driver over the generated matcher.mjs",
	}
	if _, err := exec.LookPath("bun"); err != nil {
		recordScenarioSkip(t, ev, "bun is unavailable: the generated matcher cannot be driven")
		return
	}

	index, ok := codexPluginFile(plugin, codex.RuleIndexPath)
	if !ok {
		t.Fatalf("plugin carries no rule index")
	}
	var idx struct {
		Bound int               `json:"bound"`
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(index.Bytes, &idx); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if idx.Bound != codex.InstructionByteBound || len(idx.Rules) != 3 {
		t.Fatalf("index bound/rules = %d/%d, want %d/3", idx.Bound, len(idx.Rules), codex.InstructionByteBound)
	}
	matcher, _ := codexPluginFile(plugin, codex.MatcherPath)
	for _, body := range []string{"# TypeScript", "# Specs", "# Markdown"} {
		if strings.Contains(string(matcher.Bytes), body) {
			t.Errorf("the matcher module carries rule body %q, so a matched body could be returned", body)
		}
	}

	spec := codexMatcherSpec{
		Paths: map[string]string{
			"match":    "src/a.ts",
			"overlap":  "docs/spec/x.md",
			"nonmatch": "src/a.txt",
			"absolute": "/etc/passwd",
			"escape":   "../escape.md",
		},
		Normalize: map[string]string{
			"absolute": "/etc/passwd",
			"escape":   "../escape.md",
		},
		Operations: []map[string]any{
			{"tool": "bash", "input": map[string]any{"command": "cat docs/spec/x.md"}},
			{"tool": "apply_patch", "input": map[string]any{"path": "docs/spec/x.md"}},
			{"tool": "read", "input": map[string]any{"path": "docs/spec/x.md"}},
		},
	}
	got, first := runCodexMatcher(t, plugin, spec)
	if ids := got.Paths["match"]; len(ids) != 1 || ids[0] != "shipped:rules/ts/style.md" {
		t.Errorf("matchPath(src/a.ts) = %v, want the TypeScript rule", ids)
	}
	if ids := got.Paths["overlap"]; strings.Join(ids, ",") != "shipped:rules/docs/spec.md,shipped:rules/markdown/style.md" {
		t.Errorf("matchPath(docs/spec/x.md) = %v, want both overlapping identities in canonical order", ids)
	}
	if ids := got.Paths["nonmatch"]; len(ids) != 0 {
		t.Errorf("matchPath(src/a.txt) = %v, want no match", ids)
	}
	if ids := got.Paths["absolute"]; len(ids) != 0 {
		t.Errorf("matchPath accepted an absolute path: %v", ids)
	}
	if ids := got.Paths["escape"]; len(ids) != 0 {
		t.Errorf("matchPath accepted a parent-escaping path: %v", ids)
	}
	if got.Normalized["absolute"] != nil || got.Normalized["escape"] != nil {
		t.Errorf("normalize accepted a non-exact relative candidate: %v", got.Normalized)
	}
	for i, op := range got.Operations {
		if covered, _ := op["covered"].(bool); covered {
			t.Errorf("operation %d (%v) was covered although no structured input is proven", i, spec.Operations[i])
		}
		if matched, _ := op["matched"].([]any); len(matched) != 0 {
			t.Errorf("operation %d received a matched body: %v", i, matched)
		}
		if reason, _ := op["reason"].(string); reason == "" {
			t.Errorf("operation %d reports no uncovered reason", i)
		}
	}

	_, second := runCodexMatcher(t, plugin, spec)
	if first != second {
		t.Errorf("two matcher runs disagree")
	}

	ev.Outcome = "exact match and overlap resolved in canonical order; nonmatch empty; absolute/escape refused; every operation uncovered with no matched body"
	recordScenario(t, ev)
}

// TestCP8BCodexNoFalseDelivery proves the end-to-end no-delivery claim over the
// shipped plugin: the matcher module inlines rule identities but no rule body,
// the manifest wires no hook, no structured tool input is proven, and a
// path-shaped payload on an unproven tool resolves uncovered with no matched
// body.
//
// Criterion: an operation without a mediated exact path is uncovered, receives
// no matched rule body, and remains unsupported end-to-end.
func TestCP8BCodexNoFalseDelivery(t *testing.T) {
	plugin := codexShippedPlugin(t)
	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/no-false-delivery",
		Group:     "codex",
		Engine:    "harness/codex + bun",
		Criterion: "no proven tool input exists, so every operation is uncovered and receives no matched rule body",
		Command:   "codex.BuildPlugin(shipped corpus) → bun matcher driver",
	}
	if _, err := exec.LookPath("bun"); err != nil {
		recordScenarioSkip(t, ev, "bun is unavailable: the generated matcher cannot be driven")
		return
	}

	if len(plugin.Tools) != 0 {
		t.Fatalf("plugin proves tool inputs %+v although CP0 observed no hook event", plugin.Tools)
	}
	manifest, ok := codexPluginFile(plugin, codex.PluginManifestPath)
	if !ok {
		t.Fatalf("plugin carries no manifest")
	}
	if strings.Contains(string(manifest.Bytes), `"hooks"`) {
		t.Errorf("the shipped plugin manifest wires a hooks key")
	}
	matcher, _ := codexPluginFile(plugin, codex.MatcherPath)
	for _, body := range []string{"# TypeScript style", "# Specs: the body is current truth"} {
		if strings.Contains(string(matcher.Bytes), body) {
			t.Errorf("the matcher module carries rule body %q", body)
		}
	}
	if !strings.Contains(string(matcher.Bytes), "shipped:rules/typescript/style.md") {
		t.Errorf("the matcher module does not inline the shipped rule identities")
	}

	spec := codexMatcherSpec{
		Paths:     map[string]string{"ts": "src/a.ts"},
		Normalize: map[string]string{},
		Operations: []map[string]any{
			// A matching path on a tool whose structured input CP0 never
			// exercised must not be delivered a body.
			{"tool": "read", "input": map[string]any{"path": "src/a.ts"}},
			{"tool": "bash", "input": map[string]any{"command": "cat src/a.ts"}},
			{"tool": "web_search", "input": map[string]any{"query": "src/a.ts"}},
		},
	}
	got, _ := runCodexMatcher(t, plugin, spec)
	if ids := got.Paths["ts"]; len(ids) == 0 {
		t.Fatalf("the matcher did not resolve a shipped rule for src/a.ts; identities are missing")
	}
	for i, op := range got.Operations {
		if covered, _ := op["covered"].(bool); covered {
			t.Errorf("operation %d (%v) was covered despite no proven structured input", i, spec.Operations[i])
		}
		if matched, _ := op["matched"].([]any); len(matched) != 0 {
			t.Errorf("operation %d received a matched body: %v", i, matched)
		}
	}

	ev.Outcome = "shipped plugin proves no tool input, wires no hook, carries no rule body, and delivers nothing for every observed operation"
	recordScenario(t, ev)
}

// TestCP8BCodexRuleProducers proves the generic rule and wiki producers handle
// rename, deletion, malformed metadata, and identity collision: a renamed
// domain prunes its stale card, deleting every domain removes every card, a
// malformed card is refused before any write, and a shipped/wiki native-name
// collision fails validation.
//
// Criterion: rule parsing rejects malformed metadata and native-name collisions
// before any target mutation, and wiki refresh prunes renamed or deleted cards.
func TestCP8BCodexRuleProducers(t *testing.T) {
	stateRoot := filepath.Join(isolatedHome(t), ".claude")
	wikiDir := filepath.Join(stateRoot, "rules", "wiki")

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/rule-producers",
		Group:     "codex",
		Engine:    "rules + wiki (generic producers)",
		Criterion: "rename and deletion prune wiki cards; malformed metadata and native-name collisions are refused before mutation",
		Command:   "wiki.Refresh → rules.LoadShipped/LoadWiki → rules.Validate",
		Paths:     []string{stateRoot},
	}

	card := func(domain string) wiki.PointerCard {
		return wiki.PointerCard{Domain: domain, Include: []string{"**/*." + domain}, Body: "Domain: " + domain + ".\n"}
	}
	// Rename: writing a new domain prunes the card whose domain left the set.
	if _, err := wiki.Refresh(stateRoot, "cp8b-producers", wiki.RefreshRepo, []wiki.PointerCard{card("typescript")}); err != nil {
		t.Fatalf("refresh typescript: %v", err)
	}
	if !fileExists(filepath.Join(wikiDir, "typescript.md")) {
		t.Fatalf("the typescript card was not written")
	}
	if _, err := wiki.Refresh(stateRoot, "cp8b-producers", wiki.RefreshRepo, []wiki.PointerCard{card("python")}); err != nil {
		t.Fatalf("refresh python: %v", err)
	}
	if fileExists(filepath.Join(wikiDir, "typescript.md")) {
		t.Errorf("a renamed domain left its stale card behind")
	}
	if !fileExists(filepath.Join(wikiDir, "python.md")) {
		t.Errorf("the renamed domain's card was not written")
	}
	// Deletion: an empty card set prunes every owned card.
	if _, err := wiki.Refresh(stateRoot, "cp8b-producers", wiki.RefreshRepo, nil); err != nil {
		t.Fatalf("refresh empty: %v", err)
	}
	if fileExists(filepath.Join(wikiDir, "python.md")) {
		t.Errorf("a deleted domain left its card behind")
	}

	// Malformed: a card with no include globs is refused before any write, and
	// a shipped rule with an absolute pattern is refused naming its source.
	if _, err := wiki.Refresh(stateRoot, "cp8b-producers", wiki.RefreshRepo, []wiki.PointerCard{{
		Domain: "broken",
		Body:   "no globs\n",
	}}); err == nil {
		t.Errorf("the wiki producer accepted a card with no include globs")
	}
	if fileExists(filepath.Join(wikiDir, "broken.md")) {
		t.Errorf("a refused malformed card was written")
	}
	malformed := filepath.Join(stateRoot, "rules", "broken.md")
	cp8aMkfile(t, malformed, "---\npaths:\n  - \"/absolute/**\"\n---\n\nBody.\n")
	if _, err := rules.LoadShipped(filepath.Join(stateRoot, "rules")); err == nil {
		t.Errorf("the shipped producer accepted an absolute pattern")
	} else if !strings.Contains(err.Error(), "broken.md") {
		t.Errorf("the refusal %q does not name the malformed source", err)
	}
	if err := os.Remove(malformed); err != nil {
		t.Fatal(err)
	}

	// Collision: a shipped `atomic-wiki/<domain>.md` and a wiki card of the same
	// domain share one native name, so the combined set must fail validation.
	cp8aMkfile(t, filepath.Join(stateRoot, "rules", "atomic-wiki", "style.md"), "---\npaths:\n  - \"**/*.md\"\n---\n\nShipped body.\n")
	if _, err := wiki.Refresh(stateRoot, "cp8b-producers", wiki.RefreshRepo, []wiki.PointerCard{card("style")}); err != nil {
		t.Fatalf("refresh style: %v", err)
	}
	shipped, err := rules.LoadShipped(filepath.Join(stateRoot, "rules"))
	if err != nil {
		t.Fatalf("load shipped: %v", err)
	}
	cards, err := rules.LoadWiki(filepath.Join(stateRoot, "rules"), "cp8b-producers")
	if err != nil {
		t.Fatalf("load wiki: %v", err)
	}
	combined := append(append([]rules.RuleRecord(nil), shipped...), cards...)
	err = rules.Validate(combined)
	if err == nil {
		t.Fatalf("a native-name collision between the shipped rule %s and the wiki card passed validation", "rules/atomic-wiki/style.md")
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Errorf("validation error %q does not name a collision", err)
	}

	ev.Outcome = "renamed and deleted domains pruned; malformed card and rule refused by source; shipped/wiki native-name collision failed validation"
	recordScenario(t, ev)
}

// TestCP8BCodexChangedDerivativeConflict proves a hand edit inside the owned
// plugin package is reported as a conflict by the read-only status and doctor
// conflict category, and is never overwritten by an inspection.
//
// Criterion: a later derivative edit lands in the conflicts category and no
// read-only inspection overwrites it.
func TestCP8BCodexChangedDerivativeConflict(t *testing.T) {
	home := isolatedHome(t)
	codexRoot := codexHomeRoot(t, home)
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/changed-derivative-conflict",
		Group:     "codex",
		Engine:    "install + doctor",
		Criterion: "a changed derivative of an owned resource is reported as a conflict and no read-only inspection overwrites it",
		Command:   "Converge (codex, enroll) → hand edit → install.Steps.Status + doctor category 22",
		Paths:     []string{home, codexRoot},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}, Enroll: true}); err != nil {
		t.Fatalf("enroll codex: %v", err)
	}
	target := filepath.Join(codex.PackageResource(home), filepath.FromSlash(codex.MatcherPath))
	tampered := "# hand edited derivative\n"
	if err := os.WriteFile(target, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := steps.Status(install.Selection{Kind: harness.KindCodex})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var conflicted bool
	for _, status := range report.Targets {
		for _, r := range status.Resources {
			if r.Resource == codex.PackageResource(home) && r.State == harness.RowConflict {
				conflicted = true
			}
		}
	}
	if !conflicted {
		t.Fatalf("read-only status did not report the edited package as a conflict: %+v", report.Targets)
	}

	results, err := doctor.RunWith(doctor.Opts{
		Home:     home,
		Only:     []int{22},
		RepoRoot: isolatedHome(t),
	}, false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(results) != 1 || results[0].Index != 22 {
		t.Fatalf("doctor results = %+v, want category 22", results)
	}
	if results[0].Severity != doctor.WARN {
		t.Errorf("conflicts category = %s, want WARN for a changed derivative", results[0].Severity)
	}
	if !strings.Contains(results[0].Detail, "carries bytes Atomic did not record") {
		t.Errorf("conflicts detail = %q, want the changed-derivative phrasing", results[0].Detail)
	}
	if got := mustRead(t, target); string(got) != tampered {
		t.Errorf("a read-only inspection overwrote the changed derivative")
	}

	ev.Outcome = "edited package reported RowConflict by status and WARN by category 22; the derivative was not overwritten"
	recordScenario(t, ev)
}

// TestCP8BCodexSessionRowsUncovered records the Codex primary, resumed, and
// child runtime rows as uncovered: CP0 reached thread creation and the account
// rejected the configured model before any hook event, so no session row has an
// observation.
//
// Criterion: Codex primary, resumed, and child session delivery stays
// unsupported and uncovered.
func TestCP8BCodexSessionRowsUncovered(t *testing.T) {
	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/session-rows-uncovered",
		Group:     "codex",
		Engine:    "capability record",
		Criterion: "Codex primary, resumed, and child session delivery stays unsupported and uncovered",
		Command:   "none — CP0 rejected the model before any hook event",
	}
	recordScenarioSkip(t, ev, "CP0 codex-runtime reached thread creation and the account rejected the configured model before any hook event, so primary, resumed, and child session delivery have no observation (whether another credential or model resolves the rejection is unverified)")
}

// TestCP8BCodexSharedOwnership proves one home-anchored plugin package carries
// one ownership record with three enrolled homes as consumers, while an
// unenrolled home that can discover the package is reported as visibility only.
//
// Criterion: one physical resource has one ownership record even when several
// targets can discover it; consumers and visibility stay separate.
func TestCP8BCodexSharedOwnership(t *testing.T) {
	home := isolatedHome(t)
	roots := []string{
		filepath.Join(home, "codex-a"),
		filepath.Join(home, "codex-b"),
		filepath.Join(home, "codex-c"),
	}
	unenrolled := filepath.Join(home, "codex-unenrolled")
	t.Setenv(codex.HomeEnv, roots[0])
	for _, root := range append(roots, unenrolled) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/shared-ownership",
		Group:     "codex",
		Engine:    "harness/codex",
		Criterion: "one physical package has one ownership record with three consumers while an unenrolled home is visibility only",
		Command:   "codex.Enroll (three roots) → harness.Resources",
		Paths:     []string{home, roots[0], roots[1], roots[2], unenrolled},
	}

	a := codexAdapter()
	for i, root := range roots {
		result, err := a.Enroll(codex.EnrollRequest{Home: home, Root: root, OperationID: "cp8b-shared-" + string(rune('a'+i))})
		if err != nil {
			t.Fatalf("enroll %s: %v", root, err)
		}
		if result.PackageRoot != codex.PackageResource(home) {
			t.Fatalf("enroll %s published to %s, want the one home-anchored package", root, result.PackageRoot)
		}
		if i > 0 && len(result.Applied) != 0 {
			t.Errorf("enroll %s rewrote the already-converged shared package: %v", root, result.Applied)
		}
	}

	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var instances []harness.Instance
	for _, root := range append(roots, unenrolled) {
		instances = append(instances, harness.Instance{Kind: harness.KindCodex, ID: root, NativeRoot: root, Home: home, Exists: true})
	}
	resources := harness.Resources(ledger, instances, harness.SharedRoots(home))
	var pkg *harness.Resource
	for i := range resources {
		if resources[i].ID == codex.PackageResource(home) {
			pkg = &resources[i]
		}
	}
	if pkg == nil {
		t.Fatalf("resources = %+v, want the shared package record", resources)
	}
	if len(pkg.Consumers) != 3 {
		t.Errorf("package consumers = %v, want the three enrolled homes", pkg.Consumers)
	}
	unenrolledKey := (harness.Target{Kind: harness.KindCodex, Instance: unenrolled}).Key()
	if !contains(pkg.VisibleTo, unenrolledKey) {
		t.Errorf("package visible-to = %v, want the unenrolled home %s", pkg.VisibleTo, unenrolledKey)
	}
	if contains(pkg.Consumers, unenrolledKey) {
		t.Errorf("an unenrolled home was counted as a consumer")
	}
	for _, root := range roots {
		if !contains(pkg.Consumers, (harness.Target{Kind: harness.KindCodex, Instance: root}).Key()) {
			t.Errorf("enrolled home %s missing from consumers %v", root, pkg.Consumers)
		}
	}

	ev.Outcome = "one package record with three consumers; the unenrolled home reported as visibility only"
	recordScenario(t, ev)
}

// TestCP8BCodexConcurrentLifecycle proves a concurrent Codex enrollment cannot
// proceed while the one advisory lifecycle lock is held, and that an enrollment
// whose shared package carries an incompatible recorded generation refuses
// before any package mutation.
//
// Criterion: concurrent enrollment serializes on the one lifecycle lock, and an
// incompatible shared generation refuses before mutation naming both targets.
func TestCP8BCodexConcurrentLifecycle(t *testing.T) {
	home := isolatedHome(t)
	first := filepath.Join(home, "codex-a")
	second := filepath.Join(home, "codex-b")
	for _, root := range []string{first, second} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/concurrent-lifecycle",
		Group:     "codex",
		Engine:    "harness/codex + installstate",
		Criterion: "a concurrent enrollment blocks while the lifecycle lock is held; an incompatible shared generation refuses before mutation",
		Command:   "hold AcquireLock → codex.Enroll; perturb recorded generation → codex.Enroll",
		Paths:     []string{home, first, second},
	}

	a := codexAdapter()
	if _, err := a.Enroll(codex.EnrollRequest{Home: home, Root: first, OperationID: "cp8b-concurrent-first"}); err != nil {
		t.Fatalf("enroll first: %v", err)
	}

	// A concurrent enrollment blocks while the lock is held.
	lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "cp8b-concurrent-hold"})
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := a.Enroll(codex.EnrollRequest{Home: home, Root: second, OperationID: "cp8b-concurrent-second"})
		done <- err
	}()
	select {
	case err := <-done:
		_ = lock.Release()
		t.Fatalf("enrollment completed while the lifecycle lock was held (err=%v)", err)
	case <-time.After(250 * time.Millisecond):
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("enrollment after lock release: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("enrollment did not proceed after the lock was released")
	}

	// An incompatible recorded generation refuses before mutation.
	ledgerPath := config.LedgerPath(home)
	ledger, err := installstate.LoadLedger(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for i := range ledger.Rows {
		if ledger.Rows[i].Resource == codex.PackageResource(home) {
			ledger.Rows[i].Generation = "incompatible-generation"
			changed = true
		}
	}
	if !changed {
		t.Fatalf("no package ownership row to age")
	}
	if err := ledger.Save(ledgerPath); err != nil {
		t.Fatal(err)
	}
	third := filepath.Join(home, "codex-c")
	if err := os.MkdirAll(third, 0o755); err != nil {
		t.Fatal(err)
	}
	before := treeDigest(t, codex.PackageResource(home))
	_, err = a.Enroll(codex.EnrollRequest{Home: home, Root: third, OperationID: "cp8b-concurrent-third"})
	if err == nil {
		t.Fatalf("enrollment succeeded despite an incompatible recorded generation")
	}
	if !strings.Contains(err.Error(), "refuses before plugin tree mutation") {
		t.Errorf("refusal = %q, want the shared-generation refusal", err)
	}
	if after := treeDigest(t, codex.PackageResource(home)); after != before {
		t.Errorf("the package changed despite the refusal")
	}

	ev.Outcome = "concurrent enrollment blocked on the held lock then completed; incompatible shared generation refused before mutation"
	recordScenario(t, ev)
}

// TestCP8BCodexInterruptionRecovery proves an unresolved journal is reconciled
// oldest-first before a Codex enrollment plans any mutation, consuming the
// journal as completed operational state.
//
// Criterion: a lifecycle operation recovers unresolved journals oldest-first
// before planning any mutation.
func TestCP8BCodexInterruptionRecovery(t *testing.T) {
	home := isolatedHome(t)
	codexRoot := codexHomeRoot(t, home)
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/interruption-recovery",
		Group:     "codex",
		Engine:    "harness/codex + installstate",
		Criterion: "an unresolved journal is recovered before the enrollment plans any mutation",
		Command:   "Converge (codex, enroll) → applied-unrecorded journal → ConvergeEnrolled",
		Paths:     []string{home, codexRoot},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}, Enroll: true}); err != nil {
		t.Fatalf("enroll codex: %v", err)
	}
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var row *installstate.Row
	for i := range ledger.Rows {
		if ledger.Rows[i].Resource == codex.PackageResource(home) {
			row = &ledger.Rows[i]
		}
	}
	if row == nil {
		t.Fatalf("no package ownership row to build the interrupted operation from")
	}
	// An applied-but-unrecorded tree write: the native bytes already match the
	// intended digest, so recovery commits them rather than discarding.
	m := installstate.Mutation{
		Unit: "cp8b-codex-recovery", Resource: row.Resource, Target: row.Target,
		Kind: row.Applied.Kind, Path: row.Applied.Path, Intended: row.Applied.Digest,
		PriorObserved: true, BackupSum: "unused",
	}
	journalPath := cp8aWriteJournal(t, home, cp8aJournal("cp8b-codex-recovery", []installstate.Mutation{m},
		[]installstate.Progress{{Unit: m.Unit, State: installstate.StateApplied, At: cp8aFixedTime()}}))
	if !fileExists(journalPath) {
		t.Fatalf("the unresolved journal was not written")
	}

	reports, err := steps.ConvergeEnrolled()
	if err != nil {
		t.Fatalf("converge enrolled: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) > 0 {
		t.Fatalf("reports = %+v, want the enrolled Codex target converged", reports)
	}
	if fileExists(journalPath) {
		// Recovery marks the journal completed in place; only a cleanup removes
		// the file, so the completed state is what proves it was consumed.
		j, err := installstate.LoadJournal(journalPath)
		if err != nil {
			t.Fatalf("load recovered journal: %v", err)
		}
		if !j.Completed {
			t.Errorf("the unresolved journal was neither removed nor marked completed")
		}
	} else {
		t.Errorf("the recovered journal disappeared without being consumed")
	}

	ev.Outcome = "applied-unrecorded journal recovered and consumed before the enrollment converged"
	recordScenario(t, ev)
}

// TestCP8BCodexRepair proves repair restores an edited owned package through the
// same convergence engine and refuses an unenrolled target, which it never
// enrolls as a side effect.
//
// Criterion: repair operates only on explicitly enrolled targets and restores
// drift without enrolling a newly discovered instance.
func TestCP8BCodexRepair(t *testing.T) {
	home := isolatedHome(t)
	codexRoot := codexHomeRoot(t, home)
	steps := scenarioSteps(home)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/repair",
		Group:     "codex",
		Engine:    "install + harness/codex",
		Criterion: "repair restores drift on an enrolled Codex target and refuses, without enrolling, an unenrolled one",
		Command:   "Converge (codex, enroll) → corrupt package → Converge (repair) + unenrolled refusal",
		Paths:     []string{home, codexRoot},
	}

	if _, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}, Enroll: true}); err != nil {
		t.Fatalf("enroll codex: %v", err)
	}
	packageRoot := codex.PackageResource(home)
	target := filepath.Join(packageRoot, filepath.FromSlash(codex.MatcherPath))
	plugin := codexShippedPlugin(t)
	want, ok := codexPluginFile(plugin, codex.MatcherPath)
	if !ok {
		t.Fatalf("plugin carries no matcher module")
	}
	if err := os.WriteFile(target, []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reports, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Blockers) > 0 {
		t.Fatalf("repair reports = %+v, want one unblocked target", reports)
	}
	if got := mustRead(t, target); string(got) != string(want.Bytes) {
		t.Errorf("repair did not restore the published matcher module")
	}

	// An unenrolled home refuses repair and is not enrolled by the attempt.
	fresh := isolatedHome(t)
	freshRoot := codexHomeRoot(t, fresh)
	if _, err := scenarioSteps(fresh).Converge(install.ConvergeRequest{Selection: install.Selection{Kind: harness.KindCodex}}); err == nil {
		t.Errorf("repair of an unenrolled Codex target succeeded")
	}
	if enrolledTarget(t, fresh, harness.KindCodex, freshRoot) {
		t.Errorf("the refused repair enrolled a newly discovered instance")
	}

	ev.Outcome = "repair restored the edited package; an unenrolled target refused and was never enrolled"
	recordScenario(t, ev)
}

// TestCP8BCodexPromisedCapabilitiesUnsupported proves every capability the
// Codex milestone promises stays unsupported except the observed registration
// row: the capability matrix, the plugin gaps, the unproven surfaces, and the
// withheld hook roles all report a non-supported status with evidence.
//
// Criterion: a missing capability row produces unsupported and is reported; no
// promised surface reads as parity.
func TestCP8BCodexPromisedCapabilitiesUnsupported(t *testing.T) {
	plugin := codexShippedPlugin(t)
	root := isolatedHome(t)
	t.Setenv(codex.HomeEnv, root)

	ev := ScenarioEvidence{
		Scenario:  "cp8b/codex/promised-capabilities-unsupported",
		Group:     "codex",
		Engine:    "harness/codex",
		Criterion: "every promised Codex capability outside the observed registration row is unsupported with evidence",
		Command:   "codex.BuildPlugin + Adapter.RuntimeState + CodexCapabilities",
		Paths:     []string{root},
	}

	matrix := harness.CodexCapabilities()
	for _, role := range codexWithheldRoles {
		row := matrix.Capability(role)
		if row.Status == harness.StatusSupported {
			t.Errorf("capability %s reports supported although CP0 proved no hook event", role)
		}
		if row.Evidence == "" && row.Limitation == "" {
			t.Errorf("capability %s carries neither evidence nor a limitation", role)
		}
	}
	for _, gap := range plugin.Gaps {
		if gap.Status == harness.StatusSupported {
			t.Errorf("plugin gap %s reports supported", gap.Role)
		}
		if gap.Evidence == "" && gap.Limitation == "" {
			t.Errorf("plugin gap %s carries neither evidence nor a limitation", gap.Role)
		}
	}
	for _, gap := range plugin.Unproven {
		if gap.Surface == "" || gap.Evidence == "" {
			t.Errorf("unproven surface %+v is missing its identity or evidence", gap)
		}
	}
	if len(plugin.Hooks.Registered) != 0 || plugin.Hooks.ConfigFile != "" {
		t.Errorf("plugin registered hooks %+v although every justifying row is unsupported", plugin.Hooks)
	}
	for _, row := range codexAdapter().RuntimeState(root) {
		if row.Status != harness.StatusUnsupported || row.Evidence == "" {
			t.Errorf("runtime surface %q = %s (%q), want unsupported with evidence", row.Surface, row.Status, row.Evidence)
		}
	}

	ev.Outcome = "every capability row outside registration is unsupported with evidence; no hook registered; no unproven surface omitted"
	recordScenario(t, ev)
}
