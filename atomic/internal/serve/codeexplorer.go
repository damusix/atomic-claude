// codeexplorer.go — per-repo Code Explorer + SQL schema view, backing the
// /api/code/* JSON endpoints (bottom of this file):
//   - GET /api/code/node?id=&member=            — node detail
//   - GET /api/code/{callers,callees,impact}    — subgraph traversal
//   - GET /api/code/files                       — indexed file list
//   - GET /api/code/schema                      — SQL schema view
//   - GET /api/code/file?path=&member=          — symbols defined in a file
//
// Design seam:
//
//	CodeEngine interface covers the engine methods serve uses.
//	EngineProvider func(ctx, projectRoot, dbPath) (CodeEngine, error) opens an
//	engine per request; the production default wraps *engine.Engine.
//	Tests inject a fakeCodeEngine. The db path is resolved the same way CP7
//	does for repo scope: <realmRoot>/.claude/.atomic-index/atomic.db.
package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// CodeEngine interface
// ---------------------------------------------------------------------------

// CodeEngine is the narrow interface serve uses for per-repo code exploration.
// *engine.Engine satisfies this interface; tests inject a fake.
type CodeEngine interface {
	SearchNodes(ctx context.Context, opts types.SearchOptions) ([]types.SearchResult, error)
	GetNode(ctx context.Context, id string) (types.Node, error)
	GetNodesByName(ctx context.Context, name string, kind types.NodeKind) ([]types.Node, error)
	GetCallers(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error)
	GetCallees(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error)
	GetImpactRadius(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error)
	GetFiles(ctx context.Context) ([]types.FileRecord, error)
	GetNodesInFile(ctx context.Context, filePath string) ([]types.Node, error)
	GetNodesByKind(ctx context.Context, kind types.NodeKind) ([]types.Node, error)
	GetOutgoingEdges(ctx context.Context, nodeID string) ([]types.Edge, error)
	GetAllNodes(ctx context.Context) ([]types.Node, error)
	GetAllEdges(ctx context.Context) ([]types.Edge, error)
	Close()
}

// EngineProvider opens a CodeEngine for the given projectRoot and dbPath.
// The engine must be closed by the caller after use.
type EngineProvider func(ctx context.Context, projectRoot, dbPath string) (CodeEngine, error)

