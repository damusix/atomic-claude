// The realm snapshot store behind live-reload: one realm-wide snapshot
// (fingerprint, nav paths, link graph) published through a single atomic
// pointer, so nav, page, rail, and graph all read the same state.
//
// Refresh stats the document scope once and parses the recorded files only
// when their fingerprint changes.
package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultTickInterval = 10 * time.Second

	// defaultQuietWindow keeps a file out of the manifest until its mtime has
	// settled, so a file mid-write cannot flip the fingerprint under a reader.
	defaultQuietWindow = 2 * time.Second
)

type fileManifestEntry struct {
	size        int64
	modUnixNano int64
	physical    string
	info        fs.FileInfo
	version     uint64
}

// realmSnapshot is one immutable view of realm state. Publishing it as a
// single pointer swap makes a torn read (new fingerprint, stale graph)
// impossible — there is only ever one field to read.
type realmSnapshot struct {
	fp       string
	graphFP  string
	navPaths []string
	graph    *Graph
	manifest map[string]fileManifestEntry
	quiet    map[string]fileManifestEntry
}

// snapshotStore owns the published snapshot, the fingerprint walk, and the
// funnel collapsing concurrent triggers into one rebuild.
type snapshotStore struct {
	root         string
	scopeRoot    string
	tickInterval time.Duration
	quietWindow  time.Duration

	ptr atomic.Pointer[realmSnapshot]

	rebuilding sync.Mutex

	// rebuildCalls is test instrumentation proving concurrent callers collapse
	// to one rebuild.
	rebuildCalls atomic.Int64
}

