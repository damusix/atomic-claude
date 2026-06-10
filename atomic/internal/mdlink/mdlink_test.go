package mdlink_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// makeFile creates a file at root/rel with content (creating parent dirs).
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

// TestLinkify_NoResolvableTokens verifies prose with no matching disk paths is unchanged.
func TestLinkify_NoResolvableTokens(t *testing.T) {
	dir := t.TempDir()
	content := "Run `atomic signals scan` or `git status` to refresh.\n"
	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	got := mdlink.Linkify(content, fileAbs, dir)
	if got != content {
		t.Errorf("content changed unexpectedly:\ngot:  %q\nwant: %q", got, content)
	}
}

// TestLinkify_ResolvesExistingFile verifies a token that resolves to a real file is linked.
func TestLinkify_ResolvesExistingFile(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	// File at .claude/project/signals.md, base=dir
	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "See `agents/atomic-builder.md` for details.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	// Rel from .claude/project/ to agents/atomic-builder.md = ../../agents/atomic-builder.md
	want := "See [`agents/atomic-builder.md`](../../agents/atomic-builder.md) for details.\n"
	if got != want {
		t.Errorf("link not emitted correctly:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestLinkify_SkipSet verifies junk dirs that resolve on disk are never linked.
func TestLinkify_SkipSet(t *testing.T) {
	dir := t.TempDir()
	// All of these exist on disk but must stay plain text.
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

// TestLinkify_Idempotent verifies re-running on already-linkified content is byte-identical.
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

// TestLinkify_FenceSkip verifies content inside fenced code blocks is not linkified.
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

// TestLinkify_FenceSkip_ProseAround verifies prose outside a fence is linked but inside is not.
func TestLinkify_FenceSkip_ProseAround(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "Before `agents/atomic-builder.md` stuff.\n```\nSee `agents/atomic-builder.md` inside.\n```\nAfter `agents/atomic-builder.md` end.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	// Inside the fence must not be linked.
	if !contains(got, "```\nSee `agents/atomic-builder.md` inside.\n```") {
		t.Errorf("fenced content was modified:\n%q", got)
	}
	// Outside the fence must be linked.
	if !contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("prose outside fence was not linked:\n%q", got)
	}
}

// TestLinkify_AlreadyLinked verifies a token already in markdown link syntax is skipped.
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

// TestLinkify_DirToken verifies a token that resolves to a directory is also linked.
func TestLinkify_DirToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "See `agents/` for all agents.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	// filepath.Rel strips the trailing slash; the important thing is the link was emitted.
	if !contains(got, "[`agents/`](../../agents") {
		t.Errorf("directory token not linked:\ngot: %q", got)
	}
}

// TestLinkify_DepthFromDomainFile verifies correct relative path from a domain file
// under .claude/project/signals/ (one level deeper than signals.md itself).
func TestLinkify_DepthFromDomainFile(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "atomic/internal/wiki/wiki.go")

	// Domain file at .claude/project/signals/wiki.md, base=dir
	fileAbs := filepath.Join(dir, ".claude", "project", "signals", "wiki.md")
	content := "Key file: `atomic/internal/wiki/wiki.go`\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	// Rel from .claude/project/signals/ to atomic/internal/wiki/wiki.go
	// = ../../../atomic/internal/wiki/wiki.go
	want := "Key file: [`atomic/internal/wiki/wiki.go`](../../../atomic/internal/wiki/wiki.go)\n"
	if got != want {
		t.Errorf("wrong relative path from domain file:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestLinkify_DomainDetailChain verifies the exact chain the spec requires:
// token ".claude/project/signals/auth.md" in signals.md links to "signals/auth.md"
// (which is what the doctor extracts and joins as root/.claude/project/signals/auth.md).
func TestLinkify_DomainDetailChain(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, ".claude/project/signals/auth.md")

	// signals.md is at .claude/project/signals.md
	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	// Token as the inferrer writes it: repo-root-relative path
	content := "| auth | src/auth/ | auth desc | `.claude/project/signals/auth.md` |\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	// Rel from .claude/project/ to .claude/project/signals/auth.md = signals/auth.md
	if !contains(got, "[`.claude/project/signals/auth.md`](signals/auth.md)") {
		t.Errorf("domain detail chain link wrong:\ngot: %q", got)
	}
}

// TestLinkify_TableCell verifies linkify handles pipe-table rows correctly
// (doesn't break table syntax by inserting extra pipes or corrupting alignment).
func TestLinkify_TableCell(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "| domain | src/ | desc | `agents/atomic-builder.md` |\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	if !contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("table cell not linked:\ngot: %q", got)
	}
	// Table row structure must still be intact
	if !contains(got, "| domain | src/ | desc |") {
		t.Errorf("table structure broken:\ngot: %q", got)
	}
}

// TestLinkify_NestedFence verifies that a 4-backtick outer fence containing a
// 3-backtick inner block is handled correctly per CommonMark rules:
//   - The inner 3-backtick lines are NOT treated as fence boundaries.
//   - Content inside the outer fence (including the inner block) is left literal.
//   - A link-eligible token OUTSIDE all fences IS linkified.
//
// Pre-fix, the bare bool toggle flips inFence on the inner 3-backtick lines,
// so the inner content gets linkified — this test catches that regression.
func TestLinkify_NestedFence(t *testing.T) {
	dir := t.TempDir()
	// agents/atomic-builder.md must exist on disk so Linkify would linkify it
	// if fence tracking is broken.
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")

	// Outer fence: 4 backticks. Inner block: 3 backticks.
	// `agents/atomic-builder.md` appears three times:
	//   (a) inside the outer fence before the inner block (must stay literal)
	//   (b) BETWEEN the inner 3-backtick lines (must stay literal — this is where
	//       the bool-toggle bug exposes content: inFence flips false on the inner
	//       opener, so content between the inner ``` lines is treated as prose)
	//   (c) in prose below the outer fence (must be linked)
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

	// The outer fence lines must be preserved verbatim.
	if !contains(got, "````\n") {
		t.Errorf("outer fence opener missing from output:\n%q", got)
	}

	// All three interior occurrences must stay literal (plain backtick spans).
	// (a) before the inner 3-backtick opener
	if !contains(got, "inner prose `agents/atomic-builder.md` should stay literal") {
		t.Errorf("first inner token was linkified (before inner 3-backtick opener):\n%q", got)
	}
	// (b) BETWEEN the inner 3-backtick lines — this is the line the bool-toggle
	//     bug exposes as prose (inFence flips false on the inner ``` opener).
	if !contains(got, "`agents/atomic-builder.md` between inner fences also literal") {
		t.Errorf("token between inner fences was linkified (bool-toggle bug):\n%q", got)
	}
	// (c) after the inner 3-backtick closer, still inside the outer fence
	if !contains(got, "more inner content `agents/atomic-builder.md` also literal") {
		t.Errorf("third inner token was linkified:\n%q", got)
	}

	// Token OUTSIDE the fence must be linkified.
	wantLink := "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)"
	if !contains(got, wantLink) {
		t.Errorf("prose token after outer fence was NOT linkified:\ngot: %q\nwant substring: %q", got, wantLink)
	}

	// The prose-after line must contain the link, not the plain token.
	wantAfterLine := "prose after " + wantLink + " should be linked"
	if !contains(got, wantAfterLine) {
		t.Errorf("prose-after line has wrong form:\ngot: %q\nwant substring: %q", got, wantAfterLine)
	}
}

// TestLinkify_TildeFence verifies that tilde fences (~~~) are also tracked and
// their contents are not linkified.
func TestLinkify_TildeFence(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "~~~\nSee `agents/atomic-builder.md` inside tilde fence.\n~~~\nAfter `agents/atomic-builder.md` end.\n"
	got := mdlink.Linkify(content, fileAbs, dir)

	// Inside the tilde fence must not be linked.
	if !contains(got, "~~~\nSee `agents/atomic-builder.md` inside tilde fence.\n~~~") {
		t.Errorf("tilde fence content was modified:\n%q", got)
	}
	// Outside the tilde fence must be linked.
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
