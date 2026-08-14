package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/docs"
	"github.com/damusix/atomic-claude/atomic/internal/doctemplate"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/migrate"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
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
	{[]string{"code", "mcp"}, "", "Run the MCP server over stdio (proxy by default; --daemon --source --db runs the daemon itself; --no-watch disables sync poller)"},
	// config subcommands
	{[]string{"config", "get"}, "<key>", "Print resolved config value"},
	{[]string{"config", "set"}, "<key> <val>", "Set config value"},
	{[]string{"config", "unset"}, "<key>", "Revert key to built-in default"},
	{[]string{"config", "list"}, "", "List all resolved key=value pairs"},
	{[]string{"config", "path"}, "", "Print path to config.toml"},
	{[]string{"config", "agents"}, "", "Set per-agent model tiers interactively"},
	{[]string{"config", "resolve"}, "", "Resolve Pi agent configuration"},
	// wiki subcommands
	{[]string{"wiki", "scan"}, "", "Scaffold wiki/, scan repos, register in ~/.claude/CLAUDE.md"},
	{[]string{"wiki", "stale"}, "", "Exit 0 fresh, 1 stale, 2 error (DRIFT/STALE lines on stdout)"},
	{[]string{"wiki", "linkify"}, "", "Linkify path tokens in wiki artifacts in-place"},
	{[]string{"wiki", "init"}, "", "Write the fixed-content CLAUDE.md scaffold and the scope marker for --scope repo|realm (idempotent)"},
	{[]string{"wiki", "stamp"}, "<file>", "Write reflects_rev/reflects/sources fingerprint frontmatter (summary|concern|knowledge)"},
	// wiki bucket (3-level)
	{[]string{"wiki", "bucket", "add"}, "<name>", "Register a capture bucket; create index.md stub and manifest dir"},
	{[]string{"wiki", "bucket", "list"}, "", "List registered buckets with baseline count and pending/fresh status"},
	{[]string{"wiki", "bucket", "diff"}, "<name>", "Print new/changed/removed files vs baseline; exit 0 empty, 1 non-empty"},
	{[]string{"wiki", "bucket", "promote"}, "<name>", "Snapshot bucket and rotate baseline→previous, current→baseline"},
	{[]string{"wiki", "bucket", "doc"}, "<bucket> <slug>", "Scaffold <bucket>/<slug>.md from the embedded doc template; --router also scaffolds the sibling subtree"},
	{[]string{"wiki", "bucket", "skill"}, "<bucket>", "Scaffold the realm per-bucket SKILL.md for <bucket> (no-op if present)"},
	{[]string{"wiki", "bucket", "index"}, "[<bucket>]", "Rebuild the <bucket-docs> region for one bucket (or all when omitted) plus the realm bucket list"},
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

