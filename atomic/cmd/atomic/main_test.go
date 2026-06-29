package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	"github.com/damusix/atomic-claude/atomic/internal/docs"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/migrate"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

// cp2WantMeta is the ground truth for every CP2-ported subcommand: the exact
// Short and args_hint values from cliusage.go. CP4's deriveCommands reads
// cmd.Short for Description and Annotations["args_hint"] for Args; a byte-for-byte
// mismatch here means the derived Commands() slice diverges from cliusage.go.
var cp2WantMeta = []struct {
	path     []string
	argsHint string
	short    string
}{
	{[]string{"signals", "scan"}, "", "Walk repo and write docs/wiki/scan.md"},
	{[]string{"signals", "show"}, "", "Print docs/wiki/scan.md to stdout"},
	{[]string{"signals", "stale"}, "", "Exit 0 fresh, 1 stale, 2 error"},
	{[]string{"signals", "diff"}, "", "Print unified diff of signals file"},
	{[]string{"signals", "linkify"}, "", "Linkify path tokens in docs/wiki/index.md and docs/wiki/*.md"},
	{[]string{"reminder", "add"}, "<text>", "Create a reminder file; prints assigned id"},
	{[]string{"reminder", "list"}, "", "List all reminders"},
	{[]string{"reminder", "show"}, "<id>", "Print body of a reminder"},
	{[]string{"reminder", "rm"}, "<id>", "Delete a reminder"},
	{[]string{"hooks", "session-start"}, "", "Print session-start hook payload"},
	{[]string{"hooks", "install"}, "", "Install session-start hook"},
	{[]string{"hooks", "uninstall"}, "", "Remove session-start hook"},
	{[]string{"claude", "install"}, "", "Install artifact bundle"},
	{[]string{"claude", "update"}, "", "Update artifact bundle"},
	{[]string{"claude", "list"}, "", "List bundled artifacts"},
	{[]string{"claude", "diff"}, "", "Diff bundle vs on-disk"},
	{[]string{"claude", "uninstall"}, "", "Generate uninstall prompt"},
	{[]string{"docker", "init"}, "", "Scaffold Docker eval environment"},
	{[]string{"docs", "scan"}, "", "Scan docs and write doc-surfaces.md"},
	{[]string{"docs", "stale"}, "", "Exit 0 fresh, 1 stale, 2 error"},
	{[]string{"profile", "refresh"}, "", "Refresh ## Environment in profile.md"},
	{[]string{"prompt", "git-cleanup"}, "", "Emit the git-cleanup cold-op brief"},
	{[]string{"prompt", "claude-merge"}, "", "Emit the CLAUDE.md merge cold-op brief"},
}

// TestCP2CobraMetadata walks the Cobra command tree for every CP2-ported
// subcommand and asserts the exact Short and Annotations["args_hint"] values
// match cliusage.go byte-for-byte. WHY: CP4's deriveCommands reads these fields
// to reproduce the Commands() slice; a silent mismatch would cause the A1 linter
// to false-positive or false-negative against artifact citations.
func TestCP2CobraMetadata(t *testing.T) {
	var repo string
	root := buildRootCmd(&repo)

	for _, w := range cp2WantMeta {
		label := fmt.Sprintf("%v", w.path)
		found, _, _ := root.Find(w.path)
		if found == nil || found == root {
			t.Errorf("%s: command not found in Cobra tree", label)
			continue
		}
		if found.Short != w.short {
			t.Errorf("%s Short:\n  got:  %q\n  want: %q", label, found.Short, w.short)
		}
		if got := found.Annotations["args_hint"]; got != w.argsHint {
			t.Errorf("%s args_hint:\n  got:  %q\n  want: %q", label, got, w.argsHint)
		}
	}
}

