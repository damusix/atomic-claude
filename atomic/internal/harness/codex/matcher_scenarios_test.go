// CP6B offline guarantee matrix: the generated Codex matcher driven under the
// JavaScript runtime the plugin would run in. Codex CLI 0.147.0 proved no hook
// event, so the matcher registers nothing and claims no delivery; the rows here
// prove only what the module itself does with an operation — match, nonmatch,
// overlap, canonical ordering, uncovered operations, and deterministic bytes.
//
// The module is the artifact a later checkpoint wires once CP0 proves an event,
// so its behavior is proved by importing it and feeding it a synthetic operation
// spec, exactly as CP4B2 drives the generated OMP extension. Every claim maps to
// a CP0 row: the empty proven-input table is `codex.hook-absence`, and the
// canonical ordering and glob translation are the shared `rules` contract.
package codex

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// matcherDriver imports the generated matcher and runs one operation spec,
// writing the raw result document so a second run is byte-comparable. It mirrors
// CP4B2's extension driver: the module under proof is imported, never reimplemented.
const matcherDriver = `import { readFileSync, writeFileSync } from "node:fs";
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

// matcherCorpus builds a small canonical corpus with overlapping rules. Two
// rules scope docs/spec/**, and at least one candidate path matches no rule, so
// a match that ignored ordering, scanned only the first rule, or matched
// unconditionally would fail.
func matcherCorpus(t *testing.T) *artifacts.Catalog {
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
	// Authored out of identity order so the generated index ordering is what the
	// assertions observe, not the insertion order.
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

// matcherSpec is the synthetic operation sequence one Bun run consumes. Keys are
// stable names so a mismatch names the row.
type matcherSpec struct {
	Paths      map[string]string `json:"paths"`
	Normalize  map[string]string `json:"normalize"`
	Operations []map[string]any  `json:"operations"`
}

// matcherResult is one driver run, decoded from the output document.
type matcherResult struct {
	Paths      map[string][]string `json:"paths"`
	Normalized map[string]any      `json:"normalized"`
	Operations []map[string]any    `json:"operations"`
}

// TestCodexMatcherOfflineGuarantees drives the generated matcher under Bun and
// proves the offline contract: a matching path yields its rule identities in
// canonical order, an overlapping path yields every identity, a nonmatching path
// yields none, every operation without a CP0-proven structured input path is
// uncovered and receives no matched body, and the module bytes are deterministic.
func TestCodexMatcherOfflineGuarantees(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skipf("bun is unavailable: %v", err)
	}
	plugin, err := BuildPlugin(matcherCorpus(t), harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("build plugin: %v", err)
	}
	matcher, ok := findFile(plugin, MatcherPath)
	if !ok {
		t.Fatal("plugin carries no matcher module")
	}
	index, ok := findFile(plugin, RuleIndexPath)
	if !ok {
		t.Fatal("plugin carries no rule index")
	}

	// The published index carries the bound and the complete rule set, so the
	// module it feeds is never a truncated view of the corpus.
	var idx struct {
		Bound int               `json:"bound"`
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(index.Bytes, &idx); err != nil {
		t.Fatalf("decode rule index: %v", err)
	}
	if idx.Bound != InstructionByteBound {
		t.Errorf("index bound = %d, want Atomic's %d-byte preflight bound", idx.Bound, InstructionByteBound)
	}
	if len(idx.Rules) != 3 {
		t.Fatalf("index rules = %d, want all 3; a truncated index misreports which rules are in force", len(idx.Rules))
	}
	for _, want := range []string{"shipped:rules/docs/spec.md", "shipped:rules/markdown/style.md", "shipped:rules/ts/style.md"} {
		if !strings.Contains(string(matcher.Bytes), want) {
			t.Errorf("matcher module does not carry %s; the module must inline the complete index", want)
		}
	}
	// A rule body is never part of the module: matching yields identities only, so
	// no handler could return a body for an operation CP0 never proved.
	for _, body := range []string{"# TypeScript", "# Specs", "# Markdown"} {
		if strings.Contains(string(matcher.Bytes), body) {
			t.Errorf("matcher module carries rule body %q, so a matched body could be returned", body)
		}
	}

	// Determinism: identical corpus, identical module bytes.
	again, err := BuildPlugin(matcherCorpus(t), harness.CodexCapabilities())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if again.Generation != plugin.Generation {
		t.Errorf("generation %s != %s", plugin.Generation, again.Generation)
	}
	againMatcher, _ := findFile(again, MatcherPath)
	if string(againMatcher.Bytes) != string(matcher.Bytes) {
		t.Error("the matcher module is not deterministic across builds")
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "matcher.mjs"), string(matcher.Bytes))
	writeFile(t, filepath.Join(dir, "driver.ts"), matcherDriver)
	spec := matcherSpec{
		Paths: map[string]string{
			"match":         "src/a.ts",
			"overlap":       "docs/spec/x.md",
			"nonmatch":      "src/a.txt",
			"nonmatch_spec": "docs/spec/x.txt",
			"markdown":      "README.md",
			"absolute":      "/etc/passwd",
			"escape":        "../escape.md",
			"dot_prefixed":  "./docs/spec/x.md",
		},
		Normalize: map[string]string{
			"absolute":     "/etc/passwd",
			"escape":       "../escape.md",
			"windows":      `C:\Users\x.md`,
			"empty":        "",
			"dot_prefixed": "./docs/spec/x.md",
			"double_slash": "docs//spec/x.md",
		},
		Operations: []map[string]any{
			// A shell command string is never parsed for a scope path.
			{"tool": "bash", "input": map[string]any{"command": "cat docs/spec/x.md"}},
			// A hosted tool carries no proven structured input field.
			{"tool": "web_search", "input": map[string]any{"query": "docs/spec/x.md"}},
			// A local tool whose schema CP0 never exercised is uncovered even when
			// its input carries a path the canonical matcher would otherwise match.
			{"tool": "apply_patch", "input": map[string]any{"path": "docs/spec/x.md"}},
			{"tool": "read", "input": map[string]any{"path": "docs/spec/x.md"}},
			// A path-shaped field on an uncovered tool is not a structured input.
			{"tool": "bash", "input": map[string]any{"path": "docs/spec/x.md"}},
		},
	}
	specBytes, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "spec.json"), string(specBytes))

	first := runMatcherDriver(t, bun, dir, "out.json")
	second := runMatcherDriver(t, bun, dir, "out-again.json")

	var got matcherResult
	if err := json.Unmarshal([]byte(first), &got); err != nil {
		t.Fatalf("decode matcher output %q: %v", first, err)
	}

	// Match: one rule, in canonical order.
	if ids := got.Paths["match"]; len(ids) != 1 || ids[0] != "shipped:rules/ts/style.md" {
		t.Errorf("matchPath(src/a.ts) = %v, want the TypeScript rule", ids)
	}
	// Overlap: every matching identity, ascending by canonical RecordID — not the
	// authored order and not a first-match short circuit.
	if ids := got.Paths["overlap"]; strings.Join(ids, ",") != "shipped:rules/docs/spec.md,shipped:rules/markdown/style.md" {
		t.Errorf("matchPath(docs/spec/x.md) = %v, want both overlapping identities in canonical order", ids)
	}
	// Nonmatch: a path no rule scopes resolves to nothing.
	if ids := got.Paths["nonmatch"]; len(ids) != 0 {
		t.Errorf("matchPath(src/a.txt) = %v, want no match", ids)
	}
	if ids := got.Paths["nonmatch_spec"]; len(ids) != 0 {
		t.Errorf("matchPath(docs/spec/x.txt) = %v, want no match", ids)
	}
	if ids := got.Paths["markdown"]; strings.Join(ids, ",") != "shipped:rules/markdown/style.md" {
		t.Errorf("matchPath(README.md) = %v, want the markdown rule", ids)
	}
	// An absolute or parent-escaping candidate is refused, never matched.
	if ids := got.Paths["absolute"]; len(ids) != 0 {
		t.Errorf("matchPath accepted an absolute path: %v", ids)
	}
	if ids := got.Paths["escape"]; len(ids) != 0 {
		t.Errorf("matchPath accepted a parent-escaping path: %v", ids)
	}
	// Normalization is exact-path only: an equivalent relative form collapses and
	// still matches.
	if ids := got.Paths["dot_prefixed"]; strings.Join(ids, ",") != "shipped:rules/docs/spec.md,shipped:rules/markdown/style.md" {
		t.Errorf("matchPath(./docs/spec/x.md) = %v, want the collapsed path's matches", ids)
	}
	for _, refused := range []string{"absolute", "escape", "windows", "empty"} {
		if got.Normalized[refused] != nil {
			t.Errorf("normalize(%s) = %v, want null for a non-exact-relative candidate", refused, got.Normalized[refused])
		}
	}
	if got.Normalized["dot_prefixed"] != "docs/spec/x.md" {
		t.Errorf("normalize(./docs/spec/x.md) = %v, want docs/spec/x.md", got.Normalized["dot_prefixed"])
	}
	if got.Normalized["double_slash"] != "docs/spec/x.md" {
		t.Errorf("normalize(docs//spec/x.md) = %v, want docs/spec/x.md", got.Normalized["double_slash"])
	}

	// Uncovered: no operation carries a proven structured input path, so none is
	// covered and none receives a matched body — even when its payload contains a
	// path the canonical matcher would match.
	if len(got.Operations) != len(spec.Operations) {
		t.Fatalf("driver returned %d operations, want %d", len(got.Operations), len(spec.Operations))
	}
	for i, op := range got.Operations {
		if covered, _ := op["covered"].(bool); covered {
			t.Errorf("operation %d (%v) was covered although Codex proved no structured input", i, spec.Operations[i])
		}
		matched, _ := op["matched"].([]any)
		if len(matched) != 0 {
			t.Errorf("operation %d (%v) received a matched body: %v", i, spec.Operations[i], matched)
		}
		if reason, _ := op["reason"].(string); reason == "" {
			t.Errorf("operation %d reports no uncovered reason", i)
		}
	}

	// Deterministic evaluation: the same spec produces byte-identical output.
	if first != second {
		t.Errorf("two matcher runs disagree:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// runMatcherDriver runs the Bun driver once and returns the output document.
func runMatcherDriver(t *testing.T, bun, dir, out string) string {
	t.Helper()
	cmd := exec.Command(bun, "run", "driver.ts", "spec.json", out)
	cmd.Dir = dir
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bun driver failed: %v\n%s", err, combined)
	}
	data, err := os.ReadFile(filepath.Join(dir, out))
	if err != nil {
		t.Fatalf("read driver output %s: %v", out, err)
	}
	return string(data)
}

// TestCodexMatcherRefusesOverBoundGeneration proves the payload bound: an index
// over Atomic's own preflight ceiling refuses the whole generation, returns no
// partial plugin, and names the refusal — so no truncated rule index can be
// published or driven.
func TestCodexMatcherRefusesOverBoundGeneration(t *testing.T) {
	glob := strings.Repeat("a", InstructionByteBound+1024)
	cat, err := artifacts.NewCatalog([]artifacts.Artifact{{
		ID:     "rule:rules/big.md",
		Kind:   artifacts.KindRule,
		Source: "rules/big.md",
		Body:   []byte("---\npaths:\n  - \"" + glob + "\"\n---\n\nBody.\n"),
	}})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	plugin, err := BuildPlugin(cat, harness.CodexCapabilities())
	if err == nil {
		t.Fatal("plugin generated with an over-bound rule index")
	}
	if !strings.Contains(err.Error(), "refusing") || !strings.Contains(err.Error(), "truncating") {
		t.Errorf("error %q does not state the refuse-not-truncate contract", err)
	}
	if plugin.Generation != "" || len(plugin.Files) != 0 || len(plugin.Index) != 0 {
		t.Errorf("a refused generation returned a partial plugin: %+v", plugin)
	}
}