// TestRootCmdExact22Verbs verifies the Cobra root command has exactly the 22
// expected top-level verbs and no extra auto-generated commands (completion,
// help) leaked into the visible command set.
// WHY: DisableDefaultCmd and SetHelpCommand suppress Cobra's auto-adds;
// this test is the gate that catches any regression where Cobra re-adds them
// or a new verb is accidentally introduced.
func TestRootCmdExact22Verbs(t *testing.T) {
	var repoOverride string
	root := buildRootCmd(&repoOverride)

	want := []string{
		"bus", "claude", "code", "config", "docker", "docs", "doctor",
		"followups", "hooks", "migrate", "profile", "prompt", "reminder",
		"repl", "repo", "serve", "signals", "template", "update", "validate", "where", "wiki",
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

// testBusDispatchHome creates a short, /tmp-rooted (not t.TempDir()) home
// directory for this test's real Unix domain socket, mirroring
// internal/bus/client_test.go's testBusHome — t.TempDir() embeds the full
// test name and can exceed the ~104-108 byte sun_path limit on macOS/Linux.
func testBusDispatchHome(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "atomicbus-dispatch")
	if err != nil {
		return t.TempDir()
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestRunBus_DispatchUsesRealHomeFromEnv is the mandatory dispatch-layer
// real-filesystem test the checkpoint 4 brief requires: checkpoint 1's own
// disk test (internal/bus/identity_test.go) injects home directly into
// State.Load/Save, so it can never observe the os.UserHomeDir()-to-home
// hand-off inside runBus — that hand-off is only reachable, and only
// breakable, here.
//
// A real daemon is bound at bus.SocketPath(home) in this process, and a
// member is seeded on it directly (bypassing the CLI entirely, via
// bus.Dial). The subprocess then runs `atomic bus who dispatch-room --json`
// with HOME redirected to that same home and nothing else — if runBus
// resolved the wrong path (e.g. home+"/.claude", the scope-root class of
// bug .claude/skills/atomic-cli-contrib/SKILL.md §3-4 warns about), the
// subprocess would fail to dial the real socket and exit 6, not merely
// return an empty roster. runBus calls os.Exit, so it is exercised in a
// subprocess (the standard Go idiom for os.Exit-calling code), matching
// TestRunProfile_UsesHomeNotClaudeHome's established pattern.
func TestRunBus_DispatchUsesRealHomeFromEnv(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_RUN_BUS_HELPER") == "1" {
		runBus([]string{"who", "dispatch-room", "--json"})
		return
	}

	home := testBusDispatchHome(t)
	if err := bus.EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	ln, err := net.Listen("unix", bus.SocketPath(home))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	hub := bus.NewHub(home)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bus.Serve(ctx, ln, hub, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not shut down within the bounded wait")
		}
	})

	client, err := bus.Dial(home, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := client.Do(bus.Request{
		Op: bus.OpJoin, Room: "dispatch-room", Name: "probe", Kind: bus.KindAgent, Session: "sess-probe",
	}); err != nil {
		t.Fatalf("seed join: %v", err)
	}
	client.Close()

	cmd := exec.Command(os.Args[0], "-test.run=TestRunBus_DispatchUsesRealHomeFromEnv")
	cmd.Env = append(os.Environ(), "ATOMIC_TEST_RUN_BUS_HELPER=1", "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess runBus failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"probe"`) {
		t.Errorf("subprocess output does not contain the seeded member; got:\n%s", out)
	}
}

// TestCodeMCPDaemonArgv_MatchesCobraTree is the GitHub issue #193 regression
// test: DefaultSpawn's argv (codemcp.DaemonArgv) must always name a
// Cobra-registered command, or the spawned daemon subprocess dies on
// "unknown flag" before its handler ever runs — the only symptom being
// "daemon did not start within 10s" at the proxy. Driving DaemonArgv's own
// output through the real root command (rather than hand-writing the argv
// here) means a future drift between DefaultSpawn and the Cobra tree fails
// this test instead of silently reintroducing the bug.
//
// runCode calls os.Exit, so the dispatch is exercised in a subprocess (the
// standard Go idiom for os.Exit-calling code), matching
// TestRunBus_DispatchUsesRealHomeFromEnv's established pattern.
func TestCodeMCPDaemonArgv_MatchesCobraTree(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_CODE_MCP_DAEMON_ARGV_HELPER") == "1" {
		var repoOverride string
		root := buildRootCmd(&repoOverride)
		root.SetArgs(codemcp.DaemonArgv(os.Getenv("ATOMIC_TEST_MCP_SRC"), os.Getenv("ATOMIC_TEST_MCP_DB"), codemcp.WatchOptions{}))
		if err := root.Execute(); err != nil {
			// Mirrors main()'s own Execute() error handling so the subprocess's
			// stderr/exit code match what a real invocation would produce.
			fmt.Fprintf(os.Stderr, "atomic: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// dbPath's parent path component is a regular file, not a directory: the
	// daemon handler's own MkdirAll fails fast and deterministically (no
	// accept loop ever starts, no socket ever binds), giving a handler-level
	// error to assert against instead of racing a real daemon's lifetime.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}
	dbPath := filepath.Join(blocker, "sub", "atomic.db")
	srcDir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=TestCodeMCPDaemonArgv_MatchesCobraTree")
	cmd.Env = append(os.Environ(),
		"ATOMIC_TEST_CODE_MCP_DAEMON_ARGV_HELPER=1",
		"ATOMIC_TEST_MCP_SRC="+srcDir,
		"ATOMIC_TEST_MCP_DB="+dbPath,
	)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if strings.Contains(output, "unknown flag") {
		t.Fatalf("DaemonArgv produced args Cobra could not parse (spawn/cobra-tree drift):\n%s", output)
	}
	if err == nil {
		t.Fatalf("expected the daemon handler to fail on an unusable db path, got success:\n%s", output)
	}
	if !strings.Contains(output, "atomic code mcp:") {
		t.Errorf("expected the handler's own \"atomic code mcp:\" error prefix, got:\n%s", output)
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

// TestScanRepoOverride covers the pre-scan that makes the global --repo
// override actually take effect (cli-repo-flag-never-parses): every leaf
// command sets DisableFlagParsing:true, so Cobra's own persistent-flag
// parsing of --repo is a no-op regardless of position — this scan is the
// only place --repo is read.
func TestScanRepoOverride(t *testing.T) {
	cases := []struct {
		name      string
		argv      []string
		wantValue string
		wantArgs  []string
		wantErr   bool
	}{
		{
			name:      "before the verb",
			argv:      []string{"atomic", "--repo", "/tmp/other", "signals", "show"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:      "after the verb",
			argv:      []string{"atomic", "signals", "show", "--repo", "/tmp/other"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:      "equals form",
			argv:      []string{"atomic", "signals", "show", "--repo=/tmp/other"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:      "own flags survive alongside --repo",
			argv:      []string{"atomic", "wiki", "scan", "--root", "/tmp/x", "--repo", "/tmp/other"},
			wantValue: "/tmp/other",
			wantArgs:  []string{"atomic", "wiki", "scan", "--root", "/tmp/x"},
		},
		{
			name:      "absent",
			argv:      []string{"atomic", "signals", "show"},
			wantValue: "",
			wantArgs:  []string{"atomic", "signals", "show"},
		},
		{
			name:    "missing value at end of argv",
			argv:    []string{"atomic", "signals", "show", "--repo"},
			wantErr: true,
		},
		{
			name:    "missing value: next token is another flag",
			argv:    []string{"atomic", "--repo", "--json", "signals", "show"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, cleaned, err := scanRepoOverride(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got value=%q cleaned=%v", value, cleaned)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if value != tc.wantValue {
				t.Errorf("value = %q, want %q", value, tc.wantValue)
			}
			if len(cleaned) != len(tc.wantArgs) {
				t.Fatalf("cleaned = %v, want %v", cleaned, tc.wantArgs)
			}
			for i, a := range cleaned {
				if a != tc.wantArgs[i] {
					t.Errorf("cleaned[%d] = %q, want %q", i, a, tc.wantArgs[i])
				}
			}
		})
	}
}

// TestRepoFlagExempt verifies that migrate, config resolve, and wiki stamp —
// the three verbs whose own --repo flag already carries different,
// established semantics — are detected as exempt from the global pre-scan,
// while every other verb (including ones that also take their own flags, or
// share a top-level name with an exempt verb's parent) is not.
func TestRepoFlagExempt(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"migrate alone", []string{"migrate"}, true},
		{"migrate with its own --repo", []string{"migrate", "--repo", "/x"}, true},
		{"migrate with --realm", []string{"migrate", "--realm", "/x"}, true},
		{"config resolve", []string{"config", "resolve", "--repo", "/x", "--json"}, true},
		{"wiki stamp with positional file before flags", []string{"wiki", "stamp", "f.md", "--repo", "/x"}, true},
		{"config get is not exempt", []string{"config", "get", "some.key"}, false},
		{"wiki scan is not exempt", []string{"wiki", "scan", "--root", "/x"}, false},
		{"signals is not exempt", []string{"signals", "show"}, false},
		{"code is not exempt", []string{"code", "status", "--repo", "/x"}, false},
		{"empty argv", []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoFlagExempt(tc.argv); got != tc.want {
				t.Errorf("repoFlagExempt(%v) = %v, want %v", tc.argv, got, tc.want)
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

	result, err := runClaudeInstall(target, scope, "install", false, false)
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

	result, err := runClaudeInstall(target, scope, "install", false, true)
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

	result, err := runClaudeInstall(target, scope, "install", true, false)
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

	_, err = runClaudeUninstall(targetDir, targetDir, devNull)
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

	prompt, err := runClaudeUninstall(targetDir, targetDir, devNull)
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
	home := t.TempDir()
	code := profileAction([]string{}, home, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(no args): got exit code %d, want 2", code)
	}
}

// TestProfileAction_UnknownVerbUsageError verifies that an unknown sub-verb
// returns exit code 2 and does not silently succeed.
func TestProfileAction_UnknownVerbUsageError(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{"bogus"}, home, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(bogus): got exit code %d, want 2", code)
	}
}

// TestProfileAction_RefreshWritesFile verifies that "refresh" (no flags) creates
// profile.md and stamps the lastcheck attribute with the injected date.
// WHY: proves the main.go dispatch actually reaches Refresh; the profile-package
// unit tests cover the core logic, but this test verifies the wiring.
func TestProfileAction_RefreshWritesFile(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{"refresh"}, home, "2026-05-28")
	if code != 0 {
		t.Fatalf("profileAction(refresh): got exit code %d, want 0", code)
	}

	profilePath := filepath.Join(home, ".atomic", "profile.md")
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
	home := t.TempDir()
	code := profileAction([]string{"refresh", "--if-stale", "7h"}, home, "2026-05-28")
	if code != 1 {
		t.Errorf("profileAction(refresh --if-stale 7h): got exit code %d, want 1", code)
	}
}

// TestProfileAction_IfStaleNoOpWhenFresh verifies that --if-stale with a fresh
// lastcheck does not modify the file. WHY: the --if-stale gate exists precisely
// to avoid spurious re-runs during session start.
func TestProfileAction_IfStaleNoOpWhenFresh(t *testing.T) {
	home := t.TempDir()
	atomicDir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# User profile\n\n## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	statBefore, _ := os.Stat(profilePath)

	code := profileAction([]string{"refresh", "--if-stale", "7d"}, home, "2026-05-28")
	if code != 0 {
		t.Fatalf("profileAction(refresh --if-stale 7d) fresh: got exit code %d, want 0", code)
	}

	statAfter, _ := os.Stat(profilePath)
	if !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Error("profileAction: file mtime changed even though lastcheck was fresh")
	}
}

// TestRunProfile_UsesHomeNotClaudeHome is the regression guard for the
// runProfile chain bug (docs/spec/configurable-state-paths.md issue #150):
// runProfile must pass home directly to profileAction, not <home>/.claude —
// config.ProfilePath resolves <home>/.atomic/profile.md, so an extra ".claude"
// join wrote to the wrong path. profileAction's own tests above inject a
// tempdir directly as home, which is exactly what let this bug in runProfile's
// own home-resolution glue go unnoticed. runProfile calls os.Exit, so it is
// exercised in a subprocess (the standard Go idiom for os.Exit-calling code)
// with HOME redirected to a temp dir — the real ~/.claude and ~/.atomic are
// never touched.
func TestRunProfile_UsesHomeNotClaudeHome(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_RUN_PROFILE_HELPER") == "1" {
		runProfile([]string{"refresh"})
		return
	}

	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestRunProfile_UsesHomeNotClaudeHome")
	// PATH is stripped so the profile detectors find no real tools: the test
	// guards home-path resolution only, and a real probe (e.g. bazel) writes
	// its cache into the temp HOME and races t.TempDir cleanup on CI.
	cmd.Env = append(os.Environ(), "ATOMIC_TEST_RUN_PROFILE_HELPER=1", "HOME="+home, "PATH=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subprocess runProfile failed: %v\n%s", err, out)
	}

	profilePath := filepath.Join(home, ".atomic", "profile.md")
	if _, err := os.Stat(profilePath); err != nil {
		t.Errorf("expected profile.md at %s (home, not home/.claude), stat err = %v", profilePath, err)
	}
	wrongPath := filepath.Join(home, ".claude", ".atomic", "profile.md")
	if _, err := os.Stat(wrongPath); !os.IsNotExist(err) {
		t.Errorf("profile.md incorrectly written under home/.claude/.atomic (%s); stat err = %v", wrongPath, err)
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

	prompt, err := runClaudeUninstall(targetDir, targetDir, devNull)
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

// TestCP5FindAllPaths verifies that rootCmd.Find returns a non-nil, non-root
// command for every path in cliusage.Commands(). WHY (SC3): every command path
// registered in the golden cliusage surface must be reachable in the live Cobra
// tree so that --help rendering and DeriveCommands produce complete output. A
// missing path means a command is declared in the fixture but absent from the
// tree — the A1 linter would pass while the actual command is unreachable.
//
// Paths are sourced from cliusage.Commands() so the assertion automatically
// covers whatever the current command set is; no hardcoded count is used.
func TestCP5FindAllPaths(t *testing.T) {
	var repoOverride string
	root := buildRootCmd(&repoOverride)

	for _, cmd := range cliusage.Commands() {
		path := cmd.Path
		t.Run(strings.Join(path, "/"), func(t *testing.T) {
			found, _, _ := root.Find(path)
			if found == nil || found == root {
				t.Errorf("Find(%v) returned nil or root — command not reachable in Cobra tree", path)
			}
		})
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

// --- self-update fast path (docs/spec/selfupdate-state.md, parent fast path) ---

func TestStripBackgroundCheckMarker(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantFound bool
		wantArgs  []string
	}{
		{
			name:      "marker present, trailing",
			args:      []string{"--check", backgroundCheckMarker},
			wantFound: true,
			wantArgs:  []string{"--check"},
		},
		{
			name:      "marker present, leading",
			args:      []string{backgroundCheckMarker, "--check"},
			wantFound: true,
			wantArgs:  []string{"--check"},
		},
		{
			name:      "marker absent",
			args:      []string{"--check"},
			wantFound: false,
			wantArgs:  []string{"--check"},
		},
		{
			name:      "no args",
			args:      nil,
			wantFound: false,
			wantArgs:  []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found, cleaned := stripBackgroundCheckMarker(tc.args)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if strings.Join(cleaned, " ") != strings.Join(tc.wantArgs, " ") {
				t.Errorf("cleaned = %v, want %v", cleaned, tc.wantArgs)
			}
		})
	}
}

// writeTestUpdateConfig writes a minimal config.toml under home with the
// given [update] table body, so tests can exercise config.Update.Check
// without going through the full Set/WritePersist machinery.
func writeTestUpdateConfig(t *testing.T, home, updateTableBody string) {
	t.Helper()
	dir := config.Dir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[update]\n" + updateTableBody
	if err := os.WriteFile(config.TOMLPath(home), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSelfupdateFastPath_SpawnGates covers success criterion: "Spawn fires
// only when ALL gates hold ... last_check stamped and persisted BEFORE the
// spawn". Each subtest flips exactly one gate false against an otherwise
// spawn-eligible baseline. The injected spawn func means no subtest ever
// forks a real process.
func TestSelfupdateFastPath_SpawnGates(t *testing.T) {
	baseNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return baseNow }

	t.Run("all gates hold: spawns exactly once, last_check persisted before spawn fires", func(t *testing.T) {
		home := t.TempDir()
		var calls int
		spawn := func(exe string) error {
			calls++
			// The stamp-before-spawn ordering: by the time spawn runs,
			// last_check must already be on disk.
			st := selfupdate.LoadState(config.StatePath(home))
			if st.Update.LastCheck.IsZero() {
				t.Error("last_check was not persisted before spawn was invoked")
			}
			return nil
		}
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 1 {
			t.Errorf("expected exactly 1 spawn, got %d", calls)
		}
	})

	t.Run("child's own update invocation never re-spawns a grandchild", func(t *testing.T) {
		home := t.TempDir()
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "update", "1.0.0", false, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("verb=update must never spawn, got %d spawns", calls)
		}
	})

	t.Run("--no-update-check present: never spawns", func(t *testing.T) {
		home := t.TempDir()
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", true, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("--no-update-check must suppress spawn, got %d spawns", calls)
		}
	})

	t.Run("config update.check=false: never spawns", func(t *testing.T) {
		home := t.TempDir()
		writeTestUpdateConfig(t, home, "check = false\n")
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("update.check=false must suppress spawn, got %d spawns", calls)
		}
	})

	t.Run("last_check within the hour: never spawns", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LastCheck = baseNow.Add(-30 * time.Minute)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 0 {
			t.Errorf("fresh last_check must suppress spawn, got %d spawns", calls)
		}
	})

	t.Run("last_check exactly 1h ago: spawns (inclusive boundary)", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LastCheck = baseNow.Add(-time.Hour)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		calls := 0
		spawn := func(exe string) error { calls++; return nil }
		selfupdateFastPath(home, "signals", "1.0.0", false, io.Discard, now, spawn)
		if calls != 1 {
			t.Errorf("expected 1 spawn at the 1h boundary, got %d", calls)
		}
	})
}

// TestSelfupdateFastPath_Banner covers success criterion: "Banner prints
// from state only (no network), at most once per 24h, stamps last_notified."
// Every subtest sets noUpdateCheck=true and verb="update" so the spawn gate
// never fires — isolating banner behavior from the spawn decision above.
func TestSelfupdateFastPath_Banner(t *testing.T) {
	baseNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return baseNow }
	noopSpawn := func(exe string) error { return nil }

	t.Run("newer version, never notified: prints and stamps last_notified", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "999.0.0"
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)

		if !strings.Contains(buf.String(), "999.0.0") {
			t.Errorf("expected banner mentioning 999.0.0, got %q", buf.String())
		}
		got := selfupdate.LoadState(config.StatePath(home))
		if !got.Update.LastNotified.Equal(baseNow) {
			t.Errorf("last_notified = %v, want %v", got.Update.LastNotified, baseNow)
		}
	})

	t.Run("notified 1h ago: suppressed within the 24h window", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "999.0.0"
		state.Update.LastNotified = baseNow.Add(-time.Hour)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if buf.Len() != 0 {
			t.Errorf("expected no banner within the 24h window, got %q", buf.String())
		}
	})

	t.Run("notified 25h ago: banners again past the window", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "999.0.0"
		state.Update.LastNotified = baseNow.Add(-25 * time.Hour)
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if !strings.Contains(buf.String(), "999.0.0") {
			t.Errorf("expected a banner past the 24h window, got %q", buf.String())
		}
	})

	t.Run("no latest_version recorded yet: never banners", func(t *testing.T) {
		home := t.TempDir()
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if buf.Len() != 0 {
			t.Errorf("expected no banner with empty state, got %q", buf.String())
		}
	})

	// F-1: the banner must never print a "v"-prefixed version, regardless of
	// what is already on disk in state.json — defense-in-depth alongside the
	// check-branch write site normalizing before it ever writes latest_version.
	t.Run("v-prefixed latest_version on disk: banner strips the v", func(t *testing.T) {
		home := t.TempDir()
		state := selfupdate.State{}
		state.Update.LatestVersion = "v9.9.9"
		if err := selfupdate.WriteState(config.StatePath(home), state); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		selfupdateFastPath(home, "update", "1.0.0", true, &buf, now, noopSpawn)
		if strings.Contains(buf.String(), "v9.9.9") {
			t.Errorf("banner must never print a v-prefixed version, got %q", buf.String())
		}
		if !strings.Contains(buf.String(), "9.9.9") {
			t.Errorf("expected normalized version 9.9.9 in banner, got %q", buf.String())
		}
	})
}

