package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

// sha256HexString returns the hex-encoded SHA256 of data.
func sha256HexString(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestShouldRunPostUpdateDoctor tests precedence:
// flag (--no-doctor) > config (update.run_doctor=false) > default true.
func TestShouldRunPostUpdateDoctor(t *testing.T) {
	cases := []struct {
		name      string
		noDoctor  bool
		runDoctor bool
		want      bool
	}{
		{"flag suppresses, config true", true, true, false},
		{"flag suppresses, config false", true, false, false},
		{"no flag, config true", false, true, true},
		{"no flag, config false", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunPostUpdateDoctor(tc.noDoctor, tc.runDoctor)
			if got != tc.want {
				t.Errorf("shouldRunPostUpdateDoctor(noDoctor=%v, runDoctor=%v) = %v, want %v",
					tc.noDoctor, tc.runDoctor, got, tc.want)
			}
		})
	}
}

func TestScanNoUpdateCheck(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		wantFound bool
		wantArgs  []string
	}{
		{
			name:      "flag before subcommand",
			argv:      []string{"atomic", "--no-update-check", "signals", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag after subcommand",
			argv:      []string{"atomic", "signals", "scan", "--no-update-check"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag equals true",
			argv:      []string{"atomic", "--no-update-check=true", "signals", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag equals false strips token but returns false",
			argv:      []string{"atomic", "--no-update-check=false", "signals", "scan"},
			wantFound: false,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag absent",
			argv:      []string{"atomic", "signals", "scan"},
			wantFound: false,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
		{
			name:      "flag between subcommand and sub-verb",
			argv:      []string{"atomic", "signals", "--no-update-check", "scan"},
			wantFound: true,
			wantArgs:  []string{"atomic", "signals", "scan"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, cleaned := scanNoUpdateCheck(tc.argv)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if len(cleaned) != len(tc.wantArgs) {
				t.Errorf("cleaned = %v, want %v", cleaned, tc.wantArgs)
				return
			}
			for i, a := range cleaned {
				if a != tc.wantArgs[i] {
					t.Errorf("cleaned[%d] = %q, want %q", i, a, tc.wantArgs[i])
				}
			}
		})
	}
}

// hookScriptName mirrors hooks.scriptName for assertions.
// If the constant moves, this test fails loudly — that's intended.
const hookScriptName = "session-start-reminders.sh"

// TestRunClaudeInstallWiresHooks proves that `atomic claude install` lays the
// bundle AND registers the session-start hook in one shot. Encodes the WHY:
// the previous flow required users to chain `atomic hooks install` separately,
// which was undocumented in the curl|bash output and a real onboarding gap.
func TestRunClaudeInstallWiresHooks(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, "install", false, false)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if len(result.Plan) == 0 {
		t.Fatal("expected non-empty install plan")
	}
	if !result.HooksInstalled {
		t.Errorf("expected HooksInstalled=true, got false; hookError=%v", result.HooksError)
	}

	scriptPath := filepath.Join(scope, ".claude", "hooks", hookScriptName)
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("expected hook script at %s: %v", scriptPath, err)
	}

	settingsPath := filepath.Join(scope, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("expected settings.json at %s: %v", settingsPath, err)
	}
}

// TestRunClaudeInstallNoHooksFlag verifies the opt-out path. Users with their
// own hook config need a way to install the bundle without atomic touching
// settings.json.
func TestRunClaudeInstallNoHooksFlag(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, "install", false, true)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if result.HooksInstalled {
		t.Error("expected HooksInstalled=false when noHooks=true")
	}

	scriptPath := filepath.Join(scope, ".claude", "hooks", hookScriptName)
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Errorf("expected no hook script at %s, got err=%v", scriptPath, err)
	}
}

// TestRunClaudeInstallDryRunSkipsHooks dry-run must be observation-only;
// touching settings.json under dry-run would defeat its purpose.
func TestRunClaudeInstallDryRunSkipsHooks(t *testing.T) {
	scope := t.TempDir()
	target := filepath.Join(scope, ".claude")

	result, err := runClaudeInstall(target, "install", true, false)
	if err != nil {
		t.Fatalf("runClaudeInstall: %v", err)
	}
	if result.HooksInstalled {
		t.Error("expected HooksInstalled=false under dry-run")
	}

	scriptPath := filepath.Join(scope, ".claude", "hooks", hookScriptName)
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Errorf("expected no hook script under dry-run, got err=%v", err)
	}
}

