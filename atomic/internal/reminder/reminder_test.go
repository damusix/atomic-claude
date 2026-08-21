package reminder_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

// remindersDir mirrors reminder.Add's write target: the project-keyed home,
// sandboxed under the TestMain-installed temp $HOME.
func remindersDir(root string) string {
	return config.ProjectRemindersDir(root)
}

// TestMain sandboxes every test in this package under a temp $HOME, since
// reminder.Add/List now resolve their directory relative to the real home
// via config.ProjectRemindersDir rather than a path under root.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "reminder-test-home")
	if err != nil {
		panic(err)
	}
	restore := config.SetHomeDirForTest(home)
	code := m.Run()
	restore()
	os.RemoveAll(home)
	os.Exit(code)
}

func TestAdd_WritesFileWithCorrectFrontmatter(t *testing.T) {
	root := t.TempDir()
	id, err := reminder.Add(root, "benchmark the new query plan")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !strings.HasPrefix(id, "r-") {
		t.Errorf("id %q should start with 'r-'", id)
	}

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

	if !strings.Contains(raw, "id: "+id) {
		t.Errorf("file missing id field %q; got:\n%s", id, raw)
	}
	today := time.Now().UTC().Format("2006-01-02")
	if !strings.Contains(raw, "created: "+today) {
		t.Errorf("file missing created field %q; got:\n%s", today, raw)
	}
	if !strings.Contains(raw, "benchmark the new query plan") {
		t.Errorf("body missing from file; got:\n%s", raw)
	}
}

// Add now writes to the project-keyed home, which is harness-independent —
// unlike ScratchpadDir-derived paths, ProjectRemindersDir does not vary with
// harness.dir. A ".pi" harness therefore no longer moves the write path.
func TestAdd_IgnoresHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()
	id, err := reminder.Add(root, "check the .pi harness wiring")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	dir := config.ProjectRemindersDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file under %s, got %d", dir, len(entries))
	}

	if _, err := os.Stat(filepath.Join(root, ".pi", ".scratchpad", "reminders")); !os.IsNotExist(err) {
		t.Errorf(".pi/.scratchpad/reminders should not exist — reminders no longer live under the harness dir, stat err=%v", err)
	}

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != id {
		t.Errorf("List = %+v, want 1 row with id %q", rows, id)
	}
}

// A reminder written before migration, under a legacy harness-scoped
// directory, must still surface via List's union fallback — the compat
// behavior TestAdd_UnderNonDefaultHarnessDir used to pin under the old
// (now-retired) assumption that harness.dir moved the reminders path.
func TestList_UnionsLegacyHarnessScopedReminders(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()

	legacyDir := config.RemindersDirLegacy(root)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, legacyDir, "2026-05-16-legacy.md", "---\nid: r-leg\ncreated: 2026-05-16\n---\n\nLegacy under .pi\n")

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "r-leg" {
		t.Fatalf("List = %+v, want the legacy row surfaced via union", rows)
	}
}

// The union is not conditioned on the project-keyed directory being empty:
// a reminder written after migration (project-keyed non-empty) must not hide
// a reminder still sitting in the legacy directory.
func TestList_TrueUnion_BothDirsPopulated(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()

	legacyDir := config.RemindersDirLegacy(root)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, legacyDir, "2026-05-16-legacy.md", "---\nid: r-leg\ncreated: 2026-05-16\n---\n\nLegacy under .pi\n")

	// Project-keyed directory now non-empty via a normal Add.
	newID, err := reminder.Add(root, "post-migration reminder")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List = %+v, want 2 rows (legacy + project-keyed), got %d", rows, len(rows))
	}
	var sawLegacy, sawNew bool
	for _, r := range rows {
		if r.ID == "r-leg" {
			sawLegacy = true
		}
		if r.ID == newID {
			sawNew = true
		}
	}
	if !sawLegacy {
		t.Errorf("legacy reminder r-leg missing from union List = %+v", rows)
	}
	if !sawNew {
		t.Errorf("new project-keyed reminder %q missing from union List = %+v", newID, rows)
	}
}

// The same id present in both directories must appear exactly once, with the
// project-keyed copy's content winning.
func TestList_DedupeByID_ProjectKeyedWins(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()

	legacyDir := config.RemindersDirLegacy(root)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, legacyDir, "2026-05-16-dup.md", "---\nid: r-dup\ncreated: 2026-05-16\n---\n\nLegacy copy\n")

	projectDir := config.ProjectRemindersDir(root)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, projectDir, "2026-06-01-dup.md", "---\nid: r-dup\ncreated: 2026-06-01\n---\n\nProject-keyed copy\n")

	rows, err := reminder.List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("List = %+v, want exactly 1 deduped row, got %d", rows, len(rows))
	}
	if rows[0].Created != "2026-06-01" {
		t.Errorf("List[0].Created = %q, want project-keyed copy's %q", rows[0].Created, "2026-06-01")
	}
}

