package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
	"github.com/damusix/atomic-claude/atomic/internal/where"
	"github.com/damusix/atomic-claude/atomic/internal/workflow"
)

// SessionStart writes under $HOME and its profile refresh detects logins from
// the env, so both are sandboxed: no real ~/.claude, no inherited tokens.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "hooks-test-home")
	if err != nil {
		panic(err)
	}
	origHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}
	if err := os.Unsetenv(config.StateDirEnvVar); err != nil {
		panic(err)
	}
	for _, name := range profile.WorkflowEnvVars() {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
	restore := config.SetHomeDirForTest(home)
	code := m.Run()
	restore()
	if err := os.Setenv("HOME", origHome); err != nil {
		panic(err)
	}
	os.RemoveAll(home)
	os.Exit(code)
}

func legacyRemindersDir(root string) string {
	return filepath.Join(config.ProjectStateDir(root), "reminders")
}

// addReminderWithDate writes a reminder-like file where reminders used to live.
func addReminderWithDate(t *testing.T, root, body string, daysAgo int) {
	t.Helper()
	dir := legacyRemindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().AddDate(0, 0, -daysAgo).Format("2006-01-02")
	name := fmt.Sprintf("%s-note-%d.md", created, daysAgo)
	content := "---\ncreated: " + created + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stubNoWherePosition pins the plain no-wiki/no-realm case, keeping empty-output
// assertions off the real registry and the test process's own ancestor tree.
func stubNoWherePosition(t *testing.T) {
	t.Helper()
	orig := hooks.WherePosition
	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = orig })
}

// tempHome points $HOME at a temp dir and stubs the profile refresh, whose real
// subprocesses write into $HOME past the call and race t.TempDir's RemoveAll.
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	hooks.ProfileRefresh = func(_, _ string, _ int) (bool, error) { return false, nil }
	t.Cleanup(func() { hooks.ProfileRefresh = hooks.DefaultProfileRefresh })
	return home
}

func TestInstall_EmptyDir_RegistersInlineCommand(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// No wrapper script is written — the command is inlined into settings.json.
	scriptPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	if _, err := os.Stat(scriptPath); err == nil {
		t.Errorf("wrapper script should not be created, but %s exists", scriptPath)
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not found: %v", err)
	}
	if cmd := sessionStartCommandIn(t, raw); cmd != "atomic hooks session-start --harness claude" {
		t.Errorf("registered command = %q, want %q", cmd, "atomic hooks session-start --harness claude")
	}
}

// sessionStartCommandIn fails the test when the expected structure is absent.
func sessionStartCommandIn(t *testing.T, raw []byte) string {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings.json invalid JSON: %v", err)
	}
	hooks_, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks key missing: %v", settings)
	}
	ss, ok := hooks_["SessionStart"].([]any)
	if !ok || len(ss) == 0 {
		t.Fatalf("SessionStart missing: %v", hooks_)
	}
	entry, _ := ss[0].(map[string]any)
	inner, _ := entry["hooks"].([]any)
	if len(inner) == 0 {
		t.Fatalf("inner hooks missing: %v", entry)
	}
	h, _ := inner[0].(map[string]any)
	cmd, _ := h["command"].(string)
	return cmd
}

func TestInstall_ExistingUnrelatedKeys_Preserved(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"theme": "dark", "hooks": {"PreToolUse": [{"matcher": ".*", "hooks": [{"type": "command", "command": "echo hi"}]}]}}`
	os.WriteFile(settingsPath, []byte(initial), 0o644)

	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)

	if settings["theme"] != "dark" {
		t.Errorf("theme key not preserved: %v", settings["theme"])
	}
	hooks_, _ := settings["hooks"].(map[string]any)
	if _, ok := hooks_["PreToolUse"]; !ok {
		t.Errorf("PreToolUse not preserved: %v", hooks_)
	}
	if _, ok := hooks_["SessionStart"]; !ok {
		t.Errorf("SessionStart not added: %v", hooks_)
	}
}

func TestInstall_Idempotent(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Errorf("expected 1 SessionStart entry after idempotent install, got %d", len(ss))
	}
}

func TestInstall_ExistingSessionStartElsewhere_Appends(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	initial := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "/other/hook.sh"}]}]}}`
	os.WriteFile(settingsPath, []byte(initial), 0o644)

	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 2 {
		t.Errorf("expected 2 SessionStart entries after append, got %d", len(ss))
	}
}