// remindersPath returns the path to the reminders directory used by the CLI
// dispatch. Mirrors the constant in the reminder package so this test breaks
// loudly if the path ever changes.
func remindersPath(root string) string {
	return filepath.Join(root, ".claude", ".scratchpad", "reminders")
}

// TestReminderSetDueCLIWiring exercises the set-due dispatch path end-to-end:
// add a reminder via the same package function runReminder calls, then invoke
// SetDue (also called directly by runReminder), and assert the on-disk file
// has only the due: field changed while id, created, transport, and body are
// untouched.
func TestReminderSetDueCLIWiring(t *testing.T) {
	root := t.TempDir()

	const body = "deploy the staging release"
	const transport = "cron"
	const origDue = "2026-05-20T09:00:00Z"
	const newDue = "2026-06-01T12:00:00Z"

	// Add a reminder with an initial due and transport — mirrors what
	// `atomic reminder add --due <iso> --transport <kind> <text>` dispatches to.
	id, err := reminder.Add(root, body, reminder.WithDue(origDue), reminder.WithTransport(transport))
	if err != nil {
		t.Fatalf("reminder.Add: %v", err)
	}

	// Invoke SetDue — exactly what runReminder dispatches for "set-due".
	if err := reminder.SetDue(root, id, newDue); err != nil {
		t.Fatalf("reminder.SetDue: %v", err)
	}

	// Read the on-disk file and assert field-by-field.
	entries, err := os.ReadDir(remindersPath(root))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 reminder file, got %d", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(remindersPath(root), entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(raw)

	if !strings.Contains(content, "due: "+newDue) {
		t.Errorf("expected due field %q in file; got:\n%s", newDue, content)
	}
	if strings.Contains(content, "due: "+origDue) {
		t.Errorf("old due %q should be gone; got:\n%s", origDue, content)
	}
	if !strings.Contains(content, "id: "+id) {
		t.Errorf("id field %q missing after SetDue; got:\n%s", id, content)
	}
	if !strings.Contains(content, "transport: "+transport) {
		t.Errorf("transport field %q missing after SetDue; got:\n%s", transport, content)
	}
	if !strings.Contains(content, body) {
		t.Errorf("body %q missing after SetDue; got:\n%s", body, content)
	}
}

// TestReminderSetDueErrorPaths exercises the error branches that runReminder
// propagates to stderr+exit(1) for set-due.
func TestReminderSetDueErrorPaths(t *testing.T) {
	root := t.TempDir()

	// Unknown id — no reminder file exists.
	err := reminder.SetDue(root, "r-nonexistent", "2026-06-01T12:00:00Z")
	if err == nil {
		t.Fatal("expected error for unknown id, got nil")
	}
	if !strings.Contains(err.Error(), "no reminder with id") {
		t.Errorf("expected 'no reminder with id' in error; got: %v", err)
	}

	// Valid id but malformed ISO timestamp.
	id, err := reminder.Add(root, "check the dashboard")
	if err != nil {
		t.Fatalf("reminder.Add: %v", err)
	}
	err = reminder.SetDue(root, id, "not-a-timestamp")
	if err == nil {
		t.Fatal("expected error for malformed ISO, got nil")
	}
	if !strings.Contains(err.Error(), "must be RFC3339") {
		t.Errorf("expected 'must be RFC3339' in error; got: %v", err)
	}

	// Missing args: simulated by calling SetDue with empty id.
	err = reminder.SetDue(root, "", "2026-06-01T12:00:00Z")
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}