// DefaultEngineProvider returns the production EngineProvider:
// NewWithDBPath → Open(ctx). The caller must call Close() after use.
func DefaultEngineProvider() EngineProvider {
	return func(ctx context.Context, projectRoot, dbPath string) (CodeEngine, error) {
		eng, err := engine.NewWithDBPath(projectRoot, dbPath)
		if err != nil {
			return nil, fmt.Errorf("code explorer: create engine: %w", err)
		}
		if err := eng.Open(ctx); err != nil {
			eng.Close()
			return nil, fmt.Errorf("code explorer: open index: %w", err)
		}
		return eng, nil
	}
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// CodeExplorerOptions configures NewCodeExplorerAPIHandler.
type CodeExplorerOptions struct {
	// RealmRoot is the root of the repository (or realm) being served.
	RealmRoot string
	// ClaudeMDPath is used by realm.Resolve to discover federation members.
	ClaudeMDPath string
	// WikiIndexPath is the realm wiki/index.md, used to discover self-indexed
	// members (those carrying their own .claude/.atomic-index/atomic.db).
	WikiIndexPath string
	// EngineProvider opens an engine per request. nil → DefaultEngineProvider().
	EngineProvider EngineProvider
}

// codeExplorerHandler holds the member resolver and engine provider shared by
// every /api/code/* JSON handler (apiCodeExplorerHandler embeds it).
type codeExplorerHandler struct {
	memberResolver
	provider EngineProvider
}

// newCodeExplorerHandler builds the shared resolver/provider state for the
// /api/code/* JSON handlers.
func newCodeExplorerHandler(opts CodeExplorerOptions) *codeExplorerHandler {
	prov := opts.EngineProvider
	if prov == nil {
		prov = DefaultEngineProvider()
	}
	return &codeExplorerHandler{
		memberResolver: memberResolver{
			realmRoot:     opts.RealmRoot,
			claudeMDPath:  opts.ClaudeMDPath,
			wikiIndexPath: opts.WikiIndexPath,
		},
		provider: prov,
	}
}

// memberByPrefix finds a discovered member by its realm-relative Prefix. The
// empty prefix selects the single repo-scope member. ok is false when no member
// matches (realm scope with a missing/blank member param).
func memberByPrefix(members []codeMember, prefix string) (codeMember, bool) {
	for _, m := range members {
		if m.Prefix == prefix {
			return m, true
		}
	}
	return codeMember{}, false
}

// openEngineFor opens an engine for a specific member.
func (h *codeExplorerHandler) openEngineFor(ctx context.Context, m codeMember) (CodeEngine, error) {
	return h.provider(ctx, m.Path, m.DBPath)
}

// engineForRequest opens the engine for the member named by the ?member= query
// param (realm scope) or the local index at the served root (repo/member scope,
// or when the param does not resolve). It returns the member's realm-relative
// prefix so callers can thread it into rendered drill-down links and /file/
// locations. The caller must Close() the returned engine.
func (h *codeExplorerHandler) engineForRequest(ctx context.Context, r *http.Request) (CodeEngine, string, error) {
	prefix := strings.TrimSpace(r.URL.Query().Get("member"))
	if m, ok := memberByPrefix(h.members(), prefix); ok {
		eng, err := h.openEngineFor(ctx, m)
		return eng, m.Prefix, err
	}
	eng, err := h.provider(ctx, h.realmRoot, h.localDBPath())
	return eng, "", err
}

// ---------------------------------------------------------------------------
// callers / callees / impact subgraph mode (shared by the /api/code/*
// handlers below)
// ---------------------------------------------------------------------------

type subgraphMode int

const (
	modeCallers subgraphMode = iota
	modeCallees
	modeImpact
)

// ---------------------------------------------------------------------------
// /code/schema  (SQL schema view)
// ---------------------------------------------------------------------------

// tableSchema holds a rendered table's schema data.
type tableSchema struct {
	Node      types.Node
	Columns   []types.Node
	FKSources []types.Node // nodes that reference this table (FK-like)
	Writers   []types.Node // nodes that write to this table (writes edges)
}

// computeSchema resolves table/view schema entries — columns (contains
// edges), FK sources (references edges from other tables), and writers
// (writes edges from routines) — for the given table and view node sets.
// Shared by the HTML /code/schema handler and the JSON /api/code/schema
// handler so both surface identical data.
//
// For each table/view node, columns come from its own outgoing contains
// edges. FK sources and writers are collected by scanning the outgoing edges
// of every table/view/function/method/procedure node and inverting
// references/writes edges that target a table or view in this set:
//   - edge {src: ordersTbl, tgt: usersTbl, kind: references} → ordersTbl is an FK source of usersTbl
//   - edge {src: insertProc, tgt: usersTbl, kind: writes} → insertProc is a writer of usersTbl
func computeSchema(ctx context.Context, eng CodeEngine, tables, views []types.Node) (tableSchemas, viewSchemas []tableSchema, err error) {
	nodeByID := make(map[string]types.Node)
	for _, t := range tables {
		nodeByID[t.ID] = t
	}
	for _, v := range views {
		nodeByID[v.ID] = v
	}

	extraNodes := make([]types.Node, 0, len(tables)+len(views))
	extraNodes = append(extraNodes, tables...)
	extraNodes = append(extraNodes, views...)
	for _, k := range []types.NodeKind{types.NodeKindFunction, types.NodeKindMethod, types.NodeKindProcedure} {
		kn, kerr := eng.GetNodesByKind(ctx, k)
		if kerr == nil {
			extraNodes = append(extraNodes, kn...)
		}
	}

	fkSourcesByTable := make(map[string][]types.Node) // tableID → nodes referencing this table
	writersByTable := make(map[string][]types.Node)   // tableID → nodes writing this table
	for _, srcNode := range extraNodes {
		edges, eerr := eng.GetOutgoingEdges(ctx, srcNode.ID)
		if eerr != nil {
			continue // best-effort
		}
		for _, e := range edges {
			switch e.Kind {
			case types.EdgeKindReferences:
				if _, ok := nodeByID[e.Target]; ok {
					fkSourcesByTable[e.Target] = appendIfNew(fkSourcesByTable[e.Target], srcNode)
				}
			case types.EdgeKindWrites:
				writersByTable[e.Target] = appendIfNew(writersByTable[e.Target], srcNode)
			}
		}
	}

	build := func(nodes []types.Node) []tableSchema {
		out := make([]tableSchema, 0, len(nodes))
		for _, tableNode := range nodes {
			ts := tableSchema{Node: tableNode}
			edges, _ := eng.GetOutgoingEdges(ctx, tableNode.ID)
			for _, e := range edges {
				if e.Kind == types.EdgeKindContains {
					colNode, cerr := eng.GetNode(ctx, e.Target)
					if cerr == nil {
						ts.Columns = append(ts.Columns, colNode)
					}
				}
			}
			ts.FKSources = fkSourcesByTable[tableNode.ID]
			ts.Writers = writersByTable[tableNode.ID]
			out = append(out, ts)
		}
		return out
	}

	return build(tables), build(views), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// appendIfNew appends n to nodes only if n.ID is not already present.
func appendIfNew(nodes []types.Node, n types.Node) []types.Node {
	for _, existing := range nodes {
		if existing.ID == n.ID {
			return nodes
		}
	}
	return append(nodes, n)
}

// ---------------------------------------------------------------------------
// /api/code/* — CP4: JSON siblings of the /code/* explorer routes.
// ---------------------------------------------------------------------------

// apiCodeNode is the full node shape for code-intel API responses (the API
// contracts table: id, name, kind, filePath, startLine, signature, language,
// docstring).
type apiCodeNode struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      types.NodeKind `json:"kind"`
	FilePath  string         `json:"filePath"`
	StartLine int            `json:"startLine"`
	Signature string         `json:"signature,omitempty"`
	Language  types.Language `json:"language,omitempty"`
	Docstring string         `json:"docstring,omitempty"`
}

func apiCodeNodeFrom(n types.Node) apiCodeNode {
	return apiCodeNode{
		ID:        n.ID,
		Name:      n.Name,
		Kind:      n.Kind,
		FilePath:  n.FilePath,
		StartLine: n.StartLine,
		Signature: n.Signature,
		Language:  n.Language,
		Docstring: n.Docstring,
	}
}

// apiCodeExplorerHandler serves the JSON /api/code/* siblings of
// codeExplorerHandler's HTML routes, reusing its member resolution and
// engine provisioning via embedding.
type apiCodeExplorerHandler struct {
	*codeExplorerHandler
}

// NewCodeExplorerAPIHandler returns an http.Handler for the JSON
// /api/code/{node,callers,callees,impact,files,schema,file} routes.
func NewCodeExplorerAPIHandler(opts CodeExplorerOptions) http.Handler {
	h := newCodeExplorerHandler(opts)
	return &apiCodeExplorerHandler{h}
}

func (h *apiCodeExplorerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/api/code/node"):
		h.handleAPINode(w, r)
	case strings.HasSuffix(path, "/api/code/callers"):
		h.handleAPISubgraph(w, r, modeCallers)
	case strings.HasSuffix(path, "/api/code/callees"):
		h.handleAPISubgraph(w, r, modeCallees)
	case strings.HasSuffix(path, "/api/code/impact"):
		h.handleAPISubgraph(w, r, modeImpact)
	case strings.HasSuffix(path, "/api/code/files"):
		h.handleAPIFiles(w, r)
	case strings.HasSuffix(path, "/api/code/schema"):
		h.handleAPISchema(w, r)
	case strings.HasSuffix(path, "/api/code/file"):
		h.handleAPIFile(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "unknown route: "+path)
	}
}

