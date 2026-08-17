// Per-repo code exploration and the SQL schema view, behind /api/code/*.
//
// The CodeEngine interface and EngineProvider are the test seam: an engine is
// opened per request, and tests inject a fake in place of *engine.Engine.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// CodeEngine is the narrow slice of *engine.Engine that serve uses.
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
	// IndexAll is the only method here that writes, reached solely by the
	// loopback-only reindex endpoint.
	IndexAll(ctx context.Context) error
	Close()
}

// EngineProvider opens a CodeEngine the caller must Close.
type EngineProvider func(ctx context.Context, projectRoot, dbPath string) (CodeEngine, error)

// DefaultEngineProvider returns the production EngineProvider.
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

// CodeExplorerOptions configures NewCodeExplorerAPIHandler.
type CodeExplorerOptions struct {
	// RealmRoot is the root of the repository or realm being served.
	RealmRoot string
	// ClaudeMDPath lets realm.Resolve discover federation members.
	ClaudeMDPath string
	// WikiIndexPath locates members carrying their own index db.
	WikiIndexPath string
	// EngineProvider nil takes DefaultEngineProvider.
	EngineProvider EngineProvider
}

// codeExplorerHandler holds the member resolver and engine provider shared by
// every /api/code/* handler.
type codeExplorerHandler struct {
	memberResolver
	provider EngineProvider
}

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

// memberByPrefix finds a member by realm-relative prefix; the empty prefix
// selects the single repo-scope member.
func memberByPrefix(members []codeMember, prefix string) (codeMember, bool) {
	for _, m := range members {
		if m.Prefix == prefix {
			return m, true
		}
	}
	return codeMember{}, false
}

func (h *codeExplorerHandler) openEngineFor(ctx context.Context, m codeMember) (CodeEngine, error) {
	return h.provider(ctx, m.Path, m.DBPath)
}

// engineForRequest opens the engine for the ?member= query param, falling back
// to the local index at the served root. The returned prefix threads into
// drill-down links; the caller must Close the engine.
func (h *codeExplorerHandler) engineForRequest(ctx context.Context, r *http.Request) (CodeEngine, string, error) {
	prefix := strings.TrimSpace(r.URL.Query().Get("member"))
	if m, ok := memberByPrefix(h.members(), prefix); ok {
		eng, err := h.openEngineFor(ctx, m)
		return eng, m.Prefix, err
	}
	eng, err := h.provider(ctx, h.realmRoot, h.localDBPath())
	return eng, "", err
}

type subgraphMode int

const (
	modeCallers subgraphMode = iota
	modeCallees
	modeImpact
)

// tableSchema holds a rendered table's schema data.
type tableSchema struct {
	Node      types.Node
	Columns   []types.Node
	FKSources []types.Node // nodes that reference this table (FK-like)
	Writers   []types.Node // nodes that write to this table (writes edges)
}

// computeSchema resolves each table's columns, FK sources, and writers.
// Columns come from a table's own contains edges; the other two are found by
// inverting the references and writes edges that point at it, since the graph
// only records the outgoing direction.
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

	fkSourcesByTable := make(map[string][]types.Node) // tableID → referencing nodes
	writersByTable := make(map[string][]types.Node)   // tableID → writing nodes
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

// routineSchema is a stored routine and the tables it touches.
type routineSchema struct {
	Node   types.Node
	Reads  []types.Node
	Writes []types.Node
}

// computeRoutines resolves SQL-declared routines and the tables each touches.
// Kind alone is not a sufficient filter — a repo's TypeScript functions are
// NodeKindFunction too — so language gates the set.
func computeRoutines(ctx context.Context, eng CodeEngine, tables, views []types.Node) []routineSchema {
	tableByID := make(map[string]types.Node, len(tables)+len(views))
	for _, t := range tables {
		tableByID[t.ID] = t
	}
	for _, v := range views {
		tableByID[v.ID] = v
	}

	var routines []types.Node
	for _, k := range []types.NodeKind{types.NodeKindProcedure, types.NodeKindFunction} {
		nodes, err := eng.GetNodesByKind(ctx, k)
		if err != nil {
			continue // best-effort
		}
		for _, n := range nodes {
			if n.Language == types.LanguageSQL {
				routines = append(routines, n)
			}
		}
	}

	out := make([]routineSchema, 0, len(routines))
	for _, r := range routines {
		rs := routineSchema{Node: r}
		edges, err := eng.GetOutgoingEdges(ctx, r.ID)
		if err != nil {
			out = append(out, rs)
			continue
		}
		for _, e := range edges {
			target, ok := tableByID[e.Target]
			if !ok {
				continue
			}
			// There is no reads edge kind: a routine's SELECT lands as
			// references, the same kind an FK uses.
			switch e.Kind {
			case types.EdgeKindWrites:
				rs.Writes = appendIfNew(rs.Writes, target)
			case types.EdgeKindReferences:
				rs.Reads = appendIfNew(rs.Reads, target)
			}
		}
		out = append(out, rs)
	}
	return out
}

// computeSQLTypes resolves user-defined SQL types, extracted as type_alias.
// Language gates the set: type_alias is overwhelmingly TypeScript in a mixed
// repo.
func computeSQLTypes(ctx context.Context, eng CodeEngine) []types.Node {
	nodes, err := eng.GetNodesByKind(ctx, types.NodeKindTypeAlias)
	if err != nil {
		return nil
	}
	out := make([]types.Node, 0)
	for _, n := range nodes {
		if n.Language == types.LanguageSQL {
			out = append(out, n)
		}
	}
	return out
}

