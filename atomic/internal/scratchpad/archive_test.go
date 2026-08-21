package scratchpad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestMain sandboxes every test in this package under a temp $HOME. Archive
// resolves the project-keyed archive home via config.ProjectStateDir, which
// defaults to the real ~/.atomic without this.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "atomic-scratchpad-test-home")
	if err != nil {
		panic(err)
	}
	restore := config.SetHomeDirForTest(home)
	code := m.Run()
	restore()
	os.RemoveAll(home)
	os.Exit(code)
}

// Archive sets status=archived and moves the bundle to
// archive/<slug>/<created>/, keyed by the bundle's own creation date.
func TestArchiveMovesBundleAndSetsStatus(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "my-feature", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	liveDir := bundleDir(root, "my-feature")

	dest, err := Archive(root, "my-feature")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if _, err := os.Stat(liveDir); !os.IsNotExist(err) {
		t.Errorf("expected live bundle dir to be gone, got err=%v", err)
	}
	meta, err := Load(dest)
	if err != nil {
		t.Fatalf("Load(dest): %v", err)
	}
	if meta.Status != "archived" {
		t.Errorf("Status = %q, want archived", meta.Status)
	}

	wantDest := config.ArchiveDir(root, "my-feature", meta.Created[:10])
	if dest != wantDest {
		t.Errorf("dest = %q, want %q", dest, wantDest)
	}
}

// Archiving the same slug twice, after re-creating it, must produce two
// distinct dated directories — neither ever overwritten.
func TestArchiveTwiceAfterRecreateProducesTwoDirectories(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "s", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	firstDest, err := Archive(root, "s")
	if err != nil {
		t.Fatalf("Archive (1st): %v", err)
	}

	if _, _, err := New(root, "s", "implement"); err != nil {
		t.Fatalf("New (re-create): %v", err)
	}
	secondDest, err := Archive(root, "s")
	if err != nil {
		t.Fatalf("Archive (2nd): %v", err)
	}

	if firstDest == secondDest {
		t.Fatalf("expected two distinct archive dirs, got the same: %q", firstDest)
	}
	for _, dir := range []string{firstDest, secondDest} {
		if _, err := os.Stat(filepath.Join(dir, "meta.toml")); err != nil {
			t.Errorf("expected %s to still hold its bundle: %v", dir, err)
		}
	}
}

// A same-day collision (create -> archive -> re-create -> re-archive within
// one day, so both share one <created> date) takes the next free "-2" suffix
// rather than overwriting the first archive.
func TestArchiveSameDayCollisionTakesSuffixedDir(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "s", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	// Pin both bundles to the same creation date so the collision reproduces
	// deterministically, without depending on the test running twice within
	// the same real-world second.
	pin := func(created string) {
		m, err := Load(bundleDir(root, "s"))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		m.Created = created
		if err := Save(bundleDir(root, "s"), m); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	pin("2026-08-20T09:00:00Z")

	firstDest, err := Archive(root, "s")
	if err != nil {
		t.Fatalf("Archive (1st): %v", err)
	}
	if filepath.Base(firstDest) != "2026-08-20" {
		t.Fatalf("firstDest = %q, want basename 2026-08-20", firstDest)
	}

	if _, _, err := New(root, "s", "implement"); err != nil {
		t.Fatalf("New (re-create): %v", err)
	}
	pin("2026-08-20T15:00:00Z")

	secondDest, err := Archive(root, "s")
	if err != nil {
		t.Fatalf("Archive (2nd, same day): %v", err)
	}

	if secondDest == firstDest {
		t.Fatalf("second archive collided with the first: %q", secondDest)
	}
	if filepath.Base(secondDest) != "2026-08-20-2" {
		t.Errorf("secondDest = %q, want basename 2026-08-20-2", secondDest)
	}

	for _, dir := range []string{firstDest, secondDest} {
		if _, err := os.Stat(filepath.Join(dir, "meta.toml")); err != nil {
			t.Errorf("expected %s to still hold its bundle: %v", dir, err)
		}
	}
}

// An empty Created must fail loudly rather than collapse the required
// <slug>/<created>/ two-level shape to one level.
func TestArchiveOnEmptyCreatedErrorsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "s", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	m, err := Load(bundleDir(root, "s"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Created = ""
	if err := Save(bundleDir(root, "s"), m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	liveDir := bundleDir(root, "s")

	if _, err := Archive(root, "s"); err == nil {
		t.Fatalf("expected Archive to error on an empty Created")
	}

	if _, err := os.Stat(liveDir); err != nil {
		t.Errorf("expected live bundle dir to still exist (not moved), got err=%v", err)
	}
	archiveRoot := ArchiveRoot(root)
	if entries, _ := os.ReadDir(archiveRoot); len(entries) != 0 {
		t.Errorf("expected nothing written to the archive root, got %v", entries)
	}
}

// A hand-edited Created that looks like a path-escape payload must fail
// rather than let the destination land outside the archive root.
func TestArchiveOnPathEscapeCreatedErrorsAndStaysInsideArchiveRoot(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "s", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	m, err := Load(bundleDir(root, "s"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.Created = "../../../evil"
	if err := Save(bundleDir(root, "s"), m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	liveDir := bundleDir(root, "s")

	dest, err := Archive(root, "s")
	if err == nil {
		t.Fatalf("expected Archive to error on a path-escape Created, got dest=%q", dest)
	}

	if _, err := os.Stat(liveDir); err != nil {
		t.Errorf("expected live bundle dir to still exist (not moved), got err=%v", err)
	}
	archiveRoot := ArchiveRoot(root)
	rel, relErr := filepath.Rel(archiveRoot, dest)
	if relErr == nil && !strings.HasPrefix(rel, "..") && dest != "" {
		t.Errorf("dest %q must not resolve inside %q when Archive errored", dest, archiveRoot)
	}

	homeParent := filepath.Dir(filepath.Dir(archiveRoot)) // .../.atomic
	if entries, _ := os.ReadDir(homeParent); len(entries) > 0 {
		for _, e := range entries {
			if e.Name() == "e" || e.Name() == "evil" {
				t.Errorf("path escape landed at %s/%s", homeParent, e.Name())
			}
		}
	}
}

func TestArchiveOnMissingSlugErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := Archive(root, "does-not-exist"); err == nil {
		t.Fatalf("expected an error archiving a slug with no bundle")
	}
}

// HasArchive is an exact-slug stat check, regardless of how many dated
// archives the slug holds.
func TestHasArchiveExactMatch(t *testing.T) {
	root := t.TempDir()
	if _, _, err := New(root, "s", "implement"); err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := HasArchive(root, "s"); ok {
		t.Fatalf("expected no archive before Archive is called")
	}

	if _, err := Archive(root, "s"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	dir, ok := HasArchive(root, "s")
	if !ok {
		t.Fatalf("expected HasArchive to find s after archiving")
	}
	if filepath.Base(dir) != "s" {
		t.Errorf("HasArchive dir = %q, want basename s", dir)
	}

	if _, ok := HasArchive(root, "s-other"); ok {
		t.Fatalf("expected no exact match for a different slug")
	}
}
