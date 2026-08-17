package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
)

// wantCobraSubcommandMeta mirrors cliusage.go byte-for-byte. deriveCommands
// reads cmd.Short and Annotations["args_hint"], so any drift here means the
// derived Commands() slice diverges from cliusage.go.
var wantCobraSubcommandMeta = []struct {
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

// A silent Short/args_hint mismatch makes the A1 linter false-positive or
// false-negative against artifact citations.
func TestCobraSubcommandMetadata(t *testing.T) {
	var repo string
	root := buildRootCmd(&repo)

	for _, w := range wantCobraSubcommandMeta {
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

// wantDelegatedSubcommandMeta mirrors cliusage.go byte-for-byte, for the same
// reason as wantCobraSubcommandMeta above.
var wantDelegatedSubcommandMeta = []struct {
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

// Covers the 3-level wiki bucket nesting.
func TestDelegatedSubcommandMetadata(t *testing.T) {
	var repo string
	root := buildRootCmd(&repo)

	for _, w := range wantDelegatedSubcommandMeta {
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

// The gate for the A1 linter. SetRoot is never called in tests, so Commands()
// returns the static table and serves as the golden. On failure, fix the Cobra
// side in main.go, not the golden. Comparison is set-for-set because VisitAll
// walks flags alphabetically while the golden does not, and the linter builds a
// map from them anyway.
func TestDeriveCommandsGolden(t *testing.T) {
	golden := cliusage.Commands()

	var repo string
	root := buildRootCmd(&repo)
	derived := cliusage.DeriveCommands(root)

	assertCommandSetsEqual(t, derived, golden)
}

// assertCommandSetsEqual compares paths, Args, Description and flag sets;
// flag order within a command is ignored.
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

// Catches a regression where Cobra re-adds the completion or help verbs that
// DisableDefaultCmd and SetHelpCommand suppress, or a verb slips in unnoticed.
func TestRootCmdExact22Verbs(t *testing.T) {
	var repoOverride string
	root := buildRootCmd(&repoOverride)

	want := []string{
		"bus", "claude", "code", "config", "docker", "docs", "doctor",
		"followups", "hooks", "migrate", "profile", "prompt", "reminder",
		"repl", "repo", "serve", "signals", "template", "update", "validate", "where", "wiki",
	}

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

	for _, name := range visible {
		if name == "completion" || name == "help" {
			t.Errorf("unexpected command leaked into top-level: %q", name)
		}
	}
}

// A path in the golden surface but absent from the live tree passes the A1
// linter while the command itself is unreachable. Paths come from
// cliusage.Commands(), so no count is hardcoded here.
func TestFindAllPaths(t *testing.T) {
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
