package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/scratchpad"
)

// TestMain sandboxes every test in this package under a temp $HOME and a
// fixed harness dir, since relocateReportsAndReminders/redateScratchpadBundles
// resolve project-keyed state via config.ProjectStateDir, which defaults to
// the real ~/.atomic without this.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "atomic-migrate-test-home")
	if err != nil {
		panic(err)
	}
	restoreHome := config.SetHomeDirForTest(home)
	restoreHarness := config.SetHarnessDirForTest(".claude")
	code := m.Run()
	restoreHarness()
	restoreHome()
	os.RemoveAll(home)
	os.Exit(code)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old

	buf := make([]byte, 64*1024)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeDoc(t *testing.T, root, kind, slug string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "docs", kind, slug+".md"), "# "+slug+"\n")
}

// --- relocation ---

func TestScratchpadRelocateMovesReportsAndReminders(t *testing.T) {
	root := t.TempDir()
	legacyReports := filepath.Join(config.ScratchpadDir(root), "session-reports", "main")
	writeFile(t, filepath.Join(legacyReports, "2026-08-01-note.md"), "report body")
	legacyReminders := config.RemindersDirLegacy(root)
	writeFile(t, filepath.Join(legacyReminders, "r1.md"), "reminder body")

	if err := scratchpadRelocate(&Context{Root: root}); err != nil {
		t.Fatalf("scratchpadRelocate: %v", err)
	}

	newReport := filepath.Join(config.ReportsRoot(root), "main", "2026-08-01-note.md")
	if data, err := os.ReadFile(newReport); err != nil || string(data) != "report body" {
		t.Fatalf("expected report relocated to %s, err=%v", newReport, err)
	}
	newReminder := filepath.Join(config.ProjectRemindersDir(root), "r1.md")
	if data, err := os.ReadFile(newReminder); err != nil || string(data) != "reminder body" {
		t.Fatalf("expected reminder relocated to %s, err=%v", newReminder, err)
	}
	if _, err := os.Stat(filepath.Join(config.ScratchpadDir(root), "session-reports")); !os.IsNotExist(err) {
		t.Errorf("expected legacy session-reports dir removed, err=%v", err)
	}
	if _, err := os.Stat(legacyReminders); !os.IsNotExist(err) {
		t.Errorf("expected legacy reminders dir removed, err=%v", err)
	}
}

func TestScratchpadRelocateSecondRunNoops(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(config.ScratchpadDir(root), "session-reports", "main", "note.md"), "x")
	writeFile(t, filepath.Join(config.RemindersDirLegacy(root), "r1.md"), "y")

	if err := scratchpadRelocate(&Context{Root: root}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := scratchpadRelocate(&Context{Root: root}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	// still exactly one copy at destination, not duplicated or errored
	data, err := os.ReadFile(filepath.Join(config.ReportsRoot(root), "main", "note.md"))
	if err != nil || string(data) != "x" {
		t.Fatalf("report missing or altered after second run: err=%v data=%q", err, data)
	}
}

func TestScratchpadRelocateSkipsDestinationCollision(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(config.ScratchpadDir(root), "reminders", "r1.md"), "new content")
	writeFile(t, filepath.Join(config.ProjectRemindersDir(root), "r1.md"), "existing content")

	out := captureStdout(t, func() {
		if err := scratchpadRelocate(&Context{Root: root}); err != nil {
			t.Fatalf("scratchpadRelocate: %v", err)
		}
	})

	if !strings.Contains(out, "r1.md") {
		t.Errorf("expected skip report naming r1.md, got %q", out)
	}
	// left in source, untouched
	data, err := os.ReadFile(filepath.Join(config.ScratchpadDir(root), "reminders", "r1.md"))
	if err != nil || string(data) != "new content" {
		t.Fatalf("expected colliding file left in source: err=%v data=%q", err, data)
	}
	// destination copy never overwritten
	data, err = os.ReadFile(filepath.Join(config.ProjectRemindersDir(root), "r1.md"))
	if err != nil || string(data) != "existing content" {
		t.Fatalf("expected destination file untouched: err=%v data=%q", err, data)
	}
}

