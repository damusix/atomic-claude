package cliusage_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/cliusage"
)

// TestCommandsNotEmpty verifies the surface table is non-empty and that
// every Command has a non-empty verb path and description — failing on the
// zero-value struct would indicate a transcription error.
func TestCommandsNotEmpty(t *testing.T) {
	cmds := cliusage.Commands()
	if len(cmds) == 0 {
		t.Fatal("Commands() returned empty slice")
	}
	for i, c := range cmds {
		if len(c.Path) == 0 {
			t.Errorf("command[%d] has empty Path", i)
		}
		if c.Description == "" {
			t.Errorf("command[%d] %v has empty Description", i, c.Path)
		}
	}
}

// TestRenderContainsAllVerbPaths verifies that every verb-path in the surface
// table appears in the rendered Commands block. This proves the renderer walks
// the full table — any omission would be a regression visible in --help.
func TestRenderContainsAllVerbPaths(t *testing.T) {
	output := renderToString(t)
	for _, c := range cliusage.Commands() {
		want := strings.Join(c.Path, " ")
		if !strings.Contains(output, want) {
			t.Errorf("rendered Commands block missing verb-path %q", want)
		}
	}
}

// TestRenderContainsAllFlags verifies every flag listed in the surface table
// appears in the rendered block. Guards the --format-json class of authoring bug.
func TestRenderContainsAllFlags(t *testing.T) {
	output := renderToString(t)
	for _, c := range cliusage.Commands() {
		for _, f := range c.Flags {
			if !strings.Contains(output, f) {
				t.Errorf("command %v: rendered Commands block missing flag %q", c.Path, f)
			}
		}
	}
}

// TestRenderRepresentativeSurface asserts specific verb-path+flag combos that
// a future Checkpoint 2 scanner will query. These encode the WHY: if someone
// accidentally removes a flag from the table, the CP-2 validator would silently
// start passing wrong citations.
func TestRenderRepresentativeSurface(t *testing.T) {
	output := renderToString(t)

	cases := []struct {
		desc   string
		substr string
	}{
		// multi-word path, JSON output flag
		{"code search --json present", "code search"},
		{"code search --limit present", "--limit"},
		// flag-heavy verb
		{"doctor --fix present", "--fix"},
		{"doctor --json present", "--json"},
		{"doctor --only present", "--only"},
		{"doctor --skip present", "--skip"},
		{"doctor --stale-days present", "--stale-days"},
		{"doctor --verbose present", "--verbose"},
		// validate line present
		{"validate verb present", "validate"},
		// claude install flags
		{"claude install present", "claude install"},
		{"claude install --dry-run present", "--dry-run"},
		{"claude install --target present", "--target"},
		// multi-word paths
		{"code callers present", "code callers"},
		{"code callees present", "code callees"},
		{"code impact present", "code impact"},
		{"code explore present", "code explore"},
		{"signals scan present", "signals scan"},
		{"signals scan --out present", "--out"},
		{"wiki scan present", "wiki scan"},
		{"hooks session-start present", "hooks session-start"},
		{"followups add present", "followups add"},
	}

	for _, tc := range cases {
		if !strings.Contains(output, tc.substr) {
			t.Errorf("%s: substring %q not found in rendered Commands block", tc.desc, tc.substr)
		}
	}
}

