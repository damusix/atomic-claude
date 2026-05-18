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
// Add generates a non-colliding filename with the id suffix.
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

	// Assert filename shapes: one plain <date>-<slug>.md and one <date>-<slug>-r????.md.
	today := time.Now().UTC().Format("2006-01-02")
	plainCount := 0
	suffixCount := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		// Plain: <date>-same-text.md (no extra -r segment)
		// Suffixed: <date>-same-text-r<hex>.md
		base := strings.TrimSuffix(name, ".md")
		prefix := today + "-same-text"
		if base == prefix {
			plainCount++
		} else if strings.HasPrefix(base, prefix+"-r") {
			suffixCount++
		}
	}
	if plainCount != 1 {
		t.Errorf("expected 1 plain filename, got %d", plainCount)
	}
	if suffixCount != 1 {
		t.Errorf("expected 1 suffixed filename (with -r????), got %d", suffixCount)
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

// TestList_TieBreakByID verifies that when two reminders share the same created
// date, they are sorted by id ascending.
func TestList_TieBreakByID(t *testing.T) {
	root := t.TempDir()
	dir := remindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Both files have the same created date; ids differ — r-aaa < r-zzz.
	writeFixture(t, dir, "2026-05-16-alpha.md", "---\nid: r-zzz\ncreated: 2026-05-16\n---\n\nZeta reminder\n")
	writeFixture(t, dir, "2026-05-16-beta.md", "---\nid: r-aaa\ncreated: 2026-05-16\n---\n\nAlpha reminder\n")

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// r-aaa < r-zzz → r-aaa should come first.
	if rows[0].ID != "r-aaa" {
		t.Errorf("first row should be r-aaa (id tie-break), got %q", rows[0].ID)
	}
	if rows[1].ID != "r-zzz" {
		t.Errorf("second row should be r-zzz, got %q", rows[1].ID)
	}
}

// TestAdd_FrontmatterKeyOrder verifies that Add writes id before created in the file.
func TestAdd_FrontmatterKeyOrder(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Add(root, "check database indices")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	dir := remindersDir(root)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	content := string(raw)

	idIdx := strings.Index(content, "id:")
	createdIdx := strings.Index(content, "created:")
	if idIdx == -1 || createdIdx == -1 {
		t.Fatalf("missing id or created in file:\n%s", content)
	}
	if idIdx > createdIdx {
		t.Errorf("expected id: before created: in file:\n%s", content)
	}
}

// TestList_PreviewIsRaw verifies that reminder.List returns the raw first body
// line without truncation. Truncation is the rendering layer's responsibility
// (hooks package truncates for display; main.go may truncate for its own output).
func TestList_PreviewIsRaw(t *testing.T) {
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
	// Raw body is returned untruncated.
	if preview != long {
		t.Errorf("List should return raw body; got %q, want %q", preview, long)
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

// TestAdd_DueAndTransportFlags verifies that --due and --transport values are
// written into frontmatter when supplied.
func TestAdd_DueAndTransportFlags(t *testing.T) {
	root := t.TempDir()
	due := "2026-05-24T09:00:00Z"
	transport := "routine"
	id, err := reminder.Add(root, "benchmark the query plan", reminder.WithDue(due), reminder.WithTransport(transport))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !strings.HasPrefix(id, "r-") {
		t.Errorf("id %q should start with 'r-'", id)
	}

	entries, _ := os.ReadDir(remindersDir(root))
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(remindersDir(root), entries[0].Name()))
	content := string(raw)

	if !strings.Contains(content, "due: "+due) {
		t.Errorf("file missing due field; got:\n%s", content)
	}
	if !strings.Contains(content, "transport: "+transport) {
		t.Errorf("file missing transport field; got:\n%s", content)
	}
}

// TestAdd_NoDueNoTransport verifies legacy callers (no options) still work and
// the file does not contain due or transport keys.
func TestAdd_NoDueNoTransport(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Add(root, "legacy reminder")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, _ := os.ReadDir(remindersDir(root))
	raw, _ := os.ReadFile(filepath.Join(remindersDir(root), entries[0].Name()))
	content := string(raw)

	if strings.Contains(content, "due:") {
		t.Errorf("file should NOT contain due when not supplied; got:\n%s", content)
	}
	if strings.Contains(content, "transport:") {
		t.Errorf("file should NOT contain transport when not supplied; got:\n%s", content)
	}
}

// TestAdd_FrontmatterKeyOrderV2 verifies the four-key order: id, created, due, transport.
func TestAdd_FrontmatterKeyOrderV2(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Add(root, "order test",
		reminder.WithDue("2026-05-24T09:00:00Z"),
		reminder.WithTransport("cron"),
	)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	dir := remindersDir(root)
	entries, _ := os.ReadDir(dir)
	raw, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	content := string(raw)

	idIdx := strings.Index(content, "id:")
	createdIdx := strings.Index(content, "created:")
	dueIdx := strings.Index(content, "due:")
	transportIdx := strings.Index(content, "transport:")

	if idIdx == -1 || createdIdx == -1 || dueIdx == -1 || transportIdx == -1 {
		t.Fatalf("missing one or more frontmatter keys in:\n%s", content)
	}
	if !(idIdx < createdIdx && createdIdx < dueIdx && dueIdx < transportIdx) {
		t.Errorf("key order wrong; expected id < created < due < transport in:\n%s", content)
	}
}

// TestAdd_InvalidDue verifies that a malformed due timestamp is rejected.
func TestAdd_InvalidDue(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Add(root, "bad due", reminder.WithDue("not-a-date"))
	if err == nil {
		t.Error("Add with malformed due should return an error")
	}
}

// TestAdd_InvalidTransport verifies that an unknown transport kind is rejected.
func TestAdd_InvalidTransport(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Add(root, "bad transport", reminder.WithTransport("ftp"))
	if err == nil {
		t.Error("Add with unknown transport should return an error")
	}
}

// TestSetDue_HappyPath verifies SetDue rewrites only the due field in place.
func TestSetDue_HappyPath(t *testing.T) {
	root := t.TempDir()
	id, err := reminder.Add(root, "snooze me",
		reminder.WithDue("2026-05-24T09:00:00Z"),
		reminder.WithTransport("cron"),
	)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	newDue := "2026-05-25T09:00:00Z"
	if err := reminder.SetDue(root, id, newDue); err != nil {
		t.Fatalf("SetDue: %v", err)
	}

	entries, _ := os.ReadDir(remindersDir(root))
	raw, _ := os.ReadFile(filepath.Join(remindersDir(root), entries[0].Name()))
	content := string(raw)

	if !strings.Contains(content, "due: "+newDue) {
		t.Errorf("expected new due %q in file; got:\n%s", newDue, content)
	}
	// id, transport unchanged
	if !strings.Contains(content, "id: "+id) {
		t.Errorf("id field changed unexpectedly; got:\n%s", content)
	}
	if !strings.Contains(content, "transport: cron") {
		t.Errorf("transport field changed unexpectedly; got:\n%s", content)
	}
	// old due must be gone
	if strings.Contains(content, "due: 2026-05-24T09:00:00Z") {
		t.Errorf("old due still present; got:\n%s", content)
	}
}

// TestSetDue_UnknownID verifies SetDue errors on missing id.
func TestSetDue_UnknownID(t *testing.T) {
	root := t.TempDir()
	err := reminder.SetDue(root, "r-ffff", "2026-05-25T09:00:00Z")
	if err == nil {
		t.Error("SetDue with unknown id should return an error")
	}
}

// TestSetDue_MalformedISO verifies SetDue rejects non-RFC3339 timestamps.
func TestSetDue_MalformedISO(t *testing.T) {
	root := t.TempDir()
	id, err := reminder.Add(root, "snooze me", reminder.WithDue("2026-05-24T09:00:00Z"), reminder.WithTransport("cron"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := reminder.SetDue(root, id, "not-a-date"); err == nil {
		t.Error("SetDue with malformed ISO should return an error")
	}
}

// TestList_ExposeDueAndTransport verifies that Row.Due and Row.Transport are
// populated from frontmatter when present, and zero-valued for legacy files.
func TestList_ExposeDueAndTransport(t *testing.T) {
	root := t.TempDir()
	dir := remindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Modern file with due and transport.
	writeFixture(t, dir, "2026-05-17-modern.md",
		"---\nid: r-mod\ncreated: 2026-05-17\ndue: 2026-05-24T09:00:00Z\ntransport: routine\n---\n\nModern reminder\n")
	// Legacy file without due/transport.
	writeFixture(t, dir, "2026-05-16-legacy.md",
		"---\nid: r-leg\ncreated: 2026-05-16\n---\n\nLegacy reminder\n")

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Sorted ascending by created: legacy first, modern second.
	legacy := rows[0]
	modern := rows[1]

	if legacy.Due != "" {
		t.Errorf("legacy row Due should be empty, got %q", legacy.Due)
	}
	if legacy.Transport != "" {
		t.Errorf("legacy row Transport should be empty, got %q", legacy.Transport)
	}
	if modern.Due != "2026-05-24T09:00:00Z" {
		t.Errorf("modern row Due wrong; got %q", modern.Due)
	}
	if modern.Transport != "routine" {
		t.Errorf("modern row Transport wrong; got %q", modern.Transport)
	}
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeFixture %q: %v", name, err)
	}
}
