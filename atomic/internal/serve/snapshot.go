// snapshot.go — CP1 (live-reload): the realm snapshot store.
//
// Today, three independent walks compute overlapping state: nav.go walks the
// docs tree on every /nav request, serve.go builds the link graph once at
// startup and never again, and graphcache.go walks the filesystem for a
// fingerprint on every /graph/data request. Because they run independently, a
// file added after startup shows up in some views and not others, and none of
// them notice a change without a full server restart.
//
// snapshotStore replaces all three with one realm-wide snapshot — fingerprint,
// nav paths, and link graph — behind a single atomic pointer, refreshed by one
// rebuild funnel that any caller (a ticker, a lazy per-request check, the
// startup warm) can trigger through the same accessor: ensureFresh.
//
// Two walk tiers exist:
//   - fingerprint(): a cheap, stat-only, quiet-window-filtered manifest walk.
//     Run on every ensureFresh call to detect drift without touching file
//     content.
//   - rebuild(): the expensive walk (BuildLinkGraph reads and parses every
//     page), run only when fingerprint() reports the realm has changed.
//
// Concurrent callers that all observe staleness do not each perform a
// rebuild. Only one wins a non-blocking CAS gate; the rest return the current
// (possibly one-tick-stale) snapshot immediately rather than waiting — the
// dedup key is that gate, not the computed fingerprint value, because two
// walkers racing a changing filesystem can legitimately compute different
// fingerprints for the same moment.
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
	// defaultTickInterval is the ticker cadence a store uses when a caller
	// does not need a different cadence (production default; CP3 wires the
	// live ticker at this interval).
	defaultTickInterval = 10 * time.Second

	// defaultQuietWindow is how recently a file may have been modified before
	// its size/mtime resets are trusted enough to enter the fingerprint
	// manifest — a file mid-write should not flip the fingerprint out from
	// under a reader.
	defaultQuietWindow = 2 * time.Second
)

// fileManifestEntry is one file's (size, mtime) pair as observed by a
// fingerprint walk. Comparable, so two entries can be diffed with !=.
type fileManifestEntry struct {
	size        int64
	modUnixNano int64
}

// realmSnapshot is one immutable view of realm state: the filesystem
// fingerprint, the navigable markdown paths, and the resolved link graph.
// Published as a single atomic pointer swap so a reader that grabs the
// pointer once observes internally consistent state — a torn read (new fp,
// stale graph) is impossible because there is only ever one field to read.
type realmSnapshot struct {
	fp       string
	navPaths []string
	graph    *Graph
	manifest map[string]fileManifestEntry
}

// snapshotStore owns the current realmSnapshot behind a single atomic
// pointer, the quiet-window fingerprint walk, and the rebuild funnel that
// collapses concurrent triggers into one rebuild.
type snapshotStore struct {
	root         string
	tickInterval time.Duration
	quietWindow  time.Duration

	ptr atomic.Pointer[realmSnapshot]

	// rebuilding gates the funnel: only the goroutine that wins this
	// CompareAndSwap performs a rebuild. Losers return the current
	// (possibly stale) snapshot immediately — a later ensureFresh call
	// (next tick, next request) retries. This is the generation-keyed
	// dedup: the gate, not the fingerprint value, decides "already in
	// flight" (SC7).
	rebuilding atomic.Bool

	// rebuildCalls counts completed rebuild() calls. Test instrumentation
	// only — proves concurrent ensureFresh callers collapse to one rebuild.
	rebuildCalls atomic.Int64
}

// newSnapshotStore constructs a store rooted at root with the given tick
// interval and quiet window. The store starts with an empty snapshot; the
// first ensureFresh call performs the initial rebuild.
func newSnapshotStore(root string, tickInterval, quietWindow time.Duration) *snapshotStore {
	s := &snapshotStore{root: root, tickInterval: tickInterval, quietWindow: quietWindow}
	s.ptr.Store(&realmSnapshot{})
	return s
}

// seed publishes an initial snapshot built from an already-constructed graph
// (e.g. one built once at server startup) instead of performing a rebuild
// walk. The snapshot's fingerprint reflects the current on-disk state, so a
// later ensureFresh call correctly detects whether anything has changed since
// g was built. Callers that already hold a graph use this to hand ongoing
// revalidation over to the store without discarding what they built.
func (s *snapshotStore) seed(g *Graph) {
	entries, fp := s.fingerprint()
	s.ptr.Store(&realmSnapshot{
		fp:       fp,
		navPaths: append([]string(nil), g.Nodes()...),
		graph:    g,
		manifest: entries,
	})
}

// current returns the currently published snapshot. Never nil after
// construction.
func (s *snapshotStore) current() *realmSnapshot {
	return s.ptr.Load()
}

// ensureFresh computes the quiet-window fingerprint and, when it differs from
// the published snapshot's fp, triggers a rebuild. It is the single accessor
// shared by the ticker, lazy per-request validation, and the startup warm
// (SC: "ticker, lazy request-path validation, and startup warm all call the
// same snapshot accessor"). Returns the (possibly just-published) snapshot
// and the changed-relpath manifest diff (nil when no rebuild occurred).
func (s *snapshotStore) ensureFresh() (*realmSnapshot, []string) {
	entries, fp := s.fingerprint()
	cur := s.current()
	if cur.graph != nil && cur.fp == fp {
		return cur, nil
	}
	if !s.rebuilding.CompareAndSwap(false, true) {
		// A rebuild for this staleness signal is already in flight — skip
		// rather than block. The caller observes the current snapshot; a
		// later call catches the change once the in-flight rebuild lands.
		return cur, nil
	}
	defer s.rebuilding.Store(false)

	return s.rebuild(entries, fp)
}

// rebuild walks nav paths and the link graph (BuildLinkGraph — content
// reads), diffs the new manifest against the prior snapshot's, and publishes
// the result via one atomic pointer swap. entries/fp are the fingerprint
// already computed by the caller so this does not re-walk to get them.
//
// A file that vanishes between the fingerprint walk and BuildLinkGraph's
// content read is skipped for this rebuild without error (BuildLinkGraph
// already continues past a read failure); it disappears from navPaths/graph
// for this pass and, if truly gone, stays gone on the next fingerprint walk —
// if it reappears, the next rebuild picks it up (SC5).
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

// fingerprint performs the quiet-window-filtered, stat-only manifest walk:
// every non-hidden file under root (same shouldSkipDir/hiddenFile filters as
// BuildLinkGraph, so the fingerprint tracks exactly what the graph depends
// on), excluding any file whose mtime falls within the quiet window of now.
// No file content is read — cheap enough to run on every ensureFresh call,
// including a zero-subscriber tick (SC4).
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
			// Vanished between directory listing and stat — skip for this
			// walk without error; a later walk reflects whatever is there.
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

// diffManifest returns the sorted set of relpaths added, edited (size or
// mtime differs), or removed between prev and next. Either map may be nil
// (the store's first rebuild has no prior manifest) — every entry in next is
// then reported as changed.
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