// cp3WantMeta is the ground truth for every CP3-ported subcommand: the exact
// Short and args_hint values from cliusage.go. Byte-for-byte match is required
// so that CP4's deriveCommands reproduces the Commands() slice exactly.
var cp3WantMeta = []struct {
	path     []string
	argsHint string
	short    string
}{
	// code subcommands
	{[]string{"code", "index"}, "", "Index all source files"},
	{[]string{"code", "sync"}, "", "Incrementally re-index changed files"},
	{[]string{"code", "status"}, "", "Show index status"},
	{[]string{"code", "search"}, "<query>", "Search indexed nodes"},
	{[]string{"code", "callers"}, "<symbol>", "Find callers of symbol"},
	{[]string{"code", "callees"}, "<symbol>", "Find callees of symbol"},
	{[]string{"code", "impact"}, "<symbol>", "Find impact radius of symbol"},
	{[]string{"code", "node"}, "<symbol>", "Show node detail"},
	{[]string{"code", "files"}, "[pattern]", "List indexed files"},
	{[]string{"code", "affected"}, "", "Find affected test files"},
	{[]string{"code", "explore"}, "<query>", "Gather context for a query"},
	{[]string{"code", "mcp"}, "", "Run the MCP server over stdio (proxy + daemon; --no-watch disables sync poller)"},
	// config subcommands
	{[]string{"config", "get"}, "<key>", "Print resolved config value"},
	{[]string{"config", "set"}, "<key> <val>", "Set config value; re-renders config.resolved.md"},
	{[]string{"config", "unset"}, "<key>", "Revert key to built-in default"},
	{[]string{"config", "list"}, "", "List all resolved key=value pairs"},
	{[]string{"config", "path"}, "", "Print path to config.toml"},
	{[]string{"config", "agents"}, "", "Set per-agent model tiers interactively"},
	// wiki subcommands
	{[]string{"wiki", "scan"}, "", "Scaffold wiki/, scan repos, register in ~/.claude/CLAUDE.md"},
	{[]string{"wiki", "stale"}, "", "Exit 0 fresh, 1 stale, 2 error (DRIFT/STALE lines on stdout)"},
	{[]string{"wiki", "linkify"}, "", "Linkify path tokens in wiki artifacts in-place"},
	// wiki bucket (3-level)
	{[]string{"wiki", "bucket", "add"}, "<name>", "Register a capture bucket; create index.md stub and manifest dir"},
	{[]string{"wiki", "bucket", "list"}, "", "List registered buckets with baseline count and pending/fresh status"},
	{[]string{"wiki", "bucket", "diff"}, "<name>", "Print new/changed/removed files vs baseline; exit 0 empty, 1 non-empty"},
	{[]string{"wiki", "bucket", "promote"}, "<name>", "Snapshot bucket and rotate baseline→previous, current→baseline"},
	// followups subcommands
	{[]string{"followups", "list"}, "", "List open follow-up entries"},
	{[]string{"followups", "add"}, "", "Create entry"},
	{[]string{"followups", "close"}, "<id>", "Close an entry"},
	{[]string{"followups", "render"}, "", "Regenerate INDEX.md"},
	{[]string{"followups", "path"}, "", "Print followups folder path"},
}

// TestCP3CobraMetadata walks the Cobra command tree for every CP3-ported
// subcommand and asserts the exact Short and Annotations["args_hint"] values
// match cliusage.go byte-for-byte. Covers the 3-level wiki bucket nesting.
func TestCP3CobraMetadata(t *testing.T) {
	var repo string
	root := buildRootCmd(&repo)

	for _, w := range cp3WantMeta {
		label := fmt.Sprintf("%v", w.path)
		found, _, _ := root.Find(w.path)
		if found == nil || found == root {
			t.Errorf("%s: command not found in Cobra tree", label)
			continue
		}
		if found.Short != w.short {
			t.Errorf("%s Short:\n  got:  %q\n  want: %q", label, found.Short, w.short)
		}
		if got := found.Annotations["args_hint"]; got != w.argsHint {
			t.Errorf("%s args_hint:\n  got:  %q\n  want: %q", label, got, w.argsHint)
		}
	}
}