// TestSelfupdateFastPath_ExecutableFailureReportsToWriter covers F-2:
// os.Executable() failing in the spawn path must report to w, matching the
// sibling failure branches (write-state, spawn) immediately above and below
// it, instead of returning silently.
func TestSelfupdateFastPath_ExecutableFailureReportsToWriter(t *testing.T) {
	home := t.TempDir()
	orig := executableFn
	executableFn = func() (string, error) { return "", errors.New("boom") }
	defer func() { executableFn = orig }()

	baseNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return baseNow }
	spawnCalled := false
	spawn := func(exe string) error { spawnCalled = true; return nil }

	var buf bytes.Buffer
	selfupdateFastPath(home, "signals", "1.0.0", false, &buf, now, spawn)

	if spawnCalled {
		t.Error("spawn must not be invoked when executableFn fails")
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("expected the executable-resolution failure reported to w, got %q", buf.String())
	}
}

// TestDefaultUpdateSpawn_StartsDetachedWithoutWaiting proves the real spawn
// path starts a process and returns without waiting on it — using /bin/echo
// rather than a built atomic binary, since defaultUpdateSpawn only cares
// that the target process starts and is released, not what it does.
func TestDefaultUpdateSpawn_StartsDetachedWithoutWaiting(t *testing.T) {
	if _, err := exec.LookPath("/bin/echo"); err != nil {
		t.Skip("/bin/echo not available")
	}
	if err := defaultUpdateSpawn("/bin/echo"); err != nil {
		t.Fatalf("defaultUpdateSpawn: %v", err)
	}
}

