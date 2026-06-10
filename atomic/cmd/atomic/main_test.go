package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/docs"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
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

	installed, drifted, err := hooks.IsInstalled(scope)
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed || drifted {
		t.Errorf("IsInstalled = (installed=%v, drifted=%v), want (true, false)", installed, drifted)
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

	installed, _, _ := hooks.IsInstalled(scope)
	if installed {
		t.Error("expected hook not registered when noHooks=true")
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

	installed, _, _ := hooks.IsInstalled(scope)
	if installed {
		t.Error("expected hook not registered under dry-run")
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

// TestRunDocsScanDispatch verifies that docsAction("scan") writes the cache
// file to the repo root. Encodes the WHY: CLI wiring must reach the correct
// package function through the dispatch switch; a misconfigured import path
// or switch fall-through would silently produce no output.
func TestRunDocsScanDispatch(t *testing.T) {
	root := t.TempDir()
	// Create a docs/ dir so Scan has something to walk.
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "index.md"), []byte("# Index\n\n## Intro\n"), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}

	// Exercise the dispatch switch, not docs.Scan directly.
	code := docsAction([]string{"scan"}, root)
	if code != 0 {
		t.Fatalf("docsAction(scan) returned exit code %d, want 0", code)
	}

	cachePath := filepath.Join(root, ".claude", "project", "doc-surfaces.md")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not written by docsAction(scan): %v", err)
	}
	if !strings.Contains(string(data), "docs/index.md") {
		t.Errorf("cache missing 'docs/index.md'; got:\n%s", string(data))
	}
}

// TestRunDocsStaleDispatch verifies that docsAction("stale") returns the
// correct exit codes. Encodes the WHY: exit codes are the contract for CI
// consumers; the mapping nil→0, ErrStale→1, other error→2 must be exercised
// through the dispatch switch, not by calling docs.Stale directly.
func TestRunDocsStaleDispatch(t *testing.T) {
	root := t.TempDir()

	// No cache yet → non-ErrStale error (cache missing) → exit code 2.
	code := docsAction([]string{"stale"}, root)
	if code != 2 {
		t.Fatalf("docsAction(stale) with no cache: got exit code %d, want 2", code)
	}

	// Create a docs dir + file, scan to produce a fresh cache.
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# Guide\n"), 0o644); err != nil {
		t.Fatalf("write guide.md: %v", err)
	}
	if err := docs.Scan(root); err != nil {
		t.Fatalf("docs.Scan: %v", err)
	}

	// After a fresh scan the cache is current → exit code 0.
	code = docsAction([]string{"stale"}, root)
	if code != 0 {
		t.Errorf("docsAction(stale) after fresh scan: got exit code %d, want 0", code)
	}
}

// TestRunDocsNoSubcommandUsage verifies that docsAction with no subcommand
// returns exit code 1. Encodes the WHY: every other dispatch function in
// main.go returns a non-zero code when called with no verb; docs must follow
// the same contract. A zero return here would silently succeed on `atomic docs`.
func TestRunDocsNoSubcommandUsage(t *testing.T) {
	root := t.TempDir()

	code := docsAction([]string{}, root)
	if code != 1 {
		t.Errorf("docsAction with no args: got exit code %d, want 1", code)
	}
}

// TestRunDocsUnknownVerbDispatch verifies that docsAction with an unknown verb
// returns exit code 1. Encodes the WHY: unknown verbs must not silently
// succeed or fall through to a no-op.
func TestRunDocsUnknownVerbDispatch(t *testing.T) {
	root := t.TempDir()

	code := docsAction([]string{"bogus"}, root)
	if code != 1 {
		t.Errorf("docsAction(bogus): got exit code %d, want 1", code)
	}
}

// TestProfileAction_NoArgsUsageError verifies that profileAction with no args
// returns exit code 2 (usage error). WHY: callers rely on exit 2 to distinguish
// usage errors from runtime errors.
func TestProfileAction_NoArgsUsageError(t *testing.T) {
	claudeHome := t.TempDir()
	code := profileAction([]string{}, claudeHome, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(no args): got exit code %d, want 2", code)
	}
}

