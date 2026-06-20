// graphcache.go — fingerprint-invalidated cache for the full Network View graph.
//
// The full /graph/data response (no ?node= param) is assembled from
// BuildProvenanceDAG + buildCytoElements + injectProvenanceEdges + JSON marshal.
// The link graph is already built once at startup, but the provenance walk
// (reads + sha256s every wiki page) and the whole-realm element assembly used to
// run on EVERY Network View open — a noticeable wait each time.
//
// graphDataCache assembles it once, warmed in a background goroutine at startup,
// and serves the bytes verbatim until the realm changes. Change detection is a
// sha256 fingerprint over a manifest of every non-hidden file's (relpath, size,
// mtime) under the root — cheap (stat-only, no content reads) and sensitive to
// edits. Concurrent (re)builds are deduped via singleflight, so a burst of
// requests — or the warm goroutine racing the first request — triggers exactly
// one assembly.
package serve

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/sync/singleflight"
)

// graphDataCache caches the full-view /graph/data JSON keyed by a filesystem
// fingerprint. Safe for concurrent use.
type graphDataCache struct {
	root    string
	wikiDir string
	graph   *Graph // prebuilt link graph (static for the server's lifetime)

	sf singleflight.Group // dedupes concurrent assembles by fingerprint

	mu         sync.RWMutex
	fp         string // fingerprint the cached bytes were assembled for
	cachedJSON []byte // cached full-view elements JSON (nil until first build)
}

// newGraphDataCache builds a cache over the prebuilt link graph g rooted at root.
func newGraphDataCache(root string, g *Graph) *graphDataCache {
	return &graphDataCache{
		root:    root,
		wikiDir: filepath.Join(root, "wiki"),
		graph:   g,
	}
}

// fingerprint hashes a manifest of (relpath|size|mtimeNano) for every non-hidden
// file under root, using the same dir/file filters as BuildLinkGraph so it tracks
// exactly the files the graph + provenance overlay depend on. Stat-only — no file
// contents are read, so it is cheap to recompute per request.
func (c *graphDataCache) fingerprint() string {
	h := sha256.New()
	_ = filepath.WalkDir(c.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != c.root && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if hiddenFile(d.Name()) {
			return nil
		}
		rel, relErr := filepath.Rel(c.root, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		h.Write([]byte(strconv.FormatInt(info.Size(), 10)))
		h.Write([]byte{0})
		h.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
		h.Write([]byte{'\n'})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

// assemble builds the full-view elements JSON exactly as GraphDataHandler does for
// a no-node-param request (SetEscapeHTML(false) so labels keep raw <, >, &).
func (c *graphDataCache) assemble() ([]byte, error) {
	provDAG := BuildProvenanceDAG(c.root, c.wikiDir)
	elems := buildCytoElements(c.graph)
	injectProvenanceEdges(&elems, provDAG)

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
func (c *graphDataCache) fullJSON() (data []byte, fingerprint string, err error) {
	fp := c.fingerprint()

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

		b, aErr := c.assemble()
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
