package wiki

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Registry writer tests ---

// TestRegistry_BlockAbsent_AppendsAfterAtomicClose verifies that when CLAUDE.md
// exists with an <atomic> block but no <wikis> block, the registry writer appends
// the <wikis> block immediately after </atomic> and leaves everything else byte-identical.
func TestRegistry_BlockAbsent_AppendsAfterAtomicClose(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	original := "<atomic>\nsome content\n</atomic>\n\n## Other\n\ntext here\n"
	if err := os.WriteFile(claudeMD, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	otherTmp := t.TempDir()
	indexPath := filepath.Join(otherTmp, "other-wiki", "index.md")
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)

	// Block must be present with the entry.
	if !strings.Contains(content, "<wikis>") {
		t.Error("expected <wikis> block to be present")
	}
	if !strings.Contains(content, "- "+indexPath) {
		t.Errorf("expected entry %q in <wikis> block", indexPath)
	}

	// The <atomic> block content must be untouched.
	if !strings.Contains(content, "some content") {
		t.Error("<atomic> block content was altered")
	}
	if !strings.Contains(content, "</atomic>") {
		t.Error("</atomic> tag is missing")
	}

	// Everything before </atomic> must be byte-identical to original.
	atomicCloseIdx := strings.Index(content, "</atomic>")
	origAtomicCloseIdx := strings.Index(original, "</atomic>")
	if content[:atomicCloseIdx] != original[:origAtomicCloseIdx] {
		t.Error("content before </atomic> was modified")
	}

	// ## Other section must survive.
	if !strings.Contains(content, "## Other") || !strings.Contains(content, "text here") {
		t.Error("content after </atomic> was dropped")
	}
}

// TestRegistry_BlockAbsent_NoAtomicTag_AppendsAtEOF verifies that when there is
// no <atomic> block at all, the <wikis> block is appended at EOF.
func TestRegistry_BlockAbsent_NoAtomicTag_AppendsAtEOF(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	original := "# My config\n\nsome content\n"
	if err := os.WriteFile(claudeMD, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	indexPath := "/my/wiki/index.md"
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)

	if !strings.Contains(content, "<wikis>") {
		t.Error("expected <wikis> block")
	}
	if !strings.Contains(content, "- "+indexPath) {
		t.Errorf("expected entry %q", indexPath)
	}
	// Original content preserved.
	if !strings.HasPrefix(content, original) {
		t.Errorf("original content not preserved; got prefix %q", content[:min(len(content), len(original)+20)])
	}
}

// TestRegistry_FileAbsent_CreatesWithBlock verifies that when CLAUDE.md does not
// exist at all, the writer creates it containing only the <wikis> block.
func TestRegistry_FileAbsent_CreatesWithBlock(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	// Do NOT create the file.

	indexPath := "/new/wiki/index.md"
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)

	if !strings.Contains(content, "<wikis>") {
		t.Error("expected <wikis> block in newly created file")
	}
	if !strings.Contains(content, "- "+indexPath) {
		t.Errorf("expected entry %q", indexPath)
	}
}

// TestRegistry_Idempotent verifies that calling RegisterWiki twice with the same
// path does not add a duplicate entry.
func TestRegistry_Idempotent(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	indexPath := "/idempotent/wiki/index.md"

	// First call.
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("first RegisterWiki: %v", err)
	}
	// Second call.
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("second RegisterWiki: %v", err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)

	count := strings.Count(content, "- "+indexPath)
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of entry, got %d", count)
	}
}

