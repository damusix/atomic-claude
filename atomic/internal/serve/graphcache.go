// Fingerprint-invalidated cache for the docs-graph response, a whole-realm
// element build and marshal; singleflight collapses concurrent builds into one.
package serve

import (
	"bytes"
	"encoding/json"
	"sync"

	"golang.org/x/sync/singleflight"
)

// graphDataCache is safe for concurrent use.
type graphDataCache struct {
	store *snapshotStore // source of both the link graph and the fingerprint

	sf singleflight.Group

	mu         sync.RWMutex
	fp         string // fingerprint the cached bytes were assembled for
	cachedJSON []byte // nil until the first build
}

func newGraphDataCache(store *snapshotStore) *graphDataCache {
	return &graphDataCache{store: store}
}

// assemble mirrors GraphDataHandler's no-node-param path. Escaping stays off
// so labels keep raw <, >, and &.
func (c *graphDataCache) assemble(g *Graph) ([]byte, error) {
	elems := buildCytoElements(g)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(elems); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fullJSON returns the full graph and its layout cache key.
func (c *graphDataCache) fullJSON() (data []byte, fingerprint string, err error) {
	snap, _ := c.store.ensureFresh()
	return c.snapshotJSON(snap)
}

func (c *graphDataCache) snapshotJSON(snap *realmSnapshot) (data []byte, fingerprint string, err error) {
	fp := snap.graphFP

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

		b, aErr := c.assemble(snap.graph)
		if aErr != nil {
			return nil, aErr
		}
		c.mu.Lock()
		if c.store.current() == snap {
			c.fp = fp
			c.cachedJSON = b
		}
		c.mu.Unlock()
		return b, nil
	})
	if sfErr != nil {
		return nil, "", sfErr
	}
	return v.([]byte), fp, nil
}

// warm uses the published startup graph so it does not contend with refreshes.
func (c *graphDataCache) warm() {
	if snap := c.store.current(); snap.graph != nil {
		_, _, _ = c.snapshotJSON(snap)
	}
}