func TestInstall_MalformedSettings_Refuses(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{ not valid json "), 0o644)

	_, err := hooks.Install(repoRoot, scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}

	raw, _ := os.ReadFile(settingsPath)
	if string(raw) != "{ not valid json " {
		t.Errorf("malformed settings.json was modified: %q", string(raw))
	}
}

// An unparseable settings.json must block a legacy adoption's plan, not pass
// it and fail later in the settings writer.
func TestSettingsShapeError_ReportsUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{ not valid json "), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hooks.SettingsShapeError(path); err == nil {
		t.Fatal("SettingsShapeError = nil for an unparseable file, want its parse error")
	}
	if err := hooks.SettingsShapeError(filepath.Join(t.TempDir(), "absent.json")); err != nil {
		t.Errorf("SettingsShapeError on an absent file = %v, want nil", err)
	}
}

func TestInstall_ScopeProject_WritesUnderClaudeDir(t *testing.T) {
	projectRoot := t.TempDir()
	if _, err := hooks.Install(projectRoot, projectRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}
	settingsPath := filepath.Join(projectRoot, ".claude", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not found under project root: %v", err)
	}
	if cmd := sessionStartCommandIn(t, raw); cmd != "atomic hooks session-start --harness claude" {
		t.Errorf("registered command = %q, want inline command", cmd)
	}
}

// A full install+uninstall cycle must not corrupt JWCC settings.json.
func TestInstall_JWCCSettingsPreservesCommentsAndTrailingCommas(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	jwcc := `{
  // user preference
  "theme": "dark",
  "model": "claude-opus-4-6", // pinned
}
`
	os.WriteFile(settingsPath, []byte(jwcc), 0o644)

	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install on JWCC settings: %v", err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after install: %v", err)
	}

	if !strings.Contains(string(raw), "// user preference") {
		t.Errorf("install stripped JWCC comment from settings.json:\n%s", raw)
	}
	// The trailing comma after the last original key must survive (JWCC feature).
	if !strings.Contains(string(raw), `"claude-opus-4-6",`) {
		t.Errorf("install stripped trailing comma from JWCC settings.json:\n%s", raw)
	}

	if !strings.Contains(string(raw), "SessionStart") {
		t.Errorf("install did not add SessionStart to JWCC settings:\n%s", raw)
	}

	if _, err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall on JWCC settings: %v", err)
	}

	raw2, _ := os.ReadFile(settingsPath)
	if !strings.Contains(string(raw2), "// user preference") {
		t.Errorf("uninstall stripped JWCC comment from settings.json:\n%s", raw2)
	}
	if strings.Contains(string(raw2), "SessionStart") {
		t.Errorf("SessionStart should be removed after uninstall:\n%s", raw2)
	}
}

func TestUninstall_RemovesRegistration(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	hooks.Install(repoRoot, scopeRoot)

	if _, err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	installed, _, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Error("hook still registered after uninstall")
	}
}

// Uninstall removes the legacy wrapper script but not its siblings.
func TestUninstall_RemovesLegacyScriptFile(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	hooks.Install(repoRoot, scopeRoot)

	hooksDir := filepath.Join(scopeRoot, ".claude", "hooks")
	os.MkdirAll(hooksDir, 0o755)
	legacyPath := filepath.Join(hooksDir, "session-start-reminders.sh")
	os.WriteFile(legacyPath, []byte("#!/usr/bin/env bash\nexec atomic hooks session-start\n"), 0o755)
	siblingPath := filepath.Join(hooksDir, "other.sh")
	os.WriteFile(siblingPath, []byte("#!/bin/bash\necho other\n"), 0o755)

	if _, err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(legacyPath); err == nil {
		t.Error("legacy wrapper script still exists after uninstall")
	}
	if _, err := os.Stat(siblingPath); err != nil {
		t.Errorf("sibling hook removed: %v", err)
	}
}

