package followups

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// FormatClosedLine renders: - YYYY-MM-DD <id> — "<title>" — <marker>
func FormatClosedLine(id, title, marker string, when time.Time) string {
	date := when.Format("2006-01-02")
	quotedTitle := quoteTitle(title)
	singleLineMarker := collapseWhitespace(marker)
	return fmt.Sprintf("- %s %s — %s — %s", date, id, quotedTitle, singleLineMarker)
}

// AppendClosed creates CLOSED.md as needed and skips a re-append when the same
// id already appears under the same date.
func AppendClosed(path, id, title, marker string, when time.Time) error {
	line := FormatClosedLine(id, title, marker, when)

	existing := ""
	raw, err := os.ReadFile(path)
	if err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("followups closed: read %q: %w", path, err)
	}

	date := when.Format("2006-01-02")
	idToken := date + " " + id + " "
	for _, l := range strings.Split(existing, "\n") {
		if strings.Contains(l, idToken) {
			return nil
		}
	}

	// Exactly one newline must precede the appended line.
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

// ParseClosedLine reverses FormatClosedLine, unescaping the title.
func ParseClosedLine(line string) (id, title, marker, date string, err error) {
	if !strings.HasPrefix(line, "- ") {
		return "", "", "", "", fmt.Errorf("followups closed: line must start with '- ': %q", line)
	}
	rest := line[2:]

	if len(rest) < 11 {
		return "", "", "", "", fmt.Errorf("followups closed: line too short to contain date: %q", line)
	}
	date = rest[:10]
	rest = rest[11:] // skip date + space

	const sep = " — "
	parts := strings.SplitN(rest, sep, 3)
	if len(parts) < 3 {
		return "", "", "", "", fmt.Errorf("followups closed: expected 2 em-dash separators: %q", line)
	}

	id = strings.TrimSpace(parts[0])
	rawTitle := strings.TrimSpace(parts[1])
	marker = strings.TrimSpace(parts[2])

	title, err = unquoteTitle(rawTitle)
	if err != nil {
		return "", "", "", "", fmt.Errorf("followups closed: unquote title: %w", err)
	}

	return id, title, marker, date, nil
}

func quoteTitle(s string) string {
	// Backslash first, or the quote escapes get double-escaped.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func unquoteTitle(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("title must be double-quoted, got %q", s)
	}
	inner := s[1 : len(s)-1]
	inner = strings.ReplaceAll(inner, `\"`, `"`)
	inner = strings.ReplaceAll(inner, `\\`, `\`)
	return inner, nil
}

// collapseWhitespace keeps a ledger entry on one line however the marker was
// authored.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
