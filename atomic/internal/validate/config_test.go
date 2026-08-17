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

func TestRunConfigRules_C3_CodeBlockNegative(t *testing.T) {
	root := configFixtureDir(t, "pass/C3")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "C3" {
			t.Errorf("C3 triggered on fenced-block-only reference: %+v", f)
		}
	}
}

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

func TestRunConfigRules_C5_CodeBlockNegative(t *testing.T) {
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

func TestRunConfigRules_C5_IgnoresLocalOverlay(t *testing.T) {
	root := configFixtureDir(t, "pass/C5-local-ignored")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C5") {
		t.Errorf("C5 must not fire on claude.local.md; findings: %+v", findings)
	}
}

func TestRunConfigRules_C5_EmailNotRef(t *testing.T) {
	root := configFixtureDir(t, "pass/C5-email")
	findings, err := validate.RunConfigRules(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasRule(findings, "C5") {
		t.Errorf("C5 false-positive on email addresses; findings: %+v", findings)
	}
}

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