// --- runUpdateCheck (CP4: detached-child check branch + once-only staging) ---

// fakeReleaseServer wires a full fake GitHub release backend behind one
// httptest server: /releases (hit by both Check and the staging Lookup),
// the release archive, and checksums.txt (hit by Stage). Every hit against
// /releases increments releaseHits so tests can assert exactly how many
// lookups occurred (the once-only staging gate's whole point is to avoid a
// second download attempt, not a second lookup, but counting lookups also
// proves the gate short-circuits before any network call on a repeat).
type fakeReleaseServer struct {
	srv         *httptest.Server
	client      *selfupdate.Client
	releaseHits int
	archiveHits int
}

func newFakeReleaseServer(t *testing.T, tag, archiveContent string) *fakeReleaseServer {
	t.Helper()
	buildDir := t.TempDir()
	assetName := fmt.Sprintf("atomic_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	archivePath := filepath.Join(buildDir, assetName)
	if err := os.WriteFile(archivePath, []byte(archiveContent), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256HexString([]byte(archiveContent))
	checksumPath := filepath.Join(buildDir, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(fmt.Sprintf("%s  %s\n", sum, assetName)), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeReleaseServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		f.releaseHits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: tag,
			Assets: []selfupdate.Asset{
				{Name: assetName},
				{Name: "checksums.txt"},
			},
		}})
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		f.archiveHits++
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	f.client = &selfupdate.Client{
		BaseURL:     f.srv.URL,
		DownloadURL: f.srv.URL,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}
	return f
}

