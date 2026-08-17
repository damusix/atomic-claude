package followups

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CloseEntry appends to CLOSED.md, deletes the entry, and regenerates INDEX.md.
// A non-empty reason becomes the marker, else "*(closed YYYY-MM-DD)*".
func CloseEntry(dir, id, reason string, today time.Time) error {
	entryPath := filepath.Join(dir, id+".md")

	raw, err := os.ReadFile(entryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("followups close: id %q not found in %s", id, dir)
		}
		return fmt.Errorf("followups close: read %q: %w", entryPath, err)
	}
	e, err := ParseEntry(string(raw))
	if err != nil {
		return fmt.Errorf("followups close: parse %q: %w", entryPath, err)
	}

	marker := reason
	if marker == "" {
		marker = "*(closed " + today.Format("2006-01-02") + ")*"
	}

	closedPath := filepath.Join(dir, "CLOSED.md")
	if err := AppendClosed(closedPath, id, e.Title, marker, today); err != nil {
		return fmt.Errorf("followups close: %w", err)
	}

	if err := os.Remove(entryPath); err != nil {
		return fmt.Errorf("followups close: remove %q: %w", entryPath, err)
	}

	entries, err := LoadEntries(dir)
	if err != nil {
		return fmt.Errorf("followups close: reload entries: %w", err)
	}
	indexContent := Render(entries, today)
	indexPath := filepath.Join(dir, "INDEX.md")
	if err := os.WriteFile(indexPath, []byte(indexContent), 0o644); err != nil {
		return fmt.Errorf("followups close: write INDEX.md: %w", err)
	}

	return nil
}