// TestRegistry_NormalizedPathDedup verifies that two spellings of the same path
// (one with trailing component relative, one absolute) produce only one entry.
func TestRegistry_NormalizedPathDedup(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	// A real path so filepath.Abs can resolve it.
	wikiDir := filepath.Join(dir, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexPath1 := filepath.Join(wikiDir, "index.md")
	// Spelling 2: same path via redundant path separator components.
	indexPath2 := filepath.Join(wikiDir, ".", "index.md")

	if err := RegisterWiki(claudeMD, indexPath1); err != nil {
		t.Fatalf("first RegisterWiki: %v", err)
	}
	if err := RegisterWiki(claudeMD, indexPath2); err != nil {
		t.Fatalf("second RegisterWiki: %v", err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)

	// Only one entry should exist.
	if strings.Count(content, "- ") != 1 {
		t.Errorf("expected exactly 1 entry, content:\n%s", content)
	}
}

// TestRegistry_AtomicBlockUntouched verifies that a registry write on a file
// with a substantial <atomic> block leaves all content outside <wikis>…</wikis>
// byte-identical to the original file. The <wikis> span is stripped from both
// the original and the result before comparison, proving no bytes outside the
// managed block were altered.
func TestRegistry_AtomicBlockUntouched(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	original := "<atomic>\n## Subagents\n- atomic-builder\n- atomic-surgeon\n</atomic>\n\n## User section\n\ncustom stuff\n"
	if err := os.WriteFile(claudeMD, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	indexPath := "/some/wiki/index.md"
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)

	// Locate the <wikis>…</wikis> span in the result.
	wikisOpen := strings.Index(content, "<wikis>")
	wikisClose := strings.Index(content, "</wikis>")
	if wikisOpen == -1 || wikisClose == -1 {
		t.Fatalf("missing <wikis> block in output:\n%s", content)
	}
	wikisEnd := wikisClose + len("</wikis>")

	// Strip the <wikis>…</wikis> span from the result. The insertion prepends "\n"
	// before the block and buildWikisBlock appends "\n" after </wikis>, so the
	// full managed span is "\n<wikis>…</wikis>\n". Strip that entire span to
	// recover exactly the bytes that existed before insertion.
	spanStart := wikisOpen
	if spanStart > 0 && content[spanStart-1] == '\n' {
		spanStart--
	}
	spanEnd := wikisEnd
	if spanEnd < len(content) && content[spanEnd] == '\n' {
		spanEnd++
	}
	resultOutside := content[:spanStart] + content[spanEnd:]

	// The original has no <wikis> block, so "outside" is the original itself.
	// Byte-identical comparison proves the <atomic> block and all surrounding
	// content were not altered — not merely present.
	if resultOutside != original {
		t.Errorf("content outside <wikis> block is not byte-identical to original\ngot:  %q\nwant: %q", resultOutside, original)
	}

	// Sanity: wiki entry must not have leaked outside the block.
	if strings.Contains(resultOutside, "index.md") {
		t.Error("wiki entry leaked outside the <wikis> block")
	}
}

// TestRegistry_ProseMentionNotMatched verifies that a CLAUDE.md containing prose
// mentions of `<wikis>` inside an <atomic> block — including backtick-quoted
// examples like `<wikis>` and `</wikis>` — are NOT treated as a real bare-line
// block. The first RegisterWiki call must create a NEW block AFTER </atomic> and
// must NOT write into or near the prose mention. A second call finds the created
// block and dedups.
func TestRegistry_ProseMentionNotMatched(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")

	// Fixture: <atomic>…</atomic> with inline prose that mentions `<wikis>` and
	// `</wikis>` via backticks and in a sentence — none of these are bare lines.
	original := "<atomic>\n" +
		"## Wiki section\n" +
		"\n" +
		"The wiki index path is written into a `<wikis>` block in CLAUDE.md.\n" +
		"Use `</wikis>` to close it. See the `<wikis>` / `</wikis>` pair.\n" +
		"\n" +
		"</atomic>\n" +
		"\n" +
		"## User section\n" +
		"\n" +
		"custom content\n"

	if err := os.WriteFile(claudeMD, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	indexPath := "/my/wiki/index.md"

	// First call — must append a new block AFTER </atomic>, not inside <atomic>.
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("first RegisterWiki: %v", err)
	}

	got, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)

	// A real <wikis> block must now exist.
	if !strings.Contains(content, "<wikis>") {
		t.Fatal("expected <wikis> block to be created")
	}
	if !strings.Contains(content, "- "+indexPath) {
		t.Errorf("expected entry %q in <wikis> block", indexPath)
	}

	// The entry must land AFTER </atomic>.
	atomicCloseIdx := strings.Index(content, "</atomic>")
	if atomicCloseIdx == -1 {
		t.Fatal("</atomic> missing from output")
	}
	entryIdx := strings.Index(content, "- "+indexPath)
	if entryIdx <= atomicCloseIdx {
		t.Errorf("wiki entry landed inside or before </atomic> (atomicClose=%d, entry=%d)", atomicCloseIdx, entryIdx)
	}

	// The <atomic> block content must be byte-identical to the original.
	if content[:atomicCloseIdx] != original[:strings.Index(original, "</atomic>")] {
		t.Error("content before </atomic> was modified")
	}

	// ## User section must survive.
	if !strings.Contains(content, "custom content") {
		t.Error("user section content was dropped")
	}

	// Count occurrences of "- /my/wiki/index.md" — must be exactly 1.
	if strings.Count(content, "- "+indexPath) != 1 {
		t.Errorf("expected exactly 1 entry, got %d", strings.Count(content, "- "+indexPath))
	}

	// Second call — must dedup (the real block now exists).
	if err := RegisterWiki(claudeMD, indexPath); err != nil {
		t.Fatalf("second RegisterWiki: %v", err)
	}

	got2, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	content2 := string(got2)

	if strings.Count(content2, "- "+indexPath) != 1 {
		t.Errorf("second call created a duplicate; got %d occurrences", strings.Count(content2, "- "+indexPath))
	}
}

// --- wikiAction integration tests ---

// TestWikiAction_ScanWritesRegistryAndHandoff verifies that wikiAction:
//   - calls Scan (scaffold + wiki-scan block)
//   - writes registry entry to the temp claudeHome's CLAUDE.md
//   - outputs the stdout handoff with stable labels
func TestWikiAction_ScanWritesRegistryAndHandoff(t *testing.T) {
	// Build a temp root with one indexed repo and one pending repo.
	root := t.TempDir()
	claudeHome := t.TempDir() // never writes to real ~/.claude

	// indexed repo
	indexedRepo := filepath.Join(root, "repo-indexed")
	if err := os.MkdirAll(filepath.Join(indexedRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(indexedRepo, ".claude", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexedRepo, ".claude", "project", "signals.md"), []byte("signals"), 0o644); err != nil {
		t.Fatal(err)
	}

	// pending repo
	pendingRepo := filepath.Join(root, "repo-pending")
	if err := os.MkdirAll(filepath.Join(pendingRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Capture stdout.
	var buf bytes.Buffer

	code := wikiAction([]string{"scan", "--root=" + root}, claudeHome, root, &buf)
	if code != 0 {
		t.Fatalf("wikiAction returned %d, output:\n%s", code, buf.String())
	}

	out := buf.String()

	// Stable labels.
	if !strings.Contains(out, "repos") {
		t.Errorf("missing 'repos' label in handoff:\n%s", out)
	}
	if !strings.Contains(out, "indexed") {
		t.Errorf("missing 'indexed' status in handoff:\n%s", out)
	}
	if !strings.Contains(out, "pending") {
		t.Errorf("missing 'pending' status in handoff:\n%s", out)
	}
	if !strings.Contains(out, "NEXT STEPS") {
		t.Errorf("missing 'NEXT STEPS' in handoff:\n%s", out)
	}

	// Verify the registry entry in temp claudeHome.
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
	if _, err := os.Stat(claudeMD); err != nil {
		t.Fatalf("CLAUDE.md not created at temp claudeHome: %v", err)
	}
	claudeContent, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatal(err)
	}
	wikiIndexPath := filepath.Join(root, "wiki", "index.md")
	if !strings.Contains(string(claudeContent), wikiIndexPath) {
		t.Errorf("registry missing wiki index path %q in CLAUDE.md:\n%s", wikiIndexPath, claudeContent)
	}
}

// TestWikiAction_RootFlag verifies that --root=<path> selects the scan root.
func TestWikiAction_RootFlag(t *testing.T) {
	root := t.TempDir()
	claudeHome := t.TempDir()

	// One pending repo.
	repoDir := filepath.Join(root, "my-repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code := wikiAction([]string{"scan", "--root=" + root}, claudeHome, "/some/other/cwd", &buf)
	if code != 0 {
		t.Fatalf("wikiAction with --root flag returned %d, output:\n%s", code, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "repos") {
		t.Errorf("missing 'repos' label in handoff:\n%s", out)
	}
}

// TestWikiAction_NoRootFlag_UsesCwd verifies that bare `atomic wiki scan` uses cwd.
func TestWikiAction_NoRootFlag_UsesCwd(t *testing.T) {
	cwd := t.TempDir()
	claudeHome := t.TempDir()

	// One pending repo.
	repoDir := filepath.Join(cwd, "cwd-repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code := wikiAction([]string{"scan"}, claudeHome, cwd, &buf)
	if code != 0 {
		t.Fatalf("wikiAction (no root flag) returned %d, output:\n%s", code, buf.String())
	}

	// The wiki should have been created at cwd/wiki.
	if _, err := os.Stat(filepath.Join(cwd, "wiki", "index.md")); err != nil {
		t.Fatalf("wiki/index.md not created under cwd: %v", err)
	}
}