func TestUninstall_DropsHooksKeyWhenEmpty(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	hooks.Install(repoRoot, scopeRoot)
	hooks.Uninstall(repoRoot, scopeRoot)

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)

	if _, has := settings["hooks"]; has {
		if m, ok := settings["hooks"].(map[string]any); ok && len(m) == 0 {
			t.Error("empty hooks object should be dropped from settings.json")
		}
	}
}

func TestUninstall_NoScript_NoError(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if _, err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall without prior install: %v", err)
	}
}

func TestUninstall_RemovesOutputStyleWhenAtomic(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte(`{"outputStyle": "Atomic"}`), 0o644)

	if _, err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	if _, present := settings["outputStyle"]; present {
		t.Errorf("outputStyle should be removed after uninstall, got: %s", raw)
	}
}

func TestUninstall_LeavesNonAtomicOutputStyle(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte(`{"outputStyle": "Explanatory"}`), 0o644)

	if _, err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	if settings["outputStyle"] != "Explanatory" {
		t.Errorf("outputStyle should be left untouched, got: %v", settings["outputStyle"])
	}

	// Control: the same Uninstall call removes an exact "Atomic" value, so
	// this test proves the guard discriminates by value rather than passing
	// because nothing about outputStyle is wired at all.
	controlRoot := t.TempDir()
	controlSettings := filepath.Join(controlRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(controlSettings), 0o755)
	os.WriteFile(controlSettings, []byte(`{"outputStyle": "Atomic"}`), 0o644)

	if _, err := hooks.Uninstall(repoRoot, controlRoot); err != nil {
		t.Fatalf("Uninstall (control): %v", err)
	}
	controlRaw, _ := os.ReadFile(controlSettings)
	var controlSettingsMap map[string]any
	json.Unmarshal(controlRaw, &controlSettingsMap)
	if _, present := controlSettingsMap["outputStyle"]; present {
		t.Errorf("control: outputStyle should be removed when exactly Atomic, got: %v", controlSettingsMap["outputStyle"])
	}
}

