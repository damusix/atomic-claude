package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

func writeUserSettings(t *testing.T, targetDir, content string) {
	t.Helper()
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "settings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installStyleFile(t *testing.T, targetDir string) {
	t.Helper()
	stylePath := filepath.Join(targetDir, "output-styles", "atomic.md")
	if err := os.MkdirAll(filepath.Dir(stylePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stylePath, []byte("# Atomic"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckOutputStyle_Unset_Warn(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), ".claude")
	home := t.TempDir()

	r := doctor.RunCheckOutputStyleWith(targetDir, home, "")
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckOutputStyle_SetAndInstalled_PassReportsValue(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), ".claude")
	home := t.TempDir()
	installStyleFile(t, targetDir)
	writeUserSettings(t, targetDir, `{"outputStyle": "Atomic"}`)

	r := doctor.RunCheckOutputStyleWith(targetDir, home, "")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "Atomic") {
		t.Errorf("detail should report the value, got %q", r.Detail)
	}
}

func TestCheckOutputStyle_SetButStyleFileMissing_Warn(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), ".claude")
	home := t.TempDir()
	writeUserSettings(t, targetDir, `{"outputStyle": "Atomic"}`)

	r := doctor.RunCheckOutputStyleWith(targetDir, home, "")
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "not installed") {
		t.Errorf("detail should say the style is not installed, got %q", r.Detail)
	}
}

func TestCheckOutputStyle_NonAtomicValue_PassDoesNotClaimVerified(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), ".claude")
	home := t.TempDir()
	writeUserSettings(t, targetDir, `{"outputStyle": "Explanatory"}`)

	r := doctor.RunCheckOutputStyleWith(targetDir, home, "")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "Explanatory") {
		t.Errorf("detail should report the value, got %q", r.Detail)
	}
	if !strings.Contains(r.Detail, "not verified") {
		t.Errorf("detail should not claim the style file was verified, got %q", r.Detail)
	}
}

func TestCheckOutputStyle_ProjectOverride_Reported(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), ".claude")
	home := t.TempDir()
	installStyleFile(t, targetDir)
	writeUserSettings(t, targetDir, `{"outputStyle": "Atomic"}`)

	repoRoot := t.TempDir()
	repoSettingsDir := filepath.Join(repoRoot, ".claude")
	if err := os.MkdirAll(repoSettingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoSettingsDir, "settings.json"), []byte(`{"outputStyle": "Explanatory"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := doctor.RunCheckOutputStyleWith(targetDir, home, repoRoot)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (project override doesn't change user-level severity)", r.Severity)
	}
	if !strings.Contains(r.Detail, "settings.json") || !strings.Contains(r.Detail, "Explanatory") {
		t.Errorf("detail should name the overriding file and its value, got %q", r.Detail)
	}
	if !strings.Contains(r.Detail, "may override") {
		t.Errorf("detail should flag the value as a possible override, got %q", r.Detail)
	}
}

// TestCheckOutputStyle_RepoRootIsTargetHome_NoSelfOverride reproduces
// `atomic doctor` run from $HOME: repoRoot's own .claude/settings.json is
// the same file already read as the user-level value, so it must not be
// reported as an override of itself.
func TestCheckOutputStyle_RepoRootIsTargetHome_NoSelfOverride(t *testing.T) {
	home := t.TempDir()
	targetDir := filepath.Join(home, ".claude")
	installStyleFile(t, targetDir)
	writeUserSettings(t, targetDir, `{"outputStyle": "Atomic"}`)

	r := doctor.RunCheckOutputStyleWith(targetDir, home, home)
	if strings.Contains(r.Detail, "may override") {
		t.Errorf("detail should not flag the user-level file as its own override, got %q", r.Detail)
	}
}

func TestCheckOutputStyle_BothProjectFiles_BothReported(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), ".claude")
	home := t.TempDir()
	installStyleFile(t, targetDir)
	writeUserSettings(t, targetDir, `{"outputStyle": "Atomic"}`)

	repoRoot := t.TempDir()
	repoSettingsDir := filepath.Join(repoRoot, ".claude")
	if err := os.MkdirAll(repoSettingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoSettingsDir, "settings.json"), []byte(`{"outputStyle": "Explanatory"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoSettingsDir, "settings.local.json"), []byte(`{"outputStyle": "Learning"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r := doctor.RunCheckOutputStyleWith(targetDir, home, repoRoot)
	if !strings.Contains(r.Detail, "settings.json") || !strings.Contains(r.Detail, "Explanatory") {
		t.Errorf("detail should name settings.json's value, got %q", r.Detail)
	}
	if !strings.Contains(r.Detail, "settings.local.json") || !strings.Contains(r.Detail, "Learning") {
		t.Errorf("detail should name settings.local.json's value, got %q", r.Detail)
	}
}