// brokenReleaseClient returns a Client whose lookups always fail (points at
// a closed listener), for exercising runUpdateCheck's lookup-failure path
// without any real network access.
func brokenReleaseClient(t *testing.T) *selfupdate.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens here — connection refused on every request
	return &selfupdate.Client{BaseURL: "http://" + addr, HTTPClient: &http.Client{Timeout: 2 * time.Second}}
}

func TestRunUpdateCheck_ManualCheckWritesStateNeverStages(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v9.9.9", "payload")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	newer, tag, err := runUpdateCheck(context.Background(), home, false, f.client, "stable", "1.0.0", now, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !newer || tag != "9.9.9" {
		t.Fatalf("newer=%v tag=%q, want newer=true tag=9.9.9", newer, tag)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.LatestVersion != "9.9.9" {
		t.Errorf("LatestVersion = %q, want %q (no leading v — F-1)", got.Update.LatestVersion, "9.9.9")
	}
	if got.Update.LastResult != "" {
		t.Errorf("LastResult = %q, want empty on success", got.Update.LastResult)
	}
	if got.Update.StageAttemptedFor != "" {
		t.Errorf("manual --check must never stage: StageAttemptedFor = %q, want empty", got.Update.StageAttemptedFor)
	}
	if got.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("manual --check must never stage: Staged = %+v, want zero value", got.Update.Staged)
	}
	if f.archiveHits != 0 {
		t.Errorf("manual --check downloaded the archive %d times, want 0", f.archiveHits)
	}
}

func TestRunUpdateCheck_LookupFailureRecordsLastResultLeavesLatestVersionUnchanged(t *testing.T) {
	home := t.TempDir()
	// Seed a prior good value: a failed lookup must not clobber it.
	seed := selfupdate.State{}
	seed.Update.LatestVersion = "5.0.0"
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	c := brokenReleaseClient(t)
	now := func() time.Time { return time.Now() }

	_, _, err := runUpdateCheck(context.Background(), home, false, c, "stable", "1.0.0", now, io.Discard)
	if err == nil {
		t.Fatal("expected a lookup error, got nil")
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.LatestVersion != "5.0.0" {
		t.Errorf("LatestVersion = %q, want unchanged %q on lookup failure", got.Update.LatestVersion, "5.0.0")
	}
	if got.Update.LastResult == "" {
		t.Error("LastResult was not recorded on lookup failure")
	}
}

func TestRunUpdateCheck_BackgroundStagesWhenNewerAndEnabled(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	newer, tag, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !newer || tag != "2.0.0" {
		t.Fatalf("newer=%v tag=%q, want newer=true tag=2.0.0", newer, tag)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "2.0.0" {
		t.Errorf("StageAttemptedFor = %q, want %q", got.Update.StageAttemptedFor, "2.0.0")
	}
	if got.Update.Staged.Version != "2.0.0" {
		t.Errorf("Staged.Version = %q, want %q", got.Update.Staged.Version, "2.0.0")
	}
	if got.Update.Staged.SHA256 == "" {
		t.Error("Staged.SHA256 empty, want a recorded checksum")
	}
	wantDir := selfupdate.StageDir(home)
	if !strings.HasPrefix(got.Update.Staged.Path, wantDir) {
		t.Errorf("Staged.Path %q not under %q", got.Update.Staged.Path, wantDir)
	}
	if _, statErr := os.Stat(got.Update.Staged.Path); statErr != nil {
		t.Errorf("staged file missing on disk: %v", statErr)
	}
	if got.Update.Updating {
		t.Error("lock must be released after staging completes")
	}
	if !got.Update.UpdateStartedAt.IsZero() {
		t.Error("update_started_at must be cleared after staging completes")
	}
}

func TestRunUpdateCheck_OnceOnlyGateHoldsAcrossRepeatedChecks(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("first check: %v", err)
	}
	if f.archiveHits != 1 {
		t.Fatalf("archiveHits after first check = %d, want 1", f.archiveHits)
	}

	// Repeat the exact same cycle: same version, still background, still
	// newer, still enabled. The once-only budget for this version is
	// already spent — no second download.
	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("second check: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archiveHits after second check = %d, want still 1 (once-only budget already spent)", f.archiveHits)
	}
}

func TestRunUpdateCheck_NewVersionAllowsNewAttemptAfterBudgetSpent(t *testing.T) {
	home := t.TempDir()
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	f1 := newFakeReleaseServer(t, "v1.1.0", "payload-1.1.0")
	if _, _, err := runUpdateCheck(context.Background(), home, true, f1.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("first check: %v", err)
	}
	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "1.1.0" {
		t.Fatalf("StageAttemptedFor = %q, want %q", got.Update.StageAttemptedFor, "1.1.0")
	}

	// A new release appears: same home, different version — a fresh
	// once-only budget for 1.2.0 must be available.
	f2 := newFakeReleaseServer(t, "v1.2.0", "payload-1.2.0")
	if _, _, err := runUpdateCheck(context.Background(), home, true, f2.client, "stable", "1.1.0", now, io.Discard); err != nil {
		t.Fatalf("second check: %v", err)
	}
	got = selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "1.2.0" {
		t.Errorf("StageAttemptedFor = %q, want %q (new version allows a new attempt)", got.Update.StageAttemptedFor, "1.2.0")
	}
	if f2.archiveHits != 1 {
		t.Errorf("archiveHits for the new version = %d, want 1", f2.archiveHits)
	}
}

func TestRunUpdateCheck_LockContentionSkipsStagingWithoutStamping(t *testing.T) {
	home := t.TempDir()
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	// Simulate a concurrent updater already holding the lock.
	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = now()
	if err := selfupdate.WriteState(config.StatePath(home), held); err != nil {
		t.Fatal(err)
	}

	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "" {
		t.Errorf("StageAttemptedFor = %q, want empty (lock contention must not spend the once-only budget)", got.Update.StageAttemptedFor)
	}
	if got.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("Staged = %+v, want zero value under lock contention", got.Update.Staged)
	}
	if f.archiveHits != 0 {
		t.Errorf("archiveHits = %d, want 0 under lock contention", f.archiveHits)
	}
	// The base state write (latest_version/last_result) still happens —
	// only staging is gated by the lock.
	if got.Update.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q even under lock contention", got.Update.LatestVersion, "2.0.0")
	}
}

