// api_handlers.go — CP2: additive /api/* content endpoints (page, file, rail,
// nav) for the React frontend, alongside the existing htmx routes.
//
// Every handler reuses the same view-model builders as its htmx sibling
// (context_handler.go, render.go, rail_handler.go, nav.go) so link resolution
// and rendering stay single-sourced — see the API contracts table in
// docs/spec/serve-react-frontend.md.
package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// writeAPIJSON writes v as the JSON response body with status 200 and
// Content-Type: application/json. Go's encoding/json default HTML-escaping
// stays enabled (no SetEscapeHTML(false)) per the API contracts conventions —
// distinct from writeGraphError/codegraph.go's carried-JS endpoints, which
// disable it.
func writeAPIJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "encode response: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// apiErrorEnvelope is the one error shape shared by every /api/* endpoint.
type apiErrorEnvelope struct {
	Error string `json:"error"`
}

// writeAPIError writes the {"error": "<message>"} envelope with the given
// non-200 status.
func writeAPIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, err := json.Marshal(apiErrorEnvelope{Error: msg})
	if err != nil {
		// Marshaling a plain string field cannot fail; this is unreachable in
		// practice but keeps the write from silently dropping on the floor.
		_, _ = w.Write([]byte(`{"error":"internal error"}`))
		return
	}
	_, _ = w.Write(b)
}

// ─── GET /api/page/<relpath> ────────────────────────────────────────────────

// apiPageResponse is the /api/page success payload for a file (not a
// directory). Reshapes pageWithGraphData (context_handler.go) — Breadcrumb
// moves from an HTML string to structured segments.
type apiPageResponse struct {
	HTML       string          `json:"html"`
	Title      string          `json:"title"`
	RelPath    string          `json:"relpath"`
	HasMermaid bool            `json:"hasMermaid"`
	Breadcrumb []breadcrumbSeg `json:"breadcrumb"`
}

// apiPageDirResponse is the /api/page success payload for a directory URL
// with no index file.
type apiPageDirResponse struct {
	Dir     bool       `json:"dir"`
	RelPath string     `json:"relpath"`
	Entries []dirEntry `json:"entries"`
}

// NewAPIPageHandler returns an http.Handler for GET /api/page/<relpath>.
// It reuses the exact resolution and render path NewPageHandlerWithGraph
// uses for htmx requests (index-file resolution, RenderMarkdownWithGraph,
// directory listing) so /api/page and /page render identical link
// resolution.
func NewAPIPageHandler(root string, g graphProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/api/page/")
		if relPath == "" || relPath == "/" {
			writeAPIError(w, http.StatusNotFound, "missing relpath")
			return
		}

		var graph *Graph
		if !isNilGraphProvider(g) {
			graph = g.currentGraph()
		}

		abs, ok := safeResolve(root, relPath)
		if !ok {
			writeAPIError(w, http.StatusNotFound, "path traversal rejected: "+relPath)
			return
		}

		info, statErr := os.Stat(abs)
		if statErr != nil {
			writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
			return
		}

		if info.IsDir() {
			dirRel := normRelPath(relPath)
			if idxRel, found := resolveDirIndex(root, dirRel); found {
				idxAbs, idxOK := safeResolve(root, idxRel)
				if !idxOK {
					writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
					return
				}
				data, err := readFile(idxAbs)
				if err != nil {
					writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
					return
				}
				bodyHTML, hasMermaid, renderErr := RenderMarkdownWithGraph(data, root, idxRel, graph)
				if renderErr != nil {
					writeAPIError(w, http.StatusInternalServerError, "render failed: "+renderErr.Error())
					return
				}
				writeAPIJSON(w, apiPageResponse{
					HTML:       bodyHTML,
					Title:      baseName(idxRel),
					RelPath:    idxRel,
					HasMermaid: hasMermaid,
					Breadcrumb: breadcrumbSegmentsData(idxRel),
				})
				return
			}

			entries, listOK := listDirEntries(root, dirRel)
			if !listOK {
				writeAPIError(w, http.StatusNotFound, "cannot list folder: "+relPath)
				return
			}
			writeAPIJSON(w, apiPageDirResponse{Dir: true, RelPath: dirRel, Entries: entries})
			return
		}

		data, err := readFile(abs)
		if err != nil {
			writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
			return
		}
		renderRelPath := normRelPath(relPath)
		bodyHTML, hasMermaid, renderErr := RenderMarkdownWithGraph(data, root, renderRelPath, graph)
		if renderErr != nil {
			writeAPIError(w, http.StatusInternalServerError, "render failed: "+renderErr.Error())
			return
		}
		writeAPIJSON(w, apiPageResponse{
			HTML:       bodyHTML,
			Title:      baseName(renderRelPath),
			RelPath:    renderRelPath,
			HasMermaid: hasMermaid,
			Breadcrumb: breadcrumbSegmentsData(renderRelPath),
		})
	})
}