// --- dated-bundle rename ---

func TestRedateRenamesWhenBothDocsExist(t *testing.T) {
	root := t.TempDir()
	slug := "serve-plans-page"
	dated := filepath.Join(config.ScratchpadDir(root), "2026-08-19-"+slug)
	writeFile(t, filepath.Join(dated, "BRIEF.md"), "brief")
	writeDoc(t, root, "design", slug)
	writeDoc(t, root, "spec", slug)
	mtime := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(dated, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := redateScratchpadBundles(root); err != nil {
		t.Fatalf("redateScratchpadBundles: %v", err)
	}

	renamed := filepath.Join(config.ScratchpadDir(root), slug)
	meta, err := scratchpad.Load(renamed)
	if err != nil {
		t.Fatalf("Load renamed bundle meta: %v", err)
	}
	if meta.Slug != slug {
		t.Errorf("Slug = %q, want %q", meta.Slug, slug)
	}
	if meta.Created != "2026-08-19" {
		t.Errorf("Created = %q, want date prefix 2026-08-19", meta.Created)
	}
	if meta.Updated != "2026-08-19" {
		t.Errorf("Updated = %q, want dir mtime 2026-08-19", meta.Updated)
	}
	if len(meta.Purposes) != 1 || meta.Purposes[0] != "plan" {
		t.Errorf("Purposes = %v, want [plan]", meta.Purposes)
	}
	if meta.Status != "active" {
		t.Errorf("Status = %q, want active", meta.Status)
	}
	if meta.Description != "migrated" {
		t.Errorf("Description = %q, want migrated", meta.Description)
	}
	if _, err := os.Stat(dated); !os.IsNotExist(err) {
		t.Errorf("expected old dated dir gone, err=%v", err)
	}
}

func TestRedateSecondRunNoops(t *testing.T) {
	root := t.TempDir()
	slug := "already-renamed"
	dated := filepath.Join(config.ScratchpadDir(root), "2026-08-19-"+slug)
	writeFile(t, filepath.Join(dated, "BRIEF.md"), "brief")
	writeDoc(t, root, "design", slug)
	writeDoc(t, root, "spec", slug)

	if err := redateScratchpadBundles(root); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := redateScratchpadBundles(root); err != nil {
		t.Fatalf("second run: %v", err)
	}
	renamed := filepath.Join(config.ScratchpadDir(root), slug)
	if _, err := os.Stat(filepath.Join(renamed, "meta.toml")); err != nil {
		t.Fatalf("expected renamed bundle still present after second run: %v", err)
	}
}

func TestRedateExcludesSilentlyWhenNoDocsMatch(t *testing.T) {
	root := t.TempDir()
	// <date>-spec-<slug> shape: no docs/design or docs/spec named
	// "spec-serve-plans-page" — never a candidate, not even a skip.
	dated := filepath.Join(config.ScratchpadDir(root), "2026-08-19-spec-serve-plans-page")
	writeFile(t, filepath.Join(dated, "BRIEF.md"), "brief")

	out := captureStdout(t, func() {
		if err := redateScratchpadBundles(root); err != nil {
			t.Fatalf("redateScratchpadBundles: %v", err)
		}
	})

	if strings.TrimSpace(out) != "" {
		t.Errorf("expected no output for a non-candidate directory, got %q", out)
	}
	if _, err := os.Stat(dated); err != nil {
		t.Errorf("expected non-candidate dir left untouched, err=%v", err)
	}
}

func TestRedateSkipsWhenDestinationAlreadyExists(t *testing.T) {
	root := t.TempDir()
	slug := "taken"
	dated := filepath.Join(config.ScratchpadDir(root), "2026-08-19-"+slug)
	writeFile(t, filepath.Join(dated, "BRIEF.md"), "brief")
	writeDoc(t, root, "design", slug)
	writeDoc(t, root, "spec", slug)
	// destination already occupied by a live bundle
	writeFile(t, filepath.Join(config.ScratchpadDir(root), slug, "BRIEF.md"), "existing")

	out := captureStdout(t, func() {
		if err := redateScratchpadBundles(root); err != nil {
			t.Fatalf("redateScratchpadBundles: %v", err)
		}
	})

	if !strings.Contains(out, dated) && !strings.Contains(out, "2026-08-19-"+slug) {
		t.Errorf("expected skip reason naming the dated dir, got %q", out)
	}
	if _, err := os.Stat(dated); err != nil {
		t.Errorf("expected dated dir left in place, err=%v", err)
	}
}

func TestRedateSkipsWhenSourceAlreadyHasMeta(t *testing.T) {
	root := t.TempDir()
	slug := "already-a-bundle"
	dated := filepath.Join(config.ScratchpadDir(root), "2026-08-19-"+slug)
	writeFile(t, filepath.Join(dated, "meta.toml"), "slug = \"2026-08-19-"+slug+"\"\nstatus=\"active\"\n")
	writeDoc(t, root, "design", slug)
	writeDoc(t, root, "spec", slug)

	out := captureStdout(t, func() {
		if err := redateScratchpadBundles(root); err != nil {
			t.Fatalf("redateScratchpadBundles: %v", err)
		}
	})

	if !strings.Contains(out, "meta.toml") {
		t.Errorf("expected skip reason mentioning meta.toml, got %q", out)
	}
	if _, err := os.Stat(dated); err != nil {
		t.Errorf("expected dated dir left in place, err=%v", err)
	}
	renamed := filepath.Join(config.ScratchpadDir(root), slug)
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Errorf("expected no rename to occur, err=%v", err)
	}
}