// goldenCommandsBlock is the expected rendered Commands block. Any change to
// the surface table (flags added/removed, descriptions edited, verb-path
// added/renamed) must be reflected here. Update by running:
//
//	go run ./tmp/golden_print/main.go   (in atomic/)
//
// and pasting the printed string. The golden pins the exact output so that a
// dropped or renamed flag is a test failure, not a silent drift.
const goldenCommandsBlock = "" +
	"  claude install       [--dry-run] [--target] [--no-hooks]                                     Install artifact bundle\n" +
	"  claude update        [--dry-run] [--target] [--no-hooks]                                     Update artifact bundle\n" +
	"  claude list                                                                                  List bundled artifacts\n" +
	"  claude diff          [--target]                                                              Diff bundle vs on-disk\n" +
	"  claude uninstall     [--target]                                                              Generate uninstall prompt\n" +
	"  config get           <key>                                                                   Print resolved config value\n" +
	"  config set           <key> <val>                                                             Set config value; re-renders config.resolved.md\n" +
	"  config unset         <key>                                                                   Revert key to built-in default\n" +
	"  config list          [--json]                                                                List all resolved key=value pairs\n" +
	"  config path                                                                                  Print path to config.toml\n" +
	"  docker init          [--target] [--force]                                                    Scaffold Docker eval environment\n" +
	"  doctor               [--fix] [--json] [--only] [--skip] [--stale-days] [--verbose]           Integrity check\n" +
	"  hooks session-start  [--format]                                                              Print session-start hook payload\n" +
	"  hooks install        [--scope]                                                               Install session-start hook\n" +
	"  hooks uninstall      [--scope]                                                               Remove session-start hook\n" +
	"  reminder add         <text> [--due] [--transport]                                            Create a reminder file; prints assigned id\n" +
	"  reminder list                                                                                List all reminders\n" +
	"  reminder show        <id>                                                                    Print body of a reminder\n" +
	"  reminder rm          <id>                                                                    Delete a reminder\n" +
	"  signals scan         [--out]                                                                 Walk repo and write deterministic-signals.md\n" +
	"  signals show                                                                                 Print deterministic-signals.md to stdout\n" +
	"  signals stale                                                                                Exit 0 fresh, 1 stale, 2 error\n" +
	"  signals diff                                                                                 Print unified diff of signals file\n" +
	"  signals linkify                                                                              Linkify path tokens in signals.md and signals/*.md\n" +
	"  update               [--check] [--channel] [--no-doctor] [--skip-claude-update]              Self-update the atomic binary, then refresh ~/.claude artifacts\n" +
	"  followups list       [--stale] [--json]                                                      List open follow-up entries\n" +
	"  followups add        [--id] [--title] [--kind] [--severity] [--origin] [--file] [--body]     Create entry\n" +
	"  followups close      <id> [--reason]                                                         Close an entry\n" +
	"  followups render                                                                             Regenerate INDEX.md\n" +
	"  followups path                                                                               Print followups folder path\n" +
	"  validate             [flags] [spec|config|bundle|artifacts] [paths...] [--json] [--suggest]  Lint repo artifacts\n" +
	"  docs scan                                                                                    Scan docs and write doc-surfaces.md\n" +
	"  docs stale                                                                                   Exit 0 fresh, 1 stale, 2 error\n" +
	"  profile refresh      [--if-stale]                                                            Refresh ## Environment in profile.md\n" +
	"  code index           [--profile] [--only] [--exclude]                                        Index all source files\n" +
	"  code sync                                                                                    Incrementally re-index changed files\n" +
	"  code status          [--json]                                                                Show index status\n" +
	"  code search          <query> [--json] [--limit] [--only] [--exclude]                         Search indexed nodes\n" +
	"  code callers         <symbol> [--depth] [--json] [--only] [--exclude]                        Find callers of symbol\n" +
	"  code callees         <symbol> [--depth] [--json] [--only] [--exclude]                        Find callees of symbol\n" +
	"  code impact          <symbol> [--depth] [--json] [--only] [--exclude]                        Find impact radius of symbol\n" +
	"  code node            <symbol> [--file] [--line] [--json]                                     Show node detail\n" +
	"  code files           [pattern] [--json]                                                      List indexed files\n" +
	"  code affected        [--depth] [--test-glob] [--stdin] [--json]                              Find affected test files\n" +
	"  code explore         <query> [--json] [--only] [--exclude]                                   Gather context for a query\n" +
	"  wiki scan            [--root]                                                                Scaffold wiki/, scan repos, register in ~/.claude/CLAUDE.md\n" +
	"  wiki stale           [--root]                                                                Exit 0 fresh, 1 stale, 2 error (DRIFT/STALE lines on stdout)\n" +
	"  wiki linkify         [--root]                                                                Linkify path tokens in wiki artifacts in-place\n" +
	"  wiki bucket add      <name> [--root]                                                         Register a capture bucket; create index.md stub and manifest dir\n" +
	"  wiki bucket list     [--root]                                                                List registered buckets with baseline count and pending/fresh status\n" +
	"  wiki bucket diff     <name> [--root]                                                         Print new/changed/removed files vs baseline; exit 0 empty, 1 non-empty\n" +
	"  wiki bucket promote  <name> [--root]                                                         Snapshot bucket and rotate baseline→previous, current→baseline\n" +
	"  prompt git-cleanup                                                                           Emit the git-cleanup cold-op brief\n" +
	"  prompt claude-merge                                                                          Emit the CLAUDE.md merge cold-op brief\n"

