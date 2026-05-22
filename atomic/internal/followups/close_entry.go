package followups

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CloseEntry closes an open follow-up entry by id.
// It appends a line to CLOSED.md, deletes the entry file, and regenerates INDEX.md.
// If reason is non-empty it is used as the CLOSED.md marker; otherwise a default
// "*(closed YYYY-MM-DD)*" marker is generated.
// Returns an error (exit-1 equivalent) if the id file does not exist.
func CloseEntry(dir, id, reason string, today time.Time) error {
	entryPath := filepath.Join(dir, id+".md")

	// Read the entry to get its title.
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

	// Build the marker.
	marker := reason
	if marker == "" {
		marker = "*(closed " + today.Format("2006-01-02") + ")*"
	}

	// Append to CLOSED.md.
	closedPath := filepath.Join(dir, "CLOSED.md")
	if err := AppendClosed(closedPath, id, e.Title, marker, today); err != nil {
		return fmt.Errorf("followups close: %w", err)
	}

	// Delete the entry file.
	if err := os.Remove(entryPath); err != nil {
		return fmt.Errorf("followups close: remove %q: %w", entryPath, err)
	}

	// Regenerate INDEX.md.
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
