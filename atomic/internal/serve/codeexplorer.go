// codeexplorer.go — CP8: per-repo Code Explorer + SQL schema view.
//
// Routes (all under a repo's Code tab):
//   - GET /code/node?id=<id>           — node detail by ID
//   - GET /code/node?name=<name>       — node detail by name (GetNodesByName)
//   - GET /code/callers?id=<id>[&depth=N] — callers subgraph as edge chips
//   - GET /code/callees?id=<id>[&depth=N] — callees subgraph as edge chips
//   - GET /code/impact?id=<id>[&depth=N]  — impact radius as edge chips
//   - GET /code/files                  — indexed file list linking to /file/<path>
//   - GET /code/schema                 — SQL schema view (tables/views/columns/FKs/writers)
//
// Design seam:
//
//	CodeEngine interface covers the engine methods serve uses.
//	EngineProvider func(ctx, projectRoot, dbPath) (CodeEngine, error) opens an
//	engine per request; the production default wraps *engine.Engine.
//	Tests inject a fakeCodeEngine. The db path is resolved the same way CP7
//	does for repo scope: <realmRoot>/.claude/.atomic-index/atomic.db.
//
// CP7's MemberSearchFn is kept as-is; this file adds a parallel seam for the
// richer engine API. No migration of CP7 was needed — the two seams coexist.
package serve

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
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

// CodeExplorerOptions configures NewCodeExplorerHandler.
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

// codeExplorerHandler implements http.Handler for all /code/* explorer routes.
type codeExplorerHandler struct {
	realmRoot     string
	claudeMDPath  string
	wikiIndexPath string
	provider      EngineProvider
}

// NewCodeExplorerHandler returns an http.Handler for Code Explorer routes.
// The returned handler dispatches based on URL path suffix (node/callers/callees/
// impact/files/schema).
func NewCodeExplorerHandler(opts CodeExplorerOptions) http.Handler {
	prov := opts.EngineProvider
	if prov == nil {
		prov = DefaultEngineProvider()
	}
	return &codeExplorerHandler{
		realmRoot:     opts.RealmRoot,
		claudeMDPath:  opts.ClaudeMDPath,
		wikiIndexPath: opts.WikiIndexPath,
		provider:      prov,
	}
}

