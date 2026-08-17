// The realm snapshot store behind live-reload: one realm-wide snapshot
// (fingerprint, nav paths, link graph) published through a single atomic
// pointer, so nav, page, rail, and graph all read the same state.
//
// Two walk tiers keep that cheap. fingerprint() is stat-only and runs on every
// ensureFresh; rebuild() reads and parses every page and runs only when the
// fingerprint moved.
//
// Concurrent stale observers do not each rebuild: one wins a non-blocking CAS
// gate and the rest return the current snapshot rather than wait. The gate is
// the dedup key, not the fingerprint value, because two walkers racing a
// changing filesystem can legitimately compute different fingerprints for the
// same moment.
package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	defaultTickInterval = 10 * time.Second

	// defaultQuietWindow keeps a file out of the manifest until its mtime has
	// settled, so a file mid-write cannot flip the fingerprint under a reader.
	defaultQuietWindow = 2 * time.Second
)

// fileManifestEntry is comparable, so two entries diff with !=.
type fileManifestEntry struct {
	size        int64
	modUnixNano int64
}

// realmSnapshot is one immutable view of realm state. Publishing it as a
// single pointer swap makes a torn read (new fingerprint, stale graph)
// impossible — there is only ever one field to read.
type realmSnapshot struct {
	fp       string
	navPaths []string
	graph    *Graph
	manifest map[string]fileManifestEntry
}

// snapshotStore owns the published snapshot, the fingerprint walk, and the
// funnel collapsing concurrent triggers into one rebuild.
type snapshotStore struct {
	root         string
	tickInterval time.Duration
	quietWindow  time.Duration

	ptr atomic.Pointer[realmSnapshot]

	// rebuilding gates the funnel: the CAS winner rebuilds, losers return the
	// current snapshot and let a later ensureFresh retry.
	rebuilding atomic.Bool

	// rebuildCalls is test instrumentation proving concurrent callers collapse
	// to one rebuild.
	rebuildCalls atomic.Int64
}

// newSnapshotStore returns a store with an empty snapshot; the first
// ensureFresh performs the initial rebuild.
func newSnapshotStore(root string, tickInterval, quietWindow time.Duration) *snapshotStore {
	s := &snapshotStore{root: root, tickInterval: tickInterval, quietWindow: quietWindow}
	s.ptr.Store(&realmSnapshot{})
	return s
}

// NewSnapshotStore returns the shared store at production defaults, warmed
// synchronously: a caller of an exported constructor must never observe a nil
// graph, including a background warm racing its own first request.
func NewSnapshotStore(root string) *snapshotStore {
	s := newSnapshotStore(root, defaultTickInterval, defaultQuietWindow)
	s.ensureFresh()
	return s
}

// graphProvider is how the page, rail, and graph handlers reach the current
// link graph. A bare *Graph is a fixed snapshot, for tests; a *snapshotStore
// revalidates on every read, so a file changed after construction is picked up
// without rebuilding the handler.
type graphProvider interface {
	currentGraph() *Graph
}

func (g *Graph) currentGraph() *Graph { return g }

func (s *snapshotStore) currentGraph() *Graph {
	snap, _ := s.ensureFresh()
	return snap.graph
}

// seed publishes an already-built graph instead of rebuilding, so a caller
// holding one can hand revalidation to the store without discarding it. The
// fingerprint is taken now, so a later ensureFresh still detects drift.
func (s *snapshotStore) seed(g *Graph) {
	entries, fp := s.fingerprint()
	s.ptr.Store(&realmSnapshot{
		fp:       fp,
		navPaths: append([]string(nil), g.Nodes()...),
		graph:    g,
		manifest: entries,
	})
}

// current returns the published snapshot, never nil after construction.
func (s *snapshotStore) current() *realmSnapshot {
	return s.ptr.Load()
}

// ensureFresh fingerprints the realm and rebuilds when it has drifted. It is
// the one accessor the ticker, per-request validation, and the startup warm
// all share. The returned diff is nil when no rebuild occurred.
func (s *snapshotStore) ensureFresh() (*realmSnapshot, []string) {
	entries, fp := s.fingerprint()
	cur := s.current()
	if cur.graph != nil && cur.fp == fp {
		return cur, nil
	}
	if !s.rebuilding.CompareAndSwap(false, true) {
		// Already in flight — skip rather than block; a later call sees it.
		return cur, nil
	}
	defer s.rebuilding.Store(false)

	return s.rebuild(entries, fp)
}

// rebuild rebuilds the graph and publishes it in one pointer swap. entries and
// fp come from the caller's fingerprint walk, so this does not re-walk.
//
// A file that vanishes between the fingerprint and the content read is simply
// absent from this pass; the next rebuild picks it up if it reappears.
func (s *snapshotStore) rebuild(entries map[string]fileManifestEntry, fp string) (*realmSnapshot, []string) {
	s.rebuildCalls.Add(1)

	g := BuildLinkGraph(s.root)
	navPaths := append([]string(nil), g.Nodes()...)

	prev := s.current()
	changed := diffManifest(prev.manifest, entries)

	next := &realmSnapshot{fp: fp, navPaths: navPaths, graph: g, manifest: entries}
	s.ptr.Store(next)
	return next, changed
}

// fingerprint stats every non-hidden file under root, skipping any modified
// inside the quiet window. It uses the same filters as BuildLinkGraph, so the
// fingerprint tracks exactly what the graph depends on, and reads no content.
func (s *snapshotStore) fingerprint() (map[string]fileManifestEntry, string) {
	now := time.Now()
	entries := make(map[string]fileManifestEntry)
	_ = filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != s.root && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if hiddenFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			// Vanished between listing and stat; a later walk will see it.
			return nil
		}
		if now.Sub(info.ModTime()) < s.quietWindow {
			return nil
		}
		entries[filepath.ToSlash(rel)] = fileManifestEntry{
			size:        info.Size(),
			modUnixNano: info.ModTime().UnixNano(),
		}
		return nil
	})

	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		e := entries[k]
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(strconv.FormatInt(e.size, 10)))
		h.Write([]byte{0})
		h.Write([]byte(strconv.FormatInt(e.modUnixNano, 10)))
		h.Write([]byte{'\n'})
	}
	return entries, hex.EncodeToString(h.Sum(nil))
}

// diffManifest returns the sorted relpaths added, edited, or removed. A nil
// prev — the first rebuild — reports every entry in next as changed.
func diffManifest(prev, next map[string]fileManifestEntry) []string {
	var changed []string
	for path, n := range next {
		if p, ok := prev[path]; !ok || p != n {
			changed = append(changed, path)
		}
	}
	for path := range prev {
		if _, ok := next[path]; !ok {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}
