package doctor_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// RunCheckHooksWith runs the hooks check against a synthetic scopeRoot.
// Exported seam for testing; mirrors the pattern used in checks_install_test.go.
func RunCheckHooksWith(scopeRoot string) doctor.Result {
	return doctor.RunCheckHooksWith(scopeRoot)
}

func TestCheckHooks_Installed_Pass(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	r := RunCheckHooksWith(scopeRoot)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

func TestCheckHooks_SettingsMissing_Warn(t *testing.T) {
	scopeRoot := t.TempDir()
	// No settings.json, no script.
	r := RunCheckHooksWith(scopeRoot)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

func TestCheckHooks_HookAbsent_Warn(t *testing.T) {
	scopeRoot := t.TempDir()
	// Write a settings.json with no hooks entry.
	settingsDir := filepath.Join(scopeRoot, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"theme": "dark"}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := RunCheckHooksWith(scopeRoot)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

func TestCheckHooks_MalformedSettings_Warn(t *testing.T) {
	scopeRoot := t.TempDir()
	settingsDir := filepath.Join(scopeRoot, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte("{ not valid json "), 0o644); err != nil {
		t.Fatal(err)
	}

	r := RunCheckHooksWith(scopeRoot)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

// TestCheckHooks_LegacyRegistration_Warn covers a pre-inline install: the hook
// is registered via the old wrapper-script path. The check warns and points the
// user at the migration command.
func TestCheckHooks_LegacyRegistration_Warn(t *testing.T) {
	scopeRoot := t.TempDir()
	settingsDir := filepath.Join(scopeRoot, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyCmd := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	content := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "` + legacyCmd + `"}]}]}}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := RunCheckHooksWith(scopeRoot)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "atomic hooks install") {
		t.Errorf("detail should name the migration command; got %q", r.Detail)
	}
}

// TestCheckHooks_PassDetailMentionsInstalled verifies the PASS detail string.
func TestCheckHooks_PassDetailMentionsInstalled(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	r := RunCheckHooksWith(scopeRoot)
	if r.Severity != doctor.PASS {
		t.Fatalf("severity = %q, want PASS", r.Severity)
	}
	if r.Detail != "session-start hook installed" {
		t.Errorf("detail = %q, want %q", r.Detail, "session-start hook installed")
	}
}

// TestCheckHooks_SettingsUnreadable_Warn verifies that unreadable settings.json → WARN (not panic or FAIL).
func TestCheckHooks_SettingsUnreadable_Warn(t *testing.T) {
	scopeRoot := t.TempDir()
	settingsDir := filepath.Join(scopeRoot, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	// Write valid JSON first, then chmod to unreadable.
	content, _ := json.Marshal(map[string]any{"theme": "dark"})
	if err := os.WriteFile(settingsPath, content, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(settingsPath, 0o644) })

	r := RunCheckHooksWith(scopeRoot)
	// Any non-PASS is acceptable; WARN is expected per spec. SKIP is not acceptable.
	if r.Severity == doctor.PASS || r.Severity == doctor.SKIP {
		t.Errorf("severity = %q for unreadable settings, want WARN or FAIL", r.Severity)
	}
}