// ─── GET /api/code/node?id=&member= ────────────────────────────────────────

type apiCodeNodeResponse struct {
	Member string      `json:"member"`
	Node   apiCodeNode `json:"node"`
}

func (h *apiCodeExplorerHandler) handleAPINode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "missing id parameter")
		return
	}

	eng, prefix, err := h.engineForRequest(ctx, r)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "index not available: "+err.Error())
		return
	}
	defer eng.Close()

	n, err := eng.GetNode(ctx, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "node not found: "+id)
		return
	}

	writeAPIJSON(w, apiCodeNodeResponse{Member: prefix, Node: apiCodeNodeFrom(n)})
}

// ─── GET /api/code/{callers,callees,impact}?id=&member= ────────────────────

type apiCodeEdge struct {
	Kind   types.EdgeKind `json:"kind"`
	Source string         `json:"source"`
	Target string         `json:"target"`
}

type apiCodeSubgraphResponse struct {
	Member string                 `json:"member"`
	Root   apiCodeNode            `json:"root"`
	Edges  []apiCodeEdge          `json:"edges"`
	Nodes  map[string]apiCodeNode `json:"nodes"`
}

func (h *apiCodeExplorerHandler) handleAPISubgraph(w http.ResponseWriter, r *http.Request, mode subgraphMode) {
	ctx := r.Context()
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "missing id parameter")
		return
	}

	depth := 2 // default
	if ds := strings.TrimSpace(r.URL.Query().Get("depth")); ds != "" {
		if n, err := strconv.Atoi(ds); err == nil && n > 0 {
			depth = n
		}
	}

	eng, prefix, err := h.engineForRequest(ctx, r)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "index not available: "+err.Error())
		return
	}
	defer eng.Close()

	var sg types.Subgraph
	switch mode {
	case modeCallers:
		sg, err = eng.GetCallers(ctx, id, depth)
	case modeCallees:
		sg, err = eng.GetCallees(ctx, id, depth)
	case modeImpact:
		sg, err = eng.GetImpactRadius(ctx, id, depth)
	}
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "node not found: "+id)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "graph query failed: "+err.Error())
		return
	}

	root := apiCodeNode{ID: id}
	if n, ok := sg.Nodes[id]; ok {
		root = apiCodeNodeFrom(n)
	}

	edges := make([]apiCodeEdge, len(sg.Edges))
	for i, e := range sg.Edges {
		edges[i] = apiCodeEdge{Kind: e.Kind, Source: e.Source, Target: e.Target}
	}
	nodes := make(map[string]apiCodeNode, len(sg.Nodes))
	for nid, n := range sg.Nodes {
		nodes[nid] = apiCodeNodeFrom(n)
	}

	writeAPIJSON(w, apiCodeSubgraphResponse{Member: prefix, Root: root, Edges: edges, Nodes: nodes})
}