// ─── GET /api/file/<relpath> ────────────────────────────────────────────────

// apiFileResponse is the /api/file success payload — chroma line-table HTML
// (render.go:chromaHighlightLines), the same markup /file/<relpath> serves.
type apiFileResponse struct {
	HTML  string `json:"html"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// NewAPIFileHandler returns an http.Handler for GET /api/file/<relpath>.
func NewAPIFileHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/api/file/")
		if relPath == "" || relPath == "/" {
			writeAPIError(w, http.StatusNotFound, "missing relpath")
			return
		}

		abs, ok := safeResolve(root, relPath)
		if !ok {
			writeAPIError(w, http.StatusNotFound, "path traversal rejected: "+relPath)
			return
		}

		data, err := os.ReadFile(abs) //nolint:gosec // path validated by safeResolve
		if err != nil {
			writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
			return
		}

		ext := strings.TrimPrefix(filepath.Ext(relPath), ".")
		bodyHTML := chromaHighlightLines(ext, string(data))

		writeAPIJSON(w, apiFileResponse{
			HTML:  bodyHTML,
			Title: filepath.Base(relPath),
			Path:  relPath,
		})
	})
}

// ─── GET /api/rail/<relpath> ────────────────────────────────────────────────

// apiRailBacklink is one inbound backlink entry.
type apiRailBacklink struct {
	Path string `json:"path"`
}

// apiRailResponse is the /api/rail success payload — reshapes railTmplData
// (rail_handler.go) as flat JSON: Properties (nil when the page has no
// frontmatter), Outbound edges, Backlinks, and the rail mini-graph data URL.
type apiRailResponse struct {
	RelPath      string            `json:"relpath"`
	Orphan       bool              `json:"orphan"`
	Properties   []propKV          `json:"properties"`
	Outbound     []Edge            `json:"out"`
	Backlinks    []apiRailBacklink `json:"in"`
	GraphDataURL string            `json:"graphDataURL"`
}

// NewAPIRailHandler returns an http.Handler for GET /api/rail/<relpath>.
// Reuses the same graph-membership check, railProperties parser, and Edge
// data NewRailHandler's htmx fragment uses, so /api/rail and /rail resolve
// links identically.
func NewAPIRailHandler(root string, g graphProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/api/rail/")
		if relPath == "" || relPath == "/" {
			writeAPIError(w, http.StatusNotFound, "missing relpath")
			return
		}

		if isNilGraphProvider(g) {
			writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
			return
		}

		abs, ok := safeResolve(root, relPath)
		if !ok {
			writeAPIError(w, http.StatusNotFound, "path traversal rejected: "+relPath)
			return
		}

		graph := g.currentGraph()
		rel := normRelPath(relPath)
		if !graph.Has(rel) {
			writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
			return
		}

		backlinks := graph.Backlinks(rel)
		apiBacklinks := make([]apiRailBacklink, len(backlinks))
		for i, b := range backlinks {
			apiBacklinks[i] = apiRailBacklink{Path: b}
		}

		writeAPIJSON(w, apiRailResponse{
			RelPath:      rel,
			Orphan:       graph.IsOrphan(rel),
			Properties:   railProperties(abs),
			Outbound:     graph.Outbound(rel),
			Backlinks:    apiBacklinks,
			GraphDataURL: "/graph/data?node=" + rel + "&depth=1",
		})
	})
}

// ─── GET /api/nav ───────────────────────────────────────────────────────────

// apiNavResponse is the /api/nav success payload.
type apiNavResponse struct {
	Scope  string         `json:"scope"`
	Groups []navGroupJSON `json:"groups"`
}

// NewAPINavHandler returns an http.Handler for GET /api/nav. Reuses the same
// staleness computation and folder-tree walk NewNavHandler's htmx fragment
// uses, reshaped as structured groups/navNode data instead of an HTML tree.
func NewAPINavHandler(opts NavOptions) http.Handler {
	store := opts.Store
	if store == nil && !opts.IsRealmScope {
		store = newSnapshotStore(opts.RealmRoot, defaultTickInterval, defaultQuietWindow)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.IsRealmScope && !isSSETriggered(r) {
			fn := opts.StalenessFn
			if fn == nil {
				fn = computeStaleness
			}
			staleMembers, bucketDiffs := fn(opts.RealmRoot, opts.WikiIndexPath)
			opts.StaleMembers = staleMembers
			opts.BucketDiffs = bucketDiffs
		}

		if opts.IsRealmScope {
			writeAPIJSON(w, apiNavResponse{Scope: "realm", Groups: buildRealmNavGroupsJSON(opts)})
			return
		}

		snap, _ := store.ensureFresh()
		writeAPIJSON(w, apiNavResponse{Scope: "repo", Groups: buildRepoNavGroupsJSON(snap.navPaths)})
	})
}
