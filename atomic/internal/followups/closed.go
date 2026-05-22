package followups

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// FormatClosedLine produces a single-line ledger entry for a closed follow-up.
// Format: - YYYY-MM-DD <id> — "<title>" — <marker>
// Title is double-quoted with embedded " and \ escaped.
// Marker has newlines collapsed to spaces.
func FormatClosedLine(id, title, marker string, when time.Time) string {
	date := when.Format("2006-01-02")
	quotedTitle := quoteTitle(title)
	singleLineMarker := collapseWhitespace(marker)
	return fmt.Sprintf("- %s %s — %s — %s", date, id, quotedTitle, singleLineMarker)
}

// AppendClosed appends a closed-entry line to the CLOSED.md file at path.
// Creates the file if it does not exist. Idempotent: if the last line already
// contains the same id with the same date, the line is not appended again.
func AppendClosed(path, id, title, marker string, when time.Time) error {
	line := FormatClosedLine(id, title, marker, when)

	// Read existing content if the file exists.
	existing := ""
	raw, err := os.ReadFile(path)
	if err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("followups closed: read %q: %w", path, err)
	}

	// Idempotency: check if a line with this id already appears with the same date.
	date := when.Format("2006-01-02")
	idToken := date + " " + id + " "
	for _, l := range strings.Split(existing, "\n") {
		if strings.Contains(l, idToken) {
			// Already present with same date — no-op.
			return nil
		}
	}

	// Append: ensure the file ends with exactly one newline before the new line.
	var content string
	if existing == "" {
		content = line + "\n"
	} else {
		trimmed := strings.TrimRight(existing, "\n")
		content = trimmed + "\n" + line + "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("followups closed: write %q: %w", path, err)
	}
	return nil
}

// ParseClosedLine parses a single CLOSED.md ledger line.
// Returns id, title (unescaped), marker, date string, and error.
// Expected format: - YYYY-MM-DD <id> — "<title>" — <marker>
func ParseClosedLine(line string) (id, title, marker, date string, err error) {
	// Strip leading "- "
	if !strings.HasPrefix(line, "- ") {
		return "", "", "", "", fmt.Errorf("followups closed: line must start with '- ': %q", line)
	}
	rest := line[2:]

	// Extract date (first token, 10 chars YYYY-MM-DD)
	if len(rest) < 11 {
		return "", "", "", "", fmt.Errorf("followups closed: line too short to contain date: %q", line)
	}
	date = rest[:10]
	rest = rest[11:] // skip date + space

	// Split on em-dash separator " — "
	const sep = " — "
	parts := strings.SplitN(rest, sep, 3)
	if len(parts) < 3 {
		return "", "", "", "", fmt.Errorf("followups closed: expected 2 em-dash separators: %q", line)
	}

	id = strings.TrimSpace(parts[0])
	rawTitle := strings.TrimSpace(parts[1])
	marker = strings.TrimSpace(parts[2])

	// Unquote the title.
	title, err = unquoteTitle(rawTitle)
	if err != nil {
		return "", "", "", "", fmt.Errorf("followups closed: unquote title: %w", err)
	}

	return id, title, marker, date, nil
}

// quoteTitle wraps title in double quotes, escaping embedded " and \.
func quoteTitle(s string) string {
	// Escape backslash first, then double-quote.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// unquoteTitle reverses quoteTitle: strips outer quotes, unescapes \" and \\.
func unquoteTitle(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("title must be double-quoted, got %q", s)
	}
	inner := s[1 : len(s)-1]
	// Unescape in order: \" → ", \\ → \
	inner = strings.ReplaceAll(inner, `\"`, `"`)
	inner = strings.ReplaceAll(inner, `\\`, `\`)
	return inner, nil
}

// collapseWhitespace collapses runs of any whitespace (newlines, tabs,
// repeated spaces) into a single space. Used to keep CLOSED.md ledger
// entries on one line regardless of how the reason/marker was authored.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
