package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

// addReminderWithDate writes a reminder file whose created date is backdated by
// the given number of days. It uses reminder.Add then patches the frontmatter.
func addReminderWithDate(t *testing.T, root, body string, daysAgo int) string {
	t.Helper()
	id, err := reminder.Add(root, body)
	if err != nil {
		t.Fatalf("addReminderWithDate: Add: %v", err)
	}
	if daysAgo == 0 {
		return id
	}
	// Patch the created date in the file.
	dir := filepath.Join(root, ".claude", ".scratchpad", "reminders")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("addReminderWithDate: ReadDir: %v", err)
	}
	target := time.Now().UTC().AddDate(0, 0, -daysAgo).Format("2006-01-02")
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		raw, _ := os.ReadFile(p)
		content := string(raw)
		today := time.Now().UTC().Format("2006-01-02")
		// Replace the created date in frontmatter.
		patched := strings.Replace(content, "created: "+today, "created: "+target, 1)
		if patched == content {
			continue
		}
		// Check this file has our id.
		if !strings.Contains(content, "id: "+id) {
			continue
		}
		if err := os.WriteFile(p, []byte(patched), 0o644); err != nil {
			t.Fatalf("addReminderWithDate: WriteFile: %v", err)
		}
		return id
	}
	t.Fatalf("addReminderWithDate: could not find file for id %q", id)
	return ""
}

// --- SessionStart tests ---

func TestSessionStart_EmptyReminders(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output with no reminders, got %q", out)
	}
}

func TestSessionStart_OneFreshReminder_JSONEnvelope(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "fix the auth race in middleware", 0)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty output")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}

	// suppressOutput must be true.
	if suppress, ok := payload["suppressOutput"].(bool); !ok || !suppress {
		t.Errorf("expected suppressOutput=true, got %v", payload["suppressOutput"])
	}

	// hookSpecificOutput must exist.
	hso, ok := payload["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("hookSpecificOutput missing or wrong type: %v", payload["hookSpecificOutput"])
	}
	if hso["hookEventName"] != "SessionStart" {
		t.Errorf("hookEventName = %q, want SessionStart", hso["hookEventName"])
	}
	ctx, _ := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "fix the auth race in middleware") {
		t.Errorf("additionalContext missing reminder text: %q", ctx)
	}
	if !strings.Contains(ctx, "Pending reminders (1)") {
		t.Errorf("additionalContext missing header: %q", ctx)
	}
	if !strings.Contains(ctx, "today") {
		t.Errorf("additionalContext missing 'today': %q", ctx)
	}

	// No systemMessage for fresh reminders.
	if _, has := payload["systemMessage"]; has {
		t.Errorf("unexpected systemMessage for fresh reminder: %v", payload["systemMessage"])
	}
}

func TestSessionStart_TwelveReminders_CappedAtTen(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	for i := range 12 {
		addReminderWithDate(t, root, strings.Repeat("x", i+1)+" reminder body", 0)
	}

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	// Header should say 12.
	if !strings.Contains(ctx, "Pending reminders (12)") {
		t.Errorf("header should show total count 12: %q", ctx)
	}
	// Must contain the overflow line.
	if !strings.Contains(ctx, "(and 2 more)") {
		t.Errorf("expected '(and 2 more)' in context: %q", ctx)
	}
	// Count bullet lines (each starts with "- [").
	lines := strings.Split(ctx, "\n")
	bulletCount := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- [") {
			bulletCount++
		}
	}
	if bulletCount != 10 {
		t.Errorf("expected 10 reminder bullets, got %d", bulletCount)
	}
}

func TestSessionStart_OldReminder_SystemMessage(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "revisit error handling in ingest", 15)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	sm, ok := payload["systemMessage"].(string)
	if !ok || sm == "" {
		t.Fatalf("expected systemMessage for old reminder, got %v", payload["systemMessage"])
	}
	if !strings.Contains(sm, "1 reminders pending") && !strings.Contains(sm, "1 reminder pending") {
		t.Errorf("systemMessage should mention count: %q", sm)
	}
	if !strings.Contains(sm, "days old") {
		t.Errorf("systemMessage should mention days old: %q", sm)
	}
}