// TestProfileAction_UnknownVerbUsageError verifies that an unknown sub-verb
// returns exit code 2 and does not silently succeed.
func TestProfileAction_UnknownVerbUsageError(t *testing.T) {
	claudeHome := t.TempDir()
	code := profileAction([]string{"bogus"}, claudeHome, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(bogus): got exit code %d, want 2", code)
	}
}

// TestProfileAction_RefreshWritesFile verifies that "refresh" (no flags) creates
// profile.md and stamps the lastcheck attribute with the injected date.
// WHY: proves the main.go dispatch actually reaches Refresh; the profile-package
// unit tests cover the core logic, but this test verifies the wiring.
func TestProfileAction_RefreshWritesFile(t *testing.T) {
	claudeHome := t.TempDir()
	code := profileAction([]string{"refresh"}, claudeHome, "2026-05-28")
	if code != 0 {
		t.Fatalf("profileAction(refresh): got exit code %d, want 0", code)
	}

	profilePath := filepath.Join(claudeHome, ".atomic", "profile.md")
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("profile.md not written: %v", err)
	}
	if !strings.Contains(string(content), "<deterministic lastcheck=2026-05-28>") {
		t.Errorf("profile.md missing lastcheck stamp; got:\n%s", string(content))
	}
}

// TestProfileAction_IfStaleBadDuration verifies that --if-stale with an invalid
// duration returns exit code 1 (runtime error, not usage error). WHY: the spec
// requires an explicit parse error with non-zero exit; exit 2 is for usage errors.
func TestProfileAction_IfStaleBadDuration(t *testing.T) {
	claudeHome := t.TempDir()
	code := profileAction([]string{"refresh", "--if-stale", "7h"}, claudeHome, "2026-05-28")
	if code != 1 {
		t.Errorf("profileAction(refresh --if-stale 7h): got exit code %d, want 1", code)
	}
}

