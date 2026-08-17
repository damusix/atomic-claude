package wiki

// action_test.go — tests for the shared argument scanner hardening
// (resolveWikiRoot / parseBucketDocArgs) behind `atomic wiki bucket`, plus
// wikiStampAction's flag/positional argument order handling.
//
// Covers: -h/-help/--help yield errUsageRequested and print usage without
// mutating state (`bucket add -h` used to silently create a
// bucket named "-h"); an unrecognized single-dash token is rejected with
// the same parity as an unrecognized double-dash token; parseBucketDocArgs'
// collapse to delegate at resolveWikiRoot still honors --router in any
// position; wikiStampAction accepts flags before, after, or interspersed
// with the positional <file> argument, and honors "--" as a global
// terminator ending flag parsing.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- resolveWikiRoot: help sentinel ----

func TestResolveWikiRoot_HelpTokensYieldSentinel(t *testing.T) {
	cwd := t.TempDir()
	for _, tok := range []string{"-h", "-help", "--help"} {
		t.Run(tok, func(t *testing.T) {
			_, positional, err := resolveWikiRoot([]string{tok}, cwd)
			if !errors.Is(err, errUsageRequested) {
				t.Fatalf("resolveWikiRoot(%q): got err %v, want errUsageRequested", tok, err)
			}
			if positional != nil {
				t.Errorf("resolveWikiRoot(%q): expected nil positional, got %v", tok, positional)
			}
		})
	}
}

func TestResolveWikiRoot_HelpTokenAnyPosition(t *testing.T) {
	cwd := t.TempDir()
	_, _, err := resolveWikiRoot([]string{"--root=" + cwd, "mybucket", "-h"}, cwd)
	if !errors.Is(err, errUsageRequested) {
		t.Fatalf("expected errUsageRequested after --root and a positional, got %v", err)
	}
}

// ---- resolveWikiRoot: unrecognized dash tokens ----

func TestResolveWikiRoot_UnrecognizedSingleDashRejected(t *testing.T) {
	cwd := t.TempDir()
	_, positional, err := resolveWikiRoot([]string{"-x"}, cwd)
	if err == nil {
		t.Fatalf("expected error for unrecognized single-dash token, got positional %v", positional)
	}
	if errors.Is(err, errUsageRequested) {
		t.Fatal("unrecognized token must not be classified as a help request")
	}
	if !strings.Contains(err.Error(), `"-x"`) {
		t.Errorf("expected error to name the token, got: %v", err)
	}
}

func TestResolveWikiRoot_UnrecognizedDoubleDashRejected(t *testing.T) {
	cwd := t.TempDir()
	_, _, err := resolveWikiRoot([]string{"--bogus"}, cwd)
	if err == nil {
		t.Fatal("expected error for unrecognized double-dash token, got nil")
	}
	if !strings.Contains(err.Error(), `"--bogus"`) {
		t.Errorf("expected error to name the token, got: %v", err)
	}
}

func TestResolveWikiRoot_UnrecognizedDashTokenDoesNotLeakIntoPositional(t *testing.T) {
	cwd := t.TempDir()
	// This is the exact shape of the #164 defect: a single-dash token after a
	// positional must be rejected, not appended to positional as a second name.
	_, positional, err := resolveWikiRoot([]string{"mybucket", "-x"}, cwd)
	if err == nil {
		t.Fatalf("expected rejection, got positional %v", positional)
	}
}

// ---- parseBucketDocArgs: delegates to resolveWikiRoot ----

func TestParseBucketDocArgs_HelpSentinel(t *testing.T) {
	cwd := t.TempDir()
	for _, tok := range []string{"-h", "-help", "--help"} {
		t.Run(tok, func(t *testing.T) {
			_, _, router, err := parseBucketDocArgs([]string{tok}, cwd)
			if !errors.Is(err, errUsageRequested) {
				t.Fatalf("parseBucketDocArgs(%q): got err %v, want errUsageRequested", tok, err)
			}
			if router {
				t.Errorf("parseBucketDocArgs(%q): router should be false on help sentinel", tok)
			}
		})
	}
}

