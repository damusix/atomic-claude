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

func TestRunSpecRules_S5_Pass_SixCol(t *testing.T) {
	src := fixtureBytes(t, "pass/S5/has-checkpoints-6col.md")
	findings, err := validate.RunSpecRules("testdata/spec/pass/S5/has-checkpoints-6col.md", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "S5" {
			t.Errorf("S5 false-positive on 6-col canonical header: %+v", f)
		}
	}
}

func TestRunSpecRules_S5_Fail_MissingRequiredCol(t *testing.T) {
	src := fixtureBytes(t, "fail/S5/missing-required-col.md")
	findings, err := validate.RunSpecRules("testdata/spec/fail/S5/missing-required-col.md", src)
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
		t.Errorf("expected S5 FAIL for header missing required column, got %+v", findings)
	}
}

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

func TestDispatch_FlagAfterSubcommand_JSONHonored(t *testing.T) {
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
	code := validate.RunWithOutput([]string{"spec", "--json", specFile}, &buf)
	out := buf.String()
	if code != 0 {
		t.Errorf("exit %d (want 0); output:\n%s", code, out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json after subcommand: expected JSON output with schema_version, got:\n%s", out)
	}
}

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

func TestDispatch_NoSubcommand_WholeRepo(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	noRepo := t.TempDir()
	if err := os.Chdir(noRepo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	var buf strings.Builder
	code := validate.RunWithOutput([]string{}, &buf)
	out := buf.String()

	if code != 2 {
		t.Errorf("no subcommand outside repo: got exit %d, want 2\noutput: %q", code, out)
	}
	if strings.Contains(out, "subcommand required") {
		t.Errorf("whole-repo dispatch: old stub message leaked: %q", out)
	}
}

func TestDispatch_SuggestFlagForS5(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "test.md")
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
	if code != 1 {
		t.Errorf("exit %d (want 1 for FAIL); output:\n%s", code, out)
	}
	if !strings.Contains(out, "Checkpoints") {
		t.Errorf("--suggest output should contain Checkpoints template, got:\n%s", out)
	}
}

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