// TestDeriveCommandsGolden is the CP4 gate for the A1 linter. It captures the
// hardcoded cliusage.Commands() slice as the golden fixture (SetRoot is never
// called in tests, so Commands() returns the static table) and asserts that
// cliusage.DeriveCommands(buildRootCmd(...)) reproduces the exact same surface.
//
// A failure here means the Cobra tree's metadata (Short, Annotations["args_hint"],
// or registered Flags) diverges from the golden — fix the Cobra side in main.go,
// not the golden.
//
// WHY set-for-set comparison: cobra's VisitAll visits flags alphabetically; the
// hardcoded golden has flags in non-alphabetical order for some commands. Order
// within the Flags slice is irrelevant for the A1 linter (which builds a map).
func TestDeriveCommandsGolden(t *testing.T) {
	// Golden: hardcoded pre-migration slice (SetRoot not called in tests).
	golden := cliusage.Commands()

	// Derived: walk the live Cobra tree.
	var repo string
	root := buildRootCmd(&repo)
	derived := cliusage.DeriveCommands(root)

	assertCommandSetsEqual(t, derived, golden)
}

// assertCommandSetsEqual verifies that derived and golden describe the same
// command surface: same set of paths, and for each path the same Args,
// Description, and flag set (flag ORDER within a command is ignored).
func assertCommandSetsEqual(t *testing.T, derived, golden []cliusage.Command) {
	t.Helper()

	if len(derived) != len(golden) {
		t.Errorf("command count: derived=%d, golden=%d", len(derived), len(golden))
		derivedKeys := make(map[string]bool, len(derived))
		for _, c := range derived {
			derivedKeys[strings.Join(c.Path, "/")] = true
		}
		goldenKeys := make(map[string]bool, len(golden))
		for _, c := range golden {
			goldenKeys[strings.Join(c.Path, "/")] = true
		}
		for k := range goldenKeys {
			if !derivedKeys[k] {
				t.Errorf("  missing in derived: %s", k)
			}
		}
		for k := range derivedKeys {
			if !goldenKeys[k] {
				t.Errorf("  extra in derived: %s", k)
			}
		}
		return
	}

	// Index golden by path key.
	byPath := make(map[string]cliusage.Command, len(golden))
	for _, c := range golden {
		byPath[strings.Join(c.Path, "/")] = c
	}

	for _, got := range derived {
		key := strings.Join(got.Path, "/")
		want, ok := byPath[key]
		if !ok {
			t.Errorf("derived has path not in golden: %v", got.Path)
			continue
		}
		if got.Args != want.Args {
			t.Errorf("%v: Args: derived=%q, golden=%q", got.Path, got.Args, want.Args)
		}
		if got.Description != want.Description {
			t.Errorf("%v: Description: derived=%q, golden=%q", got.Path, got.Description, want.Description)
		}
		gotF := make(map[string]bool, len(got.Flags))
		for _, f := range got.Flags {
			gotF[f] = true
		}
		wantF := make(map[string]bool, len(want.Flags))
		for _, f := range want.Flags {
			wantF[f] = true
		}
		for f := range wantF {
			if !gotF[f] {
				t.Errorf("%v: flag %q in golden but missing from derived", got.Path, f)
			}
		}
		for f := range gotF {
			if !wantF[f] {
				t.Errorf("%v: flag %q in derived but not in golden", got.Path, f)
			}
		}
	}
}

// TestRootCmdExact17Verbs verifies the Cobra root command has exactly the 17
// expected top-level verbs and no extra auto-generated commands (completion,
// help) leaked into the visible command set.
// WHY: DisableDefaultCmd and SetHelpCommand suppress Cobra's auto-adds;
// this test is the gate that catches any regression where Cobra re-adds them
// or a new verb is accidentally introduced.
func TestRootCmdExact17Verbs(t *testing.T) {
	var repoOverride string
	root := buildRootCmd(&repoOverride)

	want := []string{
		"claude", "code", "config", "docker", "docs", "doctor",
		"followups", "hooks", "migrate", "profile", "prompt", "reminder",
		"serve", "signals", "update", "validate", "wiki",
	}

	// Collect visible (non-hidden) commands only.
	var visible []string
	for _, cmd := range root.Commands() {
		if !cmd.Hidden {
			visible = append(visible, cmd.Name())
		}
	}
	sort.Strings(visible)

	if len(visible) != len(want) {
		t.Errorf("got %d top-level verbs, want %d\ngot:  %v\nwant: %v",
			len(visible), len(want), visible, want)
	}
	for i, name := range visible {
		if i >= len(want) {
			break
		}
		if name != want[i] {
			t.Errorf("verb[%d]: got %q, want %q", i, name, want[i])
		}
	}

	// Confirm no completion or help leaked into visible commands.
	for _, name := range visible {
		if name == "completion" || name == "help" {
			t.Errorf("unexpected command leaked into top-level: %q", name)
		}
	}
}

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

