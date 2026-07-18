// api_handlers.go — CP2: additive /api/* content endpoints (page, file, rail,
// nav) for the React frontend, alongside the existing htmx routes.
//
// Every handler reuses the same view-model builders as its htmx sibling
// (context_handler.go, render.go, rail_handler.go, nav.go) so link resolution
// and rendering stay single-sourced — see the API contracts table in
// docs/spec/serve-react-frontend.md.
package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
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
func NewAPIPageHandler(root string, g graphProvider, landing string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/api/page/")
		// Empty relpath is the SPA's "/" landing request — the server owns
		// scope resolution (realm → wiki/index.md, repo → README.md), the
		// client can't know it.
		if relPath == "" || relPath == "/" {
			relPath = landing
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

// ─── GET /api/search/md ─────────────────────────────────────────────────────

// apiMdSearchResult is one matching line — reshapes mdMatch (search_md.go).
type apiMdSearchResult struct {
	RelPath string `json:"relpath"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// apiMdSearchResponse is the /api/search/md success payload.
type apiMdSearchResponse struct {
	Query     string              `json:"query"`
	Truncated bool                `json:"truncated"`
	Cap       int                 `json:"cap"`
	Results   []apiMdSearchResult `json:"results"`
}

// NewAPIMdSearchHandler returns an http.Handler for GET /api/search/md?q=...
// Reuses the same mdSearchHandler.search walk NewMdSearchHandler's htmx
// fragment uses, reshaped as JSON instead of an HTML list.
func NewAPIMdSearchHandler(opts MdSearchOptions) http.Handler {
	h := &mdSearchHandler{navRoot: opts.NavRoot}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeAPIError(w, http.StatusBadRequest, "missing query parameter: q")
			return
		}

		matches, truncated := h.search(query)
		writeAPIJSON(w, apiMdSearchResponseFrom(query, matches, truncated))
	})
}

// apiMdSearchResponseFrom reshapes mdMatch results into the wire response —
// shared by the synchronous handler and the SSE stream's "md" event.
func apiMdSearchResponseFrom(query string, matches []mdMatch, truncated bool) apiMdSearchResponse {
	results := make([]apiMdSearchResult, len(matches))
	for i, m := range matches {
		results[i] = apiMdSearchResult{RelPath: m.RelPath, Line: m.Line, Snippet: m.Snippet}
	}
	return apiMdSearchResponse{Query: query, Truncated: truncated, Cap: mdSearchResultCap, Results: results}
}

// ─── GET /api/code/search ────────────────────────────────────────────────────

// apiNodeRef is the reshaped subset of types.Node the frontend needs per
// result — id, name, kind, filePath, startLine (per the API contracts table).
type apiNodeRef struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      types.NodeKind `json:"kind"`
	FilePath  string         `json:"filePath"`
	StartLine int            `json:"startLine"`
}

// apiNodeRefFrom reshapes a types.Node into the wire nodeRef shape.
func apiNodeRefFrom(n types.Node) apiNodeRef {
	return apiNodeRef{ID: n.ID, Name: n.Name, Kind: n.Kind, FilePath: n.FilePath, StartLine: n.StartLine}
}

// apiCodeSearchMember is one member's result group — reshapes memberResult
// (codesearch.go). Un-indexed members carry indexed:false and empty results
// (a data field, not an error — per the API contracts conventions).
type apiCodeSearchMember struct {
	Key     string       `json:"key"`
	Prefix  string       `json:"prefix"`
	Indexed bool         `json:"indexed"`
	Results []apiNodeRef `json:"results"`
}

// apiCodeSearchResponse is the /api/code/search success payload.
type apiCodeSearchResponse struct {
	Members []apiCodeSearchMember `json:"members"`
}

// apiCodeSearchMemberFrom reshapes a memberResult into the wire shape —
// shared by the synchronous handler and the SSE stream's "code" event.
func apiCodeSearchMemberFrom(g memberResult) apiCodeSearchMember {
	results := make([]apiNodeRef, len(g.Results))
	for i, r := range g.Results {
		results[i] = apiNodeRefFrom(r.Node)
	}
	return apiCodeSearchMember{
		Key:     g.Key,
		Prefix:  g.Prefix,
		Indexed: !g.NotIndexed,
		Results: results,
	}
}

// NewAPICodeSearchHandler returns an http.Handler for GET
// /api/code/search?q=&only=&exclude=. Reuses the same codeSearchGroups fan-out
// (concurrency/bounding unchanged) NewCodeSearchHandler's htmx fragment uses,
// reshaped as JSON member groups instead of an HTML list.
func NewAPICodeSearchHandler(opts CodeSearchOptions) http.Handler {
	fn := opts.SearchFn
	if fn == nil {
		fn = DefaultMemberSearchFn()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeAPIError(w, http.StatusBadRequest, "missing query parameter: q")
			return
		}
		only := splitCommaParam(r.URL.Query().Get("only"))
		excl := splitCommaParam(r.URL.Query().Get("exclude"))

		res, err := realm.Resolve(opts.RealmRoot, opts.ClaudeMDPath)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "scope resolve: "+err.Error())
			return
		}

		groups := codeSearchGroups(r.Context(), res, opts.RealmRoot, only, excl, query, fn, nil)
		members := make([]apiCodeSearchMember, len(groups))
		for i, g := range groups {
			members[i] = apiCodeSearchMemberFrom(g)
		}
		writeAPIJSON(w, apiCodeSearchResponse{Members: members})
	})
}

// ─── GET /api/search/stream ──────────────────────────────────────────────────

// apiSearchStreamCodeEvent is the payload for each "code" SSE event — one per
// realm member, reshaping memberResult minus its result list (Results are
// only carried when non-empty and indexed, mirroring the member/results split
// in the API contracts table).
type apiSearchStreamCodeEvent struct {
	Member  apiSearchStreamMemberInfo `json:"member"`
	Results []apiNodeRef              `json:"results"`
}

// apiSearchStreamMemberInfo is the member identity carried inside each "code"
// SSE event.
type apiSearchStreamMemberInfo struct {
	Key     string `json:"key"`
	Prefix  string `json:"prefix"`
	Indexed bool   `json:"indexed"`
}

// NewAPISearchStreamHandler returns an http.Handler for GET
// /api/search/stream?q=&src=. Emits named JSON SSE events: "md" (the
// /api/search/md payload), one "code" event per realm member as its
// concurrent search completes, then a terminal "end" ({}). Reuses the same
// mdSearchHandler.search and codeSearchGroups fan-out the JSON/htmx siblings
// use — concurrency/bounding behavior is unchanged.
func NewAPISearchStreamHandler(opts SearchStreamOptions) http.Handler {
	fn := opts.SearchFn
	if fn == nil {
		fn = DefaultMemberSearchFn()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeAPIError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}

		q := strings.TrimSpace(r.URL.Query().Get("q"))
		src := normalizeSearchSrc(r.URL.Query().Get("src"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		if q == "" {
			writeSSEJSON(w, flusher, "end", struct{}{})
			return
		}

		if src == "md" || src == "all" {
			mh := &mdSearchHandler{navRoot: opts.NavRoot}
			matches, truncated := mh.search(q)
			writeSSEJSON(w, flusher, "md", apiMdSearchResponseFrom(q, matches, truncated))
		}

		if src == "code" || src == "all" {
			streamAPICodeResults(r.Context(), w, flusher, opts.RealmRoot, opts.ClaudeMDPath, q, fn)
		}

		writeSSEJSON(w, flusher, "end", struct{}{})
	})
}

// streamAPICodeResults resolves the realm and emits one JSON "code" SSE
// event per member as its concurrent search completes. A realm with no code
// members resolved emits no "code" events — the client's "end" event closes
// the stream and the search UI shows no results.
func streamAPICodeResults(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	realmRoot, claudeMDPath, query string,
	fn MemberSearchFn,
) {
	res, err := realm.Resolve(realmRoot, claudeMDPath)
	if err != nil {
		return
	}
	codeSearchGroups(ctx, res, realmRoot, nil, nil, query, fn, func(g memberResult) {
		m := apiCodeSearchMemberFrom(g)
		writeSSEJSON(w, flusher, "code", apiSearchStreamCodeEvent{
			Member:  apiSearchStreamMemberInfo{Key: m.Key, Prefix: m.Prefix, Indexed: m.Indexed},
			Results: m.Results,
		})
	})
}

// writeSSEJSON marshals v to JSON and writes it as one Server-Sent Event
// (event: <event>\ndata: <json>\n\n). Reuses the same wire framing writeSSE
// uses (search_stream.go); JSON never contains embedded newlines so the
// multi-line data: split writeSSE performs is a no-op here.
func writeSSEJSON(w http.ResponseWriter, flusher http.Flusher, event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{}`)
	}
	writeSSE(w, flusher, event, string(b))
}
