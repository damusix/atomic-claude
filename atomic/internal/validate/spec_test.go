package validate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

// fixtureBytes reads a testdata fixture and fatals on error.
func fixtureBytes(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "spec", rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

// --- S0: ATX-only ---

// TestRunSpecRules_S0_Pass proves that a file with only ATX headings produces
// no S0 finding. WHY: S0 exists because mdparse only handles ATX correctly;
// a false-positive here would block valid spec files.
func TestRunSpecRules_S0_Pass(t *testing.T) {
	src := fixtureBytes(t, "pass/S0/atx-only.md")
	findings, err := validate.RunSpecRules("testdata/spec/pass/S0/atx-only.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "S0" {
			t.Errorf("S0 false-positive: %+v", f)
		}
	}
}

// TestRunSpecRules_S0_Fail proves that a file with a Setext heading produces
// an S0 FAIL finding. WHY: silent mis-parsing of Setext docs would produce
// wrong section bracketing; loud rejection beats silent wrong.
func TestRunSpecRules_S0_Fail(t *testing.T) {
	src := fixtureBytes(t, "fail/S0/setext.md")
	findings, err := validate.RunSpecRules("testdata/spec/fail/S0/setext.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Rule == "S0" && f.Severity == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected S0 FAIL finding, got %+v", findings)
	}
}

// --- S1: File starts with H1 ---

// TestRunSpecRules_S1_Pass proves that a file starting with # H1 produces no
// S1 finding. WHY: every spec must have a title; false-positives block valid specs.
func TestRunSpecRules_S1_Pass(t *testing.T) {
	src := fixtureBytes(t, "pass/S1/starts-with-h1.md")
	findings, err := validate.RunSpecRules("testdata/spec/pass/S1/starts-with-h1.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "S1" {
			t.Errorf("S1 false-positive: %+v", f)
		}
	}
}

// TestRunSpecRules_S1_Fail proves that a file starting with H2 (no H1) produces
// an S1 FAIL finding. WHY: a spec without a title is structurally broken; the
// rule enforces the minimal identity contract.
func TestRunSpecRules_S1_Fail(t *testing.T) {
	src := fixtureBytes(t, "fail/S1/no-h1.md")
	findings, err := validate.RunSpecRules("testdata/spec/fail/S1/no-h1.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Rule == "S1" && f.Severity == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected S1 FAIL finding, got %+v", findings)
	}
}

// --- S5: ## Checkpoints section with correct table header ---

// TestRunSpecRules_S5_Pass proves that a spec with the correct Checkpoints table
// header produces no S5 finding. WHY: S5 ensures implementation specs are
// actionable — false-positives on valid specs erode trust in the validator.
func TestRunSpecRules_S5_Pass(t *testing.T) {
	src := fixtureBytes(t, "pass/S5/has-checkpoints.md")
	findings, err := validate.RunSpecRules("testdata/spec/pass/S5/has-checkpoints.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "S5" {
			t.Errorf("S5 false-positive: %+v", f)
		}
	}
}

// TestRunSpecRules_S5_Fail proves that a spec missing the ## Checkpoints section
// entirely produces an S5 FAIL. WHY: a spec without Checkpoints cannot drive
// the implement→review loop; this is a structural incompleteness the validator
// must catch.
func TestRunSpecRules_S5_Fail(t *testing.T) {
	src := fixtureBytes(t, "fail/S5/missing-checkpoints.md")
	findings, err := validate.RunSpecRules("testdata/spec/fail/S5/missing-checkpoints.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Rule == "S5" && f.Severity == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected S5 FAIL finding, got %+v", findings)
	}
}

// TestRunSpecRules_S5_WrongHeader proves that a spec with ## Checkpoints but a
// wrong table header produces an S5 FAIL. WHY: the exact header is the machine-
// readable contract; a different header is a real defect, not a style issue.
func TestRunSpecRules_S5_WrongHeader(t *testing.T) {
	src := []byte(`# Spec with wrong table header

## Checkpoints

| CP | Lands |
|----|-------|
| 1  | foo   |

## Change log

<!-- empty -->
`)
	findings, err := validate.RunSpecRules("inline.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Rule == "S5" && f.Severity == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected S5 FAIL on wrong header, got %+v", findings)
	}
}

// --- S6: ## Change log section ---

// TestRunSpecRules_S6_Pass proves that a spec with ## Change log produces no
// S6 finding. WHY: the change log is the audit trail; false-positives on valid
// specs would discourage maintaining it.
func TestRunSpecRules_S6_Pass(t *testing.T) {
	src := fixtureBytes(t, "pass/S6/has-changelog.md")
	findings, err := validate.RunSpecRules("testdata/spec/pass/S6/has-changelog.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "S6" {
			t.Errorf("S6 false-positive: %+v", f)
		}
	}
}

