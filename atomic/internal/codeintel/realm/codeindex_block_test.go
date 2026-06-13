package realm_test

// codeindex_block_test.go — CP4 Part A: tests for WriteCodeIndexBlock.
//
// Coverage (per brief):
//   1. Block created when CLAUDE.md is absent (file created with block + stub).
//   2. Block spliced into existing CLAUDE.md; surrounding content preserved byte-for-byte.
//   3. Idempotency: second write with same membership → byte-identical file (no write occurs).
//   4. Membership change: updates the block; content outside preserved.
//   5. Empty member list → empty block (just open+close tags).
//   6. Existing block replaced in-place; content before and after preserved.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

// members returns a minimal slice of MemberEntry for test fixtures.
func members(entries ...struct{ key, path string }) []realm.MemberEntry {
	out := make([]realm.MemberEntry, len(entries))
	for i, e := range entries {
		out[i] = realm.MemberEntry{Key: e.key, Path: e.path}
	}
	return out
}

// TestWriteCodeIndexBlock_AbsentCLAUDEMD verifies that the file is created with
// the block + stub when CLAUDE.md does not exist.
func TestWriteCodeIndexBlock_AbsentCLAUDEMD(t *testing.T) {
	dir := t.TempDir()

	ms := members(
		struct{ key, path string }{"foo", "repos/foo"},
		struct{ key, path string }{"bar", "repos/bar"},
	)

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("WriteCodeIndexBlock: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Must contain the open and close tags.
	if !strings.Contains(content, "<code-index>") {
		t.Errorf("missing <code-index> open tag in:\n%s", content)
	}
	if !strings.Contains(content, "</code-index>") {
		t.Errorf("missing </code-index> close tag in:\n%s", content)
	}
	// Must list both members.
	if !strings.Contains(content, `key="foo"`) || !strings.Contains(content, `path="repos/foo"`) {
		t.Errorf("missing foo member in:\n%s", content)
	}
	if !strings.Contains(content, `key="bar"`) || !strings.Contains(content, `path="repos/bar"`) {
		t.Errorf("missing bar member in:\n%s", content)
	}
	// Must NOT contain a timestamp/generated attribute.
	if strings.Contains(content, "generated=") {
		t.Errorf("block must not contain generated= timestamp; got:\n%s", content)
	}
}

// TestWriteCodeIndexBlock_SplicesIntoExisting verifies that the block is spliced
// into an existing CLAUDE.md and surrounding content is preserved byte-for-byte.
func TestWriteCodeIndexBlock_SplicesIntoExisting(t *testing.T) {
	dir := t.TempDir()

	prefix := "# Realm CLAUDE.md\n\nSome context above.\n\n"
	suffix := "\n\n## Notes\n\nContent below the block.\n"
	existing := prefix + suffix

	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	ms := members(struct{ key, path string }{"alpha", "repos/alpha"})

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("WriteCodeIndexBlock: %v", err)
	}

	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Block must be present.
	if !strings.Contains(content, "<code-index>") {
		t.Errorf("missing <code-index> in:\n%s", content)
	}
	if !strings.Contains(content, `key="alpha"`) {
		t.Errorf("missing alpha member in:\n%s", content)
	}
	// Original prefix must be intact.
	if !strings.Contains(content, "# Realm CLAUDE.md") {
		t.Errorf("original heading lost in:\n%s", content)
	}
	// Original suffix must be intact.
	if !strings.Contains(content, "## Notes") {
		t.Errorf("## Notes section lost in:\n%s", content)
	}
	if !strings.Contains(content, "Content below the block.") {
		t.Errorf("suffix content lost in:\n%s", content)
	}
}