// ─── GET /api/code/files?member= ────────────────────────────────────────────

type apiCodeFileRecord struct {
	Path      string         `json:"path"`
	Language  types.Language `json:"language"`
	NodeCount int            `json:"nodeCount"`
}

type apiCodeFilesResponse struct {
	Files []apiCodeFileRecord `json:"files"`
}

func (h *apiCodeExplorerHandler) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eng, _, err := h.engineForRequest(ctx, r)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "index not available: "+err.Error())
		return
	}
	defer eng.Close()

	files, err := eng.GetFiles(ctx)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "file list query failed: "+err.Error())
		return
	}

	out := make([]apiCodeFileRecord, len(files))
	for i, f := range files {
		out[i] = apiCodeFileRecord{Path: f.Path, Language: f.Language, NodeCount: f.NodeCount}
	}
	writeAPIJSON(w, apiCodeFilesResponse{Files: out})
}

// ─── GET /api/code/schema?member= ───────────────────────────────────────────

type apiTableSchema struct {
	Node      apiCodeNode   `json:"node"`
	Columns   []apiCodeNode `json:"columns"`
	FKSources []apiCodeNode `json:"fkSources"`
	Writers   []apiCodeNode `json:"writers"`
}

type apiCodeSchemaResponse struct {
	Tables []apiTableSchema `json:"tables"`
}