func TestParseBucketDocArgs_UnrecognizedSingleDashRejected(t *testing.T) {
	cwd := t.TempDir()
	_, _, _, err := parseBucketDocArgs([]string{"-x"}, cwd)
	if err == nil || errors.Is(err, errUsageRequested) {
		t.Fatalf("expected non-sentinel rejection for -x, got %v", err)
	}
}

func TestParseBucketDocArgs_RouterAnyPosition(t *testing.T) {
	cwd := t.TempDir()
	cases := [][]string{
		{"--router", "mybucket", "slug"},
		{"mybucket", "--router", "slug"},
		{"mybucket", "slug", "--router"},
		{"--root=" + cwd, "mybucket", "slug", "--router"},
		{"mybucket", "--router", "slug", "--root=" + cwd},
	}
	for _, args := range cases {
		absRoot, positional, router, err := parseBucketDocArgs(args, cwd)
		if err != nil {
			t.Fatalf("args %v: unexpected error: %v", args, err)
		}
		if !router {
			t.Errorf("args %v: expected --router honored, got false", args)
		}
		if len(positional) != 2 || positional[0] != "mybucket" || positional[1] != "slug" {
			t.Errorf("args %v: expected positional [mybucket slug], got %v", args, positional)
		}
		if absRoot == "" {
			t.Errorf("args %v: expected resolved root", args)
		}
	}
}

// TestParseBucketDocArgs_RouterConsumedBeforeRootValue pins a deliberate
// behavior delta flagged in review: parseBucketDocArgs strips every
// literal "--router" token before delegating to resolveWikiRoot, so
// `--root --router` reaches resolveWikiRoot as bare "--root" and errors
// "flag --root requires a value" rather than silently taking "--router" as
// the root path. A root path is never legitimately named "--router", so
// erroring is correct — this is a pinned decision, not a regression.
func TestParseBucketDocArgs_RouterConsumedBeforeRootValue(t *testing.T) {
	cwd := t.TempDir()
	_, _, _, err := parseBucketDocArgs([]string{"--root", "--router"}, cwd)
	if err == nil {
		t.Fatal("expected error for --root immediately followed by --router, got nil")
	}
	if errors.Is(err, errUsageRequested) {
		t.Fatal("expected a value-required error, not the help sentinel")
	}
	if !strings.Contains(err.Error(), "flag --root requires a value") {
		t.Errorf("expected \"flag --root requires a value\", got: %v", err)
	}
}

func TestParseBucketDocArgs_RouterAbsentDefaultsFalse(t *testing.T) {
	cwd := t.TempDir()
	_, positional, router, err := parseBucketDocArgs([]string{"mybucket", "slug"}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if router {
		t.Error("expected router false when --router absent")
	}
	if len(positional) != 2 {
		t.Errorf("expected 2 positionals, got %v", positional)
	}
}

// ---- wikiAction integration: help probe across all seven bucket verbs ----

// TestBucketVerbs_HelpFlagPrintsUsageAndExitsZero verifies -h, -help, and
// --help all print usage to out and exit 0 for every bucket sub-verb.
func TestBucketVerbs_HelpFlagPrintsUsageAndExitsZero(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	verbs := []string{"add", "list", "diff", "promote", "doc", "skill", "index"}
	helpTokens := []string{"-h", "-help", "--help"}

	for _, verb := range verbs {
		for _, tok := range helpTokens {
			t.Run(verb+"/"+tok, func(t *testing.T) {
				var out bytes.Buffer
				code := wikiAction([]string{"bucket", verb, "--root=" + root, tok}, claudeHome, root, &out)
				if code != 0 {
					t.Fatalf("wikiAction bucket %s %s: got exit %d, want 0; output: %q", verb, tok, code, out.String())
				}
				if !strings.Contains(out.String(), "Usage:") {
					t.Errorf("wikiAction bucket %s %s: expected usage text on out, got: %q", verb, tok, out.String())
				}
			})
		}
	}
}

// TestBucketAdd_HelpProbeCreatesNothing reproduces that directly:
// `atomic wiki bucket add -h` must not create a bucket named "-h" (or
// anything else).
func TestBucketAdd_HelpProbeCreatesNothing(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "-h"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("expected exit 0 for -h, got %d; output: %q", code, out.String())
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected usage text on out, got: %q", out.String())
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected no side effects from -h probe, found: %v", names)
	}
}