func TestRunUpdateCheck_FailedStageRecordsLastResultStaysStampedNeverRetried(t *testing.T) {
	home := t.TempDir()
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	// A release whose advertised assets never match the archive name Stage
	// computes ("atomic_3.0.0_<goos>_<goarch>.tar.gz") — Stage fails
	// deterministically at asset lookup, before any download, no network
	// flakiness required to produce the failure.
	var releaseHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		releaseHits++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: "v3.0.0",
			Assets:  []selfupdate.Asset{{Name: "unrelated.tar.gz"}, {Name: "checksums.txt"}},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &selfupdate.Client{BaseURL: srv.URL, DownloadURL: srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}

	newer, tag, err := runUpdateCheck(context.Background(), home, true, c, "stable", "1.0.0", now, io.Discard)
	if err != nil {
		t.Fatalf("Check itself must still succeed: %v", err)
	}
	if !newer || tag != "3.0.0" {
		t.Fatalf("newer=%v tag=%q, want newer=true tag=3.0.0", newer, tag)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "3.0.0" {
		t.Errorf("StageAttemptedFor = %q, want %q (stays stamped despite failure)", got.Update.StageAttemptedFor, "3.0.0")
	}
	if got.Update.LastResult == "" {
		t.Error("expected a recorded staging failure in LastResult")
	}
	if got.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("Staged = %+v, want zero value on failure", got.Update.Staged)
	}
	if got.Update.Updating {
		t.Error("lock must be released even after a staging failure")
	}

	// Repeat: same version, same failure mode — must never retry.
	if _, _, err := runUpdateCheck(context.Background(), home, true, c, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("second check: %v", err)
	}
	if releaseHits != 3 {
		// 1 (first Check) + 1 (first staging Lookup) + 1 (second Check) — the
		// second staging Lookup must never fire because the gate short-circuits.
		t.Errorf("releaseHits = %d, want 3 (no retry of the failed stage attempt)", releaseHits)
	}
}

func TestRunUpdateCheck_StageDisabledByConfigNeverStages(t *testing.T) {
	home := t.TempDir()
	writeTestUpdateConfig(t, home, "stage = false\n")
	f := newFakeReleaseServer(t, "v2.0.0", "release-payload-v2")
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	if _, _, err := runUpdateCheck(context.Background(), home, true, f.client, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := selfupdate.LoadState(config.StatePath(home))
	if got.Update.StageAttemptedFor != "" {
		t.Errorf("StageAttemptedFor = %q, want empty when update.stage=false", got.Update.StageAttemptedFor)
	}
	if f.archiveHits != 0 {
		t.Errorf("archiveHits = %d, want 0 when update.stage=false", f.archiveHits)
	}
}

// TestRunUpdateCheck_StagerCompletionDoesNotClobberForegroundTakeover pins
// the owner-checked release fix: the background stager acquires the lock,
// then — while its archive download is in flight — a foreground `atomic
// update` takes over the (now stale, or --force-stamped) lock, recording a
// newer update_started_at. When the stager's own download finishes and it
// writes its completion record, the foreground's active lock must survive:
// a blind ReleaseLock would clear Updating/UpdateStartedAt out from under
// the still-in-progress foreground swap, opening a window for a third
// `atomic update` to race a concurrent os.Rename on the same binary.
func TestRunUpdateCheck_StagerCompletionDoesNotClobberForegroundTakeover(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)

	stagerAcquiredAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return stagerAcquiredAt }
	foregroundStartedAt := stagerAcquiredAt.Add(5 * time.Minute)

	buildDir := t.TempDir()
	assetName := fmt.Sprintf("atomic_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archivePath := filepath.Join(buildDir, assetName)
	content := "release-payload-v2"
	if err := os.WriteFile(archivePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	checksumPath := filepath.Join(buildDir, "checksums.txt")
	sum := sha256HexString([]byte(content))
	if err := os.WriteFile(checksumPath, []byte(fmt.Sprintf("%s  %s\n", sum, assetName)), 0o644); err != nil {
		t.Fatal(err)
	}

	var takeoverDone bool
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: "v2.0.0",
			Assets: []selfupdate.Asset{
				{Name: assetName},
				{Name: "checksums.txt"},
			},
		}})
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		// Simulate the foreground process's takeover write landing on disk
		// mid-download — before this stager's own completion write below.
		if !takeoverDone {
			takeoverDone = true
			fg := selfupdate.State{}
			fg.Update.Updating = true
			fg.Update.UpdateStartedAt = foregroundStartedAt
			if err := selfupdate.WriteState(statePath, fg); err != nil {
				t.Fatal(err)
			}
		}
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &selfupdate.Client{BaseURL: srv.URL, DownloadURL: srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}

	if _, _, err := runUpdateCheck(context.Background(), home, true, c, "stable", "1.0.0", now, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := selfupdate.LoadState(statePath)
	if !got.Update.Updating || !got.Update.UpdateStartedAt.Equal(foregroundStartedAt) {
		t.Fatalf("foreground lock clobbered by stager completion: Updating=%v UpdateStartedAt=%v, want Updating=true UpdateStartedAt=%v",
			got.Update.Updating, got.Update.UpdateStartedAt, foregroundStartedAt)
	}
	// The stager's own non-lock fields must still land even though it no
	// longer owns the lock at completion time.
	if got.Update.StageAttemptedFor != "2.0.0" {
		t.Errorf("StageAttemptedFor = %q, want %q even without lock ownership", got.Update.StageAttemptedFor, "2.0.0")
	}
	if got.Update.Staged.Version != "2.0.0" {
		t.Errorf("Staged.Version = %q, want %q", got.Update.Staged.Version, "2.0.0")
	}
}

// --- runUpdateApply (CP5: lock + staged fast-path swap in the apply branch) ---

// buildRealArchiveTarGz builds a genuine gzip-compressed tar archive
// containing one file, "atomic", with content, at dir/assetName — unlike
// fakeReleaseServer's raw-bytes fixture (checksum-only; staging never
// extracts what it downloads), CP5's swap flow actually extracts and
// renames the binary, so its tests need a real, extractable archive.
func buildRealArchiveTarGz(t *testing.T, dir, assetName, content string) (archivePath, sha string) {
	t.Helper()
	archivePath = filepath.Join(dir, assetName)
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "atomic", Mode: 0o755, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	f.Close()

	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	return archivePath, sha256HexString(data)
}