func TestUninstall_PreservesOtherRegistrations(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	scriptPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	initial := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "/other/hook.sh"}]}, {"matcher": ".*", "hooks": [{"type": "command", "command": "` + scriptPath + `"}]}]}}`
	os.WriteFile(settingsPath, []byte(initial), 0o644)
	os.MkdirAll(filepath.Dir(scriptPath), 0o755)
	os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexec atomic hooks session-start\n"), 0o755)

	if _, err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Errorf("expected 1 remaining SessionStart entry, got %d", len(ss))
	}
	entry, _ := ss[0].(map[string]any)
	innerHooks, _ := entry["hooks"].([]any)
	innerHook, _ := innerHooks[0].(map[string]any)
	if innerHook["command"] != "/other/hook.sh" {
		t.Errorf("wrong remaining hook: %v", innerHook["command"])
	}
}

func TestIsInstalled_AfterInstall_InstalledNotDrifted(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Error("installed = false, want true")
	}
	if drifted {
		t.Error("drifted = true, want false")
	}
}

func TestIsInstalled_NoSettingsFile_NotInstalled(t *testing.T) {
	scopeRoot := t.TempDir()
	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Error("installed = true, want false")
	}
	if drifted {
		t.Error("drifted = true, want false")
	}
}

func TestIsInstalled_NoHookEntry_NotInstalled(t *testing.T) {
	scopeRoot := t.TempDir()
	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(settingsPath, []byte(`{"theme":"dark"}`), 0o644)

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Error("installed = true, want false")
	}
	if drifted {
		t.Error("drifted = true, want false")
	}
}

// seedLegacyRegistration registers the old wrapper-script path as the command.
func seedLegacyRegistration(t *testing.T, scopeRoot string) {
	t.Helper()
	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyCmd := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	content := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "` + legacyCmd + `"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsInstalled_LegacyRegistration_InstalledAndDrifted(t *testing.T) {
	scopeRoot := t.TempDir()
	seedLegacyRegistration(t, scopeRoot)

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Error("installed = false, want true (legacy registration still fires)")
	}
	if !drifted {
		t.Error("drifted = false, want true (legacy wrapper-script form)")
	}
}

func TestIsInstalled_MalformedSettings_Error(t *testing.T) {
	scopeRoot := t.TempDir()
	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{ bad json"), 0o644)

	_, _, err := hooks.IsInstalled(scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}
}

// Install over a wrapper-script install must leave exactly one entry, so the
// hook cannot double-fire.
func TestInstall_MigratesLegacyRegistration(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	seedLegacyRegistration(t, scopeRoot)
	legacyPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	os.MkdirAll(filepath.Dir(legacyPath), 0o755)
	os.WriteFile(legacyPath, []byte("#!/usr/bin/env bash\nexec atomic hooks session-start\n"), 0o755)

	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := os.Stat(legacyPath); err == nil {
		t.Error("legacy wrapper script not removed by migration")
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Fatalf("expected 1 SessionStart entry after migration, got %d", len(ss))
	}
	if cmd := sessionStartCommandIn(t, raw); cmd != "atomic hooks session-start --harness claude" {
		t.Errorf("post-migration command = %q, want inline command", cmd)
	}

	installed, drifted, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed || drifted {
		t.Errorf("post-migration IsInstalled = (installed=%v, drifted=%v), want (true, false)", installed, drifted)
	}
}

// The seam receives home itself, not <home>/.claude — config.ProfilePath expects
// the former. HOME is injected so the assertion never reads the real one.
func TestSessionStart_ProfileRefreshCalled(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	home := tempHome(t)
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	var gotHome, gotToday string
	var gotDays int
	hooks.ProfileRefresh = func(home, today string, days int) (bool, error) {
		gotHome = home
		gotToday = today
		gotDays = days
		return false, nil
	}
	t.Cleanup(func() { hooks.ProfileRefresh = hooks.DefaultProfileRefresh })

	_, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	if gotDays != profile.DefaultRefreshDays {
		t.Errorf("profileRefresh called with days=%d, want profile.DefaultRefreshDays=%d", gotDays, profile.DefaultRefreshDays)
	}
	wantToday := now.Format("2006-01-02")
	if gotToday != wantToday {
		t.Errorf("profileRefresh called with today=%q, want %q", gotToday, wantToday)
	}
	if gotHome != home {
		t.Errorf("profileRefresh called with home=%q, want %q (not <home>/.claude)", gotHome, home)
	}
}

func TestSessionStartText_ProfileRefreshCalled(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	called := false
	hooks.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		called = true
		return false, nil
	}
	t.Cleanup(func() { hooks.ProfileRefresh = hooks.DefaultProfileRefresh })

	hooks.SessionStartText(root, "claude", now)
	if !called {
		t.Error("profileRefresh seam was not called by SessionStartText")
	}
}

// The refresh is best-effort: a disk failure must not drop session context.
func TestSessionStart_ProfileRefreshError_NeverBlocks(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		return false, fmt.Errorf("simulated refresh failure")
	}
	t.Cleanup(func() { hooks.ProfileRefresh = hooks.DefaultProfileRefresh })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart returned error despite best-effort refresh: %v", err)
	}
	if !strings.Contains(out, "## Workflow preferences") {
		t.Errorf("workflow section missing despite refresh failure: %q", out)
	}
}

func TestSessionStart_SeedOutputStyleCalled(t *testing.T) {
	stubNoWherePosition(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	root := t.TempDir()
	now := time.Now().UTC()

	var gotSettingsDir, gotTargetDir, gotHome string
	hooks.SeedOutputStyleFn = func(settingsDir, targetDir, home string) (bool, error) {
		gotSettingsDir, gotTargetDir, gotHome = settingsDir, targetDir, home
		return false, nil
	}
	t.Cleanup(func() { hooks.SeedOutputStyleFn = hooks.DefaultSeedOutputStyle })

	if _, err := hooks.SessionStart(root, "claude", now); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	wantTarget := filepath.Join(home, ".claude")
	if gotSettingsDir != wantTarget || gotHome != home {
		t.Errorf("seed called with settingsDir=%q home=%q, want %q and %q", gotSettingsDir, gotHome, wantTarget, home)
	}
	if gotTargetDir != wantTarget {
		t.Errorf("seed called with targetDir=%q, want %q", gotTargetDir, wantTarget)
	}
}

// Install writes the Claude root CLAUDE_CONFIG_DIR names, so the session-start
// hook must seed and read that root, not ~/.claude.
func TestSessionStart_FollowsClaudeConfigDir(t *testing.T) {
	home := tempHome(t)
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	var gotSettingsDir, gotTargetDir, gotClaudeMD string
	hooks.SeedOutputStyleFn = func(settingsDir, targetDir, _ string) (bool, error) {
		gotSettingsDir, gotTargetDir = settingsDir, targetDir
		return false, nil
	}
	t.Cleanup(func() { hooks.SeedOutputStyleFn = hooks.DefaultSeedOutputStyle })
	orig := hooks.WherePosition
	hooks.WherePosition = func(_, claudeMDPath string) (where.Report, error) {
		gotClaudeMD = claudeMDPath
		return where.Report{}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = orig })

	if _, err := hooks.SessionStart(t.TempDir(), "claude", time.Now().UTC()); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if gotSettingsDir != configDir || gotTargetDir != configDir {
		t.Errorf("seed settings=%q target=%q, want both %q (home %q)", gotSettingsDir, gotTargetDir, configDir, home)
	}
	if want := filepath.Join(configDir, "CLAUDE.md"); gotClaudeMD != want {
		t.Errorf("where position read %q, want %q", gotClaudeMD, want)
	}
}

func TestSessionStartText_SeedOutputStyleCalled(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	now := time.Now().UTC()

	called := false
	hooks.SeedOutputStyleFn = func(scopeRoot, targetDir, home string) (bool, error) {
		called = true
		return false, nil
	}
	t.Cleanup(func() { hooks.SeedOutputStyleFn = hooks.DefaultSeedOutputStyle })

	hooks.SessionStartText(root, "claude", now)
	if !called {
		t.Error("seedOutputStyleSilent seam was not called by SessionStartText")
	}
}

// The profile and preferences are harness-neutral, but the style seed writes
// Claude's settings.json, so only a Claude session may run it.
func TestSessionStartText_RefreshesProfileEverywhereSeedsStyleOnlyForClaude(t *testing.T) {
	stubNoWherePosition(t)
	for _, tc := range []struct {
		harness  string
		wantSeed bool
	}{{"claude", true}, {"omp", false}, {"codex", false}} {
		t.Run(tc.harness, func(t *testing.T) {
			refreshed, seeded := false, false
			hooks.ProfileRefresh = func(_, _ string, _ int) (bool, error) { refreshed = true; return false, nil }
			hooks.SeedOutputStyleFn = func(_, _, _ string) (bool, error) { seeded = true; return false, nil }
			t.Cleanup(func() {
				hooks.ProfileRefresh = hooks.DefaultProfileRefresh
				hooks.SeedOutputStyleFn = hooks.DefaultSeedOutputStyle
			})

			hooks.SessionStartText(t.TempDir(), tc.harness, time.Now().UTC())
			if !refreshed {
				t.Error("profile was not refreshed")
			}
			if seeded != tc.wantSeed {
				t.Errorf("style seeded = %v, want %v", seeded, tc.wantSeed)
			}
		})
	}
}

// The seed is silent: it contributes nothing to the payload, whether the
// style file is installed and the key gets written or not.
func TestSessionStart_SeedOutputStyle_PayloadUnaffected(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	home := tempHome(t)
	now := time.Now().UTC()

	outBefore, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart (style not installed): %v", err)
	}

	styleDir := filepath.Join(home, ".claude", "output-styles")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatalf("mkdir output-styles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(styleDir, "atomic.md"), []byte("---\nname: Atomic\n---\n"), 0o644); err != nil {
		t.Fatalf("write atomic.md: %v", err)
	}

	outAfter, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart (style installed): %v", err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected settings.json seeded: %v", err)
	}
	if !strings.Contains(string(raw), `"outputStyle"`) || !strings.Contains(string(raw), "Atomic") {
		t.Errorf("settings.json missing seeded outputStyle: %s", raw)
	}

	if outBefore != outAfter {
		t.Errorf("payload changed once seeded:\nbefore: %s\nafter:  %s", outBefore, outAfter)
	}
}

// A malformed settings.json must never block SessionStart, even when the
// style is installed and the seed would otherwise write to it.
func TestSessionStart_SeedOutputStyle_MalformedSettings_NeverBlocks(t *testing.T) {
	stubNoWherePosition(t)
	root := t.TempDir()
	home := tempHome(t)
	now := time.Now().UTC()

	claudeDir := filepath.Join(home, ".claude")
	styleDir := filepath.Join(claudeDir, "output-styles")
	if err := os.MkdirAll(styleDir, 0o755); err != nil {
		t.Fatalf("mkdir output-styles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(styleDir, "atomic.md"), []byte("---\nname: Atomic\n---\n"), 0o644); err != nil {
		t.Fatalf("write atomic.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed settings.json: %v", err)
	}

	// Control: prove the seed is actually reached (and its error swallowed)
	// rather than never invoked in the first place.
	called := false
	hooks.SeedOutputStyleFn = func(scopeRoot, targetDir, home string) (bool, error) {
		called = true
		return hooks.DefaultSeedOutputStyle(scopeRoot, targetDir, home)
	}
	t.Cleanup(func() { hooks.SeedOutputStyleFn = hooks.DefaultSeedOutputStyle })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart returned error despite malformed settings.json: %v", err)
	}
	if !called {
		t.Error("expected the seed to be attempted against the malformed settings file")
	}
	if !strings.Contains(out, "## Workflow preferences") {
		t.Errorf("workflow section missing despite malformed settings: %q", out)
	}
}

// A plain position adds no orientation section.
func TestSessionStart_WhereSuppressed_PlainPosition(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{}, nil // plain: RepoScope.Found=false, RealmScope.Position=RealmNone
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if ctx := additionalContext(t, out); !strings.HasPrefix(ctx, "## Workflow preferences\n") {
		t.Errorf("expected the workflow section alone for plain position, got %q", ctx)
	}
}

// A repo-scope wiki surfaces one orientation line on its own.
func TestSessionStart_WhereSurfaced_RepoScopeFound(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RepoScope: where.RepoScopeReport{Found: true, Path: "/some/repo/docs/wiki/index.md"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "/some/repo/docs/wiki/index.md") {
		t.Errorf("orientation nudge missing repo-scope path: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}

	orientation, _, _ := strings.Cut(ctx, "\n\n")
	lines := strings.Split(orientation, "\n")
	bulletCount := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- ") {
			bulletCount++
		}
	}
	if bulletCount != 1 {
		t.Errorf("expected exactly 1 orientation bullet, got %d: %q", bulletCount, ctx)
	}
}

// Realm-only position: the sibling test covers RepoScope.Found alone, leaving
// isPlainPosition's RealmScope clause and whereNudgeLine's switch unreached.
func TestSessionStart_WhereSurfaced_RealmMember(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RealmScope: where.RealmScopeReport{Position: where.RealmMember, RealmRoot: "/some/realm/root"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when realm position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "realm member of /some/realm/root") {
		t.Errorf("orientation nudge missing realm-member content: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}
}

// Covers whereNudgeLine's RealmRoot case.
func TestSessionStart_WhereSurfaced_RealmRoot(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RealmScope: where.RealmScopeReport{Position: where.RealmRoot, RealmRoot: "/some/realm/root"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when realm position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "realm root (/some/realm/root)") {
		t.Errorf("orientation nudge missing realm-root content: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}
}

// Covers whereNudgeLine's RealmOrphaned case.
func TestSessionStart_WhereSurfaced_RealmOrphaned(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RealmScope: where.RealmScopeReport{Position: where.RealmOrphaned, RealmRoot: "/some/realm/root"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output when realm position is non-trivial")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	if !strings.Contains(ctx, "orphaned under realm root /some/realm/root (not a registered member)") {
		t.Errorf("orientation nudge missing realm-orphaned content: %q", ctx)
	}
	if !strings.Contains(ctx, "atomic where") {
		t.Errorf("orientation nudge missing pointer to `atomic where`: %q", ctx)
	}
}

// The seam gets repoRoot as cwd, not the hook process's own working directory.
func TestSessionStart_WhereSeamReceivesRepoRoot(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	var gotCwd string
	hooks.WherePosition = func(cwd, _ string) (where.Report, error) {
		gotCwd = cwd
		return where.Report{}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	if _, err := hooks.SessionStart(root, "claude", now); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if gotCwd != root {
		t.Errorf("WherePosition called with cwd=%q, want repoRoot=%q", gotCwd, root)
	}
}

// Orientation is best-effort: a seam error must not block the session.
func TestSessionStart_WhereError_NeverBlocks(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()

	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{}, fmt.Errorf("simulated where resolution failure")
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })

	out, err := hooks.SessionStart(root, "claude", now)
	if err != nil {
		t.Fatalf("SessionStart returned error despite best-effort where check: %v", err)
	}
	if ctx := additionalContext(t, out); !strings.HasPrefix(ctx, "## Workflow preferences\n") {
		t.Errorf("expected the workflow section alone after a where error, got %q", ctx)
	}
}

// TestInstall_ReadOnlySettings_ReportsSkippedNotSuccess reproduces the
// regression where a read-only settings.json made Install report success
// (nil error) while writing nothing and registering no SessionStart hook.
func TestInstall_ReadOnlySettings_ReportsSkippedNotSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	original := []byte(`{"theme":"dark"}` + "\n")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, original, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(settingsPath, 0o644) })

	skipped, err := hooks.Install(repoRoot, scopeRoot)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !skipped {
		t.Fatal("expected skipped=true for a read-only settings.json")
	}
	raw, readErr := os.ReadFile(settingsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(raw) != string(original) {
		t.Errorf("read-only settings.json was modified: got %q, want %q", raw, original)
	}

	installed, _, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if installed {
		t.Error("SessionStart must not be registered when the write was skipped")
	}
}

// TestUninstall_ReadOnlySettings_ReportsSkippedNotSuccess mirrors the Install
// case for the uninstall path.
func TestUninstall_ReadOnlySettings_ReportsSkippedNotSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	if _, err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("setup Install: %v", err)
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(settingsPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(settingsPath, 0o644) })

	skipped, err := hooks.Uninstall(repoRoot, scopeRoot)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !skipped {
		t.Fatal("expected skipped=true for a read-only settings.json")
	}

	after, readErr := os.ReadFile(settingsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Errorf("read-only settings.json was modified: got %q, want %q", after, before)
	}

	installed, _, err := hooks.IsInstalled(scopeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Error("SessionStart registration should still be present since the write was skipped")
	}
}

func TestUninstall_MalformedSettings_Refuses(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{ broken"), 0o644)

	_, err := hooks.Uninstall(repoRoot, scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}
	raw, _ := os.ReadFile(settingsPath)
	if string(raw) != "{ broken" {
		t.Errorf("malformed settings.json was modified")
	}
}

func stubRepoScopeWiki(t *testing.T) {
	t.Helper()
	hooks.WherePosition = func(_, _ string) (where.Report, error) {
		return where.Report{
			RepoScope: where.RepoScopeReport{Found: true, Path: "/r/docs/wiki/index.md"},
		}, nil
	}
	t.Cleanup(func() { hooks.WherePosition = hooks.DefaultWherePosition })
}

func gitRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return repo
}

func additionalContext(t *testing.T, out string) string {
	t.Helper()
	var payload struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, out)
	}
	return payload.HookSpecificOutput.AdditionalContext
}

// The worktree path differs by harness, so "omp" proves the flag reaches the
// resolver rather than a hard-coded Claude default.
func TestSessionStart_WorkflowSectionFollowsOrientation(t *testing.T) {
	stubRepoScopeWiki(t)
	home := tempHome(t)
	root := gitRepo(t)

	out, err := hooks.SessionStart(root, "omp", time.Now().UTC())
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	resolved, err := workflow.Resolve(workflow.Options{Dir: root, Home: home, Harness: "omp"})
	if err != nil {
		t.Fatalf("workflow.Resolve: %v", err)
	}
	block := strings.TrimRight(workflow.RenderText(resolved), "\n")
	if !strings.Contains(block, ".atomic/worktrees") {
		t.Fatalf("fixture: omp block lacks its worktree path:\n%s", block)
	}

	ctx := additionalContext(t, out)
	section := "## Workflow preferences\n" + block
	if !strings.HasPrefix(ctx, "## Orientation\n") || !strings.HasSuffix(ctx, "\n\n"+section) {
		t.Errorf("want orientation, then\n%s\ngot:\n%s", section, ctx)
	}

	if text := hooks.SessionStartText(root, "omp", time.Now().UTC()); text != ctx {
		t.Errorf("text form differs from additionalContext:\n%s\nvs\n%s", text, ctx)
	}
}

// The hook never blocks the session, so an unresolvable directory drops only
// the workflow section.
func TestSessionStart_ResolverFailureOmitsWorkflowSection(t *testing.T) {
	stubRepoScopeWiki(t)
	tempHome(t)
	missing := filepath.Join(t.TempDir(), "gone")

	out, err := hooks.SessionStart(missing, "claude", time.Now().UTC())
	if err != nil {
		t.Fatalf("SessionStart returned an error on resolver failure: %v", err)
	}
	ctx := additionalContext(t, out)
	if !strings.HasPrefix(ctx, "## Orientation\n") || strings.Contains(ctx, "Workflow preferences") {
		t.Errorf("want orientation only, got:\n%s", ctx)
	}

	if text := hooks.SessionStartText(missing, "claude", time.Now().UTC()); text != ctx {
		t.Errorf("text form differs from additionalContext:\n%s\nvs\n%s", text, ctx)
	}
}

// Pending reminder files stay on disk untouched and no part of the payload
// mentions them.
func TestSessionStart_PendingRemindersProduceNoNotice(t *testing.T) {
	stubNoWherePosition(t)
	tempHome(t)
	root := gitRepo(t)
	addReminderWithDate(t, root, "fresh pending note", 0)
	addReminderWithDate(t, root, "stale pending note", 30)
	dir := legacyRemindersDir(root)
	before := snapshotDir(t, dir)

	out, err := hooks.SessionStart(root, "claude", time.Now().UTC())
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, out)
	}
	if _, has := payload["systemMessage"]; has {
		t.Errorf("payload carries systemMessage: %v", payload["systemMessage"])
	}
	if strings.Contains(strings.ToLower(out), "remind") || strings.Contains(out, "pending note") {
		t.Errorf("payload mentions reminders:\n%s", out)
	}

	if text := hooks.SessionStartText(root, "claude", time.Now().UTC()); strings.Contains(strings.ToLower(text), "remind") {
		t.Errorf("text form mentions reminders:\n%s", text)
	}

	if after := snapshotDir(t, dir); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Errorf("reminder files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// Tests that skip tempHome run the real profile refresh, so a developer's or
// CI's login must never reach workflow detection.
func TestWorkflowEnvVarsAreClearedForTests(t *testing.T) {
	for _, name := range profile.WorkflowEnvVars() {
		if _, ok := os.LookupEnv(name); ok {
			t.Errorf("%s leaked into the test process", name)
		}
	}
}

func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	files := map[string]string{}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(raw)
	}
	return files
}

func TestInstallInDirRefusesNonObjectHooks(t *testing.T) {
	dir := t.TempDir()
	path := hooks.SettingsPathInDir(dir)
	body := `{"hooks":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hooks.InstallInDir(dir)
	if err == nil || !strings.Contains(err.Error(), `"hooks" must be an object; found array`) {
		t.Fatalf("install err = %v, want the shape message", err)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Errorf("bytes changed to %s", got)
	}
	obs, err := hooks.ObserveSettingsOwnedInDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Conflict != managedfile.ConflictMalformedSettings {
		t.Errorf("observation = %+v, want a malformed-settings conflict", obs)
	}
}