// TestRunSpecRules_S6_Fail proves that a spec without ## Change log produces an
// S6 FAIL. WHY: every spec must carry an audit trail — absence signals a spec
// that has never been maintained or a freshly edited spec that lost its log.
func TestRunSpecRules_S6_Fail(t *testing.T) {
	src := fixtureBytes(t, "fail/S6/missing-changelog.md")
	findings, err := validate.RunSpecRules("testdata/spec/fail/S6/missing-changelog.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, f := range findings {
		if f.Rule == "S6" && f.Severity == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected S6 FAIL finding, got %+v", findings)
	}
}

// --- Integration: validate spec subcommand + flag parsing ---

// TestDispatch_FlagAfterSubcommand_JSONHonored proves that --json placed after
// the subcommand is honored (flag state is captured, not just exit code).
// WHY: F-1 was that validate spec --json silently produced human output; F-2
// strengthens this test to assert the flag was actually seen. A future regression
// where jsonOut is reset to false must cause this test to fail.
func TestDispatch_FlagAfterSubcommand_JSONHonored(t *testing.T) {
	// Use a temp dir with a valid spec to ensure the runner has work to do.
	dir := t.TempDir()
	specFile := filepath.Join(dir, "test.md")
	content := `# Test Spec

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | test | test.go | passes |

## Change log

<!-- empty -->
`
	if err := os.WriteFile(specFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var buf strings.Builder
	// --json after the subcommand: must be honored
	code := validate.RunWithOutput([]string{"spec", "--json", specFile}, &buf)
	out := buf.String()
	if code != 0 {
		t.Errorf("exit %d (want 0); output:\n%s", code, out)
	}
	// The output must be JSON (schema_version key), not human text.
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json after subcommand: expected JSON output with schema_version, got:\n%s", out)
	}
}

// TestDispatch_FlagBeforeSubcommand_JSONHonored proves --json before subcommand
// also produces JSON output (regression guard for the top-level parse path).
func TestDispatch_FlagBeforeSubcommand_JSONHonored(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "test.md")
	content := `# Test Spec

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | test | test.go | passes |

## Change log

<!-- empty -->
`
	if err := os.WriteFile(specFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var buf strings.Builder
	code := validate.RunWithOutput([]string{"--json", "spec", specFile}, &buf)
	out := buf.String()
	if code != 0 {
		t.Errorf("exit %d (want 0); output:\n%s", code, out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json before subcommand: expected JSON output with schema_version, got:\n%s", out)
	}
}

// TestDispatch_NoSubcommand_NeutralMessage proves that no-subcommand exits 2 with
// a neutral message (not the old "whole-repo mode not yet implemented").
// WHY: F-3 — leaking checkpoint schedule to users is unprofessional.
func TestDispatch_NoSubcommand_NeutralMessage(t *testing.T) {
	var buf strings.Builder
	code := validate.RunWithOutput([]string{}, &buf)
	if code != 2 {
		t.Errorf("no subcommand: got exit %d, want 2", code)
	}
	out := buf.String()
	if strings.Contains(out, "not yet implemented") {
		t.Errorf("no-subcommand message leaks implementation schedule: %q", out)
	}
	if !strings.Contains(out, "subcommand required") {
		t.Errorf("no-subcommand message should contain 'subcommand required', got: %q", out)
	}
}

// TestDispatch_SuggestFlagForS5 proves that --suggest for a file with S5 failure
// prints the structural template. WHY: --suggest is the only user-actionable
// output for structural defects; absence means users see FAIL with no path forward.
func TestDispatch_SuggestFlagForS5(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "test.md")
	// Missing Checkpoints section → S5 FAIL
	content := `# Test Spec

## Change log

<!-- empty -->
`
	if err := os.WriteFile(specFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var buf strings.Builder
	code := validate.RunWithOutput([]string{"spec", "--suggest", specFile}, &buf)
	out := buf.String()
	// Should exit 1 (FAIL) and include the suggestion template.
	if code != 1 {
		t.Errorf("exit %d (want 1 for FAIL); output:\n%s", code, out)
	}
	if !strings.Contains(out, "Checkpoints") {
		t.Errorf("--suggest output should contain Checkpoints template, got:\n%s", out)
	}
}

// TestDispatch_SpecOnAtomicValidateMd proves that atomic-validate's own spec
// passes all four rules (S0/S1/S5/S6). WHY: the tool must eat its own dogfood —
// if the canonical spec file fails its own rules, the rules are wrong or the
// spec is defective.
func TestDispatch_SpecOnAtomicValidateMd(t *testing.T) {
	specPath := filepath.Join("..", "..", "..", "docs", "spec", "atomic-validate.md")
	src, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("cannot read %s (not running from repo): %v", specPath, err)
	}
	findings, err := validate.RunSpecRules(specPath, src)
	if err != nil {
		t.Fatalf("RunSpecRules: %v", err)
	}
	for _, f := range findings {
		if f.Severity == "FAIL" {
			t.Errorf("atomic-validate.md fails its own rule %s: %s", f.Rule, f.Message)
		}
	}
}
