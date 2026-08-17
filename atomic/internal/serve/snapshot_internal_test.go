package serve

// Tests for snapshotStore — the realm snapshot core.
//
// Why internal: snapshotStore, realmSnapshot, and their methods are
// unexported (an implementation detail behind the graphDataCache/handler
// seam), so these tests live in package serve rather than serve_test.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// writeSnapFile creates a file at root/relPath (making parent dirs), with an
// mtime old enough to be outside any reasonable quiet window used in these
// tests — so a freshly-written fixture file is immediately eligible for the
// fingerprint manifest unless a test explicitly wants otherwise.
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

// TestEnsureFresh_OneWalkPopulatesFpNavPathsAndGraph verifies SC1: a single
// ensureFresh rebuild populates the fingerprint, nav paths, and link graph
// together, and current() exposes all three from one consistent snapshot.
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
	// current() must expose the exact same published snapshot (single atomic
	// pointer swap — no separate state to fall out of sync).
	if cur := store.current(); cur != snap {
		t.Error("current() must return the same snapshot ensureFresh just published")
	}
}

// TestEnsureFresh_QuietWindowExcludesRecentFile verifies SC4: a file whose
// mtime is within the quiet window of now does not flip the fingerprint, so
// it is not (yet) picked up by a rebuild; once its mtime ages past the
// window, the next ensureFresh call detects it.
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

	// Write a brand-new file — its mtime is "now", inside the quiet window.
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

	// Age past the quiet window: the next ensureFresh call must detect it.
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

// TestRebuild_UnreadableFileSkippedWithoutError verifies SC5's contract at the
// error-handling level: a file that fails to read during rebuild (simulated
// via a permission-denied file — the same os.ReadFile error path a file
// vanishing between stat and read would hit) is skipped for that rebuild
// without aborting it (BuildLinkGraph still discovers its name — file listing
// needs no read permission — but its content-derived metadata is left unset),
// and a later rebuild (once the file becomes readable again — standing in for
// "reappears") picks up its content.
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
	// The rebuild must complete for every other file despite the read failure.
	if !snap.graph.Has("ok.md") || snap.graph.Meta("ok.md").Title == "" {
		t.Error("rebuild: the unreadable file must not abort processing of the rest of the rebuild")
	}
	// Content-derived metadata for the unreadable file must be unset (its read
	// failed and was skipped, not substituted with stale or partial data).
	if title := snap.graph.Meta("blocked.md").Title; title != "" {
		t.Errorf("rebuild: unreadable file must have no content-derived metadata yet, got title %q", title)
	}

	// Restore readability and age the mtime so it clears the quiet window,
	// then force a rebuild: the file's content must now be picked up.
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

// TestEnsureFresh_ConcurrentCallersCollapseToOneRebuild verifies SC7:
// concurrent ensureFresh calls racing a stale fingerprint collapse to exactly
// one rebuild, gated by the rebuild-in-flight CAS, not by the computed
// fingerprint value.
func TestEnsureFresh_ConcurrentCallersCollapseToOneRebuild(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "a.md", "# A\n")

	store := newSnapshotStore(root, defaultTickInterval, defaultQuietWindow)
	// Baseline rebuild so the store has a published snapshot already.
	store.ensureFresh()

	// Change the realm so the next ensureFresh call observes staleness.
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

	// The realm state must reflect the winning rebuild regardless of which
	// goroutine performed it.
	if !store.current().graph.Has("b.md") {
		t.Error("concurrent ensureFresh: the published snapshot must reflect the rebuild that ran")
	}
}

// TestDiffManifest_AddedEditedRemoved verifies the manifest diff used to
// compute the changed-relpath set consumed by the event payload.
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

// TestSnapshotStore_Seed verifies that seed() publishes the given graph
// as-is (no rebuild) with a fingerprint reflecting current on-disk state, so
// a caller that already built a graph can hand ongoing revalidation to the
// store without discarding what it built.
func TestSnapshotStore_Seed(t *testing.T) {
	root := t.TempDir()
	writeSnapFile(t, root, "page.md", "# Page\n")

	// A graph built from a different (empty) root — proves seed() publishes
	// exactly the graph it was given, not one rebuilt from root.
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

	// A subsequent ensureFresh call with nothing changed on disk must not
	// trigger a rebuild (the seeded fp must match a fresh fingerprint walk).
	baseline := store.rebuildCalls.Load()
	snap, _ := store.ensureFresh()
	if store.rebuildCalls.Load() != baseline {
		t.Error("seed: an unchanged realm must not trigger a rebuild after seeding")
	}
	if snap.graph != injected {
		t.Error("seed: ensureFresh must keep returning the seeded graph until the realm actually changes")
	}
}
