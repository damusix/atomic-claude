// Fingerprint-invalidated cache for the full docs-graph response.
//
// Assembling it means a provenance walk that reads and hashes every wiki page,
// plus a whole-realm element build — too slow to repeat on every open. This
// cache owns only that assembly; the fingerprint and the link graph both come
// from the shared snapshot store, so it never walks the filesystem itself.
// Singleflight keyed on the fingerprint collapses a burst of requests, or the
// warm goroutine racing the first request, into one assembly.
package serve

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sync"

	"golang.org/x/sync/singleflight"
)

// graphDataCache is safe for concurrent use.
type graphDataCache struct {
	root    string
	wikiDir string
	store   *snapshotStore // source of both the link graph and the fingerprint

	sf singleflight.Group

	mu         sync.RWMutex
	fp         string // fingerprint the cached bytes were assembled for
	cachedJSON []byte // nil until the first build
}

func newGraphDataCache(root string, store *snapshotStore) *graphDataCache {
	return &graphDataCache{
		root:    root,
		wikiDir: filepath.Join(root, "wiki"),
		store:   store,
	}
}

// assemble mirrors GraphDataHandler's no-node-param path. Escaping stays off
// so labels keep raw <, >, and &.
func (c *graphDataCache) assemble(g *Graph) ([]byte, error) {
	provDAG := BuildProvenanceDAG(c.root, c.wikiDir)
	elems := buildCytoElements(g)
	// Unscoped: the whole DAG belongs in a full view, nodes included.
	injectProvenanceEdges(&elems, provDAG, false)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(elems); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fullJSON reassembles only when the fingerprint moved. The fingerprint is
// returned so the handler can hand it to the client, which keys its layout
// cache off it — it changes on any edit, not just a node-set change.
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
		// Another caller may have finished between the RUnlock above and here.
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

// warm precomputes at startup so the first graph open serves cached bytes.
// Errors are non-fatal: the request path falls back to a live assemble.
func (c *graphDataCache) warm() {
	_, _, _ = c.fullJSON()
}
