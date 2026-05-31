package validate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

// configFixtureDir returns the absolute path to a config testdata fixture dir.
func configFixtureDir(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "config", rel))
	if err != nil {
		t.Fatalf("resolve fixture dir %s: %v", rel, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture dir %s missing: %v", abs, err)
	}
	return abs
}

// hasRule reports whether findings contains at least one finding with the given rule.
func hasRule(findings []validate.Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// --- C1: CLAUDE.md "Subagents available for dispatch" references exist ---

// TestRunConfigRules_C1_Pass proves that when CLAUDE.md lists an agent in the
// "Subagents available for dispatch" section and agents/<name>.md exists, C1
// produces no finding. WHY: C1 catches the invisible-feature class where an
// agent is documented but never created (or vice versa).
func TestRunConfigRules_C1_Pass(t *testing.T) {
	root := configFixtureDir(t, "pass/C1")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C1") {
		t.Errorf("C1 false-positive; findings: %+v", findings)
	}
}

// TestRunConfigRules_C1_Fail proves that when CLAUDE.md lists atomic-foo but
// agents/atomic-foo.md is absent, C1 produces a FAIL finding. WHY: C1 must
// catch stale registry entries — an agent listed in CLAUDE.md but never
// created is invisible to dispatchers.
func TestRunConfigRules_C1_Fail(t *testing.T) {
	root := configFixtureDir(t, "fail/C1")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(findings, "C1") {
		t.Errorf("expected C1 FAIL finding, got %+v", findings)
	}
	for _, f := range findings {
		if f.Rule == "C1" && f.Severity != "FAIL" {
			t.Errorf("C1 finding must be FAIL, got %q", f.Severity)
		}
	}
}

// --- C3: subagent_type in commands resolves ---

// TestRunConfigRules_C3_Pass proves that a command referencing a known agent
// (atomic-builder with file present) and a built-in (general-purpose) produces
// no C3 finding. WHY: C3 catches commands that dispatch to non-existent agents.
func TestRunConfigRules_C3_Pass(t *testing.T) {
	root := configFixtureDir(t, "pass/C3")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C3") {
		t.Errorf("C3 false-positive; findings: %+v", findings)
	}
}

// TestRunConfigRules_C3_Fail proves that a command referencing atomic-ghost
// (no agents/atomic-ghost.md) produces a C3 FAIL finding. WHY: dispatching to
// a non-existent subagent is a runtime failure; C3 catches it statically.
func TestRunConfigRules_C3_Fail(t *testing.T) {
	root := configFixtureDir(t, "fail/C3")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(findings, "C3") {
		t.Errorf("expected C3 FAIL finding, got %+v", findings)
	}
}

// TestRunConfigRules_C3_CodeBlockNegative proves that subagent_type inside a
// fenced code block does NOT produce a C3 finding. WHY: commands often show
// usage examples in code blocks; treating those as real dispatch refs would
// create false positives on every command that documents dispatch.
func TestRunConfigRules_C3_CodeBlockNegative(t *testing.T) {
	root := configFixtureDir(t, "pass/C3")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// example-only.md has subagent_type: "atomic-ghost" only in a fenced block.
	// Must not trigger C3.
	for _, f := range findings {
		if f.Rule == "C3" {
			t.Errorf("C3 triggered on fenced-block-only reference: %+v", f)
		}
	}
}

// --- C5: @-refs in CLAUDE.md resolve ---

// TestRunConfigRules_C5_Pass proves that @-refs pointing to existing files
// produce no C5 finding. WHY: C5 ensures signals and other context files are
// actually wired; a missing file means Claude silently runs without that context.
func TestRunConfigRules_C5_Pass(t *testing.T) {
	root := configFixtureDir(t, "pass/C5")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C5") {
		t.Errorf("C5 false-positive; findings: %+v", findings)
	}
}

// TestRunConfigRules_C5_Fail proves that @.claude/project/missing.md (absent
// file) produces a C5 FAIL finding. WHY: C5 is the primary guard against
// stale @-ref wiring — a missing file means Claude's context is silently
// incomplete on every session in that repo.
func TestRunConfigRules_C5_Fail(t *testing.T) {
	root := configFixtureDir(t, "fail/C5")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(findings, "C5") {
		t.Errorf("expected C5 FAIL finding, got %+v", findings)
	}
}

