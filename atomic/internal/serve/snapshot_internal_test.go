package serve

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Backdates the mtime past any quiet window these tests use, so a fixture file
// is eligible for the fingerprint manifest the moment it is written.
func writeSnapFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(abs, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", abs, err)
	}
}

func TestEnsureFresh_OneWalkPopulatesFpNavPathsAndGraph(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "alpha.md", "# Alpha\n\nSee [beta](beta.md).\n")
	writeSnapFile(t, root, "beta.md", "# Beta\n\nNo links.\n")

	store := newSnapshotStore(root, defaultTickInterval, defaultQuietWindow)

	snap, _ := store.ensureFresh()

	if snap.fp == "" {
		t.Error("ensureFresh: expected a non-empty fingerprint after the first rebuild")
	}
	if len(snap.navPaths) != 2 {
		t.Errorf("ensureFresh: expected 2 nav paths, got %d: %v", len(snap.navPaths), snap.navPaths)
	}
	if snap.graph == nil {
		t.Fatal("ensureFresh: expected a non-nil graph after the first rebuild")
	}
	if !snap.graph.Has("alpha.md") || !snap.graph.Has("beta.md") {
		t.Errorf("ensureFresh: graph missing expected nodes: %v", snap.graph.Nodes())
	}
	// One atomic pointer swap publishes all three, so there is no second piece of
	// state that can fall out of sync.
	if cur := store.current(); cur != snap {
		t.Error("current() must return the same snapshot ensureFresh just published")
	}
}

// A file still being written must not flip the fingerprint; it becomes visible
// only once its mtime ages past the quiet window.
func TestEnsureFresh_QuietWindowExcludesRecentFile(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "stable.md", "# Stable\n")

	quietWindow := 100 * time.Millisecond
	store := newSnapshotStore(root, defaultTickInterval, quietWindow)

	snap1, _ := store.ensureFresh()
	if len(snap1.navPaths) != 1 {
		t.Fatalf("baseline: expected 1 nav path, got %d: %v", len(snap1.navPaths), snap1.navPaths)
	}
	fp1 := snap1.fp

	// Bypasses writeSnapFile so the mtime is "now", inside the quiet window.
	if err := os.WriteFile(filepath.Join(root, "fresh.md"), []byte("# Fresh\n"), 0o644); err != nil {
		t.Fatalf("write fresh.md: %v", err)
	}

	snap2, changed2 := store.ensureFresh()
	if snap2.fp != fp1 {
		t.Errorf("quiet window: fingerprint changed on a file still inside the quiet window (fp1=%q fp2=%q)", fp1, snap2.fp)
	}
	if len(changed2) != 0 {
		t.Errorf("quiet window: expected no changed relpaths yet, got %v", changed2)
	}
	if len(snap2.navPaths) != 1 {
		t.Errorf("quiet window: fresh.md must not appear in nav paths yet, got %v", snap2.navPaths)
	}

	time.Sleep(quietWindow + 50*time.Millisecond)

	snap3, changed3 := store.ensureFresh()
	if snap3.fp == fp1 {
		t.Error("quiet window: fingerprint must change once fresh.md ages past the quiet window")
	}
	foundChanged := false
	for _, c := range changed3 {
		if c == "fresh.md" {
			foundChanged = true
		}
	}
	if !foundChanged {
		t.Errorf("quiet window: expected fresh.md in the changed set, got %v", changed3)
	}
	if len(snap3.navPaths) != 2 {
		t.Errorf("quiet window: expected fresh.md to now appear in nav paths, got %v", snap3.navPaths)
	}
}