// newSnapshotStore returns a store with an empty snapshot; the first
// ensureFresh performs the initial rebuild.
func newSnapshotStore(root string, tickInterval, quietWindow time.Duration) *snapshotStore {
	s := &snapshotStore{root: root, scopeRoot: resolveDir(root), tickInterval: tickInterval, quietWindow: quietWindow}
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

func (s *snapshotStore) currentGraphInScope(scope documentScope) *Graph {
	if scope.root != s.scopeRoot {
		return s.currentGraph()
	}
	snap, _ := s.ensureFreshInScope(scope)
	return snap.graph
}

// seed publishes an already-built graph instead of rebuilding, so a caller
// holding one can hand revalidation to the store without discarding it. The
// fingerprint is taken now, so a later ensureFresh still detects drift.
func (s *snapshotStore) seed(g *Graph) {
	s.rebuilding.Lock()
	defer s.rebuilding.Unlock()

	scope := loadDocumentScope(s.root)
	if scope.bound {
		s.refresh(scope)
		return
	}
	entries, quiet, fp := scope.scanDocuments(s.quietWindow, nil)
	quietInputs, graphFP := splitQuietInputs(entries, quiet, fp)
	s.ptr.Store(&realmSnapshot{
		fp:       fp,
		graphFP:  graphFP,
		navPaths: append([]string(nil), g.Nodes()...),
		graph:    g,
		manifest: entries,
		quiet:    quietInputs,
	})
}

// current returns the published snapshot, never nil after construction.
func (s *snapshotStore) current() *realmSnapshot {
	return s.ptr.Load()
}

// ensureFresh revalidates the snapshot for requests. A nil diff means no
// stable document changed, even if the graph was rebuilt.
func (s *snapshotStore) ensureFresh() (*realmSnapshot, []string) {
	s.rebuilding.Lock()
	defer s.rebuilding.Unlock()
	return s.refresh(loadDocumentScope(s.root))
}

func (s *snapshotStore) tryEnsureFresh() (*realmSnapshot, []string) {
	if !s.rebuilding.TryLock() {
		return nil, nil
	}
	defer s.rebuilding.Unlock()
	return s.refresh(loadDocumentScope(s.root))
}

func (s *snapshotStore) ensureFreshInScope(scope documentScope) (*realmSnapshot, []string) {
	s.rebuilding.Lock()
	defer s.rebuilding.Unlock()

	return s.refresh(scope)
}

func (s *snapshotStore) refresh(scope documentScope) (*realmSnapshot, []string) {
	cur := s.current()
	entries, quiet, fp := scope.scanDocuments(s.quietWindow, cur)
	if cur.graph != nil && cur.fp == fp && !cur.lostQuietInput(entries) {
		return cur, nil
	}
	return s.rebuild(scope, entries, quiet, fp)
}

func (s *realmSnapshot) lostQuietInput(entries map[string]fileManifestEntry) bool {
	for path, old := range s.quiet {
		cur, ok := entries[path]
		if !ok || cur.physical != old.physical || cur.version != old.version || cur.info.Mode() != old.info.Mode() {
			return true
		}
	}
	return false
}

func (s *snapshotStore) rebuild(scope documentScope, entries map[string]fileManifestEntry, quiet []string, fp string) (*realmSnapshot, []string) {
	s.rebuildCalls.Add(1)

	g := buildLinkGraph(scope, entries)
	navPaths := append([]string(nil), g.Nodes()...)
	quietInputs, graphFP := splitQuietInputs(entries, quiet, fp)

	var changed []string
	if prev := s.current(); prev.fp != fp {
		changed = diffManifest(prev.manifest, entries)
	}
	next := &realmSnapshot{fp: fp, graphFP: graphFP, navPaths: navPaths, graph: g, manifest: entries, quiet: quietInputs}
	s.ptr.Store(next)
	return next, changed
}

func splitQuietInputs(entries map[string]fileManifestEntry, quiet []string, fp string) (map[string]fileManifestEntry, string) {
	if len(quiet) == 0 {
		return nil, fp
	}
	h := sha256.New()
	h.Write([]byte(fp))
	hashManifestEntries(h, quiet, entries)
	inputs := make(map[string]fileManifestEntry, len(quiet))
	for _, path := range quiet {
		inputs[path] = entries[path]
		delete(entries, path)
	}
	return inputs, hex.EncodeToString(h.Sum(nil))
}

func (s documentScope) scanDocuments(quietWindow time.Duration, previous *realmSnapshot) (entries map[string]fileManifestEntry, quiet []string, fp string) {
	cutoff := time.Now().Add(-quietWindow)
	entries = make(map[string]fileManifestEntry)
	var keys []string
	_ = s.walk(".", func(rel, physical string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		entry := fileManifestEntry{
			size:        info.Size(),
			modUnixNano: info.ModTime().UnixNano(),
			physical:    physical,
			info:        info,
		}
		if previous != nil {
			old, ok := previous.manifest[rel]
			if !ok {
				old, ok = previous.quiet[rel]
			}
			if ok {
				entry.version = old.version
				if !os.SameFile(info, old.info) {
					entry.version++
				}
			}
		}
		entries[rel] = entry
		if quietWindow > 0 && info.ModTime().After(cutoff) {
			quiet = append(quiet, rel)
		} else {
			keys = append(keys, rel)
		}
		return nil
	})

	h := sha256.New()
	if s.bound {
		h.Write([]byte{1})
	}
	hashManifestEntries(h, keys, entries)
	keys = keys[:0]
	for path, mount := range s.roots {
		if s.mountAvailable(mount) {
			keys = append(keys, path)
		}
	}
	sort.Strings(keys)
	for _, path := range keys {
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write([]byte(s.roots[path].Target))
		h.Write([]byte{0})
	}
	for _, state := range s.privateStateRoots() {
		h.Write([]byte(state))
		h.Write([]byte{0})
	}
	return entries, quiet, hex.EncodeToString(h.Sum(nil))
}

func hashManifestEntries(h hash.Hash, keys []string, entries map[string]fileManifestEntry) {
	sort.Strings(keys)
	var storage [256]byte
	buf := storage[:0]
	for _, k := range keys {
		e := entries[k]
		buf = append(buf[:0], k...)
		buf = append(buf, 0)
		buf = strconv.AppendInt(buf, e.size, 10)
		buf = append(buf, 0)
		buf = strconv.AppendInt(buf, e.modUnixNano, 10)
		buf = append(buf, 0)
		buf = strconv.AppendUint(buf, e.version, 10)
		buf = append(buf, 0)
		buf = append(buf, e.physical...)
		buf = append(buf, 0)
		buf = strconv.AppendUint(buf, uint64(e.info.Mode()), 10)
		buf = append(buf, '\n')
		h.Write(buf)
	}
}

// diffManifest returns the sorted relpaths added, edited, or removed. A nil
// prev — the first rebuild — reports every entry in next as changed.
func diffManifest(prev, next map[string]fileManifestEntry) []string {
	var changed []string
	for path, n := range next {
		if p, ok := prev[path]; !ok || p.size != n.size || p.modUnixNano != n.modUnixNano || p.physical != n.physical || p.version != n.version ||
			(p.info != nil && n.info != nil && p.info.Mode() != n.info.Mode()) {
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