// fakeSwapServer serves a real, extractable release (archive + checksums.txt
// + /releases lookup) for one tag — used by CP5 tests that exercise actual
// extraction/swap, as opposed to fakeReleaseServer's checksum-only fixture.
type fakeSwapServer struct {
	srv         *httptest.Server
	client      *selfupdate.Client
	assetName   string
	archivePath string
	sha256      string
	archiveHits int
}

func newFakeSwapServer(t *testing.T, tag, binaryContent string) *fakeSwapServer {
	t.Helper()
	dir := t.TempDir()
	assetName := fmt.Sprintf("atomic_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	archivePath, sha := buildRealArchiveTarGz(t, dir, assetName, binaryContent)
	checksumPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(fmt.Sprintf("%s  %s\n", sha, assetName)), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeSwapServer{assetName: assetName, archivePath: archivePath, sha256: sha}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: tag,
			Assets: []selfupdate.Asset{
				{Name: assetName},
				{Name: "checksums.txt"},
			},
		}})
	})
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		f.archiveHits++
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	f.client = &selfupdate.Client{BaseURL: f.srv.URL, DownloadURL: f.srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}
	return f
}

func TestRunUpdateApply_StagedFastPathSwapsWithoutDownloadingArchive(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v2-content"
	f := newFakeSwapServer(t, "v2.0.0", binaryContent)

	// Place a byte-identical copy of the release archive in the staged
	// directory — mirrors what a prior background Stage() call (CP4) would
	// have left behind.
	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stageDir, f.assetName)
	data, err := os.ReadFile(f.archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{Version: "2.0.0", Path: stagedPath, SHA256: f.sha256}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}

	if f.archiveHits != 0 {
		t.Errorf("archive endpoint hit %d times, want 0 on the staged fast path", f.archiveHits)
	}
	got, err := os.ReadFile(currentBin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared after a successful swap")
	}
	if state.Update.UpdatedAt.IsZero() {
		t.Error("updated_at must be stamped on success")
	}
	if state.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("staged record must be cleared after swap, got %+v", state.Update.Staged)
	}
	if _, statErr := os.Stat(stagedPath); statErr == nil {
		t.Error("staged file should be removed best-effort after a successful swap")
	}
}