// members discovers the code members for the served scope (federation ∪ per-member
// self-indexes). Resolved per request — cheap (reads config + the wiki scan).
func (h *codeExplorerHandler) members() []codeMember {
	res, err := realm.Resolve(h.realmRoot, h.claudeMDPath)
	if err != nil {
		return nil
	}
	wikiIndexPath := h.wikiIndexPath
	if wikiIndexPath == "" && res.RealmRoot != "" {
		wikiIndexPath = filepath.Join(res.RealmRoot, "wiki", "index.md")
	}
	return discoverCodeMembers(res, h.realmRoot, wikiIndexPath)
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

// memberQuery returns the &member= query-string fragment for a prefix, or "" for
// the empty (repo-scope) prefix. Used to thread the member through drill-down
// links (callers/callees/impact/node) so each follow-up opens the right db.
func memberQuery(prefix string) string {
	if prefix == "" {
		return ""
	}
	return "&member=" + template.URLQueryEscaper(prefix)
}

func (h *codeExplorerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isHTMX := r.Header.Get("HX-Request") == "true"
	path := r.URL.Path

	// Strip the /code/ prefix to get the action name.
	// The handler is mounted at /code/node, /code/callers, etc., so path IS
	// the full path.
	switch {
	case strings.HasSuffix(path, "/code/node"):
		h.handleNode(w, r, isHTMX)
	case strings.HasSuffix(path, "/code/callers"):
		h.handleCallers(w, r, isHTMX)
	case strings.HasSuffix(path, "/code/callees"):
		h.handleCallees(w, r, isHTMX)
	case strings.HasSuffix(path, "/code/impact"):
		h.handleImpact(w, r, isHTMX)
	case strings.HasSuffix(path, "/code/files"):
		h.handleFiles(w, r, isHTMX)
	case strings.HasSuffix(path, "/code/schema"):
		h.handleSchema(w, r, isHTMX)
	case strings.HasSuffix(path, "/code/file"):
		h.handleFileIntel(w, r, isHTMX)
	default:
		http.NotFound(w, r)
	}
}

// localDBPath returns the canonical local db path for the realm root.
func (h *codeExplorerHandler) localDBPath() string {
	return filepath.Join(h.realmRoot, ".claude", ".atomic-index", "atomic.db")
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
// /code/node
// ---------------------------------------------------------------------------

func (h *codeExplorerHandler) handleNode(w http.ResponseWriter, r *http.Request, isHTMX bool) {
	ctx := r.Context()
	q := r.URL.Query()
	id := strings.TrimSpace(q.Get("id"))
	name := strings.TrimSpace(q.Get("name"))

	eng, prefix, err := h.engineForRequest(ctx, r)
	if err != nil {
		h.renderError(w, isHTMX, "index not available — run atomic code index")
		return
	}
	defer eng.Close()

	var nodes []types.Node

	switch {
	case id != "":
		n, err := eng.GetNode(ctx, id)
		if err != nil {
			h.renderError(w, isHTMX, "node not found: "+template.HTMLEscapeString(id))
			return
		}
		nodes = []types.Node{n}

	case name != "":
		nn, err := eng.GetNodesByName(ctx, name, "")
		if err != nil || len(nn) == 0 {
			h.renderError(w, isHTMX, "node not found: "+template.HTMLEscapeString(name))
			return
		}
		nodes = nn

	default:
		h.renderError(w, isHTMX, "provide id= or name= query param")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var sb strings.Builder

	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Node Detail</title></head><body>`)
	}

	for _, n := range nodes {
		renderNodeDetail(&sb, n, prefix)
	}

	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
}

// renderNodeDetail writes the HTML for one node's detail view. prefix is the
// member's realm-relative path: it prefixes the /file/ location link and rides
// along on the callers/callees/impact drill-down links so each opens the same
// member's index.
func renderNodeDetail(sb *strings.Builder, n types.Node, prefix string) {
	sb.WriteString(`<div class="code-node-detail">`)
	sb.WriteString(`<h2 class="code-node-name">`)
	sb.WriteString(template.HTMLEscapeString(n.Name))
	sb.WriteString(`</h2>`)

	sb.WriteString(`<dl class="code-node-meta">`)

	sb.WriteString(`<dt>Kind</dt><dd><span class="code-node-kind">`)
	sb.WriteString(template.HTMLEscapeString(string(n.Kind)))
	sb.WriteString(`</span></dd>`)

	if n.FilePath != "" {
		sb.WriteString(`<dt>Location</dt><dd>`)
		href := fmt.Sprintf("/file/%s#L%d", joinMemberPath(prefix, n.FilePath), n.StartLine)
		sb.WriteString(`<a href="`)
		sb.WriteString(template.HTMLEscapeString(href))
		sb.WriteString(`">`)
		sb.WriteString(template.HTMLEscapeString(n.FilePath))
		if n.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(":%d", n.StartLine))
		}
		sb.WriteString(`</a></dd>`)
	}

	if n.Signature != "" {
		sb.WriteString(`<dt>Signature</dt><dd><code class="code-node-sig">`)
		sb.WriteString(template.HTMLEscapeString(n.Signature))
		sb.WriteString(`</code></dd>`)
	}

	if n.Language != "" {
		sb.WriteString(`<dt>Language</dt><dd>`)
		sb.WriteString(template.HTMLEscapeString(string(n.Language)))
		sb.WriteString(`</dd>`)
	}

	if n.Docstring != "" {
		sb.WriteString(`<dt>Doc</dt><dd>`)
		sb.WriteString(template.HTMLEscapeString(n.Docstring))
		sb.WriteString(`</dd>`)
	}

	sb.WriteString(`</dl>`)

	// Navigation links to related views. member= rides along so each drill-down
	// opens the same member's index.
	mq := memberQuery(prefix)
	sb.WriteString(`<nav class="code-node-nav">`)
	nodeURL := "/code/callers?id=" + template.URLQueryEscaper(n.ID) + mq
	sb.WriteString(`<a href="`)
	sb.WriteString(nodeURL)
	sb.WriteString(`" hx-get="`)
	sb.WriteString(nodeURL)
	sb.WriteString(`" hx-target="#code-modal-intel">callers</a> `)

	nodeURL = "/code/callees?id=" + template.URLQueryEscaper(n.ID) + mq
	sb.WriteString(`<a href="`)
	sb.WriteString(nodeURL)
	sb.WriteString(`" hx-get="`)
	sb.WriteString(nodeURL)
	sb.WriteString(`" hx-target="#code-modal-intel">callees</a> `)

	nodeURL = "/code/impact?id=" + template.URLQueryEscaper(n.ID) + mq
	sb.WriteString(`<a href="`)
	sb.WriteString(nodeURL)
	sb.WriteString(`" hx-get="`)
	sb.WriteString(nodeURL)
	sb.WriteString(`" hx-target="#code-modal-intel">impact</a>`)
	sb.WriteString(`</nav>`)

	sb.WriteString(`</div>`)
}