func TestSessionStart_FormatText(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	addReminderWithDate(t, root, "benchmark the new query plan", 2)

	out, err := hooks.SessionStartText(root, now)
	if err != nil {
		t.Fatalf("SessionStartText: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty text output")
	}
	// Must be plain markdown, not JSON.
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("text format should not be JSON: %q", out)
	}
	if !strings.Contains(out, "benchmark the new query plan") {
		t.Errorf("text missing reminder: %q", out)
	}
}

func TestSessionStart_FormatText_EmptyReminders(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	out, err := hooks.SessionStartText(root, now)
	if err != nil {
		t.Fatalf("SessionStartText: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty text output with no reminders, got %q", out)
	}
}

func TestSessionStart_BodyTruncated(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	longBody := strings.Repeat("a", 100)
	addReminderWithDate(t, root, longBody, 0)

	out, err := hooks.SessionStart(root, now)
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	var payload map[string]any
	json.Unmarshal([]byte(out), &payload)
	hso := payload["hookSpecificOutput"].(map[string]any)
	ctx := hso["additionalContext"].(string)

	// The 100-char body should be truncated.
	if !strings.Contains(ctx, "…") {
		t.Errorf("expected ellipsis in truncated body: %q", ctx)
	}
	if strings.Contains(ctx, longBody) {
		t.Errorf("expected body to be truncated, found full text: %q", ctx)
	}
}

// TestSessionStart_AgoBuckets tests the "N ago" relative time formatting.
func TestSessionStart_AgoBuckets(t *testing.T) {
	cases := []struct {
		days int
		want string
	}{
		{0, "today"},
		{1, "yesterday"},
		{2, "2 days ago"},
		{6, "6 days ago"},
		{7, "1 week ago"},
		{13, "1 week ago"},
		{14, "2 weeks ago"},
		{29, "4 weeks ago"},
		{30, "1 month ago"},
		{89, "2 months ago"},
		{100, "3 months ago"},
	}
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.want, " ", "_"), func(t *testing.T) {
			root := t.TempDir()
			now := time.Now().UTC()
			addReminderWithDate(t, root, "test reminder body", c.days)
			out, err := hooks.SessionStart(root, now)
			if err != nil {
				t.Fatalf("SessionStart: %v", err)
			}
			if out == "" {
				t.Fatal("expected non-empty output")
			}
			var payload map[string]any
			json.Unmarshal([]byte(out), &payload)
			hso := payload["hookSpecificOutput"].(map[string]any)
			ctx := hso["additionalContext"].(string)
			if !strings.Contains(ctx, c.want) {
				t.Errorf("days=%d: expected %q in context, got: %q", c.days, c.want, ctx)
			}
		})
	}
}

// --- Install tests ---

func TestInstall_EmptyDir_CreatesScriptAndSettings(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}

	scriptPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("script not found: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("script not executable: %v", info.Mode())
	}
	raw, _ := os.ReadFile(scriptPath)
	if !strings.Contains(string(raw), "atomic hooks session-start") {
		t.Errorf("script content wrong: %q", string(raw))
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not found: %v", err)
	}
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

	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
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

func TestInstall_Idempotent_SameScript(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	raw, _ := os.ReadFile(settingsPath)
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	hooks_, _ := settings["hooks"].(map[string]any)
	ss, _ := hooks_["SessionStart"].([]any)
	// Should only have one entry, not two.
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

	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
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

	err := hooks.Install(repoRoot, scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}

	// File must be untouched.
	raw, _ := os.ReadFile(settingsPath)
	if string(raw) != "{ not valid json " {
		t.Errorf("malformed settings.json was modified: %q", string(raw))
	}
}

func TestInstall_ScopeProject_WritesUnderClaudeDir(t *testing.T) {
	// scopeRoot here acts as the project root — same as repoRoot for project scope.
	projectRoot := t.TempDir()
	if err := hooks.Install(projectRoot, projectRoot); err != nil {
		t.Fatalf("Install: %v", err)
	}
	scriptPath := filepath.Join(projectRoot, ".claude", "hooks", "session-start-reminders.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("script not found under project root: %v", err)
	}
}

