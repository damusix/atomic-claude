package doctor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/followups"
)

// makeFollowupsFolder populates the followups tree from a map of filename to
// raw document content.
func makeFollowupsFolder(t *testing.T, root string, entries map[string]string) {
	t.Helper()
	dir := config.FollowupsDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	for name, content := range entries {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// writeIndex writes an INDEX.md file into the followups folder.
func writeIndex(t *testing.T, root, content string) {
	t.Helper()
	dir := config.FollowupsDir(root)
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

// "No followups at all" is a benign state, not something to WARN about.
func TestCheckFollowupsSkip_FolderAbsent(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.SKIP {
		t.Errorf("severity = %v, want SKIP (detail: %s)", r.Severity, r.Detail)
	}
}

// A past review_by is reported, never auto-closed.
func TestCheckFollowupsWarn_StaleEntry(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"stale-F-1.md": staleEntry("stale-F-1", "A stale entry"),
	})
	// Matching INDEX, so index drift does not fire alongside staleness.
	dir := config.FollowupsDir(root)
	entries, _, _ := followups.LoadEntriesWithErrors(dir)
	idx := followups.Render(entries, time.Now())
	writeIndex(t, root, idx)

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (detail: %s)", r.Severity, r.Detail)
	}
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

// Catches an entry hand-edited without regenerating the index.
func TestCheckFollowupsWarn_IndexDrift(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"fresh-F-1.md": freshEntry("fresh-F-1", "A fresh entry"),
	})
	writeIndex(t, root, "# stale index content\n")

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (detail: %s)", r.Severity, r.Detail)
	}
}

func TestCheckFollowupsWarn_InvalidFrontmatter(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"broken-F-1.md": "no frontmatter here\n",
	})
	// The broken file yields zero valid entries, so an empty render is the
	// in-sync INDEX and only the parse error fires.
	writeIndex(t, root, followups.Render(nil, time.Now()))

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (detail: %s)", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty; want filename mention")
	}
}

func TestCheckFollowupsPass_FreshAndInSync(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, map[string]string{
		"fresh-F-1.md": freshEntry("fresh-F-1", "A fresh entry"),
		"fresh-F-2.md": freshEntry("fresh-F-2", "Another fresh entry"),
	})
	dir := config.FollowupsDir(root)
	entries, _, _ := followups.LoadEntriesWithErrors(dir)
	today := time.Now()
	idx := followups.Render(entries, today)
	writeIndex(t, root, idx)

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}

func TestCheckFollowups_UnderNonDefaultStateDir(t *testing.T) {
	root := t.TempDir()
	unpinnedState := config.ProjectDir(root)
	restore := config.SetStateDirForTest(".state")
	defer restore()

	dir := filepath.Join(root, ".state", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh-F-1.md"), []byte(freshEntry("fresh-F-1", "A fresh entry")), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _, _ := followups.LoadEntriesWithErrors(dir)
	idx := followups.Render(entries, time.Now())
	if err := os.WriteFile(filepath.Join(dir, "INDEX.md"), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS under a pinned state dir (detail: %s)", r.Severity, r.Detail)
	}

	// The unpinned state tree is absent, so a check ignoring the pin would
	// have reported SKIP above.
	if _, err := os.Stat(unpinnedState); !os.IsNotExist(err) {
		t.Fatalf("test setup error: %s should not exist, stat err=%v", unpinnedState, err)
	}
}

func TestCheckFollowupsPass_EmptyFolder(t *testing.T) {
	root := t.TempDir()
	makeFollowupsFolder(t, root, nil)
	writeIndex(t, root, followups.Render(nil, time.Now()))

	r := doctor.RunCheckFollowupsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}