func apiTableSchemaFrom(ts tableSchema) apiTableSchema {
	cols := make([]apiCodeNode, len(ts.Columns))
	for i, c := range ts.Columns {
		cols[i] = apiCodeNodeFrom(c)
	}
	fks := make([]apiCodeNode, len(ts.FKSources))
	for i, f := range ts.FKSources {
		fks[i] = apiCodeNodeFrom(f)
	}
	writers := make([]apiCodeNode, len(ts.Writers))
	for i, w := range ts.Writers {
		writers[i] = apiCodeNodeFrom(w)
	}
	return apiTableSchema{Node: apiCodeNodeFrom(ts.Node), Columns: cols, FKSources: fks, Writers: writers}
}

func (h *apiCodeExplorerHandler) handleAPISchema(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	eng, _, err := h.engineForRequest(ctx, r)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "index not available: "+err.Error())
		return
	}
	defer eng.Close()

	tables, err := eng.GetNodesByKind(ctx, types.NodeKindTable)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "schema query failed: "+err.Error())
		return
	}
	views, err := eng.GetNodesByKind(ctx, types.NodeKindView)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "schema query failed: "+err.Error())
		return
	}

	tableSchemas, viewSchemas, err := computeSchema(ctx, eng, tables, views)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "schema query failed: "+err.Error())
		return
	}

	out := make([]apiTableSchema, 0, len(tableSchemas)+len(viewSchemas))
	for _, ts := range tableSchemas {
		out = append(out, apiTableSchemaFrom(ts))
	}
	for _, ts := range viewSchemas {
		out = append(out, apiTableSchemaFrom(ts))
	}
	writeAPIJSON(w, apiCodeSchemaResponse{Tables: out})
}

// ─── GET /api/code/file?path=&member= ───────────────────────────────────────

type apiCodeFileNode struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      types.NodeKind `json:"kind"`
	StartLine int            `json:"startLine"`
}

// apiCodeFileResponse is the /api/code/file success payload. Degraded is set
// (and Nodes left empty) for the not-indexed/no-intel soft states — a data
// field per the API contracts conventions, not an error envelope.
type apiCodeFileResponse struct {
	Path     string            `json:"path"`
	Member   string            `json:"member"`
	Nodes    []apiCodeFileNode `json:"nodes"`
	Degraded string            `json:"degraded,omitempty"`
}

func (h *apiCodeExplorerHandler) handleAPIFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		writeAPIError(w, http.StatusBadRequest, "missing path parameter")
		return
	}

	m, memberRel, ok := memberForPath(h.members(), filePath)
	if !ok {
		writeAPIJSON(w, apiCodeFileResponse{Path: filePath, Degraded: "no code intelligence for this file (not indexed)"})
		return
	}

	eng, err := h.openEngineFor(ctx, m)
	if err != nil {
		writeAPIJSON(w, apiCodeFileResponse{Path: filePath, Member: m.Prefix, Degraded: "index not available — run atomic code index"})
		return
	}
	defer eng.Close()

	nodes, err := eng.GetNodesInFile(ctx, memberRel)
	if err != nil || len(nodes) == 0 {
		writeAPIJSON(w, apiCodeFileResponse{Path: filePath, Member: m.Prefix, Degraded: "no code intelligence for this file (not indexed)"})
		return
	}

	out := make([]apiCodeFileNode, len(nodes))
	for i, n := range nodes {
		out[i] = apiCodeFileNode{ID: n.ID, Name: n.Name, Kind: n.Kind, StartLine: n.StartLine}
	}
	writeAPIJSON(w, apiCodeFileResponse{Path: filePath, Member: m.Prefix, Nodes: out})
}