// TestBucketAdd_DoubleDashHelpProbeCreatesNothing is the --help counterpart.
func TestBucketAdd_DoubleDashHelpProbeCreatesNothing(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()

	var out bytes.Buffer
	code := wikiAction([]string{"bucket", "add", "--help"}, claudeHome, root, &out)
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d; output: %q", code, out.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no side effects from --help probe, found %d entries", len(entries))
	}
}

// TestBucketVerbs_UnrecognizedSingleDashTokenExits2 verifies parity with the
// existing double-dash rejection: an unrecognized single-dash token is
// rejected with exit 2 across every bucket sub-verb.
func TestBucketVerbs_UnrecognizedSingleDashTokenExits2(t *testing.T) {
	root, _, wikiDir := setupBucketCLIRoot(t)
	claudeHome := t.TempDir()
	writeBucketCLIFile(t, filepath.Join(wikiDir, "index.md"), "# Wiki\n")

	verbs := []string{"add", "list", "diff", "promote", "doc", "skill", "index"}
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			var out bytes.Buffer
			code := wikiAction([]string{"bucket", verb, "--root=" + root, "-x"}, claudeHome, root, &out)
			if code != 2 {
				t.Errorf("wikiAction bucket %s -x: got exit %d, want 2; output: %q", verb, code, out.String())
			}
		})
	}
}

// ---- wikiStampAction: flag/positional argument order ----
//
// Stdlib flag.FlagSet stops parsing at the first non-flag argument, so the
// documented `atomic wiki stamp <file> --repo <path>` form (flags after the
// positional) never consumed the trailing flags and fell through to the
// "supply either --repo ..." error. wikiStampAction must accept flags in any
// position — flags-first, flags-after-path, and interspersed.

// makeStampGitRepo creates a git repo with one commit and returns its dir.
func makeStampGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "t@t.com"},
		{"git", "-C", dir, "config", "user.name", "T"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	addCmds := [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "init"},
	}
	for _, c := range addCmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return dir
}