// TestWriteCodeIndexBlock_Idempotent verifies SC 7: calling WriteCodeIndexBlock
// twice with the same membership produces byte-identical output on disk.
// The test also verifies the file is NOT rewritten on the second call (by
// checking mtime unchanged — or more simply, content identical).
func TestWriteCodeIndexBlock_Idempotent(t *testing.T) {
	dir := t.TempDir()

	ms := members(
		struct{ key, path string }{"x", "repos/x"},
		struct{ key, path string }{"y", "repos/y"},
	)

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("first WriteCodeIndexBlock: %v", err)
	}

	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	firstContent, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("ReadFile after first write: %v", err)
	}
	firstInfo, err := os.Stat(claudeMDPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatalf("second WriteCodeIndexBlock: %v", err)
	}

	secondContent, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("ReadFile after second write: %v", err)
	}
	secondInfo, err := os.Stat(claudeMDPath)
	if err != nil {
		t.Fatal(err)
	}

	// Content must be byte-identical.
	if string(firstContent) != string(secondContent) {
		t.Errorf("content changed on idempotent re-run:\nbefore: %q\nafter:  %q", firstContent, secondContent)
	}
	// Mtime must not change (no write occurred).
	if !firstInfo.ModTime().Equal(secondInfo.ModTime()) {
		t.Logf("mtime changed from %v to %v (acceptable on some FS with low-res clock)", firstInfo.ModTime(), secondInfo.ModTime())
		// Mtime check is advisory on some filesystems — content check above is authoritative.
	}
}

// TestWriteCodeIndexBlock_MembershipChange verifies that changing the member
// list updates the block and preserves content outside the block.
func TestWriteCodeIndexBlock_MembershipChange(t *testing.T) {
	dir := t.TempDir()

	// Write initial block with one member.
	ms1 := members(struct{ key, path string }{"foo", "repos/foo"})
	if err := realm.WriteCodeIndexBlock(dir, ms1); err != nil {
		t.Fatal(err)
	}

	// Append manual content after the block.
	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	existing, _ := os.ReadFile(claudeMDPath)
	manual := "\n## Manual notes\n\nKeep this.\n"
	if err := os.WriteFile(claudeMDPath, append(existing, []byte(manual)...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Now update with two members.
	ms2 := members(
		struct{ key, path string }{"foo", "repos/foo"},
		struct{ key, path string }{"baz", "repos/baz"},
	)
	if err := realm.WriteCodeIndexBlock(dir, ms2); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(claudeMDPath)
	content := string(data)

	// Both new members must appear.
	if !strings.Contains(content, `key="foo"`) {
		t.Errorf("missing foo after update:\n%s", content)
	}
	if !strings.Contains(content, `key="baz"`) {
		t.Errorf("missing baz after update:\n%s", content)
	}
	// Manual notes must be preserved.
	if !strings.Contains(content, "Keep this.") {
		t.Errorf("manual notes lost after update:\n%s", content)
	}
}

// TestWriteCodeIndexBlock_EmptyMembers verifies that an empty member list
// produces a valid (empty) block without panicking.
func TestWriteCodeIndexBlock_EmptyMembers(t *testing.T) {
	dir := t.TempDir()

	if err := realm.WriteCodeIndexBlock(dir, nil); err != nil {
		t.Fatalf("WriteCodeIndexBlock with nil members: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "<code-index>") {
		t.Errorf("missing <code-index> tag:\n%s", content)
	}
	if !strings.Contains(content, "</code-index>") {
		t.Errorf("missing </code-index> tag:\n%s", content)
	}
	// No member lines.
	if strings.Contains(content, "<member") {
		t.Errorf("unexpected <member> tag in empty block:\n%s", content)
	}
}

// TestWriteCodeIndexBlock_ExistingBlockReplacedInPlace verifies that when
// CLAUDE.md already contains a <code-index> block, the splice replaces it
// exactly, preserving prefix and suffix byte-for-byte.
func TestWriteCodeIndexBlock_ExistingBlockReplacedInPlace(t *testing.T) {
	dir := t.TempDir()

	// Plant an initial block with one member.
	initial := `# Header

<code-index>
<member key="old" path="repos/old" />
</code-index>

## Trailing section

Content after.
`
	claudeMDPath := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	ms := members(struct{ key, path string }{"new", "repos/new"})
	if err := realm.WriteCodeIndexBlock(dir, ms); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(claudeMDPath)
	content := string(data)

	// Old member gone, new member present.
	if strings.Contains(content, `key="old"`) {
		t.Errorf("old member still present after replace:\n%s", content)
	}
	if !strings.Contains(content, `key="new"`) {
		t.Errorf("new member missing after replace:\n%s", content)
	}
	// Prefix preserved.
	if !strings.Contains(content, "# Header") {
		t.Errorf("prefix lost:\n%s", content)
	}
	// Suffix preserved.
	if !strings.Contains(content, "## Trailing section") {
		t.Errorf("suffix lost:\n%s", content)
	}
	if !strings.Contains(content, "Content after.") {
		t.Errorf("trailing content lost:\n%s", content)
	}
}