// artifactRefreshArgs builds the re-exec argv for the post-swap refresh.
// The hook clause encodes the one policy in this flow: the refresh must
// never be the thing that first registers hooks or overrides an explicit
// --no-hooks install choice — only an existing registration is renewed.
func TestArtifactRefreshArgs(t *testing.T) {
	got := artifactRefreshArgs(true)
	want := []string{"claude", "update", "--no-update-check"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("hooksInstalled=true: args = %v, want %v", got, want)
	}

	got = artifactRefreshArgs(false)
	want = []string{"claude", "update", "--no-update-check", "--no-hooks"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("hooksInstalled=false: args = %v, want %v", got, want)
	}
}

// --- atomic prompt dispatch ---

// TestPromptAction_KnownNames verifies that promptAction exits 0 and writes
// non-empty text for each registered brief name. Encodes the WHY: the embed
// + dispatch chain must be end-to-end verified; a broken embed path or a
// typo in the name table would silently produce empty output.
func TestPromptAction_KnownNames(t *testing.T) {
	names := []string{"git-cleanup", "claude-merge"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			var errOut strings.Builder
			code := promptAction([]string{name}, &out, &errOut)
			if code != 0 {
				t.Fatalf("promptAction(%q) returned exit code %d, want 0; stderr: %s", name, code, errOut.String())
			}
			if strings.TrimSpace(out.String()) == "" {
				t.Errorf("promptAction(%q) wrote empty stdout", name)
			}
		})
	}
}

