package reminder_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

func remindersDir(root string) string {
	return filepath.Join(root, ".claude", ".scratchpad", "reminders")
}

// TestAdd_WritesFileWithCorrectFrontmatter verifies Add creates a file with
// the right frontmatter fields and body.
func TestAdd_WritesFileWithCorrectFrontmatter(t *testing.T) {
	root := t.TempDir()
	id, err := reminder.Add(root, "benchmark the new query plan")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !strings.HasPrefix(id, "r-") {
		t.Errorf("id %q should start with 'r-'", id)
	}

	// Verify a file was written.
	entries, err := os.ReadDir(remindersDir(root))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	content, err := os.ReadFile(filepath.Join(remindersDir(root), entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	raw := string(content)

	// Frontmatter id matches returned id.
	if !strings.Contains(raw, "id: "+id) {
		t.Errorf("file missing id field %q; got:\n%s", id, raw)
	}
	// created is today (UTC).
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.Contains(raw, "created: "+today) {
		t.Errorf("file missing created field %q; got:\n%s", today, raw)
	}
	// Body is present.
	if !strings.Contains(raw, "benchmark the new query plan") {
		t.Errorf("body missing from file; got:\n%s", raw)
	}
}

// TestAdd_RejectsEmptyBody ensures blank/whitespace-only body is rejected.
func TestAdd_RejectsEmptyBody(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", "   ", "\t\n"} {
		_, err := reminder.Add(root, bad)
		if err == nil {
			t.Errorf("Add(%q) should have returned an error for empty body", bad)
		}
	}
}

// TestAdd_CollisionRetry verifies that if the target path already exists,
// Add generates a non-colliding filename.
func TestAdd_CollisionRetry(t *testing.T) {
	root := t.TempDir()
	dir := remindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Add once to learn the exact filename.
	id1, err := reminder.Add(root, "same text")
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}

	// Add again with the same body — must not fail and must produce a different file.
	id2, err := reminder.Add(root, "same text")
	if err != nil {
		t.Fatalf("second Add (collision retry): %v", err)
	}
	if id1 == id2 {
		t.Errorf("collision produced duplicate id: %q", id1)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("expected 2 files after collision, got %d", len(entries))
	}
}

// TestList_EmptyDir returns empty output without error.
func TestList_EmptyDir(t *testing.T) {
	root := t.TempDir()
	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List on empty dir: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// TestList_MissingDir returns empty output without error.
func TestList_MissingDir(t *testing.T) {
	root := t.TempDir()
	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List on missing dir: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// TestList_SortedByCreatedThenID verifies ascending sort.
func TestList_SortedByCreatedThenID(t *testing.T) {
	root := t.TempDir()
	dir := remindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write two files manually with distinct dates to control order.
	writeFixture(t, dir, "2026-05-14-aaa.md", "---\nid: r-0001\ncreated: 2026-05-14\n---\n\nOlder reminder\n")
	writeFixture(t, dir, "2026-05-15-bbb.md", "---\nid: r-0002\ncreated: 2026-05-15\n---\n\nNewer reminder\n")

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ID != "r-0001" {
		t.Errorf("first row should be older (r-0001), got %q", rows[0].ID)
	}
	if rows[1].ID != "r-0002" {
		t.Errorf("second row should be newer (r-0002), got %q", rows[1].ID)
	}
}

// TestList_TruncatesLongBody verifies first-line truncation at 80 chars.
func TestList_TruncatesLongBody(t *testing.T) {
	root := t.TempDir()
	dir := remindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	long := strings.Repeat("x", 90)
	writeFixture(t, dir, "2026-05-16-long.md", "---\nid: r-long\ncreated: 2026-05-16\n---\n\n"+long+"\n")

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	preview := rows[0].Preview
	if len([]rune(preview)) > 81 { // 80 chars + ellipsis = 81
		t.Errorf("preview too long (%d chars): %q", len([]rune(preview)), preview)
	}
	if !strings.HasSuffix(preview, "…") {
		t.Errorf("preview should end with ellipsis, got %q", preview)
	}
}

// TestShow_ReturnBodyStripsFrontmatter verifies frontmatter is stripped.
func TestShow_ReturnBodyStripsFrontmatter(t *testing.T) {
	root := t.TempDir()
	id, err := reminder.Add(root, "check the logs")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	body, err := reminder.Show(root, id)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if strings.Contains(body, "id:") || strings.Contains(body, "created:") {
		t.Errorf("Show should strip frontmatter; got:\n%s", body)
	}
	if !strings.Contains(body, "check the logs") {
		t.Errorf("Show body missing reminder text; got:\n%s", body)
	}
}

// TestShow_UnknownIDErrors verifies exit with error for unknown id.
func TestShow_UnknownIDErrors(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Show(root, "r-ffff")
	if err == nil {
		t.Error("Show with unknown id should return an error")
	}
}

// TestRm_DeletesFile verifies rm removes the reminder.
func TestRm_DeletesFile(t *testing.T) {
	root := t.TempDir()
	id, err := reminder.Add(root, "to be deleted")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := reminder.Rm(root, id); err != nil {
		t.Fatalf("Rm: %v", err)
	}

	// Second rm should error.
	if err := reminder.Rm(root, id); err == nil {
		t.Error("second Rm of same id should return an error")
	}
}

// TestRm_UnknownIDErrors verifies rm errors for unknown id.
func TestRm_UnknownIDErrors(t *testing.T) {
	root := t.TempDir()
	if err := reminder.Rm(root, "r-0000"); err == nil {
		t.Error("Rm with unknown id should return an error")
	}
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFixture %q: %v", name, err)
	}
}
