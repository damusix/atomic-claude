package claudeinstall

import "testing"

// The <atomic> block parser is the foundation for deterministic CLAUDE.md
// updates: install/update must replace exactly the atomic-owned block and
// nothing else. These tests pin the line-anchored detection semantics
// (mirroring the <wikis> block parser in internal/wiki): a line whose
// trimmed content is exactly "<atomic>" opens, "</atomic>" closes. Inline
// or backtick mentions never match.

func TestExtractAtomicBlockHappyPath(t *testing.T) {
	content := "# CLAUDE.md\n\n<atomic>\n\n## Principles\n\nstuff\n\n</atomic>\n\n## User section\n"
	block, ok := extractAtomicBlock(content)
	if !ok {
		t.Fatal("extractAtomicBlock: ok = false, want true")
	}
	want := "<atomic>\n\n## Principles\n\nstuff\n\n</atomic>\n"
	if block != want {
		t.Errorf("block = %q, want %q", block, want)
	}
}

func TestExtractAtomicBlockNoTags(t *testing.T) {
	if _, ok := extractAtomicBlock("# Plain file\n\nNo tags here.\n"); ok {
		t.Error("ok = true for tagless content, want false")
	}
}

func TestExtractAtomicBlockUnclosed(t *testing.T) {
	if _, ok := extractAtomicBlock("<atomic>\nnever closed\n"); ok {
		t.Error("ok = true for unclosed block, want false")
	}
}

func TestExtractAtomicBlockCloseBeforeOpen(t *testing.T) {
	if _, ok := extractAtomicBlock("</atomic>\nbody\n<atomic>\n"); ok {
		t.Error("ok = true for close-before-open, want false")
	}
}

func TestExtractAtomicBlockMultipleOpens(t *testing.T) {
	content := "<atomic>\na\n</atomic>\n<atomic>\nb\n</atomic>\n"
	if _, ok := extractAtomicBlock(content); ok {
		t.Error("ok = true for multiple blocks, want false — ambiguous boundary must fall back to merge")
	}
}

func TestExtractAtomicBlockInlineMentionIgnored(t *testing.T) {
	// Prose mentioning the literal tag must not open a block.
	content := "The `<atomic>` tag bounds owned content.\n\n<atomic>\nreal\n</atomic>\n"
	block, ok := extractAtomicBlock(content)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := "<atomic>\nreal\n</atomic>\n"
	if block != want {
		t.Errorf("block = %q, want %q", block, want)
	}
}

func TestExtractAtomicBlockNoTrailingNewlineAtEOF(t *testing.T) {
	content := "<atomic>\nbody\n</atomic>"
	block, ok := extractAtomicBlock(content)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if block != content {
		t.Errorf("block = %q, want %q", block, content)
	}
}

func TestReplaceAtomicBlockPreservesOutsideContent(t *testing.T) {
	current := "# Heading\n\n<atomic>\nold body\n</atomic>\n\n## My TypeScript rules\n\nkeep me\n"
	merged, ok := replaceAtomicBlock(current, "<atomic>\nnew body\n</atomic>\n")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := "# Heading\n\n<atomic>\nnew body\n</atomic>\n\n## My TypeScript rules\n\nkeep me\n"
	if merged != want {
		t.Errorf("merged = %q, want %q", merged, want)
	}
}

func TestReplaceAtomicBlockNoTags(t *testing.T) {
	if _, ok := replaceAtomicBlock("no tags\n", "<atomic>\nx\n</atomic>\n"); ok {
		t.Error("ok = true for tagless target, want false")
	}
}