// TestRunConfigRules_C5_CodeBlockNegative proves that @-refs inside a fenced
// code block in CLAUDE.md do NOT trigger C5. WHY: documentation and example
// files commonly embed @-ref syntax in code blocks; those are never
// auto-loaded by Claude Code and must not generate false positives.
func TestRunConfigRules_C5_CodeBlockNegative(t *testing.T) {
	// pass/C5 has CLAUDE.md with @.claude/project/missing.md only in a
	// fenced code block — must produce zero C5 findings.
	root := configFixtureDir(t, "pass/C5")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "C5" {
			t.Errorf("C5 triggered on fenced-block-only @-ref: %+v", f)
		}
	}
}

// TestRunConfigRules_C5_IgnoresLocalOverlay proves that a broken @-ref in
// claude.local.md does NOT trigger C5. WHY: claude.local.md is a user-owned
// project-local overlay that may contain backtick spans resembling @-refs
// (e.g. npm package paths like @fortawesome/...). C5 polices only CLAUDE.md —
// the shipped contract — not author overlays.
func TestRunConfigRules_C5_IgnoresLocalOverlay(t *testing.T) {
	// Fixture: claude.local.md has a broken @-ref in prose; CLAUDE.md is clean.
	// C5 must produce zero findings — the overlay is not scanned.
	root := configFixtureDir(t, "pass/C5-local-ignored")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C5") {
		t.Errorf("C5 must not fire on claude.local.md; findings: %+v", findings)
	}
}

// --- C7: no duplicate name: across agents/*.md ---

// TestRunConfigRules_C7_Pass proves that agents with distinct name: values
// produce no C7 finding. WHY: C7 guards against silent agent collisions where
// the later-loaded definition silently overrides the earlier one.
func TestRunConfigRules_C7_Pass(t *testing.T) {
	root := configFixtureDir(t, "pass/C7")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C7") {
		t.Errorf("C7 false-positive; findings: %+v", findings)
	}
}

// TestRunConfigRules_C7_Fail proves that two agent files sharing name: "atomic-x"
// produce a C7 FAIL finding. WHY: duplicate names cause silent runtime overrides
// — whichever definition loads later wins, making the first agent invisible.
func TestRunConfigRules_C7_Fail(t *testing.T) {
	root := configFixtureDir(t, "fail/C7")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(findings, "C7") {
		t.Errorf("expected C7 FAIL finding, got %+v", findings)
	}
}

// --- C9: agents/skills/output-styles require atomic- prefix ---

// TestRunConfigRules_C9_Pass proves that agents/atomic-foo.md produces no C9
// finding. WHY: correctly prefixed artifacts auto-bundle; a false positive here
// would block valid new agent additions.
func TestRunConfigRules_C9_Pass(t *testing.T) {
	root := configFixtureDir(t, "pass/C9")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C9") {
		t.Errorf("C9 false-positive; findings: %+v", findings)
	}
}

// TestRunConfigRules_C9_Fail proves that agents/NotAtomic.md (no atomic- prefix)
// produces a C9 WARN finding. WHY: C9 catches the silent-bundle-exclusion class —
// the file exists, looks like an agent, but bundlemirror will skip it because it
// lacks the required prefix.
func TestRunConfigRules_C9_Fail(t *testing.T) {
	root := configFixtureDir(t, "fail/C9")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasRule(findings, "C9") {
		t.Errorf("expected C9 WARN finding, got %+v", findings)
	}
	for _, f := range findings {
		if f.Rule == "C9" && f.Severity != "WARN" {
			t.Errorf("C9 finding must be WARN, got %q", f.Severity)
		}
	}
}

// TestRunConfigRules_C9_CommandsNegative proves that commands/anything.md does
// NOT trigger C9 even without the atomic- prefix. WHY: commands have no prefix
// requirement — MatchesCommand accepts any .md file — so applying C9 to
// commands would produce false positives on all non-prefixed commands.
func TestRunConfigRules_C9_CommandsNegative(t *testing.T) {
	root := configFixtureDir(t, "pass/C9")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "C9" {
			t.Errorf("C9 triggered on commands/*.md: %+v", f)
		}
	}
}
