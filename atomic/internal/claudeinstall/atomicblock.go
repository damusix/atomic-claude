package claudeinstall

import "strings"

// The <atomic>...</atomic> block in CLAUDE.md bounds atomic-owned content;
// everything outside it is user-owned. Detection is line-anchored, mirroring
// the <wikis> block parser in internal/wiki: a line whose trimmed content is
// exactly the tag opens/closes the block. Inline or backtick mentions of the
// literal text never match. Ambiguous shapes (no tags, unclosed, multiple
// blocks) report !ok so callers fall back to the LLM merge path.
const (
	atomicOpenTag  = "<atomic>"
	atomicCloseTag = "</atomic>"
)

// atomicBlockBounds returns the byte offsets [start, end) of the single
// <atomic> block in content, tags included. end covers the close-tag line's
// trailing newline when present.
func atomicBlockBounds(content string) (start, end int, ok bool) {
	start, end = -1, -1
	offset := 0
	for _, line := range strings.SplitAfter(content, "\n") {
		switch strings.TrimSpace(line) {
		case atomicOpenTag:
			if start != -1 || end != -1 {
				return 0, 0, false // second block or reopen after close — ambiguous
			}
			start = offset
		case atomicCloseTag:
			if start == -1 || end != -1 {
				return 0, 0, false // close before open, or double close
			}
			end = offset + len(line)
		}
		offset += len(line)
	}
	if start == -1 || end == -1 {
		return 0, 0, false
	}
	return start, end, true
}

// extractAtomicBlock returns the <atomic>...</atomic> block, tags included.
func extractAtomicBlock(content string) (string, bool) {
	start, end, ok := atomicBlockBounds(content)
	if !ok {
		return "", false
	}
	return content[start:end], true
}

// replaceAtomicBlock returns content with its <atomic> block swapped for
// newBlock. Everything outside the block is preserved byte-for-byte.
func replaceAtomicBlock(content, newBlock string) (string, bool) {
	start, end, ok := atomicBlockBounds(content)
	if !ok {
		return "", false
	}
	return content[:start] + newBlock + content[end:], true
}

// atomicBlocksEqual reports whether both byte slices contain a parseable
// <atomic> block and the blocks are identical.
func atomicBlocksEqual(a, b []byte) bool {
	blockA, okA := extractAtomicBlock(string(a))
	if !okA {
		return false
	}
	blockB, okB := extractAtomicBlock(string(b))
	if !okB {
		return false
	}
	return blockA == blockB
}