// ---------------------------------------------------------------------------
// /code/callers, /code/callees, /code/impact
// ---------------------------------------------------------------------------

type subgraphMode int

const (
	modeCallers subgraphMode = iota
	modeCallees
	modeImpact
)

func (h *codeExplorerHandler) handleCallers(w http.ResponseWriter, r *http.Request, isHTMX bool) {
	h.handleSubgraph(w, r, isHTMX, modeCallers)
}

func (h *codeExplorerHandler) handleCallees(w http.ResponseWriter, r *http.Request, isHTMX bool) {
	h.handleSubgraph(w, r, isHTMX, modeCallees)
}

func (h *codeExplorerHandler) handleImpact(w http.ResponseWriter, r *http.Request, isHTMX bool) {
	h.handleSubgraph(w, r, isHTMX, modeImpact)
}

func (h *codeExplorerHandler) handleSubgraph(w http.ResponseWriter, r *http.Request, isHTMX bool, mode subgraphMode) {
	ctx := r.Context()
	q := r.URL.Query()
	id := strings.TrimSpace(q.Get("id"))
	if id == "" {
		h.renderError(w, isHTMX, "provide id= query param")
		return
	}

	depth := 2 // default
	if ds := strings.TrimSpace(q.Get("depth")); ds != "" {
		if n, err := strconv.Atoi(ds); err == nil && n > 0 {
			depth = n
		}
	}

	eng, prefix, err := h.engineForRequest(ctx, r)
	if err != nil {
		h.renderError(w, isHTMX, "index not available — run atomic code index")
		return
	}
	defer eng.Close()

	var sg types.Subgraph
	var label string

	switch mode {
	case modeCallers:
		sg, err = eng.GetCallers(ctx, id, depth)
		label = "Callers"
	case modeCallees:
		sg, err = eng.GetCallees(ctx, id, depth)
		label = "Callees"
	case modeImpact:
		sg, err = eng.GetImpactRadius(ctx, id, depth)
		label = "Impact radius"
	}
	if err != nil {
		h.renderError(w, isHTMX, "graph query failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var sb strings.Builder

	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>`)
		sb.WriteString(template.HTMLEscapeString(label))
		sb.WriteString(`</title></head><body>`)
	}

	renderSubgraph(&sb, label, id, sg, prefix)

	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
}

// renderSubgraph writes the HTML for a Subgraph as clickable edge chips.
// Each edge shows its kind and links to the target node's detail page; prefix is
// the member's realm-relative path, threaded onto each /code/node link so the
// drill-down stays within the same member's index.
func renderSubgraph(sb *strings.Builder, label, rootID string, sg types.Subgraph, prefix string) {
	sb.WriteString(`<div class="code-subgraph">`)
	sb.WriteString(`<h2 class="code-subgraph-title">`)
	sb.WriteString(template.HTMLEscapeString(label))
	sb.WriteString(`</h2>`)

	if len(sg.Edges) == 0 {
		sb.WriteString(`<p class="code-subgraph-empty">No results.</p>`)
		sb.WriteString(`</div>`)
		return
	}

	// Group edges by kind for clarity.
	type edgeGroup struct {
		kind  types.EdgeKind
		edges []types.Edge
	}
	byKind := make(map[types.EdgeKind][]types.Edge)
	for _, e := range sg.Edges {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	// Sort kinds for deterministic output.
	kinds := make([]types.EdgeKind, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })

	for _, kind := range kinds {
		edges := byKind[kind]
		sb.WriteString(`<div class="code-edge-group">`)
		sb.WriteString(`<span class="code-edge-kind-label">`)
		sb.WriteString(template.HTMLEscapeString(string(kind)))
		sb.WriteString(`</span>`)
		sb.WriteString(`<ul class="code-edge-list">`)

		for _, e := range edges {
			// The "other" node (not the root).
			otherID := e.Target
			if otherID == rootID {
				otherID = e.Source
			}
			other, ok := sg.Nodes[otherID]
			if !ok {
				other = types.Node{ID: otherID, Name: otherID}
			}

			sb.WriteString(`<li class="code-edge-chip">`)
			href := "/code/node?id=" + template.URLQueryEscaper(otherID) + memberQuery(prefix)
			sb.WriteString(`<a href="`)
			sb.WriteString(href)
			sb.WriteString(`" hx-get="`)
			sb.WriteString(href)
			sb.WriteString(`" hx-target="#code-modal-intel" class="code-edge-chip-link">`)

			// Chip: [kind] name (file:line)
			sb.WriteString(`<span class="code-edge-chip-kind">[`)
			sb.WriteString(template.HTMLEscapeString(string(kind)))
			sb.WriteString(`]</span> `)
			sb.WriteString(`<span class="code-edge-chip-name">`)
			sb.WriteString(template.HTMLEscapeString(other.Name))
			sb.WriteString(`</span>`)
			if other.FilePath != "" {
				sb.WriteString(`<span class="code-edge-chip-loc"> — `)
				sb.WriteString(template.HTMLEscapeString(other.FilePath))
				if other.StartLine > 0 {
					sb.WriteString(fmt.Sprintf(":%d", other.StartLine))
				}
				sb.WriteString(`</span>`)
			}
			sb.WriteString(`</a></li>`)
		}

		sb.WriteString(`</ul></div>`)
	}

	sb.WriteString(`</div>`)
}

