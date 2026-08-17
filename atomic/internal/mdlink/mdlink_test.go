package mdlink_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

func makeFile(t *testing.T, root, rel string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLinkify_NoResolvableTokens(t *testing.T) {
	dir := t.TempDir()
	content := "Run `atomic signals scan` or `git status` to refresh.\n"
	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	got := mdlink.Linkify(content, fileAbs, dir)
	if got != content {
		t.Errorf("content changed unexpectedly:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestLinkify_ResolvesExistingFile(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "See `agents/atomic-builder.md` for details.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	want := "See [`agents/atomic-builder.md`](../../agents/atomic-builder.md) for details.\n"
	if got != want {
		t.Errorf("link not emitted correctly:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestLinkify_SkipSet(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{".git", "node_modules", "dist", "build", "target", "vendor", ".worktrees", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	makeFile(t, dir, "node_modules/foo/index.js") // nested under a skip dir
	makeFile(t, dir, "agents/atomic-builder.md")  // real path — still links

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "skip `node_modules`, `.git`, `tmp`, `node_modules/foo/index.js` but link `agents/atomic-builder.md`.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	for _, plain := range []string{"`node_modules`", "`.git`", "`tmp`", "`node_modules/foo/index.js`"} {
		seg := strings.Trim(plain, "`")
		if !strings.Contains(got, plain) || strings.Contains(got, "]("+"../../"+seg) {
			t.Errorf("expected %s to remain plain text, got: %q", plain, got)
		}
	}
	if !strings.Contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("real path should still be linked, got: %q", got)
	}
}

// A quoted bare path character always stats clean, so disk resolution cannot
// reject it — and linking one aims the reader at the repo root or above.
func TestLinkify_DegenerateTokens(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "A specifier starting with `.`, `..`, or `/` is relative; see `agents/atomic-builder.md`.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	want := "A specifier starting with `.`, `..`, or `/` is relative; " +
		"see [`agents/atomic-builder.md`](../../agents/atomic-builder.md).\n"
	if got != want {
		t.Errorf("degenerate token linked:\ngot:  %q\nwant: %q", got, want)
	}
}

// The scanner reads a doubled backtick as a zero-width token that resolves to
// the base dir, which would shred the quoted string into three pieces.
func TestLinkify_DoubleBacktickSpan(t *testing.T) {
	dir := t.TempDir()

	fileAbs := filepath.Join(dir, "docs", "wiki", "doctor.md")
	content := "It prints ``not installed; run `atomic claude install`.`` and exits 0.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	if got != content {
		t.Errorf("double-backtick span was rewritten:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestLinkify_Idempotent(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "See `agents/atomic-builder.md` for details.\n"
	pass1 := mdlink.Linkify(content, fileAbs, dir)
	pass2 := mdlink.Linkify(pass1, fileAbs, dir)

	if pass1 != pass2 {
		t.Errorf("not idempotent:\npass1: %q\npass2: %q", pass1, pass2)
	}
}

func TestLinkify_FenceSkip(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "```\nSee `agents/atomic-builder.md` here.\n```\n"
	got := mdlink.Linkify(content, fileAbs, dir)
	if got != content {
		t.Errorf("fenced block content was modified:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestLinkify_FenceSkip_ProseAround(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "Before `agents/atomic-builder.md` stuff.\n```\nSee `agents/atomic-builder.md` inside.\n```\nAfter `agents/atomic-builder.md` end.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	if !contains(got, "```\nSee `agents/atomic-builder.md` inside.\n```") {
		t.Errorf("fenced content was modified:\n%q", got)
	}
	if !contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("prose outside fence was not linked:\n%q", got)
	}
}

func TestLinkify_AlreadyLinked(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "See [`agents/atomic-builder.md`](../../agents/atomic-builder.md).\n"
	got := mdlink.Linkify(content, fileAbs, dir)
	if got != content {
		t.Errorf("already-linked token was re-wrapped:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestLinkify_DirToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "See `agents/` for all agents.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	// filepath.Rel strips the trailing slash; what matters is that a link appeared.
	if !contains(got, "[`agents/`](../../agents") {
		t.Errorf("directory token not linked:\ngot: %q", got)
	}
}

// A domain file sits one level deeper than signals.md.
func TestLinkify_DepthFromDomainFile(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "atomic/internal/wiki/wiki.go")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals", "wiki.md")
	content := "Key file: `atomic/internal/wiki/wiki.go`\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	want := "Key file: [`atomic/internal/wiki/wiki.go`](../../../atomic/internal/wiki/wiki.go)\n"
	if got != want {
		t.Errorf("wrong relative path from domain file:\ngot:  %q\nwant: %q", got, want)
	}
}

// The emitted relative link is what the doctor re-joins against the repo root.
func TestLinkify_DomainDetailChain(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, ".claude/project/signals/auth.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	// The inferrer writes repo-root-relative tokens.
	content := "| auth | src/auth/ | auth desc | `.claude/project/signals/auth.md` |\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	if !contains(got, "[`.claude/project/signals/auth.md`](signals/auth.md)") {
		t.Errorf("domain detail chain link wrong:\ngot: %q", got)
	}
}

// Linkifying a table cell must not introduce pipes or break alignment.
func TestLinkify_TableCell(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "| domain | src/ | desc | `agents/atomic-builder.md` |\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	if !contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("table cell not linked:\ngot: %q", got)
	}
	if !contains(got, "| domain | src/ | desc |") {
		t.Errorf("table structure broken:\ngot: %q", got)
	}
}

// A 3-backtick run inside a 4-backtick fence is content, not a boundary. A bare
// bool toggle flips inFence there and exposes the inner block as prose.
func TestLinkify_NestedFence(t *testing.T) {
	dir := t.TempDir()
	// The token must exist on disk, or broken fence tracking proves nothing.
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")

	// The token appears before the inner block, between the inner lines, and in
	// prose below the outer fence — only the last may be linked.
	content := strings.Join([]string{
		"prose before",
		"````",
		"inner prose `agents/atomic-builder.md` should stay literal",
		"```",
		"`agents/atomic-builder.md` between inner fences also literal",
		"```",
		"more inner content `agents/atomic-builder.md` also literal",
		"````",
		"prose after `agents/atomic-builder.md` should be linked",
		"",
	}, "\n")

	got := mdlink.Linkify(content, fileAbs, dir)

	if !contains(got, "````\n") {
		t.Errorf("outer fence opener missing from output:\n%q", got)
	}

	if !contains(got, "inner prose `agents/atomic-builder.md` should stay literal") {
		t.Errorf("first inner token was linkified (before inner 3-backtick opener):\n%q", got)
	}
	// The line a bool toggle would expose as prose.
	if !contains(got, "`agents/atomic-builder.md` between inner fences also literal") {
		t.Errorf("token between inner fences was linkified (bool-toggle bug):\n%q", got)
	}
	if !contains(got, "more inner content `agents/atomic-builder.md` also literal") {
		t.Errorf("third inner token was linkified:\n%q", got)
	}

	wantLink := "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)"
	if !contains(got, wantLink) {
		t.Errorf("prose token after outer fence was NOT linkified:\ngot: %q\nwant substring: %q", got, wantLink)
	}

	wantAfterLine := "prose after " + wantLink + " should be linked"
	if !contains(got, wantAfterLine) {
		t.Errorf("prose-after line has wrong form:\ngot: %q\nwant substring: %q", got, wantAfterLine)
	}
}

func TestLinkify_TildeFence(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "~~~\nSee `agents/atomic-builder.md` inside tilde fence.\n~~~\nAfter `agents/atomic-builder.md` end.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	if !contains(got, "~~~\nSee `agents/atomic-builder.md` inside tilde fence.\n~~~") {
		t.Errorf("tilde fence content was modified:\n%q", got)
	}
	if !contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("prose outside tilde fence was not linked:\n%q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
