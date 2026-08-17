// The /api/* content endpoints — page, file, rail, nav, search — that the
// frontend reads. Contracts are specified in docs/spec/serve-react-frontend.md.
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

// writeAPIJSON writes v as a 200 JSON body. HTML-escaping stays on here,
// unlike the graph endpoints in codegraph.go, which disable it.
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

// writeAPIError writes the error envelope with a non-200 status.
func writeAPIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, err := json.Marshal(apiErrorEnvelope{Error: msg})
	if err != nil {
		// Unreachable — marshaling one string field cannot fail — but the
		// client still gets a body rather than nothing.
		_, _ = w.Write([]byte(`{"error":"internal error"}`))
		return
	}
	_, _ = w.Write(b)
}

// apiPageResponse is the /api/page payload for a file.
type apiPageResponse struct {
	HTML       string          `json:"html"`
	Title      string          `json:"title"`
	RelPath    string          `json:"relpath"`
	HasMermaid bool            `json:"hasMermaid"`
	Breadcrumb []breadcrumbSeg `json:"breadcrumb"`
}

// apiPageDirResponse is the /api/page payload for a directory with no index.
type apiPageDirResponse struct {
	Dir     bool       `json:"dir"`
	RelPath string     `json:"relpath"`
	Entries []dirEntry `json:"entries"`
}

// NewAPIPageHandler serves GET /api/page/<relpath>.
func NewAPIPageHandler(root string, g graphProvider, landing string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/api/page/")
		// An empty relpath is the SPA's "/" request: the server owns scope
		// resolution, the client cannot know it.
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

// apiFileResponse carries the chroma line-table HTML for a source file.
type apiFileResponse struct {
	HTML  string `json:"html"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// NewAPIFileHandler serves GET /api/file/<relpath>.
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

type apiRailBacklink struct {
	Path string `json:"path"`
}

// apiRailResponse is the /api/rail payload. Properties is nil for a page with
// no frontmatter.
type apiRailResponse struct {
	RelPath      string            `json:"relpath"`
	Orphan       bool              `json:"orphan"`
	Properties   []propKV          `json:"properties"`
	Outbound     []Edge            `json:"out"`
	Backlinks    []apiRailBacklink `json:"in"`
	GraphDataURL string            `json:"graphDataURL"`
}

// NewAPIRailHandler serves GET /api/rail/<relpath>.
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

		// A nil slice marshals as null; normalize so the client never has to
		// null-check an array field.
		outbound := graph.Outbound(rel)
		if outbound == nil {
			outbound = []Edge{}
		}
		writeAPIJSON(w, apiRailResponse{
			RelPath:      rel,
			Orphan:       graph.IsOrphan(rel),
			Properties:   railProperties(abs),
			Outbound:     outbound,
			Backlinks:    apiBacklinks,
			GraphDataURL: "/graph/data?node=" + rel + "&depth=1",
		})
	})
}

// apiNavResponse is the /api/nav payload.
type apiNavResponse struct {
	Scope string `json:"scope"`
	// Name is the scope root's directory name, so the header labels what is
	// actually being served rather than a hardcoded product name.
	Name string `json:"name"`
	// Branch is recomputed per request, so a checkout shows up on the next
	// live-reload refetch rather than the next server restart.
	Branch string         `json:"branch"`
	Groups []navGroupJSON `json:"groups"`
}

// NewAPINavHandler serves GET /api/nav.
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
			identity := resolveScopeIdentity(opts.RealmRoot)
			writeAPIJSON(w, apiNavResponse{
				Scope:  "realm",
				Name:   identity.Name,
				Branch: identity.Branch,
				Groups: buildRealmNavGroupsJSON(opts),
			})
			return
		}

		snap, _ := store.ensureFresh()
		identity := resolveScopeIdentity(opts.RealmRoot)
		writeAPIJSON(w, apiNavResponse{
			Scope:  "repo",
			Name:   identity.Name,
			Branch: identity.Branch,
			Groups: buildRepoNavGroupsJSON(snap.navPaths),
		})
	})
}

// apiMdSearchResult is one matching line.
type apiMdSearchResult struct {
	RelPath string `json:"relpath"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// apiMdSearchResponse is the /api/search/md payload.
type apiMdSearchResponse struct {
	Query     string              `json:"query"`
	Truncated bool                `json:"truncated"`
	Cap       int                 `json:"cap"`
	Results   []apiMdSearchResult `json:"results"`
}

// NewAPIMdSearchHandler serves GET /api/search/md?q=…
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

// apiMdSearchResponseFrom is shared by the synchronous handler and the SSE
// stream's "md" event.
func apiMdSearchResponseFrom(query string, matches []mdMatch, truncated bool) apiMdSearchResponse {
	results := make([]apiMdSearchResult, len(matches))
	for i, m := range matches {
		results[i] = apiMdSearchResult{RelPath: m.RelPath, Line: m.Line, Snippet: m.Snippet}
	}
	return apiMdSearchResponse{Query: query, Truncated: truncated, Cap: mdSearchResultCap, Results: results}
}

// apiNodeRef is the subset of types.Node a search result carries.
type apiNodeRef struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      types.NodeKind `json:"kind"`
	FilePath  string         `json:"filePath"`
	StartLine int            `json:"startLine"`
}

func apiNodeRefFrom(n types.Node) apiNodeRef {
	return apiNodeRef{ID: n.ID, Name: n.Name, Kind: n.Kind, FilePath: n.FilePath, StartLine: n.StartLine}
}

// apiCodeSearchMember is one member's result group. An unindexed member is
// reported as data — indexed:false with no results — not as an error.
type apiCodeSearchMember struct {
	Key     string       `json:"key"`
	Prefix  string       `json:"prefix"`
	Indexed bool         `json:"indexed"`
	Results []apiNodeRef `json:"results"`
}

// apiCodeSearchResponse is the /api/code/search payload.
type apiCodeSearchResponse struct {
	Members []apiCodeSearchMember `json:"members"`
}

// apiCodeSearchMemberFrom is shared by the synchronous handler and the SSE
// stream's "code" event.
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

// NewAPICodeSearchHandler serves GET /api/code/search?q=&only=&exclude=.
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

// apiSearchStreamCodeEvent is one "code" SSE event, emitted per realm member.
type apiSearchStreamCodeEvent struct {
	Member  apiSearchStreamMemberInfo `json:"member"`
	Results []apiNodeRef              `json:"results"`
}

type apiSearchStreamMemberInfo struct {
	Key     string `json:"key"`
	Prefix  string `json:"prefix"`
	Indexed bool   `json:"indexed"`
}

// NewAPISearchStreamHandler serves GET /api/search/stream?q=&src=, emitting an
// "md" event, one "code" event per member as its search completes, then "end".
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

// streamAPICodeResults emits one "code" event per member. A realm with no code
// members emits none, and the terminal "end" closes the stream.
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

// writeSSEJSON writes v as one Server-Sent Event. Marshaled JSON has no
// embedded newlines, so writeSSE's multi-line data: split is a no-op here.
func writeSSEJSON(w http.ResponseWriter, flusher http.Flusher, event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{}`)
	}
	writeSSE(w, flusher, event, string(b))
}