// TestRenderGolden pins the rendered Commands block to the exact expected
// string. A dropped flag, renamed verb, or formatting change is a test
// failure. To update after an intentional table edit, regenerate the golden
// by running `go run ./tmp/golden_print/main.go` from atomic/ and updating
// the goldenCommandsBlock constant above.
func TestRenderGolden(t *testing.T) {
	output := renderToString(t)
	if output != goldenCommandsBlock {
		// Produce a line-level diff so the failure is actionable.
		gotLines := strings.Split(output, "\n")
		wantLines := strings.Split(goldenCommandsBlock, "\n")
		maxLen := len(gotLines)
		if len(wantLines) > maxLen {
			maxLen = len(wantLines)
		}
		for i := 0; i < maxLen; i++ {
			got := ""
			want := ""
			if i < len(gotLines) {
				got = gotLines[i]
			}
			if i < len(wantLines) {
				want = wantLines[i]
			}
			if got != want {
				t.Errorf("line %d mismatch:\n  got:  %q\n  want: %q", i+1, got, want)
			}
		}
		if !t.Failed() {
			// Lengths differ but all shared lines matched.
			t.Errorf("output length differs: got %d lines, want %d lines", len(gotLines), len(wantLines))
		}
	}
	// Structural invariants (belt-and-suspenders alongside the golden).
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		t.Error("rendered Commands block starts with a blank line")
	}
	for i, l := range lines {
		if l == "" {
			t.Errorf("rendered Commands block has empty line at index %d", i)
		}
	}
	if len(lines) != len(cliusage.Commands()) {
		t.Errorf("rendered line count %d != Commands() count %d", len(lines), len(cliusage.Commands()))
	}
}

// TestLookupByPath verifies LookupByPath finds known commands and returns nil
// for unknown paths.
func TestLookupByPath(t *testing.T) {
	got := cliusage.LookupByPath([]string{"code", "search"})
	if got == nil {
		t.Fatal("LookupByPath([code search]) returned nil")
	}
	if !containsFlag(got.Flags, "--json") {
		t.Errorf("code search: expected --json flag, got %v", got.Flags)
	}

	got2 := cliusage.LookupByPath([]string{"doctor"})
	if got2 == nil {
		t.Fatal("LookupByPath([doctor]) returned nil")
	}
	if !containsFlag(got2.Flags, "--fix") {
		t.Errorf("doctor: expected --fix flag, got %v", got2.Flags)
	}

	// Unknown path returns nil.
	if cliusage.LookupByPath([]string{"nonexistent", "verb"}) != nil {
		t.Error("LookupByPath([nonexistent verb]) should return nil")
	}
}

// TestTopLevelVerbs verifies TopLevelVerbs returns a non-empty set covering
// the documented top-level nouns.
func TestTopLevelVerbs(t *testing.T) {
	verbs := cliusage.TopLevelVerbs()
	required := []string{"code", "signals", "validate", "wiki", "followups", "claude", "config", "docs", "doctor", "update", "profile", "hooks", "reminder", "docker", "prompt"}
	for _, v := range required {
		if !verbs[v] {
			t.Errorf("TopLevelVerbs missing %q", v)
		}
	}
}

// TestCodeFanOutVerbs_HaveOnlyExclude verifies SC 9: the six fan-out code verbs
// carry --only and --exclude, and no code verb carries --db (which was
// explicitly rejected in the spec).
func TestCodeFanOutVerbs_HaveOnlyExclude(t *testing.T) {
	fanOutVerbs := [][]string{
		{"code", "index"},
		{"code", "search"},
		{"code", "callers"},
		{"code", "callees"},
		{"code", "impact"},
		{"code", "explore"},
	}

	for _, path := range fanOutVerbs {
		cmd := cliusage.LookupByPath(path)
		if cmd == nil {
			t.Errorf("LookupByPath(%v) returned nil", path)
			continue
		}
		if !containsFlag(cmd.Flags, "--only") {
			t.Errorf("%v: missing --only flag; got %v", path, cmd.Flags)
		}
		if !containsFlag(cmd.Flags, "--exclude") {
			t.Errorf("%v: missing --exclude flag; got %v", path, cmd.Flags)
		}
		if containsFlag(cmd.Flags, "--db") {
			t.Errorf("%v: must NOT have --db flag (rejected in spec); got %v", path, cmd.Flags)
		}
	}
}

// TestCodeNonFanOutVerbs_NoOnlyExclude verifies that the non-fan-out code verbs
// (sync, status, node, files, affected) do NOT carry --only/--exclude, since
// they don't fan out across realm members.
func TestCodeNonFanOutVerbs_NoOnlyExclude(t *testing.T) {
	nonFanOutVerbs := [][]string{
		{"code", "sync"},
		{"code", "status"},
		{"code", "node"},
		{"code", "files"},
		{"code", "affected"},
	}

	for _, path := range nonFanOutVerbs {
		cmd := cliusage.LookupByPath(path)
		if cmd == nil {
			t.Errorf("LookupByPath(%v) returned nil", path)
			continue
		}
		if containsFlag(cmd.Flags, "--only") {
			t.Errorf("%v: should NOT have --only (non-fan-out verb); got %v", path, cmd.Flags)
		}
		if containsFlag(cmd.Flags, "--exclude") {
			t.Errorf("%v: should NOT have --exclude (non-fan-out verb); got %v", path, cmd.Flags)
		}
	}
}

// helpers

func renderToString(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	cliusage.RenderCommandsBlock(&sb)
	return sb.String()
}

func containsFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}