// TestInstall_JWCCSettingsPreservesCommentsAndTrailingCommas verifies that when
// settings.json contains JWCC extensions (// comments and trailing commas), a
// full install+uninstall cycle does not corrupt them.
func TestInstall_JWCCSettingsPreservesCommentsAndTrailingCommas(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	// Write a settings.json with JWCC features.
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

	// Install should succeed and add the hook registration.
	if err := hooks.Install(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Install on JWCC settings: %v", err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after install: %v", err)
	}

	// The comment must survive.
	if !strings.Contains(string(raw), "// user preference") {
		t.Errorf("install stripped JWCC comment from settings.json:\n%s", raw)
	}
	// The trailing comma after the last original key must survive (JWCC feature).
	// The input has `"claude-opus-4-6",` with a trailing comma — that comma must
	// still be present after the install merge.
	if !strings.Contains(string(raw), `"claude-opus-4-6",`) {
		t.Errorf("install stripped trailing comma from JWCC settings.json:\n%s", raw)
	}

	// The hook must be registered.
	if !strings.Contains(string(raw), "SessionStart") {
		t.Errorf("install did not add SessionStart to JWCC settings:\n%s", raw)
	}

	// Uninstall should also preserve comments.
	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall on JWCC settings: %v", err)
	}

	raw2, _ := os.ReadFile(settingsPath)
	if !strings.Contains(string(raw2), "// user preference") {
		t.Errorf("uninstall stripped JWCC comment from settings.json:\n%s", raw2)
	}
	// hooks key should be gone after uninstall.
	if strings.Contains(string(raw2), "SessionStart") {
		t.Errorf("SessionStart should be removed after uninstall:\n%s", raw2)
	}
}

// --- Uninstall tests ---

func TestUninstall_RemovesScriptAndRegistration(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	hooks.Install(repoRoot, scopeRoot)

	// Add a sibling script to verify siblings are preserved.
	siblingPath := filepath.Join(scopeRoot, ".claude", "hooks", "other.sh")
	os.WriteFile(siblingPath, []byte("#!/bin/bash\necho other\n"), 0o755)

	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	scriptPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	if _, err := os.Stat(scriptPath); err == nil {
		t.Error("script still exists after uninstall")
	}

	// Sibling must still exist.
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
		// Only fail if it's empty.
		if m, ok := settings["hooks"].(map[string]any); ok && len(m) == 0 {
			t.Error("empty hooks object should be dropped from settings.json")
		}
	}
}

func TestUninstall_NoScript_NoError(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()
	// Don't install first — just try to uninstall.
	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
		t.Fatalf("Uninstall without prior install: %v", err)
	}
}

func TestUninstall_PreservesOtherRegistrations(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	// Pre-populate with another SessionStart hook AND our hook.
	scriptPath := filepath.Join(scopeRoot, ".claude", "hooks", "session-start-reminders.sh")
	initial := `{"hooks": {"SessionStart": [{"matcher": ".*", "hooks": [{"type": "command", "command": "/other/hook.sh"}]}, {"matcher": ".*", "hooks": [{"type": "command", "command": "` + scriptPath + `"}]}]}}`
	os.WriteFile(settingsPath, []byte(initial), 0o644)
	// Write the script file so uninstall can remove it.
	os.MkdirAll(filepath.Dir(scriptPath), 0o755)
	os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexec atomic hooks session-start\n"), 0o755)

	if err := hooks.Uninstall(repoRoot, scopeRoot); err != nil {
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
	// The remaining entry should be the other hook.
	entry, _ := ss[0].(map[string]any)
	innerHooks, _ := entry["hooks"].([]any)
	innerHook, _ := innerHooks[0].(map[string]any)
	if innerHook["command"] != "/other/hook.sh" {
		t.Errorf("wrong remaining hook: %v", innerHook["command"])
	}
}

func TestUninstall_MalformedSettings_Refuses(t *testing.T) {
	scopeRoot := t.TempDir()
	repoRoot := t.TempDir()

	settingsPath := filepath.Join(scopeRoot, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	os.WriteFile(settingsPath, []byte("{ broken"), 0o644)

	err := hooks.Uninstall(repoRoot, scopeRoot)
	if err == nil {
		t.Fatal("expected error for malformed settings.json, got nil")
	}
	raw, _ := os.ReadFile(settingsPath)
	if string(raw) != "{ broken" {
		t.Errorf("malformed settings.json was modified")
	}
}