// ---------------------------------------------------------------------------
// /code/files
// ---------------------------------------------------------------------------

func (h *codeExplorerHandler) handleFiles(w http.ResponseWriter, r *http.Request, isHTMX bool) {
	ctx := r.Context()

	eng, prefix, err := h.engineForRequest(ctx, r)
	if err != nil {
		h.renderError(w, isHTMX, "index not available — run atomic code index")
		return
	}
	defer eng.Close()

	files, err := eng.GetFiles(ctx)
	if err != nil {
		h.renderError(w, isHTMX, "file list query failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var sb strings.Builder

	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Indexed Files</title></head><body>`)
	}

	sb.WriteString(`<div class="code-files">`)
	sb.WriteString(`<h2 class="code-files-title">Indexed files (`)
	sb.WriteString(strconv.Itoa(len(files)))
	sb.WriteString(`)</h2>`)

	if len(files) == 0 {
		sb.WriteString(`<p class="code-files-empty">No indexed files found.</p>`)
	} else {
		sb.WriteString(`<ul class="code-files-list">`)
		for _, f := range files {
			sb.WriteString(`<li class="code-file-item">`)
			href := "/file/" + joinMemberPath(prefix, f.Path)
			sb.WriteString(`<a href="`)
			sb.WriteString(template.HTMLEscapeString(href))
			sb.WriteString(`">`)
			sb.WriteString(template.HTMLEscapeString(f.Path))
			sb.WriteString(`</a>`)
			if f.Language != "" {
				sb.WriteString(` <span class="code-file-lang">`)
				sb.WriteString(template.HTMLEscapeString(string(f.Language)))
				sb.WriteString(`</span>`)
			}
			if f.NodeCount > 0 {
				sb.WriteString(fmt.Sprintf(` <span class="code-file-nodes">%d nodes</span>`, f.NodeCount))
			}
			sb.WriteString(`</li>`)
		}
		sb.WriteString(`</ul>`)
	}

	sb.WriteString(`</div>`)

	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
}

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

