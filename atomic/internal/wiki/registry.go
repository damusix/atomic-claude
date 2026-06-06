package wiki

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// wikisMarkerOpen is the literal open tag for the registry block.
const wikisMarkerOpen = "<wikis>"

// wikisMarkerClose is the literal close tag for the registry block.
const wikisMarkerClose = "</wikis>"

// atomicClose is the closing tag of the <atomic> block.
const atomicClose = "</atomic>"

// RegisterWiki writes the wiki's index.md absolute path into the <wikis> block
// in the CLAUDE.md file at claudeMDPath.
//
// Three insertion cases, all idempotent:
//   - block present → add the entry iff absent; dedup by normalized path.
//   - block absent → append a fresh <wikis> block after </atomic> (or at EOF).
//   - file absent → create it containing just the block.
//
// Block detection is line-anchored: a line whose trimmed content is exactly
// "<wikis>" opens the block; a line whose trimmed content is exactly "</wikis>"
// closes it. Inline or backtick mentions of the literal text (e.g. inside a
// sentence or as `<wikis>`) do NOT match.
//
// The <atomic> block and all other content are never altered.
func RegisterWiki(claudeMDPath, indexPath string) error {
	// Normalize the path for consistent dedup comparison.
	normalized, err := normalizePath(indexPath)
	if err != nil {
		return fmt.Errorf("wiki registry: normalize path: %w", err)
	}

	// Read existing content (file may not exist).
	data, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wiki registry: read %s: %w", claudeMDPath, err)
	}

	var newContent string
	if os.IsNotExist(err) || len(data) == 0 {
		// File absent or empty — create with just the block.
		newContent = buildWikisBlock([]string{normalized})
	} else {
		newContent = rewriteWikisBlock(string(data), normalized)
	}

	return writeFileAtomic(claudeMDPath, []byte(newContent))
}

// normalizePath returns filepath.Clean(filepath.Abs(p)). No symlink resolution.
func normalizePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// findBareLineBlock scans lines for a block whose open/close tags occupy a
// line by themselves (trimmed). Returns the byte offsets of the start of the
// open-tag line and the end (exclusive) of the close-tag line within content,
// plus the body text between the tags. Returns (-1, -1, "") if not found.
//
// "Bare line" means: strings.TrimSpace(line) == tag. A line like
// "`<wikis>`" or "see <wikis> for details" does NOT match.
func findBareLineBlock(content, openTag, closeTag string) (blockStart, blockEnd int, body string) {
	lines := strings.Split(content, "\n")
	openLine := -1
	pos := 0
	for i, line := range lines {
		lineLen := len(line)
		// Account for the \n that was consumed by Split (except for the last segment).
		if i < len(lines)-1 {
			lineLen++ // +1 for the \n
		}
		if openLine == -1 {
			if strings.TrimSpace(line) == openTag {
				openLine = i
				blockStart = pos
			}
		} else {
			if strings.TrimSpace(line) == closeTag {
				blockEnd = pos + lineLen
				// body is everything between the open-tag line's \n and the close-tag line start.
				// Rebuild from lines[openLine+1 .. i-1] to avoid index arithmetic bugs.
				bodyLines := lines[openLine+1 : i]
				body = strings.Join(bodyLines, "\n")
				if len(bodyLines) > 0 {
					body += "\n"
				}
				return blockStart, blockEnd, body
			}
		}
		pos += lineLen
	}
	return -1, -1, ""
}

// findBareAtomicClose returns the byte offset immediately after the bare-line
// </atomic> tag (i.e. including its trailing newline if present). Returns -1
// if no bare-line </atomic> exists.
func findBareAtomicClose(content string) int {
	lines := strings.Split(content, "\n")
	pos := 0
	for i, line := range lines {
		lineLen := len(line)
		if i < len(lines)-1 {
			lineLen++ // +1 for \n
		}
		if strings.TrimSpace(line) == atomicClose {
			return pos + lineLen
		}
		pos += lineLen
	}
	return -1
}

// rewriteWikisBlock either inserts a new entry into an existing bare-line
// <wikis> block or appends a new block after a bare-line </atomic> (or at
// EOF). Inline/backtick mentions of "<wikis>" are ignored.
func rewriteWikisBlock(content, normalized string) string {
	blockStart, _, body := findBareLineBlock(content, wikisMarkerOpen, wikisMarkerClose)
	if blockStart == -1 {
		// No bare-line <wikis> block — insert one.
		return insertWikisBlock(content, normalized)
	}

	// Block present — check whether normalized is already recorded.
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Entry format: "- <path>"
		if strings.HasPrefix(trimmed, "- ") {
			existing := strings.TrimPrefix(trimmed, "- ")
			existingNorm, err := normalizePath(existing)
			if err == nil && existingNorm == normalized {
				// Already recorded — no change.
				return content
			}
		}
	}

	// Add the new entry immediately after the open tag line.
	// blockStart points to the start of the "<wikis>" line.
	// We need to insert after the first \n following the open tag.
	openTagEnd := blockStart + len(wikisMarkerOpen)
	// Skip the \n that follows the open tag line.
	if openTagEnd < len(content) && content[openTagEnd] == '\n' {
		openTagEnd++
	}
	entry := "- " + normalized + "\n"
	before := content[:openTagEnd]
	after := content[openTagEnd:]
	return before + entry + after
}

// insertWikisBlock builds a fresh <wikis> block and inserts it after the
// bare-line </atomic> if present, or appends it at EOF.
func insertWikisBlock(content, normalized string) string {
	block := "\n" + buildWikisBlock([]string{normalized})

	insertAt := findBareAtomicClose(content)
	if insertAt == -1 {
		// No bare-line </atomic> — append at EOF.
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + block
	}

	// Insert immediately after the bare-line </atomic>.
	before := content[:insertAt]
	after := content[insertAt:]
	return before + block + after
}

// buildWikisBlock renders a complete <wikis>…</wikis> block for the given paths.
func buildWikisBlock(paths []string) string {
	var sb strings.Builder
	sb.WriteString(wikisMarkerOpen)
	sb.WriteString("\n")
	for _, p := range paths {
		fmt.Fprintf(&sb, "- %s\n", p)
	}
	sb.WriteString(wikisMarkerClose)
	sb.WriteString("\n")
	return sb.String()
}

// writeFileAtomic writes data to path via a temp file + rename.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	return nil
}

// PrintHandoff writes the stdout handoff to w. This is the deterministic output
// printed after a successful `atomic wiki scan`:
//
//	<N> repos · <M> indexed · <K> pending
//	<status> <path> [→ signals path]
//	...
//	NEXT STEPS
//	<pending repo list>
func PrintHandoff(w io.Writer, members []Member) {
	total := len(members)
	indexed := 0
	pending := 0
	for _, m := range members {
		switch m.Status {
		case "indexed":
			indexed++
		case "pending":
			pending++
		}
	}

	fmt.Fprintf(w, "%d repos · %d indexed · %d pending\n", total, indexed, pending)
	fmt.Fprintln(w)
	for _, m := range members {
		if m.Status == "indexed" && m.SignalsPath != "" {
			fmt.Fprintf(w, "%s %s → %s\n", m.Status, m.Path, m.SignalsPath)
		} else {
			fmt.Fprintf(w, "%s %s\n", m.Status, m.Path)
		}
	}

	if pending > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "NEXT STEPS")
		for _, m := range members {
			if m.Status == "pending" {
				fmt.Fprintf(w, "  run /refresh-wiki for: %s\n", m.Path)
			}
		}
	}
}