// TestPromptAction_UnknownName verifies that promptAction exits 1 and writes
// to stderr for an unregistered brief name. Encodes the WHY: a non-zero exit
// on bad input is the contract consumers (validate artifacts, CI) rely on to
// catch stale citations before they reach production.
func TestPromptAction_UnknownName(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := promptAction([]string{"no-such-brief"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("promptAction(\"no-such-brief\") returned exit code 0, want non-zero")
	}
	if strings.TrimSpace(errOut.String()) == "" {
		t.Errorf("promptAction(\"no-such-brief\") wrote nothing to stderr")
	}
	if out.String() != "" {
		t.Errorf("promptAction(\"no-such-brief\") wrote unexpected stdout: %q", out.String())
	}
}

// TestPromptAction_NoArgs verifies that promptAction exits 1 with a usage
// message when called with no arguments.
func TestPromptAction_NoArgs(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := promptAction([]string{}, &out, &errOut)
	if code == 0 {
		t.Fatalf("promptAction with no args returned exit code 0, want non-zero")
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("no-args error message missing 'Usage:'; stderr: %q", errOut.String())
	}
}

// --- migrate helpers ---

// makeOldSignalsLayout creates a minimal old signals layout in root:
//
//	.claude/project/signals.md     (router with an @-ref line)
//	.claude/project/signals/dom.md (domain file)
//	CLAUDE.md                      (contains @.claude/project/signals.md)
func makeOldSignalsLayout(t *testing.T, root string) {
	t.Helper()
	mkfile := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mkfile(".claude/project/signals.md", "# signals router\n")
	mkfile(".claude/project/signals/dom.md", "# dom\ndom content\n")
	mkfile("CLAUDE.md", "@.claude/project/signals.md\n")
}

// TestMigrateSchemaToSemver covers the schemaToSemver conversion table.
func TestMigrateSchemaToSemver(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, ""},
		{1, "1.0.0"},
		{2, "2.0.0"},
	}
	for _, tc := range cases {
		if got := schemaToSemver(tc.n); got != tc.want {
			t.Errorf("schemaToSemver(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestMigrateSemverToSchema covers the reverse conversion.
func TestMigrateSemverToSchema(t *testing.T) {
	cases := []struct {
		v    string
		want int
	}{
		{"", 0},
		{"0.0.0", 0},
		{"1.0.0", 1},
		{"2.3.4", 2},
	}
	for _, tc := range cases {
		if got := semverToSchema(tc.v); got != tc.want {
			t.Errorf("semverToSchema(%q) = %d, want %d", tc.v, got, tc.want)
		}
	}
}

// TestScopedMigrations returns only migrations matching the given scope.
func TestScopedMigrations(t *testing.T) {
	reg := []migrate.Migration{
		{TargetVersion: "1.0.0", Scope: "install"},
		{TargetVersion: "2.0.0", Scope: "repo"},
		{TargetVersion: "3.0.0", Scope: "install"},
	}
	install := scopedMigrations("install", reg)
	if len(install) != 2 {
		t.Errorf("install scope: got %d, want 2", len(install))
	}
	repo := scopedMigrations("repo", reg)
	if len(repo) != 1 {
		t.Errorf("repo scope: got %d, want 1", len(repo))
	}
	none := scopedMigrations("other", reg)
	if len(none) != 0 {
		t.Errorf("unknown scope: got %d, want 0", len(none))
	}
}

// TestMigrateRepoActionOldLayout is the end-to-end happy path for
// `atomic migrate --repo <path>` on an old-layout temp repo.
// After the call: docs/wiki/index.md exists, has <wiki-schema>1</wiki-schema>,
// @-ref is rewired in CLAUDE.md.
func TestMigrateRepoActionOldLayout(t *testing.T) {
	root := t.TempDir()
	makeOldSignalsLayout(t, root)

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("migrateRepoAction: %v", err)
	}

	// docs/wiki/index.md must exist.
	indexPath := filepath.Join(root, "docs", "wiki", "index.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	content := string(data)

	// <wiki-schema>1</wiki-schema> must be present.
	if !strings.Contains(content, "<wiki-schema>1</wiki-schema>") {
		t.Errorf("index.md missing <wiki-schema>1</wiki-schema>:\n%s", content)
	}

	// Schema stamped by WriteWikiSchema on success.
	if got := migrate.ReadWikiSchema(root); got != 1 {
		t.Errorf("ReadWikiSchema after migration: got %d, want 1", got)
	}

	// @-ref rewired in CLAUDE.md.
	claudeData, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if strings.Contains(string(claudeData), "@.claude/project/signals.md") {
		t.Errorf("CLAUDE.md still has old @-ref:\n%s", claudeData)
	}
	if !strings.Contains(string(claudeData), "@docs/wiki/index.md") {
		t.Errorf("CLAUDE.md missing new @-ref:\n%s", claudeData)
	}
}

// TestMigrateRepoActionIdempotent: calling migrateRepoAction twice on the same
// repo is safe — second call is a no-op (schema already at 1).
func TestMigrateRepoActionIdempotent(t *testing.T) {
	root := t.TempDir()
	makeOldSignalsLayout(t, root)

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("first migrateRepoAction: %v", err)
	}

	// Sentinel to detect re-writes.
	indexPath := filepath.Join(root, "docs", "wiki", "index.md")
	after1, _ := os.ReadFile(indexPath)

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("second migrateRepoAction: %v", err)
	}
	after2, _ := os.ReadFile(indexPath)
	if string(after1) != string(after2) {
		t.Errorf("index.md was modified on idempotent re-run")
	}
}

// TestMigrateRepoActionNoSignals: a repo with no signals layout is a no-op.
func TestMigrateRepoActionNoSignals(t *testing.T) {
	root := t.TempDir()

	if err := migrateRepoAction(root); err != nil {
		t.Fatalf("migrateRepoAction on empty repo: %v", err)
	}

	// docs/wiki/index.md must NOT have been created.
	if _, err := os.Stat(filepath.Join(root, "docs", "wiki", "index.md")); err == nil {
		t.Error("docs/wiki/index.md should not exist for a no-signals repo")
	}
}

// withRealmConfirmStub replaces realmConfirmFn for the duration of f, then
// restores it. Allows tests to control what runMigrateRealm does when prompted.
func withRealmConfirmStub(result bool, err error, f func()) {
	orig := realmConfirmFn
	realmConfirmFn = func(_, _ string, _ bool) (bool, error) { return result, err }
	defer func() { realmConfirmFn = orig }()
	f()
}

// makeRealmWithMember creates a realm directory containing one member sub-dir
// with the given layout setup function applied.
func makeRealmWithMember(t *testing.T, setup func(memberRoot string)) (realmRoot, memberPath string) {
	t.Helper()
	realm := t.TempDir()
	member := filepath.Join(realm, "member-repo")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatalf("mkdir member: %v", err)
	}
	if setup != nil {
		setup(member)
	}
	return realm, member
}

