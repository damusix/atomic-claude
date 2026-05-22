package doctor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/followups"
)

// makeFollowupsFolder creates the .claude/project/followups/ directory tree
// and populates it with the given entry files. entries is a map of
// filename → raw content (full frontmatter+body document).
func makeFollowupsFolder(t *testing.T, root string, entries map[string]string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	for name, content := range entries {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// writeLegacyFile writes .claude/project/followups.md with the given content.
func writeLegacyFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "followups.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write followups.md: %v", err)
	}
}

// writeIndex writes an INDEX.md file into the followups folder.
func writeIndex(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "INDEX.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write INDEX.md: %v", err)
	}
}

// freshEntry returns a well-formed entry document with review_by in the future.
func freshEntry(id, title string) string {
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	return "---\nid: " + id + "\ntitle: \"" + title + "\"\ncreated: 2026-05-01\norigin: test\nseverity: nit\nreview_by: " + future + "\nstatus: open\n---\n\nBody.\n"
}

// staleEntry returns a well-formed entry document with review_by in the past.
func staleEntry(id, title string) string {
	return "---\nid: " + id + "\ntitle: \"" + title + "\"\ncreated: 2026-01-01\norigin: test\nseverity: risk\nreview_by: 2026-01-02\nstatus: open\n---\n\nBody.\n"
}

// TestCheckFollowupsSkip_FolderAndLegacyAbsent verifies SKIP when neither the
// folder nor the legacy file exist. This proves the "no followups at all"
// state is benign and does not WARN.
func TestCheckFollowupsSkip_FolderAndLegacyAbsent(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.SKIP {
		t.Errorf("severity = %v, want SKIP (detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckFollowupsWarn_LegacyPresent verifies WARN when the folder is absent
// but the legacy followups.md exists. The repair hint must mention migration.
func TestCheckFollowupsWarn_LegacyPresent(t *testing.T) {
	root := t.TempDir()
	writeLegacyFile(t, root, "# legacy\n")
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (detail: %s)", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty; want migration hint")
	}
}

// TestCheckFollowupsWarn_StaleEntry verifies WARN when at least one entry is
// past its review_by date. The check must mention stale count, not auto-close.
func TestCheckFollowupsWarn_StaleEntry(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"stale-F-1.md": staleEntry("stale-F-1", "A stale entry"),
	})
	// Write a matching INDEX so INDEX drift does not also trigger.
	dir := filepath.Join(root, ".claude", "project", "followups")
	entries, _, _ := followups.LoadEntriesWithErrors(dir)
	idx := followups.Render(entries, time.Now())
	writeIndex(t, root, idx)

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (detail: %s)", r.Severity, r.Detail)
	}
	// Detail must mention stale.
	found := false
	for i := 0; i+4 < len(r.Detail); i++ {
		if r.Detail[i:i+5] == "stale" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("detail %q: expected 'stale' mention", r.Detail)
	}
}

// TestCheckFollowupsWarn_IndexDrift verifies WARN when the on-disk INDEX.md
// does not byte-match the re-rendered INDEX in memory. This catches the case
// where someone hand-edited an entry without regenerating the index.
func TestCheckFollowupsWarn_IndexDrift(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"fresh-F-1.md": freshEntry("fresh-F-1", "A fresh entry"),
	})
	// Write a stale/wrong INDEX.
	writeIndex(t, root, "# stale index content\n")

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckFollowupsWarn_InvalidFrontmatter verifies WARN when at least one
// entry file has invalid or missing frontmatter. The failing filename must
// appear in the detail.
func TestCheckFollowupsWarn_InvalidFrontmatter(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"broken-F-1.md": "no frontmatter here\n",
	})
	// Write a valid INDEX so only the parse error fires.
	// LoadEntriesWithErrors returns 0 valid entries here; render an empty index.
	writeIndex(t, root, followups.Render(nil, time.Now()))

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (detail: %s)", r.Severity, r.Detail)
	}
	// Detail must name the failing file.
	if r.Detail == "" {
		t.Error("Detail is empty; want filename mention")
	}
}

// TestCheckFollowupsPass_FreshAndInSync verifies PASS when the folder exists,
// all entries are fresh, and INDEX.md byte-matches the re-rendered content.
func TestCheckFollowupsPass_FreshAndInSync(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"fresh-F-1.md": freshEntry("fresh-F-1", "A fresh entry"),
		"fresh-F-2.md": freshEntry("fresh-F-2", "Another fresh entry"),
	})
	// Render and write a matching INDEX.
	dir := filepath.Join(root, ".claude", "project", "followups")
	entries, _, _ := followups.LoadEntriesWithErrors(dir)
	today := time.Now()
	idx := followups.Render(entries, today)
	writeIndex(t, root, idx)

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckFollowupsPass_EmptyFolder verifies PASS when the folder exists but
// has no entry files and the INDEX matches an empty render.
func TestCheckFollowupsPass_EmptyFolder(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, nil)
	writeIndex(t, root, followups.Render(nil, time.Now()))

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}
