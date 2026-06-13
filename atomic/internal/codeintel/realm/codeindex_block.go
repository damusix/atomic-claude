package realm

// codeindex_block.go — CP4: idempotent <code-index> block writer for the realm CLAUDE.md.
//
// The block lists the non-excluded, indexed members so that Claude sessions
// opened inside any member repo pick up the realm's code-index awareness via
// the upward CLAUDE.md walk (which crosses git boundaries).
//
// Unlike wiki's <wiki-scan> block the <code-index> block carries NO timestamp.
// A timestamp would diff on every index run and violate SC 7 (regen-only-on-change).
// The block is purely structural: member keys + realm-relative paths.
//
// Write contract:
//   - Compute desired block string from the given member list.
//   - Read the current CLAUDE.md (absent → create with a minimal stub).
//   - Splice or append the block; preserve all surrounding content byte-for-byte.
//   - Write ONLY if the resulting file content differs from what is on disk.
//
// This is the same splice pattern as wiki.rewriteScanBlock /
// wiki.writeWikiScanBlock; duplicated here to keep the realm package
// self-contained and avoid a dependency cycle (wiki → codeintel would be bad).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// codeIndexMarkerOpen is the prefix of the managed block open tag.
const codeIndexMarkerOpen = "<code-index>"

// codeIndexMarkerClose is the close tag of the managed block.
const codeIndexMarkerClose = "</code-index>"

// WriteCodeIndexBlock writes (or replaces) the <code-index> block in
// <realmRoot>/CLAUDE.md.  It creates the file if absent.
// The function is idempotent: calling it twice with the same member list
// produces byte-identical output and skips the write on the second call.
//
// members must already be filtered to the non-excluded set — the caller is
// responsible for the filter.
func WriteCodeIndexBlock(realmRoot string, members []MemberEntry) error {
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	block := buildCodeIndexBlock(members)

	existing, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("realm: read %s: %w", claudeMDPath, err)
	}

	var newContent string
	if os.IsNotExist(err) || len(existing) == 0 {
		// Create fresh: block + minimal stub so the file is immediately useful.
		newContent = block + "\n" + codeIndexDefaultStub()
	} else {
		newContent = rewriteCodeIndexBlock(string(existing), block)
	}

	// Write only if content differs (SC 7: regen-only-on-change).
	if string(existing) == newContent {
		return nil
	}

	return os.WriteFile(claudeMDPath, []byte(newContent), 0o644)
}

// buildCodeIndexBlock produces the full <code-index> … </code-index> block
// string for the given members.  NO timestamp — the block must be
// byte-identical across consecutive runs with the same membership.
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

// rewriteCodeIndexBlock splices newBlock into content, replacing any existing
// <code-index>…</code-index> block in-place.  Content outside the block is
// preserved byte-for-byte.  If no block exists the new block is appended.
func rewriteCodeIndexBlock(content, newBlock string) string {
	openIdx := strings.Index(content, codeIndexMarkerOpen)
	if openIdx == -1 {
		// No existing block — append.
		result := content
		if !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		return result + "\n" + newBlock
	}

	// Find the close tag.
	closeIdx := strings.Index(content[openIdx:], codeIndexMarkerClose)
	if closeIdx == -1 {
		// Malformed (open but no close) — replace from open tag to EOF.
		return content[:openIdx] + newBlock
	}

	blockEnd := openIdx + closeIdx + len(codeIndexMarkerClose)

	before := content[:openIdx]
	after := content[blockEnd:]

	return before + newBlock + after
}

// codeIndexDefaultStub is the minimal narrative appended when creating a fresh
// realm CLAUDE.md.
func codeIndexDefaultStub() string {
	return "\n<!-- Realm CLAUDE.md — managed by `atomic code index`. Edit below this block freely. -->\n"
}