func TestRunUpdateApply_StagedVersionMismatchFallsBackToDownload(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v3-content"
	f := newFakeSwapServer(t, "v3.0.0", binaryContent)

	// Staged record names an OLDER version than the fresh lookup returns.
	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleName := fmt.Sprintf("atomic_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	stalePath := filepath.Join(stageDir, staleName)
	if err := os.WriteFile(stalePath, []byte("stale-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{Version: "2.0.0", Path: stalePath, SHA256: sha256HexString([]byte("stale-content"))}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (version mismatch must fall back to a real download)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Staged != (selfupdate.StagedInfo{}) {
		t.Errorf("stale staged record must be discarded, got %+v", state.Update.Staged)
	}
}

func TestRunUpdateApply_StagedChecksumMismatchFallsBackToDownload(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v4-content"
	f := newFakeSwapServer(t, "v4.0.0", binaryContent)

	// Same version, same asset name, but different bytes than the fresh
	// release now serves — simulates a re-cut release since staging.
	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stageDir, f.assetName)
	if err := os.WriteFile(stagedPath, []byte("corrupted-or-stale-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{Version: "4.0.0", Path: stagedPath, SHA256: sha256HexString([]byte("corrupted-or-stale-bytes"))}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (checksum mismatch must fall back to a real download)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
}

func TestRunUpdateApply_StagedFileMissingFallsBackToDownload(t *testing.T) {
	home := t.TempDir()
	const binaryContent = "new-binary-v5-content"
	f := newFakeSwapServer(t, "v5.0.0", binaryContent)

	seed := selfupdate.State{}
	seed.Update.Staged = selfupdate.StagedInfo{
		Version: "5.0.0",
		Path:    filepath.Join(t.TempDir(), "gone.tar.gz"),
		SHA256:  "deadbeef",
	}
	if err := selfupdate.WriteState(config.StatePath(home), seed); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (missing staged file must fall back to a real download)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
}

func TestRunUpdateApply_UpToDateReportsAndClearsLock(t *testing.T) {
	home := t.TempDir()
	f := newFakeSwapServer(t, "v1.0.0", "same-version-content")

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("current-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("runUpdateApply: %v", err)
	}
	if !strings.Contains(buf.String(), "up to date") {
		t.Errorf("expected an up-to-date report, got %q", buf.String())
	}
	if f.archiveHits != 0 {
		t.Errorf("archive endpoint hit %d times, want 0 when already up to date", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "current-binary" {
		t.Error("binary must not be replaced when already up to date")
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared when already up to date")
	}
}

func TestRunUpdateApply_FreshLockRefusesNamingAge(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = started
	if err := selfupdate.WriteState(statePath, held); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return started.Add(3 * time.Minute) }
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Never contacted — refusal happens before any lookup.
	c := brokenReleaseClient(t)
	var buf bytes.Buffer
	err := runUpdateApply(context.Background(), home, c, "stable", "1.0.0", currentBin, false, now, &buf)
	if err == nil {
		t.Fatal("expected refusal for a fresh lock")
	}
	if !strings.Contains(err.Error(), "3m0s") {
		t.Errorf("expected the lock's age in the error, got: %v", err)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "old-binary" {
		t.Error("binary must be untouched on refusal")
	}
}

func TestRunUpdateApply_StaleLockTakenOverSwaps(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = started
	if err := selfupdate.WriteState(statePath, held); err != nil {
		t.Fatal(err)
	}

	const binaryContent = "new-binary-v6-content"
	f := newFakeSwapServer(t, "v6.0.0", binaryContent)

	now := func() time.Time { return started.Add(11 * time.Minute) } // past the 10-minute stale threshold
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, false, now, &buf); err != nil {
		t.Fatalf("expected takeover of the abandoned lock to succeed: %v", err)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}
}

// TestRunUpdateApply_ForceBypassesLockButNotChecksum covers the success
// criterion: --force bypasses lock contention only. A corrupted staged
// archive under --force must still fail its checksum re-verify and fall
// back to a real download rather than swapping the corrupted bytes in.
func TestRunUpdateApply_ForceBypassesLockButNotChecksum(t *testing.T) {
	home := t.TempDir()
	statePath := config.StatePath(home)
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	const binaryContent = "new-binary-v7-content"
	f := newFakeSwapServer(t, "v7.0.0", binaryContent)

	stageDir := selfupdate.StageDir(home)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stageDir, f.assetName)
	if err := os.WriteFile(stagedPath, []byte("corrupted-payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	held := selfupdate.State{}
	held.Update.Updating = true
	held.Update.UpdateStartedAt = started
	held.Update.Staged = selfupdate.StagedInfo{Version: "7.0.0", Path: stagedPath, SHA256: sha256HexString([]byte("corrupted-payload"))}
	if err := selfupdate.WriteState(statePath, held); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return started.Add(30 * time.Second) } // well within the stale window — force must still bypass it
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runUpdateApply(context.Background(), home, f.client, "stable", "1.0.0", currentBin, true, now, &buf); err != nil {
		t.Fatalf("--force with a fallback download available should still succeed: %v", err)
	}
	if f.archiveHits != 1 {
		t.Errorf("archive endpoint hit %d times, want 1 (corrupted staged archive must fall back to download, not swap directly)", f.archiveHits)
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != binaryContent {
		t.Errorf("binary content = %q, want %q (must be the freshly downloaded content, not the corrupted staged bytes)", got, binaryContent)
	}
}

func TestRunUpdateApply_LockClearedOnLookupFailure(t *testing.T) {
	home := t.TempDir()
	c := brokenReleaseClient(t)
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := runUpdateApply(context.Background(), home, c, "stable", "1.0.0", currentBin, false, now, &buf)
	if err == nil {
		t.Fatal("expected a lookup error")
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared best-effort after a lookup failure")
	}
}

func TestRunUpdateApply_LockClearedOnApplyFailure(t *testing.T) {
	home := t.TempDir()
	// A release advertising no matching archive asset — Apply fails
	// deterministically at asset lookup, before any network flakiness.
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]selfupdate.Release{{
			TagName: "v9.0.0",
			Assets:  []selfupdate.Asset{{Name: "unrelated.tar.gz"}},
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &selfupdate.Client{BaseURL: srv.URL, DownloadURL: srv.URL, HTTPClient: &http.Client{Timeout: 5 * time.Second}}

	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := runUpdateApply(context.Background(), home, c, "stable", "1.0.0", currentBin, false, now, &buf)
	if err == nil {
		t.Fatal("expected an Apply failure")
	}

	state := selfupdate.LoadState(config.StatePath(home))
	if state.Update.Updating {
		t.Error("lock must be cleared best-effort after an apply failure")
	}
	got, _ := os.ReadFile(currentBin)
	if string(got) != "old-binary" {
		t.Error("binary must be untouched on apply failure")
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

// --- atomic template dispatch ---

// TestTemplateAction_KnownNames verifies that templateAction exits 0 and
// writes non-empty text for each registered document-template name. Encodes
// the WHY: command artifacts instruct Claude to seed workflow documents from
// `atomic template <name>` — a broken embed path or missing template would
// silently hand back an empty skeleton and the improvised-structure problem
// the templates exist to prevent would return.
func TestTemplateAction_KnownNames(t *testing.T) {
	for _, name := range doctemplate.Names() {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			var errOut strings.Builder
			code := templateAction([]string{name}, &out, &errOut)
			if code != 0 {
				t.Fatalf("templateAction(%q) returned exit code %d, want 0; stderr: %s", name, code, errOut.String())
			}
			if strings.TrimSpace(out.String()) == "" {
				t.Errorf("templateAction(%q) wrote empty stdout", name)
			}
		})
	}
}

// TestTemplateAction_UnknownName verifies that templateAction exits 1 and
// writes to stderr for an unregistered template name — the fail-loud contract
// command artifacts rely on to stop rather than improvise structure.
func TestTemplateAction_UnknownName(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := templateAction([]string{"no-such-template"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("templateAction(\"no-such-template\") returned exit code 0, want non-zero")
	}
	if strings.TrimSpace(errOut.String()) == "" {
		t.Errorf("templateAction(\"no-such-template\") wrote nothing to stderr")
	}
	if out.String() != "" {
		t.Errorf("templateAction(\"no-such-template\") wrote unexpected stdout: %q", out.String())
	}
}

// TestTemplateAction_NoArgs verifies that templateAction exits 1 with a usage
// message listing the valid names when called with no arguments.
func TestTemplateAction_NoArgs(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	code := templateAction([]string{}, &out, &errOut)
	if code == 0 {
		t.Fatalf("templateAction with no args returned exit code 0, want non-zero")
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("no-args error message missing 'Usage:'; stderr: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "design-doc") {
		t.Errorf("no-args error message missing valid names; stderr: %q", errOut.String())
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

// TestRunMigrateInstall_TwoRootSplit proves the two-root split (issue #150):
// config.toml is read/written under <home>/.atomic (config helpers get home),
// while migrate.Context.Root — the root install-scope steps operate on —
// still receives <home>/.claude. A step that captured home instead of
// <home>/.claude would silently corrupt install-scope migrations that touch
// the Claude artifact tree (e.g. renaming a file under commands/).
func TestRunMigrateInstall_TwoRootSplit(t *testing.T) {
	home := t.TempDir()

	// Seed a pre-framework config.toml under the NEW location so migrate.Run
	// has a "0.0.0" floor to migrate up from.
	cfgPath := config.TOMLPath(home)
	if err := config.WritePersist(cfgPath, config.Default()); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// Inject a fake install-scope step and restore the real registry after.
	origRegistry := migrate.Registry
	defer func() { migrate.Registry = origRegistry }()

	var capturedRoot string
	migrate.Registry = append(append([]migrate.Migration{}, origRegistry...), migrate.Migration{
		TargetVersion: "99.0.0",
		Scope:         "install",
		Up: func(ctx *migrate.Context) error {
			capturedRoot = ctx.Root
			return nil
		},
	})

	if err := runMigrateInstall(home); err != nil {
		t.Fatalf("runMigrateInstall: %v", err)
	}

	wantClaudeHome := filepath.Join(home, ".claude")
	if capturedRoot != wantClaudeHome {
		t.Errorf("migrate.Context.Root = %q, want %q", capturedRoot, wantClaudeHome)
	}

	// Config helpers must have operated on <home>/.atomic/config.toml, not
	// <home>/.claude/.atomic/config.toml.
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if cfg.Install.Version != "99.0.0" {
		t.Errorf("Install.Version = %q, want %q (config.toml under <home>/.atomic was not updated)", cfg.Install.Version, "99.0.0")
	}

	legacyCfgPath := config.TOMLPath(wantClaudeHome)
	if _, err := os.Stat(legacyCfgPath); !os.IsNotExist(err) {
		t.Errorf("expected no config.toml under <home>/.claude/.atomic, stat err = %v", err)
	}
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