// appendIfNew appends n unless its ID is already present.
func appendIfNew(nodes []types.Node, n types.Node) []types.Node {
	for _, existing := range nodes {
		if existing.ID == n.ID {
			return nodes
		}
	}
	return append(nodes, n)
}

// apiCodeNode is the node shape every code-intel API response uses.
type apiCodeNode struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      types.NodeKind `json:"kind"`
	FilePath  string         `json:"filePath"`
	StartLine int            `json:"startLine"`
	Signature string         `json:"signature,omitempty"`
	Language  types.Language `json:"language,omitempty"`
	Docstring string         `json:"docstring,omitempty"`
	// Lifted out of Node.Metadata rather than passing the raw map through, so
	// a column list is not a row of bare names and a constraint is not an
	// opaque identifier.
	DataType       string `json:"dataType,omitempty"`
	ConstraintType string `json:"constraintType,omitempty"`
	// ConstraintColumns are the columns a key covers, as declared in source,
	// so the view need not infer membership from the constraint's name.
	ConstraintColumns []string `json:"constraintColumns,omitempty"`
}

// sqlNodeMetadata is the subset of Node.Metadata the schema view reads.
type sqlNodeMetadata struct {
	Type           string   `json:"type"`
	ConstraintType string   `json:"constraint_type"`
	Columns        []string `json:"columns"`
}

func apiCodeNodeFrom(n types.Node) apiCodeNode {
	var meta sqlNodeMetadata
	if len(n.Metadata) > 0 {
		// Best-effort decoration: a node with unparseable metadata still
		// renders, just without the type badge.
		_ = json.Unmarshal(n.Metadata, &meta)
	}
	return apiCodeNode{
		ID:                n.ID,
		Name:              n.Name,
		Kind:              n.Kind,
		FilePath:          n.FilePath,
		StartLine:         n.StartLine,
		Signature:         n.Signature,
		Language:          n.Language,
		Docstring:         n.Docstring,
		DataType:          meta.Type,
		ConstraintType:    meta.ConstraintType,
		ConstraintColumns: meta.Columns,
	}
}

type apiCodeExplorerHandler struct {
	*codeExplorerHandler
	capabilities *capabilitiesCache
}

// NewCodeExplorerAPIHandler serves the
// /api/code/{node,callers,callees,impact,files,schema,file,capabilities} routes.
func NewCodeExplorerAPIHandler(opts CodeExplorerOptions) http.Handler {
	h := newCodeExplorerHandler(opts)
	return &apiCodeExplorerHandler{codeExplorerHandler: h, capabilities: &capabilitiesCache{}}
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
	case strings.HasSuffix(path, "/api/code/capabilities"):
		h.handleAPICapabilities(w, r)
	case strings.HasSuffix(path, "/api/code/file"):
		h.handleAPIFile(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "unknown route: "+path)
	}
}

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

	depth := 2
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

type apiTableSchema struct {
	Node      apiCodeNode   `json:"node"`
	Columns   []apiCodeNode `json:"columns"`
	FKSources []apiCodeNode `json:"fkSources"`
	Writers   []apiCodeNode `json:"writers"`
}

// apiCodeSchemaResponse carries Degraded as a data field, not an error
// envelope: an unindexed member is a soft state the UI renders.
type apiCodeSchemaResponse struct {
	Tables []apiTableSchema `json:"tables"`
	// Routines and Types are the rest of a schema; without them a database
	// defined largely in stored procedures reads as empty.
	Routines []apiRoutineSchema `json:"routines"`
	Types    []apiCodeNode      `json:"types"`
	Degraded string             `json:"degraded,omitempty"`
}

type apiRoutineSchema struct {
	Node   apiCodeNode   `json:"node"`
	Reads  []apiCodeNode `json:"reads"`
	Writes []apiCodeNode `json:"writes"`
}

func apiRoutineSchemaFrom(rs routineSchema) apiRoutineSchema {
	reads := make([]apiCodeNode, len(rs.Reads))
	for i, n := range rs.Reads {
		reads[i] = apiCodeNodeFrom(n)
	}
	writes := make([]apiCodeNode, len(rs.Writes))
	for i, n := range rs.Writes {
		writes[i] = apiCodeNodeFrom(n)
	}
	return apiRoutineSchema{Node: apiCodeNodeFrom(rs.Node), Reads: reads, Writes: writes}
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
		writeAPIJSON(w, apiCodeSchemaResponse{Tables: []apiTableSchema{}, Degraded: "index not available: " + err.Error()})
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

	routines := computeRoutines(ctx, eng, tables, views)
	apiRoutines := make([]apiRoutineSchema, 0, len(routines))
	for _, rs := range routines {
		apiRoutines = append(apiRoutines, apiRoutineSchemaFrom(rs))
	}

	sqlTypes := computeSQLTypes(ctx, eng)
	apiTypes := make([]apiCodeNode, 0, len(sqlTypes))
	for _, n := range sqlTypes {
		apiTypes = append(apiTypes, apiCodeNodeFrom(n))
	}

	writeAPIJSON(w, apiCodeSchemaResponse{Tables: out, Routines: apiRoutines, Types: apiTypes})
}

type apiCodeFileNode struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Kind      types.NodeKind `json:"kind"`
	StartLine int            `json:"startLine"`
}

// apiCodeFileResponse carries Degraded as a data field, not an error envelope:
// a file with no intel is a soft state the UI renders.
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