// TestRunClaudeUninstall_MissingManifest verifies that runClaudeUninstall returns
// an error (and the CLI exits 1) when no pre-install snapshot exists. This is the
// primary guard that prevents uninstall from silently doing nothing.
func TestRunClaudeUninstall_MissingManifest(t *testing.T) {
	targetDir := t.TempDir()

	// Use /dev/null as the output so TTY detection doesn't try to stat a nil file.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer devNull.Close()

	_, err = runClaudeUninstall(targetDir, devNull)
	if err == nil {
		t.Fatal("expected error when no pre-install manifest, got nil")
	}
	if !strings.Contains(err.Error(), "no pre-install snapshot") {
		t.Errorf("error %q does not mention 'no pre-install snapshot'", err.Error())
	}
}

// TestRunClaudeUninstall_NeedsMerge verifies the end-to-end NeedsMerge path:
// a file that existed pre-install has been modified on disk post-install, so the
// generated prompt must flag it as "NEEDS MERGE". Encodes the WHY: three-way
// detection must surface user modifications so uninstall doesn't silently clobber
// post-install changes to settings.json or CLAUDE.md.
func TestRunClaudeUninstall_NeedsMerge(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	if err := os.MkdirAll(preInstallDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-install: %v", err)
	}

	// settings.json is not in the embedded bundle, so embeddedSHAs["settings.json"]=="".
	// Pre-install SHA records the original content.
	preInstallContent := []byte(`{"theme":"light"}`)
	preInstallSHA := sha256HexString(preInstallContent)

	// Write the pre-install snapshot copy.
	if err := os.WriteFile(filepath.Join(preInstallDir, "settings.json"), preInstallContent, 0o644); err != nil {
		t.Fatalf("write pre-install settings.json: %v", err)
	}

	// On-disk version differs from both pre-install and embedded (none) — user modified it.
	onDiskContent := []byte(`{"theme":"dark","fontSize":14}`)
	if err := os.WriteFile(filepath.Join(targetDir, "settings.json"), onDiskContent, 0o644); err != nil {
		t.Fatalf("write on-disk settings.json: %v", err)
	}

	manifestJSON := `{
		"created": "2026-05-24T00:00:00Z",
		"atomic_version": "1.5.1",
		"files": [
			{"path": "settings.json", "sha256": "` + preInstallSHA + `", "existed": true}
		]
	}`
	if err := os.WriteFile(filepath.Join(preInstallDir, "manifest.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer devNull.Close()

	prompt, err := runClaudeUninstall(targetDir, devNull)
	if err != nil {
		t.Fatalf("runClaudeUninstall: %v", err)
	}
	if !strings.Contains(prompt, "NEEDS MERGE") {
		t.Errorf("expected 'NEEDS MERGE' in prompt for user-modified file; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "settings.json") {
		t.Errorf("expected 'settings.json' in prompt; got:\n%s", prompt)
	}
}

// TestRunClaudeUninstall_ProducesPrompt verifies that runClaudeUninstall returns
// a non-empty prompt with the required structural sections when a valid manifest
// exists.
func TestRunClaudeUninstall_ProducesPrompt(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	// Write a minimal manifest with one file to delete and one to restore.
	if err := os.MkdirAll(preInstallDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-install: %v", err)
	}
	manifestJSON := `{
		"created": "2026-05-24T00:00:00Z",
		"atomic_version": "1.5.1",
		"files": [
			{"path": "CLAUDE.md", "sha256": "abc123", "existed": true},
			{"path": "agents/atomic-builder.md", "sha256": "", "existed": false}
		]
	}`
	if err := os.WriteFile(filepath.Join(preInstallDir, "manifest.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer devNull.Close()

	prompt, err := runClaudeUninstall(targetDir, devNull)
	if err != nil {
		t.Fatalf("runClaudeUninstall: %v", err)
	}
	if prompt == "" {
		t.Fatal("expected non-empty prompt, got empty string")
	}
	if !strings.Contains(prompt, "## Atomic Claude Uninstall") {
		t.Errorf("prompt missing '## Atomic Claude Uninstall'")
	}
	if !strings.Contains(prompt, "atomic-builder.md") {
		t.Errorf("prompt missing 'atomic-builder.md'")
	}
	if !strings.Contains(prompt, "CLAUDE.md") {
		t.Errorf("prompt missing 'CLAUDE.md'")
	}
}