// TestWikiStampAction_FlagOrder is a table test over the three stamp modes
// (summary / concern / knowledge) crossed with three argument orders
// (flags-first, flags-after-path, interspersed). All nine combinations must
// exit 0 and actually write the expected frontmatter key.
func TestWikiStampAction_FlagOrder(t *testing.T) {
	repoDir := makeStampGitRepo(t)

	knowledgeRoot := t.TempDir()
	citedSignalsDir := filepath.Join(knowledgeRoot, "cited-repo", ".claude", "project")
	if err := os.MkdirAll(citedSignalsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(citedSignalsDir, "signals.md"), []byte("signals\n"), 0o644); err != nil {
		t.Fatalf("write signals.md: %v", err)
	}

	type modeCase struct {
		name     string
		pairs    [][]string // each element is one complete flag unit (name, or name+value) — never split
		wantKey  string
		fileName string // must be kebab-case for knowledge mode
	}
	modes := []modeCase{
		{name: "summary", pairs: [][]string{{"--repo", repoDir}}, wantKey: "reflects_rev", fileName: "target.md"},
		{name: "concern", pairs: [][]string{{"--root", knowledgeRoot}, {"--cites", "cited-repo"}}, wantKey: "reflects", fileName: "target.md"},
		{name: "knowledge", pairs: [][]string{{"--knowledge"}, {"--sources", "research/notes.md@abc123"}}, wantKey: "sources", fileName: "topic.md"},
	}

	flatten := func(pairs [][]string) []string {
		var out []string
		for _, p := range pairs {
			out = append(out, p...)
		}
		return out
	}

	orders := []struct {
		name  string
		build func(file string, pairs [][]string) []string
	}{
		{name: "flags-first", build: func(file string, pairs [][]string) []string {
			return append(flatten(pairs), file)
		}},
		{name: "flags-after-path", build: func(file string, pairs [][]string) []string {
			return append([]string{file}, flatten(pairs)...)
		}},
		{name: "interspersed", build: func(file string, pairs [][]string) []string {
			// Split at a pair boundary — never inside a flag/value pair,
			// which would otherwise swallow the positional as the flag's value.
			mid := len(pairs) / 2
			out := flatten(pairs[:mid])
			out = append(out, file)
			out = append(out, flatten(pairs[mid:])...)
			return out
		}},
	}

	for _, m := range modes {
		for _, o := range orders {
			t.Run(m.name+"/"+o.name, func(t *testing.T) {
				dir := t.TempDir()
				file := filepath.Join(dir, m.fileName)
				if err := os.WriteFile(file, []byte("---\ntitle: x\n---\nbody\n"), 0o644); err != nil {
					t.Fatalf("write %s: %v", file, err)
				}

				args := o.build(file, m.pairs)
				code := wikiStampAction(args)
				if code != 0 {
					t.Fatalf("wikiStampAction(%v) = %d, want 0", args, code)
				}

				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatalf("read %s: %v", file, err)
				}
				if !strings.Contains(string(data), m.wantKey+":") {
					t.Errorf("expected %q key written to %s; got:\n%s", m.wantKey, file, data)
				}
			})
		}
	}
}

// TestWikiStampAction_MissingModeFlagsStillErrors verifies a genuinely
// missing mode flag (no --repo/--root+--cites/--knowledge+--sources) still
// produces the existing "supply either" error and exit 1 — the flag/position
// fix must not mask real usage errors.
func TestWikiStampAction_MissingModeFlagsStillErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "target.md")
	if err := os.WriteFile(file, []byte("---\ntitle: x\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}

	code := wikiStampAction([]string{file})
	if code != 1 {
		t.Fatalf("wikiStampAction with no mode flags: exit %d, want 1", code)
	}
}

// TestWikiStampAction_TerminatorEndsFlagParsing pins POSIX "--" terminator
// semantics: the re-parse loop must honor the first
// bare "--" globally, not just within the single fs.Parse call that consumes
// it. Everything after "--" is positional, verbatim, never re-parsed as a
// flag — so `wiki stamp -- <file> --repo <repo>` must NOT stamp.
func TestWikiStampAction_TerminatorEndsFlagParsing(t *testing.T) {
	repoDir := makeStampGitRepo(t)

	t.Run("post-terminator flags are literal, no mode set", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "target.md")
		if err := os.WriteFile(file, []byte("---\ntitle: x\n---\nbody\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}

		code := wikiStampAction([]string{"--", file, "--repo", repoDir})
		if code != 1 {
			t.Fatalf("wikiStampAction(-- %s --repo %s) = %d, want 1 (--repo after -- must be literal)", file, repoDir, code)
		}

		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(data), "reflects_rev:") {
			t.Errorf("expected no stamp written after literal --repo, got:\n%s", data)
		}
	})

	t.Run("flags before terminator still parse, positional after stamps", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "target.md")
		if err := os.WriteFile(file, []byte("---\ntitle: x\n---\nbody\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}

		code := wikiStampAction([]string{"--repo", repoDir, "--", file})
		if code != 0 {
			t.Fatalf("wikiStampAction(--repo %s -- %s) = %d, want 0", repoDir, file, code)
		}

		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if !strings.Contains(string(data), "reflects_rev:") {
			t.Errorf("expected reflects_rev stamp written, got:\n%s", data)
		}
	})
}
