// GET /code/graph/data: a member's entire symbol graph as flat JSON for the
// force-directed view.
//
// Unlike the /api/code/* routes, which degrade quietly to a "not indexed"
// note, this endpoint errors with a non-200 body so the client can tell "no
// data yet" from "still loading". For the same reason an explicit ?member=
// that does not resolve is an error, never a silent fallback to the local
// index — a typo must not read as "single repo, zero nodes".
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

type graphNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Language string `json:"language"`
}

type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// graphDataResponse carries no per-element envelope, unlike graphoverlay.go's
// Cytoscape shape, so the client fills typed arrays directly.
type graphDataResponse struct {
	Fingerprint string      `json:"fingerprint"`
	Nodes       []graphNode `json:"nodes"`
	Edges       []graphEdge `json:"edges"`
}

type graphErrorResponse struct {
	Error string `json:"error"`
}

// CodeGraphOptions configures NewCodeGraphHandler.
type CodeGraphOptions struct {
	// RealmRoot is the root of the repository or realm being served.
	RealmRoot string
	// ClaudeMDPath lets realm.Resolve discover federation members.
	ClaudeMDPath string
	// WikiIndexPath locates self-indexed members; empty means
	// <realmRoot>/wiki/index.md.
	WikiIndexPath string
	// EngineProvider nil takes DefaultEngineProvider.
	EngineProvider EngineProvider
}

type codeGraphHandler struct {
	memberResolver
	provider EngineProvider
}

// NewCodeGraphHandler serves GET /code/graph/data.
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

// engineForGraphRequest reports unknownMember rather than falling back to the
// local index, so a typo'd ?member= surfaces instead of reading as empty.
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

	edges = dedupParallelEdges(edges)

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

// dedupParallelEdges collapses edges sharing a (source, target, kind) triple.
// The edges table is correct to store one row per call site, but a helper
// called N times from one caller would otherwise draw N stacked identical
// links. Display-layer only — nothing upstream changes.
func dedupParallelEdges(edges []types.Edge) []types.Edge {
	seen := make(map[string]struct{}, len(edges))
	out := make([]types.Edge, 0, len(edges))
	for _, e := range edges {
		key := e.Source + "\x00" + e.Target + "\x00" + string(e.Kind)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	return out
}

// graphFingerprint hashes the graph's actual identity rather than counts plus
// a timestamp, so a count-preserving mutation — renaming a symbol swaps one
// node id for another — still changes it. Otherwise the client would replay a
// cached layout keyed to node ids that no longer exist.
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

// writeGraphError writes a JSON error body with a non-200 status.
func writeGraphError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(graphErrorResponse{Error: msg})
}
