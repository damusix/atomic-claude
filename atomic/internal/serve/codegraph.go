// codegraph.go — code-graph spec CP2: full-repo code graph export.
//
// Route: GET /code/graph/data[?member=<prefix>]
//
// Serves the resolved member's ENTIRE symbol graph (all nodes + all edges) as
// flat JSON for the code graph view (cosmos.gl force-directed render). Unlike
// the sibling /code/* explorer routes (which render HTML fragments and degrade
// silently to a "not indexed" note), this endpoint is a data API: an unknown
// member or an unopenable index is reported as a non-200 JSON error body so
// the client-side adapter can distinguish "no data yet" from "loading".
//
// Member resolution mirrors codeExplorerHandler.engineForRequest, except an
// explicit ?member= that does not resolve is reported as an error instead of
// silently falling back to the local index — the data endpoint must not mask
// a typo'd member as "single repo, zero nodes".
package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// graphNode is one flat node element in the /code/graph/data response.
type graphNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Language string `json:"language"`
}

// graphEdge is one flat edge element in the /code/graph/data response.
type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// graphDataResponse is the /code/graph/data success payload. Flat means no
// nested per-element envelope (contrast with graphoverlay.go's Cytoscape
// {data:{...}} shape) — the client-side adapter fills typed arrays directly.
type graphDataResponse struct {
	Fingerprint string      `json:"fingerprint"`
	Nodes       []graphNode `json:"nodes"`
	Edges       []graphEdge `json:"edges"`
}

// graphErrorResponse is the /code/graph/data error payload (paired with a
// non-200 status).
type graphErrorResponse struct {
	Error string `json:"error"`
}

// CodeGraphOptions configures NewCodeGraphHandler.
type CodeGraphOptions struct {
	// RealmRoot is the root of the repository (or realm) being served.
	RealmRoot string
	// ClaudeMDPath is used by realm.Resolve to discover federation members.
	ClaudeMDPath string
	// WikiIndexPath is the realm wiki/index.md, used to discover self-indexed
	// members. Defaults to <realmRoot>/wiki/index.md when empty.
	WikiIndexPath string
	// EngineProvider opens an engine per request. nil → DefaultEngineProvider().
	EngineProvider EngineProvider
}

// codeGraphHandler implements http.Handler for GET /code/graph/data.
type codeGraphHandler struct {
	memberResolver
	provider EngineProvider
}

// NewCodeGraphHandler returns an http.Handler for GET /code/graph/data.
func NewCodeGraphHandler(opts CodeGraphOptions) http.Handler {
	prov := opts.EngineProvider
	if prov == nil {
		prov = DefaultEngineProvider()
	}
	return &codeGraphHandler{
		memberResolver: memberResolver{
			realmRoot:     opts.RealmRoot,
			claudeMDPath:  opts.ClaudeMDPath,
			wikiIndexPath: opts.WikiIndexPath,
		},
		provider: prov,
	}
}

// engineForGraphRequest resolves the engine for the ?member= query param. An
// explicit member that does not resolve reports unknownMember=true rather than
// the silent local-index fallback codeExplorerHandler's HTML routes use — the
// data endpoint must distinguish a typo'd member from "single repo, zero nodes".
func (h *codeGraphHandler) engineForGraphRequest(ctx context.Context, r *http.Request) (eng CodeEngine, err error, unknownMember bool) {
	prefix := strings.TrimSpace(r.URL.Query().Get("member"))
	members := h.members()

	if prefix != "" {
		m, ok := memberByPrefix(members, prefix)
		if !ok {
			return nil, nil, true
		}
		eng, err = h.provider(ctx, m.Path, m.DBPath)
		return eng, err, false
	}

	if m, ok := memberByPrefix(members, ""); ok {
		eng, err = h.provider(ctx, m.Path, m.DBPath)
		return eng, err, false
	}

	eng, err = h.provider(ctx, h.realmRoot, h.localDBPath())
	return eng, err, false
}

func (h *codeGraphHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eng, err, unknownMember := h.engineForGraphRequest(ctx, r)
	if unknownMember {
		prefix := strings.TrimSpace(r.URL.Query().Get("member"))
		writeGraphError(w, http.StatusNotFound, "unknown member: "+prefix)
		return
	}
	if err != nil {
		writeGraphError(w, http.StatusNotFound, "index not available — run atomic code index")
		return
	}
	defer eng.Close()

	nodes, err := eng.GetAllNodes(ctx)
	if err != nil {
		writeGraphError(w, http.StatusInternalServerError, "graph query failed: "+err.Error())
		return
	}
	edges, err := eng.GetAllEdges(ctx)
	if err != nil {
		writeGraphError(w, http.StatusInternalServerError, "graph query failed: "+err.Error())
		return
	}

	resp := graphDataResponse{
		Fingerprint: graphFingerprint(nodes, edges),
		Nodes:       make([]graphNode, 0, len(nodes)),
		Edges:       make([]graphEdge, 0, len(edges)),
	}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, graphNode{
			ID:       n.ID,
			Label:    n.Name,
			Kind:     string(n.Kind),
			File:     n.FilePath,
			Line:     n.StartLine,
			Language: string(n.Language),
		})
	}
	for _, e := range edges {
		resp.Edges = append(resp.Edges, graphEdge{
			Source: e.Source,
			Target: e.Target,
			Kind:   string(e.Kind),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp) // headers already sent on error; nothing to recover
}

// graphFingerprint derives a content-sensitive fingerprint from the actual
// graph identity — sorted (id, label, line) node tuples and sorted
// (source, target, kind) edge tuples — rather than counts + a timestamp. A
// count-preserving mutation (e.g. renaming a symbol, which deletes the old
// node id and inserts a new one in its place) still changes the hash, so the
// client-side layout cache never replays a stale layout keyed to node ids
// that no longer exist (SC2: "changes iff the index content changes"). Bulk
// counts + files.indexed_at cannot distinguish this case: a same-second
// re-index with an unchanged node/edge count would reproduce the same
// fingerprint even though every id changed.
func graphFingerprint(nodes []types.Node, edges []types.Edge) string {
	nodeLines := make([]string, len(nodes))
	for i, n := range nodes {
		nodeLines[i] = n.ID + "\x00" + n.Name + "\x00" + strconv.Itoa(n.StartLine)
	}
	sort.Strings(nodeLines)

	edgeLines := make([]string, len(edges))
	for i, e := range edges {
		edgeLines[i] = e.Source + "\x00" + e.Target + "\x00" + string(e.Kind)
	}
	sort.Strings(edgeLines)

	h := sha256.New()
	for _, l := range nodeLines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	for _, l := range edgeLines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeGraphError writes a JSON error body with the given non-200 status code.
func writeGraphError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(graphErrorResponse{Error: msg})
}