func TestRedateSkipsBothOnCollision(t *testing.T) {
	root := t.TempDir()
	slug := "collide"
	datedA := filepath.Join(config.ScratchpadDir(root), "2026-08-01-"+slug)
	datedB := filepath.Join(config.ScratchpadDir(root), "2026-08-02-"+slug)
	writeFile(t, filepath.Join(datedA, "BRIEF.md"), "a")
	writeFile(t, filepath.Join(datedB, "BRIEF.md"), "b")
	writeDoc(t, root, "design", slug)
	writeDoc(t, root, "spec", slug)

	out := captureStdout(t, func() {
		if err := redateScratchpadBundles(root); err != nil {
			t.Fatalf("redateScratchpadBundles: %v", err)
		}
	})

	if !strings.Contains(out, "2026-08-01-"+slug) || !strings.Contains(out, "2026-08-02-"+slug) {
		t.Errorf("expected skip reason naming both colliding dirs, got %q", out)
	}
	for _, d := range []string{datedA, datedB} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("expected %s left in place, err=%v", d, err)
		}
	}
	renamed := filepath.Join(config.ScratchpadDir(root), slug)
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Errorf("expected no rename for either colliding dir, err=%v", err)
	}
}

func TestRedateSkipsWhenOnlyOneDocExists(t *testing.T) {
	root := t.TempDir()
	slug := "half-documented"
	dated := filepath.Join(config.ScratchpadDir(root), "2026-08-19-"+slug)
	writeFile(t, filepath.Join(dated, "BRIEF.md"), "brief")
	writeDoc(t, root, "design", slug) // spec missing

	out := captureStdout(t, func() {
		if err := redateScratchpadBundles(root); err != nil {
			t.Fatalf("redateScratchpadBundles: %v", err)
		}
	})

	// Per the design doc, a stripped name with no docs at all is a silent
	// exclusion — but this fixture has exactly one of the two, which the
	// spec lists as a printed skip, not a silent exclusion.
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected a skip reason for a half-documented candidate, got empty output")
	}
	if _, err := os.Stat(dated); err != nil {
		t.Errorf("expected dated dir left in place, err=%v", err)
	}
}