// TestProfileAction_IfStaleNoOpWhenFresh verifies that --if-stale with a fresh
// lastcheck does not modify the file. WHY: the --if-stale gate exists precisely
// to avoid spurious re-runs during session start.
func TestProfileAction_IfStaleNoOpWhenFresh(t *testing.T) {
	claudeHome := t.TempDir()
	atomicDir := filepath.Join(claudeHome, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# User profile\n\n## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	statBefore, _ := os.Stat(profilePath)

	code := profileAction([]string{"refresh", "--if-stale", "7d"}, claudeHome, "2026-05-28")
	if code != 0 {
		t.Fatalf("profileAction(refresh --if-stale 7d) fresh: got exit code %d, want 0", code)
	}

	statAfter, _ := os.Stat(profilePath)
	if !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Error("profileAction: file mtime changed even though lastcheck was fresh")
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

// --- post-update artifact auto-refresh ---

// managedHome builds a temp $HOME that looks like a completed atomic install:
// ~/.claude/CLAUDE.md present and the session-start hook registered.
func managedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("# CLAUDE.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hooks.Install(home, home); err != nil {
		t.Fatalf("hooks.Install: %v", err)
	}
	return home
}

// TestDetectManagedInstall encodes the auto-refresh gate: only a home that
// already carries both the claude config (CLAUDE.md) and the session-start
// hook is treated as a managed install that `atomic update` may refresh
// without asking. Anything less means the user never completed an install,
// and touching ~/.claude would be presumptuous.
func TestDetectManagedInstall(t *testing.T) {
	t.Run("full install detected", func(t *testing.T) {
		if !detectManagedInstall(managedHome(t)) {
			t.Error("detectManagedInstall = false for CLAUDE.md + hooks, want true")
		}
	})

	t.Run("hooks without CLAUDE.md", func(t *testing.T) {
		home := t.TempDir()
		if err := hooks.Install(home, home); err != nil {
			t.Fatalf("hooks.Install: %v", err)
		}
		if detectManagedInstall(home) {
			t.Error("detectManagedInstall = true without CLAUDE.md, want false")
		}
	})

	t.Run("CLAUDE.md without hooks", func(t *testing.T) {
		home := t.TempDir()
		claudeDir := filepath.Join(home, ".claude")
		if err := os.MkdirAll(claudeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if detectManagedInstall(home) {
			t.Error("detectManagedInstall = true without hooks, want false")
		}
	})

	t.Run("empty home", func(t *testing.T) {
		if detectManagedInstall(t.TempDir()) {
			t.Error("detectManagedInstall = true for empty home, want false")
		}
	})
}

// TestMaybeRefreshArtifacts_RunsClaudeUpdate: a managed install gets its
// ~/.claude artifacts refreshed by re-execing the NEW binary. Re-exec is
// load-bearing: the running process still embeds the old bundle after the
// swap, so an in-process claudeinstall.Update would install stale artifacts.
func TestMaybeRefreshArtifacts_RunsClaudeUpdate(t *testing.T) {
	home := managedHome(t)

	var gotName string
	var gotArgs []string
	runCmd := func(name string, args ...string) error {
		gotName = name
		gotArgs = args
		return nil
	}

	var out, errW strings.Builder
	ran := maybeRefreshArtifacts("/usr/local/bin/atomic", home, runCmd, &out, &errW)
	if !ran {
		t.Fatal("maybeRefreshArtifacts = false for managed install, want true")
	}
	if gotName != "/usr/local/bin/atomic" {
		t.Errorf("exec name = %q, want the new binary path", gotName)
	}
	want := []string{"claude", "update", "--no-update-check"}
	if len(gotArgs) != len(want) {
		t.Fatalf("exec args = %v, want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Fatalf("exec args = %v, want %v", gotArgs, want)
		}
	}
	if errW.Len() != 0 {
		t.Errorf("unexpected stderr output: %q", errW.String())
	}
}

// TestMaybeRefreshArtifacts_SkipsUnmanaged: no managed install → no exec, no
// output. `atomic update` on a hooks-less machine must stay a pure binary swap.
func TestMaybeRefreshArtifacts_SkipsUnmanaged(t *testing.T) {
	called := false
	runCmd := func(name string, args ...string) error {
		called = true
		return nil
	}

	var out, errW strings.Builder
	ran := maybeRefreshArtifacts("/usr/local/bin/atomic", t.TempDir(), runCmd, &out, &errW)
	if ran {
		t.Error("maybeRefreshArtifacts = true for unmanaged home, want false")
	}
	if called {
		t.Error("runCmd called for unmanaged home")
	}
	if out.Len() != 0 || errW.Len() != 0 {
		t.Errorf("expected silence for unmanaged home, got out=%q err=%q", out.String(), errW.String())
	}
}

// TestMaybeRefreshArtifacts_SurfacesFailure: a failed refresh prints a
// warning with the manual remediation and never panics — update success is
// preserved at the caller, doctor still runs and surfaces real breakage.
func TestMaybeRefreshArtifacts_SurfacesFailure(t *testing.T) {
	home := managedHome(t)

	runCmd := func(name string, args ...string) error {
		return os.ErrPermission
	}

	var out, errW strings.Builder
	ran := maybeRefreshArtifacts("/usr/local/bin/atomic", home, runCmd, &out, &errW)
	if !ran {
		t.Fatal("maybeRefreshArtifacts = false, want true (attempt counts as ran)")
	}
	if !strings.Contains(errW.String(), "artifact refresh failed") {
		t.Errorf("stderr missing failure notice; got %q", errW.String())
	}
	if !strings.Contains(errW.String(), "atomic claude update") {
		t.Errorf("stderr missing manual remediation; got %q", errW.String())
	}
}