// A permission-denied file stands in for one that vanishes between stat and
// read: same os.ReadFile error path. Listing still names it, so it stays a node
// with unset content metadata, and a later rebuild fills it in.
func TestRebuild_UnreadableFileSkippedWithoutError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skip: running as root ignores file permission bits")
	}

	root := t.TempDir()
	writeSnapFile(t, root, "ok.md", "# OK\n")
	blockedPath := filepath.Join(root, "blocked.md")
	writeSnapFile(t, root, "blocked.md", "# Blocked\n")
	if err := os.Chmod(blockedPath, 0o000); err != nil {
		t.Fatalf("chmod blocked.md: %v", err)
	}
	defer os.Chmod(blockedPath, 0o644) //nolint:errcheck // best-effort cleanup

	store := newSnapshotStore(root, defaultTickInterval, defaultQuietWindow)

	snap, _ := store.ensureFresh()
	if !snap.graph.Has("ok.md") || snap.graph.Meta("ok.md").Title == "" {
		t.Error("rebuild: the unreadable file must not abort processing of the rest of the rebuild")
	}
	if title := snap.graph.Meta("blocked.md").Title; title != "" {
		t.Errorf("rebuild: unreadable file must have no content-derived metadata yet, got title %q", title)
	}

	// The mtime has to be aged too, or the quiet window hides the change.
	if err := os.Chmod(blockedPath, 0o644); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(blockedPath, old, old); err != nil {
		t.Fatalf("chtimes blocked.md: %v", err)
	}

	snap2, _ := store.ensureFresh()
	if title := snap2.graph.Meta("blocked.md").Title; title == "" {
		t.Error("rebuild: a file that becomes readable again must have its content picked up on a later rebuild")
	}
}

// The rebuild-in-flight CAS is what collapses racing callers, not the computed
// fingerprint value.
func TestEnsureFresh_ConcurrentCallersCollapseToOneRebuild(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "a.md", "# A\n")

	store := newSnapshotStore(root, defaultTickInterval, defaultQuietWindow)
	// Baseline rebuild, so the store already has a published snapshot.
	store.ensureFresh()

	// Makes the next ensureFresh observe staleness.
	writeSnapFile(t, root, "b.md", "# B\n")

	baseline := store.rebuildCalls.Load()

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			store.ensureFresh()
		}()
	}
	wg.Wait()

	got := store.rebuildCalls.Load() - baseline
	if got != 1 {
		t.Errorf("concurrent ensureFresh: expected exactly 1 rebuild for the staleness signal, got %d", got)
	}

	if !store.current().graph.Has("b.md") {
		t.Error("concurrent ensureFresh: the published snapshot must reflect the rebuild that ran")
	}
}

func TestDiffManifest_AddedEditedRemoved(t *testing.T) {
	prev := map[string]fileManifestEntry{
		"unchanged.md": {size: 10, modUnixNano: 100},
		"edited.md":    {size: 10, modUnixNano: 100},
		"removed.md":   {size: 5, modUnixNano: 50},
	}
	next := map[string]fileManifestEntry{
		"unchanged.md": {size: 10, modUnixNano: 100},
		"edited.md":    {size: 20, modUnixNano: 200},
		"added.md":     {size: 3, modUnixNano: 300},
	}

	got := diffManifest(prev, next)
	want := []string{"added.md", "edited.md", "removed.md"}

	if len(got) != len(want) {
		t.Fatalf("diffManifest: got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("diffManifest[%d]: got %q, want %q (full: %v)", i, got[i], w, got)
		}
	}
}

// seed() lets a caller that already built a graph hand ongoing revalidation to
// the store without that graph being discarded and rebuilt.
func TestSnapshotStore_Seed(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "page.md", "# Page\n")

	// Built from a different, empty root, so a rebuild would be visible.
	emptyRoot := t.TempDir()
	injected := BuildLinkGraph(emptyRoot)

	store := newSnapshotStore(root, defaultTickInterval, defaultQuietWindow)
	store.seed(injected)

	cur := store.current()
	if cur.graph != injected {
		t.Error("seed: current().graph must be the exact injected graph, not a rebuilt one")
	}
	if len(cur.navPaths) != 0 {
		t.Errorf("seed: navPaths must come from the injected (empty) graph, got %v", cur.navPaths)
	}
	if cur.fp == "" {
		t.Error("seed: fingerprint must be computed from current on-disk state, not left empty")
	}

	// The seeded fp must match a fresh fingerprint walk, or this rebuilds.
	baseline := store.rebuildCalls.Load()
	snap, _ := store.ensureFresh()
	if store.rebuildCalls.Load() != baseline {
		t.Error("seed: an unchanged realm must not trigger a rebuild after seeding")
	}
	if snap.graph != injected {
		t.Error("seed: ensureFresh must keep returning the seeded graph until the realm actually changes")
	}
}