func (h *codeExplorerHandler) handleSchema(w http.ResponseWriter, r *http.Request, isHTMX bool) {
	ctx := r.Context()

	eng, prefix, err := h.engineForRequest(ctx, r)
	if err != nil {
		h.renderError(w, isHTMX, "index not available — run atomic code index")
		return
	}
	defer eng.Close()

	// Collect table and view nodes.
	tables, err := eng.GetNodesByKind(ctx, types.NodeKindTable)
	if err != nil {
		h.renderError(w, isHTMX, "schema query failed: "+err.Error())
		return
	}
	views, err := eng.GetNodesByKind(ctx, types.NodeKindView)
	if err != nil {
		h.renderError(w, isHTMX, "schema query failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var sb strings.Builder

	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>SQL Schema</title></head><body>`)
	}

	sb.WriteString(`<div class="code-schema">`)
	sb.WriteString(`<h2 class="code-schema-title">SQL Schema</h2>`)

	if len(tables) == 0 && len(views) == 0 {
		sb.WriteString(`<p class="code-schema-empty">No SQL schema found in this index.</p>`)
		sb.WriteString(`</div>`)
		if !isHTMX {
			sb.WriteString(`</body></html>`)
		}
		fmt.Fprint(w, sb.String())
		return
	}

	// Build a nodeID → Node lookup from all table+view nodes so we can resolve
	// source nodes from edge endpoints.
	nodeByID := make(map[string]types.Node)
	for _, t := range tables {
		nodeByID[t.ID] = t
	}
	for _, v := range views {
		nodeByID[v.ID] = v
	}

	// For each table and view, resolve: columns (contains), FK sources (references),
	// writers (writes). We do this by scanning all outgoing edges of ALL nodes in
	// the graph — specifically, we use GetNodesByKind to get all table/view nodes
	// and then walk their outgoing edges for contains/references/writes.
	//
	// Approach: for each table node, call GetOutgoingEdges to find:
	//   - contains edges → column children
	// Then collect inverse references by scanning edges from all OTHER nodes to
	// this table. Since the engine exposes GetOutgoingEdges(nodeID) but not
	// GetIncomingEdges directly via this interface, we use a two-pass approach:
	// first collect all outgoing edges from table nodes (for contains),
	// then for references and writes we scan from additional node sets.
	//
	// For simplicity and correctness: use the outgoing edges of each table node for
	// contains (column children). For FK references and writers, we collect all
	// table/view outgoing edges and invert the relationships:
	//   - edge {src: ordersTbl, tgt: usersTbl, kind: references} → ordersTbl is an FK source of usersTbl
	//   - edge {src: insertProc, tgt: usersTbl, kind: writes} → insertProc is a writer of usersTbl

	// Map tableID → accumulated references/writers by scanning all table edges.
	fkSourcesByTable := make(map[string][]types.Node) // tableID → nodes referencing this table
	writersByTable := make(map[string][]types.Node)   // tableID → nodes writing this table

	// We scan outgoing edges from table/view nodes AND function/procedure/method nodes
	// to collect cross-table relationships (FK references, writers).
	// tables→references: another table references this one (FK).
	// functions/procedures→writes: a routine writes to this table.
	extraKinds := []types.NodeKind{
		types.NodeKindTable,
		types.NodeKindView,
		types.NodeKindFunction,
		types.NodeKindMethod,
		types.NodeKindProcedure,
	}
	extraNodes := make([]types.Node, 0, len(tables)+len(views))
	extraNodes = append(extraNodes, tables...)
	extraNodes = append(extraNodes, views...)
	for _, k := range extraKinds[2:] { // function, method, procedure
		kn, err := eng.GetNodesByKind(ctx, k)
		if err == nil {
			extraNodes = append(extraNodes, kn...)
		}
	}
	for _, srcNode := range extraNodes {
		edges, err := eng.GetOutgoingEdges(ctx, srcNode.ID)
		if err != nil {
			continue // best-effort
		}
		for _, e := range edges {
			switch e.Kind {
			case types.EdgeKindReferences:
				// srcNode references e.Target → srcNode is an FK source for e.Target.
				if _, ok := nodeByID[e.Target]; ok {
					fkSourcesByTable[e.Target] = appendIfNew(fkSourcesByTable[e.Target], srcNode)
				}
			case types.EdgeKindWrites:
				// srcNode writes e.Target → srcNode is a writer of e.Target.
				writersByTable[e.Target] = appendIfNew(writersByTable[e.Target], srcNode)
			}
		}
	}

	// Build per-table schema structs.
	renderSchemaTable := func(nodes []types.Node, kind string) {
		if len(nodes) == 0 {
			return
		}
		sb.WriteString(`<section class="code-schema-section">`)
		sb.WriteString(`<h3 class="code-schema-section-title">`)
		sb.WriteString(template.HTMLEscapeString(kind))
		sb.WriteString(`s</h3>`)

		for _, tableNode := range nodes {
			ts := tableSchema{Node: tableNode}

			// Columns: outgoing contains edges.
			edges, _ := eng.GetOutgoingEdges(ctx, tableNode.ID)
			for _, e := range edges {
				if e.Kind == types.EdgeKindContains {
					colNode, err := eng.GetNode(ctx, e.Target)
					if err == nil {
						ts.Columns = append(ts.Columns, colNode)
					}
				}
			}
			ts.FKSources = fkSourcesByTable[tableNode.ID]
			ts.Writers = writersByTable[tableNode.ID]

			renderTableSchema(&sb, ts, prefix)
		}

		sb.WriteString(`</section>`)
	}

	renderSchemaTable(tables, "Table")
	renderSchemaTable(views, "View")

	sb.WriteString(`</div>`)

	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
}

// renderTableSchema writes HTML for one table's schema. prefix is the member's
// realm-relative path, threaded onto each /code/node link so the drill-down
// opens the same member's index.
func renderTableSchema(sb *strings.Builder, ts tableSchema, prefix string) {
	mq := memberQuery(prefix)
	sb.WriteString(`<div class="code-schema-table">`)

	// Table name + link to its node detail.
	href := "/code/node?id=" + template.URLQueryEscaper(ts.Node.ID) + mq
	sb.WriteString(`<h4 class="code-schema-table-name"><a href="`)
	sb.WriteString(href)
	sb.WriteString(`">`)
	sb.WriteString(template.HTMLEscapeString(ts.Node.Name))
	sb.WriteString(`</a>`)
	if ts.Node.FilePath != "" {
		sb.WriteString(` <span class="code-schema-table-loc">`)
		sb.WriteString(template.HTMLEscapeString(ts.Node.FilePath))
		if ts.Node.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(":%d", ts.Node.StartLine))
		}
		sb.WriteString(`</span>`)
	}
	sb.WriteString(`</h4>`)

	// Columns.
	if len(ts.Columns) > 0 {
		sb.WriteString(`<ul class="code-schema-columns">`)
		for _, col := range ts.Columns {
			sb.WriteString(`<li class="code-schema-column">`)
			sb.WriteString(template.HTMLEscapeString(col.Name))
			if col.Signature != "" {
				sb.WriteString(` <code>`)
				sb.WriteString(template.HTMLEscapeString(col.Signature))
				sb.WriteString(`</code>`)
			}
			sb.WriteString(`</li>`)
		}
		sb.WriteString(`</ul>`)
	}

	// FK sources (nodes that reference this table).
	if len(ts.FKSources) > 0 {
		sb.WriteString(`<div class="code-schema-fk-sources">`)
		sb.WriteString(`<span class="code-schema-fk-label">Referenced by:</span> `)
		for i, src := range ts.FKSources {
			if i > 0 {
				sb.WriteString(`, `)
			}
			href := "/code/node?id=" + template.URLQueryEscaper(src.ID) + mq
			sb.WriteString(`<a href="`)
			sb.WriteString(href)
			sb.WriteString(`">`)
			sb.WriteString(template.HTMLEscapeString(src.Name))
			sb.WriteString(`</a>`)
		}
		sb.WriteString(`</div>`)
	}

	// Writers (nodes that write to this table).
	if len(ts.Writers) > 0 {
		sb.WriteString(`<div class="code-schema-writers">`)
		sb.WriteString(`<span class="code-schema-writers-label">Writers:</span> `)
		for i, w := range ts.Writers {
			if i > 0 {
				sb.WriteString(`, `)
			}
			href := "/code/node?id=" + template.URLQueryEscaper(w.ID) + mq
			sb.WriteString(`<a href="`)
			sb.WriteString(href)
			sb.WriteString(`">`)
			sb.WriteString(template.HTMLEscapeString(w.Name))
			sb.WriteString(`</a>`)
		}
		sb.WriteString(`</div>`)
	}

	sb.WriteString(`</div>`)
}