// TestRunMigrateRealmNonInteractiveSkipsAll verifies that when the confirm
// prompt returns ErrNonInteractive, runMigrateRealm skips all members and
// performs no migration — it must NOT auto-migrate in a non-TTY context.
func TestRunMigrateRealmNonInteractiveSkipsAll(t *testing.T) {
	realm, member := makeRealmWithMember(t, func(root string) {
		makeOldSignalsLayout(t, root)
	})

	withRealmConfirmStub(false, prompt.ErrNonInteractive, func() {
		if err := runMigrateRealm(realm); err != nil {
			t.Fatalf("runMigrateRealm: %v", err)
		}
	})

	// Migration must NOT have happened: old layout still present.
	if _, err := os.Stat(filepath.Join(member, ".claude", "project", "signals.md")); err != nil {
		t.Errorf("old signals.md should still exist (migration must have been skipped): %v", err)
	}
	if _, err := os.Stat(filepath.Join(member, "docs", "wiki", "index.md")); err == nil {
		t.Error("docs/wiki/index.md must not exist (migration must have been skipped)")
	}
}

// TestRunMigrateRealmSkipsAlreadyMigratedMember verifies that a member repo
// whose wiki schema is already >= 1 is skipped without prompting.
func TestRunMigrateRealmSkipsAlreadyMigratedMember(t *testing.T) {
	realm, member := makeRealmWithMember(t, func(root string) {
		// Write docs/wiki/index.md with <wiki-schema>1 to simulate a fully-migrated member.
		p := filepath.Join(root, "docs", "wiki", "index.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir docs/wiki: %v", err)
		}
		content := "<wiki-type>repo</wiki-type>\n<wiki-schema>1</wiki-schema>\n# index\n"
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write index.md: %v", err)
		}
	})
	_ = member

	prompted := false
	withRealmConfirmStub(false, nil, func() {
		// Override to detect if a prompt is issued.
		orig := realmConfirmFn
		realmConfirmFn = func(_, _ string, _ bool) (bool, error) {
			prompted = true
			return false, nil
		}
		defer func() { realmConfirmFn = orig }()

		if err := runMigrateRealm(realm); err != nil {
			t.Fatalf("runMigrateRealm: %v", err)
		}
	})

	if prompted {
		t.Error("already-migrated member must be skipped without prompting")
	}
}

// TestRunMigrateRealmAbortedSkipsMemberNotRealm verifies that ErrAborted on
// the confirm prompt skips that single member but does not abort the realm
// loop as a whole (no error returned).
func TestRunMigrateRealmAbortedSkipsMemberNotRealm(t *testing.T) {
	realm, member := makeRealmWithMember(t, func(root string) {
		makeOldSignalsLayout(t, root)
	})

	withRealmConfirmStub(false, prompt.ErrAborted, func() {
		if err := runMigrateRealm(realm); err != nil {
			t.Fatalf("runMigrateRealm returned error on ErrAborted: %v", err)
		}
	})

	// Migration must NOT have happened.
	if _, err := os.Stat(filepath.Join(member, ".claude", "project", "signals.md")); err != nil {
		t.Errorf("old signals.md should still exist (member was skipped): %v", err)
	}
}
