package wiki

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const wikisMarkerOpen = "<wikis>"

const wikisMarkerClose = "</wikis>"

const atomicClose = "</atomic>"

// RegisterWiki records indexPath in claudeMDPath's <wikis> block, creating the
// file or the block as needed and deduping by normalized path. It never alters
// the <atomic> block or anything else.
//
// Tags are matched only as whole lines, so a sentence or backtick span
// mentioning "<wikis>" cannot be mistaken for the block.
func RegisterWiki(claudeMDPath, indexPath string) error {
	normalized, err := normalizePath(indexPath)
	if err != nil {
		return fmt.Errorf("wiki registry: normalize path: %w", err)
	}

	data, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wiki registry: read %s: %w", claudeMDPath, err)
	}

	var newContent string
	if os.IsNotExist(err) || len(data) == 0 {
		newContent = buildWikisBlock([]string{normalized})
	} else {
		newContent = rewriteWikisBlock(string(data), normalized)
	}

	return writeFileAtomic(claudeMDPath, []byte(newContent))
}

// normalizePath is Abs then Clean, deliberately without symlink resolution.
func normalizePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// findBareLineBlock locates a block whose tags each occupy a whole line,
// returning the open line's start offset, the close line's end offset, and the
// body between them, or (-1, -1, "") when absent.
func findBareLineBlock(content, openTag, closeTag string) (blockStart, blockEnd int, body string) {
	lines := strings.Split(content, "\n")
	openLine := -1
	pos := 0
	for i, line := range lines {
		lineLen := len(line)
		if i < len(lines)-1 {
			lineLen++ // the \n Split consumed
		}
		if openLine == -1 {
			if strings.TrimSpace(line) == openTag {
				openLine = i
				blockStart = pos
			}
		} else {
			if strings.TrimSpace(line) == closeTag {
				blockEnd = pos + lineLen
				// Rebuilt from the line slice rather than sliced by offset, to
				// keep the index arithmetic out of the body entirely.
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

// findBareAtomicClose returns the offset just past the whole-line </atomic>
// tag, trailing newline included, or -1.
func findBareAtomicClose(content string) int {
	lines := strings.Split(content, "\n")
	pos := 0
	for i, line := range lines {
		lineLen := len(line)
		if i < len(lines)-1 {
			lineLen++
		}
		if strings.TrimSpace(line) == atomicClose {
			return pos + lineLen
		}
		pos += lineLen
	}
	return -1
}

func rewriteWikisBlock(content, normalized string) string {
	blockStart, _, body := findBareLineBlock(content, wikisMarkerOpen, wikisMarkerClose)
	if blockStart == -1 {
		return insertWikisBlock(content, normalized)
	}

	// Dedup on the normalized path, not the literal text.
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			existing := strings.TrimPrefix(trimmed, "- ")
			existingNorm, err := normalizePath(existing)
			if err == nil && existingNorm == normalized {
				return content
			}
		}
	}

	openTagEnd := blockStart + len(wikisMarkerOpen)
	if openTagEnd < len(content) && content[openTagEnd] == '\n' {
		openTagEnd++
	}
	entry := "- " + normalized + "\n"
	before := content[:openTagEnd]
	after := content[openTagEnd:]
	return before + entry + after
}

// insertWikisBlock places a fresh block just after </atomic>, so the registry
// sits outside the managed block, or at EOF when there is none.
func insertWikisBlock(content, normalized string) string {
	block := "\n" + buildWikisBlock([]string{normalized})

	insertAt := findBareAtomicClose(content)
	if insertAt == -1 {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + block
	}

	before := content[:insertAt]
	after := content[insertAt:]
	return before + block + after
}

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

// PrintHandoff writes the deterministic summary `atomic wiki scan` ends with:
// counts, one line per member, then next steps for anything still pending.
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