// ---------------------------------------------------------------------------
// /code/file  — file-level code-intel: symbols defined in a source file.
// ---------------------------------------------------------------------------

// handleFileIntel handles GET /code/file?path=<relpath>.
// It lists all symbols defined in the given file using GetNodesInFile and
// renders them as chips linking to their callers/callees/impact pages.
// Degrade cases (engine unavailable, file not indexed, zero symbols) render a
// brief "no code intelligence for this file (not indexed)" note; the modal
// source pane still shows the highlighted source regardless.
func (h *codeExplorerHandler) handleFileIntel(w http.ResponseWriter, r *http.Request, isHTMX bool) {
	ctx := r.Context()
	filePath := strings.TrimSpace(r.URL.Query().Get("path"))
	if filePath == "" {
		h.renderCodeFileDegrade(w, isHTMX, "no file path provided")
		return
	}

	// Map the realm-relative path to its owning member, then query that member's
	// index with the member-relative remainder (the db stores member-relative
	// paths). memberRel == filePath in single-repo scope (empty prefix).
	m, memberRel, ok := memberForPath(h.members(), filePath)
	if !ok {
		h.renderCodeFileDegrade(w, isHTMX, "no code intelligence for this file (not indexed)")
		return
	}

	eng, err := h.openEngineFor(ctx, m)
	if err != nil {
		h.renderCodeFileDegrade(w, isHTMX, "index not available — run atomic code index")
		return
	}
	defer eng.Close()

	nodes, err := eng.GetNodesInFile(ctx, memberRel)
	if err != nil || len(nodes) == 0 {
		h.renderCodeFileDegrade(w, isHTMX, "no code intelligence for this file (not indexed)")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var sb strings.Builder

	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>File Intel</title></head><body>`)
	}

	sb.WriteString(`<div class="code-file-intel">`)
	sb.WriteString(`<h3 class="code-file-intel-title">Defines (`)
	sb.WriteString(strconv.Itoa(len(nodes)))
	sb.WriteString(`)</h3>`)
	sb.WriteString(`<ul class="code-file-intel-list">`)

	for _, n := range nodes {
		sb.WriteString(`<li class="code-file-intel-chip">`)
		// Symbol name + kind badge.
		sb.WriteString(`<span class="code-file-intel-name">`)
		sb.WriteString(template.HTMLEscapeString(n.Name))
		sb.WriteString(`</span>`)
		sb.WriteString(` <span class="code-file-intel-kind code-node-kind">`)
		sb.WriteString(template.HTMLEscapeString(string(n.Kind)))
		sb.WriteString(`</span>`)
		if n.StartLine > 0 {
			sb.WriteString(fmt.Sprintf(` <span class="code-file-intel-line">:%d</span>`, n.StartLine))
		}
		// Relationship links: callers | callees | impact — each targets the modal
		// intel pane and carries member= so the drill-down opens this member's index.
		mq := memberQuery(m.Prefix)
		sb.WriteString(` <span class="code-file-intel-links">`)
		for _, rel := range []struct {
			label string
			route string
		}{
			{"callers", "/code/callers?id=" + template.URLQueryEscaper(n.ID) + mq},
			{"callees", "/code/callees?id=" + template.URLQueryEscaper(n.ID) + mq},
			{"impact", "/code/impact?id=" + template.URLQueryEscaper(n.ID) + mq},
		} {
			sb.WriteString(`<a class="code-file-intel-link" href="`)
			sb.WriteString(rel.route)
			sb.WriteString(`" hx-get="`)
			sb.WriteString(rel.route)
			sb.WriteString(`" hx-target="#code-modal-intel">`)
			sb.WriteString(rel.label)
			sb.WriteString(`</a> `)
		}
		sb.WriteString(`</span>`)
		sb.WriteString(`</li>`)
	}

	sb.WriteString(`</ul></div>`)

	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
}

// renderCodeFileDegrade renders the degrade note for /code/file when the engine
// is unavailable, the file is not indexed, or no symbols are defined.
// The note is styled to match the existing code-explorer-error style.
func (h *codeExplorerHandler) renderCodeFileDegrade(w http.ResponseWriter, isHTMX bool, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var sb strings.Builder
	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>File Intel</title></head><body>`)
	}
	sb.WriteString(`<p class="code-file-intel-empty">`)
	sb.WriteString(template.HTMLEscapeString(msg))
	sb.WriteString(`</p>`)
	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
}

// ---------------------------------------------------------------------------
// Error rendering
// ---------------------------------------------------------------------------

func (h *codeExplorerHandler) renderError(w http.ResponseWriter, isHTMX bool, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var sb strings.Builder
	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Error</title></head><body>`)
	}
	sb.WriteString(`<p class="code-explorer-error">`)
	sb.WriteString(template.HTMLEscapeString(msg))
	sb.WriteString(`</p>`)
	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
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