// A legacy-only id (visible via List's union) must still be readable by
// Show — reading a pre-migration reminder is the compatibility this window
// exists for. SetDue/Rm are a different story: they must refuse rather than
// mutate or delete the legacy file, since a legacy-only id has no
// project-keyed copy to write to.
func TestFindByID_ResolvesLegacyOnlyID(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := t.TempDir()

	legacyDir := config.RemindersDirLegacy(root)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixtureName := "2026-05-16-legacy.md"
	fixtureContent := "---\nid: r-leg\ncreated: 2026-05-16\n---\n\nLegacy under .pi\n"
	writeFixture(t, legacyDir, fixtureName, fixtureContent)
	fixturePath := filepath.Join(legacyDir, fixtureName)

	body, err := reminder.Show(root, "r-leg")
	if err != nil {
		t.Fatalf("Show(legacy-only id): %v", err)
	}
	if !strings.Contains(body, "Legacy under .pi") {
		t.Errorf("Show body = %q, want it to contain the legacy body", body)
	}

	before, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read legacy fixture before SetDue: %v", err)
	}

	err = reminder.SetDue(root, "r-leg", "2026-07-01T00:00:00Z")
	if err == nil {
		t.Fatalf("SetDue(legacy-only id) = nil error, want a refusal naming `atomic migrate --repo`")
	}
	if !strings.Contains(err.Error(), "atomic migrate --repo") {
		t.Errorf("SetDue(legacy-only id) error = %q, want it to name `atomic migrate --repo` as the remedy", err.Error())
	}

	after, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read legacy fixture after refused SetDue: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("refused SetDue mutated the legacy file: before=%q after=%q", before, after)
	}

	err = reminder.Rm(root, "r-leg")
	if err == nil {
		t.Fatalf("Rm(legacy-only id) = nil error, want a refusal naming `atomic migrate --repo`")
	}
	if !strings.Contains(err.Error(), "atomic migrate --repo") {
		t.Errorf("Rm(legacy-only id) error = %q, want it to name `atomic migrate --repo` as the remedy", err.Error())
	}

	afterRm, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("expected legacy file still present after refused Rm: %v", err)
	}
	if !bytes.Equal(before, afterRm) {
		t.Errorf("refused Rm mutated the legacy file: before=%q after=%q", before, afterRm)
	}
}

func TestAdd_RejectsEmptyBody(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", "   ", "\t\n"} {
		_, err := reminder.Add(root, bad)
		if err == nil {
			t.Errorf("Add(%q) should have returned an error for empty body", bad)
		}
	}
}

// A colliding slug must yield an id-suffixed filename, not an overwrite.
func TestAdd_CollisionRetry(t *testing.T) {
	root := t.TempDir()
	dir := remindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	id1, err := reminder.Add(root, "same text")
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}

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

	today := time.Now().UTC().Format("2006-01-02")
	plainCount := 0
	suffixCount := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		// <date>-same-text.md vs <date>-same-text-r<hex>.md
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

func TestList_SortedByCreatedThenID(t *testing.T) {
	root := t.TempDir()
	dir := remindersDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

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
	if rows[0].ID != "r-aaa" {
		t.Errorf("first row should be r-aaa (id tie-break), got %q", rows[0].ID)
	}
	if rows[1].ID != "r-zzz" {
		t.Errorf("second row should be r-zzz, got %q", rows[1].ID)
	}
}

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

// List returns the raw first body line; truncation belongs to the rendering
// layer, not here.
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
	if preview != long {
		t.Errorf("List should return raw body; got %q, want %q", preview, long)
	}
}

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

func TestShow_UnknownIDErrors(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Show(root, "r-ffff")
	if err == nil {
		t.Error("Show with unknown id should return an error")
	}
}

func TestRm_DeletesFile(t *testing.T) {
	root := t.TempDir()
	id, err := reminder.Add(root, "to be deleted")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := reminder.Rm(root, id); err != nil {
		t.Fatalf("Rm: %v", err)
	}

	if err := reminder.Rm(root, id); err == nil {
		t.Error("second Rm of same id should return an error")
	}
}

func TestRm_UnknownIDErrors(t *testing.T) {
	root := t.TempDir()
	if err := reminder.Rm(root, "r-0000"); err == nil {
		t.Error("Rm with unknown id should return an error")
	}
}

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

func TestAdd_InvalidDue(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Add(root, "bad due", reminder.WithDue("not-a-date"))
	if err == nil {
		t.Error("Add with malformed due should return an error")
	}
}

func TestAdd_InvalidTransport(t *testing.T) {
	root := t.TempDir()
	_, err := reminder.Add(root, "bad transport", reminder.WithTransport("ftp"))
	if err == nil {
		t.Error("Add with unknown transport should return an error")
	}
}

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
	if !strings.Contains(content, "id: "+id) {
		t.Errorf("id field changed unexpectedly; got:\n%s", content)
	}
	if !strings.Contains(content, "transport: cron") {
		t.Errorf("transport field changed unexpectedly; got:\n%s", content)
	}
	if strings.Contains(content, "due: 2026-05-24T09:00:00Z") {
		t.Errorf("old due still present; got:\n%s", content)
	}
}

func TestSetDue_UnknownID(t *testing.T) {
	root := t.TempDir()
	err := reminder.SetDue(root, "r-ffff", "2026-05-25T09:00:00Z")
	if err == nil {
		t.Error("SetDue with unknown id should return an error")
	}
}

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

// Legacy files predate due/transport and must read back zero-valued.
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
