// graphcache.go — fingerprint-invalidated cache for the full Network View graph.
//
// The full /graph/data response (no ?node= param) is assembled from
// BuildProvenanceDAG + buildCytoElements + injectProvenanceEdges + JSON marshal.
// The provenance walk (reads + sha256s every wiki page) and the whole-realm
// element assembly used to run on EVERY Network View open — a noticeable wait
// each time.
//
// graphDataCache assembles it once, warmed in a background goroutine at startup,
// and serves the bytes verbatim until the realm changes. Change detection and the
// link graph itself both come from a snapshotStore: the store's ensureFresh
// does the stat-only fingerprint walk (and, when stale, the heavier graph rebuild)
// so this cache no longer walks the filesystem on its own — it only owns the
// provenance+cyto-JSON assembly and its cache. Concurrent (re)builds of that
// assembly are still deduped via singleflight keyed by the store's fingerprint, so
// a burst of requests — or the warm goroutine racing the first request — triggers
// exactly one assembly.
package serve

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sync"

	"golang.org/x/sync/singleflight"
)

// graphDataCache caches the full-view /graph/data JSON keyed by the
// snapshotStore's filesystem fingerprint. Safe for concurrent use.
type graphDataCache struct {
	root    string
	wikiDir string
	store   *snapshotStore // source of the link graph + fingerprint ()

	sf singleflight.Group // dedupes concurrent assembles by fingerprint

	mu         sync.RWMutex
	fp         string // fingerprint the cached bytes were assembled for
	cachedJSON []byte // cached full-view elements JSON (nil until first build)
}

// newGraphDataCache builds a cache over store, rooted at root.
func newGraphDataCache(root string, store *snapshotStore) *graphDataCache {
	return &graphDataCache{
		root:    root,
		wikiDir: filepath.Join(root, "wiki"),
		store:   store,
	}
}

// assemble builds the full-view elements JSON exactly as GraphDataHandler does for
// a no-node-param request (SetEscapeHTML(false) so labels keep raw <, >, &).
func (c *graphDataCache) assemble(g *Graph) ([]byte, error) {
	provDAG := BuildProvenanceDAG(c.root, c.wikiDir)
	elems := buildCytoElements(g)
	// Full view: the whole DAG belongs here, nodes included.
	injectProvenanceEdges(&elems, provDAG, false)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(elems); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fullJSON returns the cached full-view elements JSON and the filesystem
// fingerprint it was assembled for, (re)assembling when the fingerprint has
// changed since the last build. Concurrent callers with the same fingerprint
// share one assemble via singleflight. The fingerprint is surfaced to the client
// (X-Graph-Fingerprint header) so the browser can key its layout cache off the
// exact realm state — it changes on any edit, not just node-set changes.
//
// ensureFresh does the store's own lightweight (or, when stale, full) walk on
// every call — the same lazy-validation cost this cache always paid, now
// shared with nav paths instead of duplicated here.
func (c *graphDataCache) fullJSON() (data []byte, fingerprint string, err error) {
	snap, _ := c.store.ensureFresh()
	fp := snap.fp
	g := snap.graph

	c.mu.RLock()
	if c.fp == fp && c.cachedJSON != nil {
		b := c.cachedJSON
		c.mu.RUnlock()
		return b, fp, nil
	}
	c.mu.RUnlock()

	v, sfErr, _ := c.sf.Do(fp, func() (any, error) {
		// Another caller may have finished the build between our RUnlock and here.
		c.mu.RLock()
		if c.fp == fp && c.cachedJSON != nil {
			b := c.cachedJSON
			c.mu.RUnlock()
			return b, nil
		}
		c.mu.RUnlock()

		b, aErr := c.assemble(g)
		if aErr != nil {
			return nil, aErr
		}
		c.mu.Lock()
		c.fp = fp
		c.cachedJSON = b
		c.mu.Unlock()
		return b, nil
	})
	if sfErr != nil {
		return nil, "", sfErr
	}
	return v.([]byte), fp, nil
}

// warm precomputes the full-view JSON in the background at startup so the first
// Network View render serves cached bytes instead of waiting on the assembly.
// Errors are non-fatal (the request path falls back to a live assemble).
func (c *graphDataCache) warm() {
	_, _, _ = c.fullJSON()
}
