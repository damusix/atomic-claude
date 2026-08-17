package realm

// The <code-index> block in the realm CLAUDE.md lists indexed members, so a
// Claude session opened in any member repo picks up realm awareness through the
// upward CLAUDE.md walk, which crosses git boundaries.
//
// The block carries no timestamp: one would diff on every index run, defeating
// the regen-only-on-change rule.
//
// The splice logic duplicates wiki's rather than importing it, since wiki →
// codeintel would be a dependency cycle.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const codeIndexMarkerOpen = "<code-index>"

const codeIndexMarkerClose = "</code-index>"

// WriteCodeIndexBlock rewrites the block in <realmRoot>/CLAUDE.md, creating the
// file if absent. Idempotent: a second call with the same members writes
// nothing. members must arrive already filtered to the non-excluded set.
func WriteCodeIndexBlock(realmRoot string, members []MemberEntry) error {
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	block := buildCodeIndexBlock(members)

	existing, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("realm: read %s: %w", claudeMDPath, err)
	}

	var newContent string
	if os.IsNotExist(err) || len(existing) == 0 {
		// Add a stub alongside the block so a fresh file is usable as-is.
		newContent = block + "\n" + codeIndexDefaultStub()
	} else {
		newContent = rewriteCodeIndexBlock(string(existing), block)
	}

	// Skip the write when nothing changed, so mtime stays stable.
	if string(existing) == newContent {
		return nil
	}

	return os.WriteFile(claudeMDPath, []byte(newContent), 0o644)
}

// buildCodeIndexBlock must stay byte-identical across runs with the same
// membership, so nothing time- or order-varying may enter it.
func buildCodeIndexBlock(members []MemberEntry) string {
	var sb strings.Builder
	sb.WriteString(codeIndexMarkerOpen)
	sb.WriteString("\n")
	for _, m := range members {
		fmt.Fprintf(&sb, "<member key=%q path=%q />\n", m.Key, m.Path)
	}
	sb.WriteString(codeIndexMarkerClose)
	return sb.String()
}

// rewriteCodeIndexBlock preserves everything outside the block byte-for-byte,
// appending when no block exists yet.
func rewriteCodeIndexBlock(content, newBlock string) string {
	openIdx := strings.Index(content, codeIndexMarkerOpen)
	if openIdx == -1 {
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result + "\n" + newBlock
	}

	closeIdx := strings.Index(content[openIdx:], codeIndexMarkerClose)
	if closeIdx == -1 {
		// Open tag with no close: replace through EOF rather than nest a block.
		return content[:openIdx] + newBlock
	}

	blockEnd := openIdx + closeIdx + len(codeIndexMarkerClose)

	before := content[:openIdx]
	after := content[blockEnd:]

	return before + newBlock + after
}

func codeIndexDefaultStub() string {
	return "\n<!-- Realm CLAUDE.md — managed by `atomic code index`. Edit below this block freely. -->\n"
}
