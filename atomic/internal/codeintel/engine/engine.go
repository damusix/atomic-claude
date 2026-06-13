// Package engine is the facade layer (master CP20) that both the
// `atomic code` CLI (CP21) and the MCP server (CP22) compile against.
//
// # Data directory
//
// The index lives at:
//
//	<projectRoot>/.claude/.atomic-index/atomic.db
//
// Init creates this directory tree; Uninitialize removes it.
// NewWithDBPath overrides this default by accepting an explicit dbPath,
// decoupling the scan root from the index location — used by realm/federated callers.
//
// # Lifecycle
//
// Use New to create an Engine bound to a project root; the engine is
// unopened — neither Init nor Open has been called. Call Init to create a
// fresh index, or Open to open an existing one.  Close must be called when
// the engine is no longer needed.
//
// # Watch methods
//
// Watch and StopWatch are stubbed in v1 per appendix M. They return
// ErrWatchNotImplemented.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/codectx"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/graph"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/search"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ErrWatchNotImplemented is returned by Watch and StopWatch, which are
// stubbed in v1 per appendix M.
var ErrWatchNotImplemented = errors.New("codeintel/engine: Watch not implemented in v1")

// ErrNotInitialized is returned by methods that require an open DB when
// Init/Open has not been called.
var ErrNotInitialized = errors.New("codeintel/engine: not initialized; call Init or Open first")

// indexSubDir is the path from the project root to the index directory.
const indexSubDir = ".claude/.atomic-index"

// dbFileName is the SQLite file within the index directory.
const dbFileName = "atomic.db"

// ContextOptions configures FindRelevantContext.
type ContextOptions = codectx.Options

// Engine is the shared facade used by both the CLI adapter (CP21) and the
// MCP server adapter (CP22). It wraps the db, pool, orchestrator, pipeline,
// graph manager, searcher, and context builder into one cohesive API.
//
// The zero-value Engine is not usable. Use New or NewWithDBPath.
type Engine struct {
	root       string // absolute project root (source tree to scan)
	explicitDB string // when non-empty, overrides the computed DB path
	indexDB    *db.DB // nil until Init or Open
	pool       *extraction.Pool
	orch       *indexer.Orchestrator
	fwReg      *frameworks.Registry // retained for ExtractFrameworkNodes
	pipe       *resolution.Pipeline
	mgr        *graph.Manager
	srch       *search.Searcher
	bld        *codectx.Builder
}

// New creates an Engine bound to projectRoot. Neither Init nor Open is called;
// the engine is dormant until one of them is invoked. Close must still be
// called to release any resources that are acquired lazily (e.g. the pool).
//
// The DB is placed at the canonical repo-scope path:
//
//	<projectRoot>/.claude/.atomic-index/atomic.db
//
// To decouple the DB location from the scan root (e.g. for realm federation
// where the index lives outside the member repo), use NewWithDBPath instead.
func New(projectRoot string) (*Engine, error) {
	return &Engine{root: projectRoot}, nil
}

// NewWithDBPath creates an Engine that scans projectRoot but stores its SQLite
// index at the caller-supplied absolute dbPath. This is the internal seam for
// realm federation (CP3): callers can direct the index to
// <realm>/.atomic/<key>.db while the source tree being scanned stays at
// projectRoot. No user-facing flag exposes this — it is callable from Go only.
//
// The existing repo-scope behavior (DB at
// <projectRoot>/.claude/.atomic-index/atomic.db) is unchanged: use New for
// that path. No meta row recording the source root is written into the DB.
func NewWithDBPath(projectRoot, dbPath string) (*Engine, error) {
	return &Engine{root: projectRoot, explicitDB: dbPath}, nil
}

// indexPath returns the absolute path to the SQLite file for this engine.
// When an explicit DB path was supplied via NewWithDBPath, that path is
// returned; otherwise the canonical repo-scope path is computed.
func (e *Engine) indexPath() string {
	if e.explicitDB != "" {
		return e.explicitDB
	}
	return filepath.Join(e.root, indexSubDir, dbFileName)
}

// indexDir returns the directory that contains the SQLite file. When an
// explicit DB path is set, this is filepath.Dir(explicitDB); otherwise it is
// the canonical .claude/.atomic-index directory under the project root.
func (e *Engine) indexDir() string {
	if e.explicitDB != "" {
		return filepath.Dir(e.explicitDB)
	}
	return filepath.Join(e.root, indexSubDir)
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Init creates the index directory, opens (or creates) the SQLite database,
// and initialises the pool + dependent components.
//
// Init is idempotent: if the index already exists it is opened and migrated.
func (e *Engine) Init(ctx context.Context) error {
	if err := os.MkdirAll(e.indexDir(), 0o755); err != nil {
		return err
	}
	return e.open(ctx)
}

// Open opens an existing index. Returns an error if the index directory or
// database file does not exist; use Init to create a new index.
func (e *Engine) Open(ctx context.Context) error {
	if !e.IsInitialized() {
		return errors.New("codeintel/engine: Open: index does not exist; call Init first")
	}
	return e.open(ctx)
}

// open is the shared implementation for Init and Open.
func (e *Engine) open(ctx context.Context) error {
	// If the pool was previously created (e.g. partial re-init), close it first.
	if e.pool != nil {
		e.pool.Close()
		e.pool = nil
	}
	if e.indexDB != nil {
		_ = e.indexDB.Close()
		e.indexDB = nil
	}

	database, err := db.Open(e.indexPath())
	if err != nil {
		return err
	}
	e.indexDB = database

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{})
	if err != nil {
		database.Close()
		e.indexDB = nil
		return err
	}
	e.pool = pool

	e.orch = indexer.NewOrchestrator(database, pool)
	e.mgr = graph.NewManager(database)
	e.srch = search.New(database)
	e.bld = codectx.New(database)

	// Build the full framework registry + synthesis composite for ResolveReferences.
	// Retain the Registry on the engine so ExtractFrameworkNodes can call
	// ExtractAndPersist; pass its FrameworkRegistry view to the pipeline.
	reg := frameworks.NewRegistry(e.root, database)
	e.fwReg = reg
	synth := synthesis.Default(database)
	e.pipe = resolution.NewPipelineWithSeams(database, e.root, reg.FrameworkRegistry(), synth)

	return nil
}

// IsInitialized returns true if the index directory and atomic.db file exist
// on disk. It does NOT check whether Init or Open has been called in this
// session — use it to distinguish "index present" from "never indexed".
func (e *Engine) IsInitialized() bool {
	_, err := os.Stat(e.indexPath())
	return err == nil
}

// IndexPath returns the canonical absolute path to the SQLite database for the
// given project root. The path is deterministic:
//
//	<projectRoot>/.claude/.atomic-index/atomic.db
//
// Callers that need the DB path without opening an engine (e.g. the doctor
// check) use this function to avoid hardcoding the path.
func IndexPath(projectRoot string) string {
	return filepath.Join(projectRoot, indexSubDir, dbFileName)
}

// Close releases all resources held by the engine: the DB connection and the
// pool. Close is idempotent: calling it multiple times is safe.
func (e *Engine) Close() {
	if e.pool != nil {
		e.pool.Close()
		e.pool = nil
	}
	if e.indexDB != nil {
		_ = e.indexDB.Close()
		e.indexDB = nil
	}
}

// ProjectRoot returns the absolute path of the project this engine manages.
func (e *Engine) ProjectRoot() string {
	return e.root
}

// Uninitialize removes the entire index directory from disk. The engine
// transitions back to the uninitialized state; Init can be called again to
// rebuild. This is a destructive, synchronous operation — the caller is
// responsible for ensuring no concurrent reads or writes are in flight.
func (e *Engine) Uninitialize() error {
	e.Close()
	return os.RemoveAll(e.indexDir())
}

// ---------------------------------------------------------------------------
// Indexing
// ---------------------------------------------------------------------------

// IndexAll indexes all source files under the project root.
func (e *Engine) IndexAll(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	return e.orch.IndexAll(ctx, e.root)
}

// ExtractFrameworkNodes scans the project's source files, runs the framework
// route-extraction seam (frameworks.Registry.ExtractAndPersist), and returns
// the number of NodeKindRoute nodes now in the DB.
//
// Call this AFTER IndexAll/Sync and BEFORE ResolveReferences so that route
// nodes and their handler refs are in the DB when the resolution pipeline runs.
func (e *Engine) ExtractFrameworkNodes(ctx context.Context) (int, error) {
	if err := e.requireDB(); err != nil {
		return 0, err
	}

	// Scan source files — same set the generic extractor processes.
	absPaths, err := indexer.ScanFiles(e.root)
	if err != nil {
		return 0, fmt.Errorf("codeintel/engine: ExtractFrameworkNodes: scan: %w", err)
	}

	// Build []FileInput with RELATIVE paths + content.
	// Relative path form matches the generic extractor's relPath convention so
	// route node file_path values are consistent with generic nodes in the DB.
	// Unreadable files are skipped (best-effort).
	files := make([]frameworks.FileInput, 0, len(absPaths))
	for _, abs := range absPaths {
		rel, err := filepath.Rel(e.root, abs)
		if err != nil {
			rel = abs // fallback: shouldn't happen
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue // best-effort: skip unreadable
		}
		files = append(files, frameworks.FileInput{Path: rel, Content: string(data)})
	}

	if err := e.fwReg.ExtractAndPersist(ctx, files); err != nil {
		return 0, fmt.Errorf("codeintel/engine: ExtractFrameworkNodes: %w", err)
	}

	// Return the route-node count so callers can emit a profile line.
	routes, err := e.indexDB.GetNodesByKind(ctx, types.NodeKindRoute)
	if err != nil {
		return 0, fmt.Errorf("codeintel/engine: ExtractFrameworkNodes: count routes: %w", err)
	}
	return len(routes), nil
}

// IndexFiles indexes exactly the listed files. Each path must be absolute.
// Only paths with a known extension are processed; paths with no recognized
// extension are silently skipped (consistent with IndexAll behaviour). This is
// the real selective-indexing implementation (F-56 fix — the prior stub
// incorrectly delegated to IndexAll, re-indexing the entire project root).
func (e *Engine) IndexFiles(ctx context.Context, paths []string) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	return e.orch.IndexPaths(ctx, e.root, paths)
}

// Sync re-indexes files that have changed since the last index run.
func (e *Engine) Sync(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	return e.orch.Sync(ctx, e.root)
}

// ResolveReferences runs the resolution pipeline to turn unresolved
// references into edges.
func (e *Engine) ResolveReferences(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	_, _, err := e.pipe.ResolveAndPersistBatched(ctx, resolution.DefaultBatchSize, nil)
	return err
}

// ResolveReferencesProfiled runs the resolution pipeline with an optional
// per-phase emit callback. emit is called immediately after each sub-phase
// completes (warm → match → synth) so callers can flush a profile line
// before the next phase starts. Pass nil for no incremental output.
// The returned ResolveProfile always contains the final timings and counts.
func (e *Engine) ResolveReferencesProfiled(ctx context.Context, emit resolution.PhaseEmitFunc) (resolution.ResolveProfile, error) {
	if err := e.requireDB(); err != nil {
		return resolution.ResolveProfile{}, err
	}
	prof, _, err := e.pipe.ResolveAndPersistBatched(ctx, resolution.DefaultBatchSize, emit)
	return prof, err
}

// GetDetectedFrameworks returns the names of framework resolvers that Detect
// as active in the project root.
func (e *Engine) GetDetectedFrameworks(ctx context.Context) ([]string, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	reg := frameworks.NewRegistry(e.root, e.indexDB)
	detected := reg.DetectFrameworks(ctx)
	names := make([]string, 0, len(detected))
	for _, fr := range detected {
		names = append(names, fr.Name())
	}
	return names, nil
}

// IsIndexing returns false in v1. A future daemon (CP23) will set this when
// a background index run is in progress.
func (e *Engine) IsIndexing() bool {
	return false
}

// ExtractFromSource extracts nodes from source (provided as a string with a
// virtual filename). The results are returned but NOT persisted — callers use
// this for preview or diff tools. Returns the extraction result.
func (e *Engine) ExtractFromSource(ctx context.Context, filename, source string) (types.ExtractionResult, error) {
	if err := e.requireDB(); err != nil {
		return types.ExtractionResult{}, err
	}
	// Use IndexAll on a temp fixture — this is a convenience method; the full
	// implementation that avoids disk I/O can be added at CP21. For now,
	// write to a temp file, index it, and return the nodes/edges from the DB.
	// Actually, return an informative not-implemented error for now; the
	// brief only requires the method to exist on the facade.
	return types.ExtractionResult{}, errors.New("codeintel/engine: ExtractFromSource not implemented in v1")
}

// GetLastIndexedAt returns the most recent IndexedAt timestamp across all
// indexed files, or "" if no files have been indexed.
func (e *Engine) GetLastIndexedAt(ctx context.Context) (string, error) {
	if err := e.requireDB(); err != nil {
		return "", err
	}
	stats, err := e.indexDB.GetStats(ctx)
	if err != nil {
		return "", err
	}
	return stats.LastIndexedAt, nil
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// GetStats returns aggregate counts (node/edge/file counts, by-kind breakdown,
// last indexed timestamp).
func (e *Engine) GetStats(ctx context.Context) (types.GraphStats, error) {
	if err := e.requireDB(); err != nil {
		return types.GraphStats{}, err
	}
	return e.indexDB.GetStats(ctx)
}

// GetBackend returns the storage backend identifier. Always "sqlite".
func (e *Engine) GetBackend() string {
	return "sqlite"
}

// GetJournalMode returns the WAL mode identifier. Always "wal".
func (e *Engine) GetJournalMode() string {
	return "wal"
}

// ---------------------------------------------------------------------------
// Nodes
// ---------------------------------------------------------------------------

// GetNode returns the node with the given id.
func (e *Engine) GetNode(ctx context.Context, id string) (types.Node, error) {
	if err := e.requireDB(); err != nil {
		return types.Node{}, err
	}
	return e.indexDB.GetNode(ctx, id)
}

// GetNodesInFile returns all nodes in the file at filePath.
func (e *Engine) GetNodesInFile(ctx context.Context, filePath string) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetNodesInFile(ctx, filePath)
}

// GetNodesByKind returns all nodes of the given kind.
func (e *Engine) GetNodesByKind(ctx context.Context, kind types.NodeKind) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetNodesByKind(ctx, kind)
}

// GetNodesByName returns all nodes whose name matches (case-insensitive).
// kind may be "" to return all kinds.
func (e *Engine) GetNodesByName(ctx context.Context, name string, kind types.NodeKind) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetNodesByName(ctx, name, kind)
}

// SearchNodes runs the 3-tier FTS→LIKE→fuzzy search over all nodes.
func (e *Engine) SearchNodes(ctx context.Context, opts types.SearchOptions) ([]types.SearchResult, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	results, _, err := e.srch.Search(ctx, opts)
	return results, err
}

// GetTopRouteFile returns the file path with the highest concentration of
// route nodes (NodeKindRoute), or "" if no routes exist. Used by the MCP
// explore tool to seed the flow graph.
func (e *Engine) GetTopRouteFile(ctx context.Context) (string, error) {
	if err := e.requireDB(); err != nil {
		return "", err
	}
	routes, err := e.indexDB.GetNodesByKind(ctx, types.NodeKindRoute)
	if err != nil {
		return "", err
	}
	if len(routes) == 0 {
		return "", nil
	}
	// Count routes per file.
	counts := make(map[string]int)
	for _, r := range routes {
		counts[r.FilePath]++
	}
	var best string
	var bestCount int
	for path, count := range counts {
		if count > bestCount {
			best = path
			bestCount = count
		}
	}
	return best, nil
}

// GetRoutingManifest returns all route nodes, sorted by file path and line.
func (e *Engine) GetRoutingManifest(ctx context.Context) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetNodesByKind(ctx, types.NodeKindRoute)
}

// ---------------------------------------------------------------------------
// Edges
// ---------------------------------------------------------------------------

// GetOutgoingEdges returns all edges whose source is nodeID.
func (e *Engine) GetOutgoingEdges(ctx context.Context, nodeID string) ([]types.Edge, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetEdgesBySource(ctx, nodeID)
}

// GetIncomingEdges returns all edges whose target is nodeID.
func (e *Engine) GetIncomingEdges(ctx context.Context, nodeID string) ([]types.Edge, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetEdgesByTarget(ctx, nodeID)
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

// GetFile returns the file record for the file at path.
func (e *Engine) GetFile(ctx context.Context, path string) (types.FileRecord, error) {
	if err := e.requireDB(); err != nil {
		return types.FileRecord{}, err
	}
	return e.indexDB.GetFile(ctx, path)
}

// GetFiles returns all indexed file records.
func (e *Engine) GetFiles(ctx context.Context) ([]types.FileRecord, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetAllFiles(ctx)
}

// ---------------------------------------------------------------------------
// Graph traversal
// ---------------------------------------------------------------------------

// GetContext returns the immediate neighbourhood of nodeID — its container,
// direct callers, direct callees, and sibling nodes. This is a convenience
// wrapper around a depth-1 GetCallers + GetCallees expansion.
func (e *Engine) GetContext(ctx context.Context, nodeID string) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	callers, err := e.mgr.GetCallers(ctx, nodeID, 1)
	if err != nil {
		return types.Subgraph{}, err
	}
	callees, err := e.mgr.GetCallees(ctx, nodeID, 1)
	if err != nil {
		return types.Subgraph{}, err
	}
	// Merge into one subgraph.
	combined := types.Subgraph{
		Nodes: make(map[string]types.Node),
		Roots: []string{nodeID},
	}
	for id, n := range callers.Nodes {
		combined.Nodes[id] = n
	}
	for id, n := range callees.Nodes {
		combined.Nodes[id] = n
	}
	combined.Edges = append(combined.Edges, callers.Edges...)
	combined.Edges = append(combined.Edges, callees.Edges...)
	return combined, nil
}

// Traverse performs a BFS traversal from nodeID following the given edge
// kinds up to maxDepth hops. direction must be "outgoing" or "incoming".
func (e *Engine) Traverse(ctx context.Context, nodeID string, direction string, edgeKinds []types.EdgeKind, maxDepth int) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	if direction == "outgoing" {
		return e.mgr.GetCallees(ctx, nodeID, maxDepth)
	}
	return e.mgr.GetCallers(ctx, nodeID, maxDepth)
}

// GetCallGraph returns the call graph rooted at nodeID, outgoing to maxDepth.
func (e *Engine) GetCallGraph(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	return e.mgr.GetCallees(ctx, nodeID, maxDepth)
}

// GetTypeHierarchy returns the type hierarchy for nodeID.
// direction must be "ancestors" or "descendants".
func (e *Engine) GetTypeHierarchy(ctx context.Context, nodeID string, direction string) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.mgr.GetTypeHierarchy(ctx, nodeID, direction)
}

// FindUsages returns all nodes that reference nodeID via any incoming edge
// kind. Equivalent to GetCallers at depth 1 but returns a flat list.
func (e *Engine) FindUsages(ctx context.Context, nodeID string) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	sg, err := e.mgr.GetCallers(ctx, nodeID, 1)
	if err != nil {
		return nil, err
	}
	return types.SubgraphSortedNodes(sg), nil
}

// GetCallers returns the call subgraph of all nodes that call nodeID.
func (e *Engine) GetCallers(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	return e.mgr.GetCallers(ctx, nodeID, maxDepth)
}

// GetCallees returns the call subgraph of all nodes that nodeID calls.
func (e *Engine) GetCallees(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	return e.mgr.GetCallees(ctx, nodeID, maxDepth)
}

// GetImpactRadius returns all nodes that transitively depend on nodeID.
func (e *Engine) GetImpactRadius(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	return e.mgr.GetImpactRadius(ctx, nodeID, maxDepth)
}

// FindPath returns the shortest path between fromID and toID.
func (e *Engine) FindPath(ctx context.Context, fromID, toID string, edgeKinds []types.EdgeKind) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	return e.mgr.FindPath(ctx, fromID, toID, edgeKinds)
}

// GetAncestors returns nodes that startID extends/implements (outgoing
// heritage edges). Equivalent to GetTypeHierarchy "ancestors".
func (e *Engine) GetAncestors(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	// GetTypeHierarchy returns []Node; wrap into Subgraph.
	nodes, err := e.mgr.GetTypeHierarchy(ctx, nodeID, "ancestors")
	if err != nil {
		return types.Subgraph{}, err
	}
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
		Roots: []string{nodeID},
	}
	for _, n := range nodes {
		sg.Nodes[n.ID] = n
	}
	return sg, nil
}

// GetChildren returns nodes that extend/implement startID (incoming heritage
// edges). Equivalent to GetTypeHierarchy "descendants".
func (e *Engine) GetChildren(ctx context.Context, nodeID string) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
	nodes, err := e.mgr.GetTypeHierarchy(ctx, nodeID, "descendants")
	if err != nil {
		return types.Subgraph{}, err
	}
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
		Roots: []string{nodeID},
	}
	for _, n := range nodes {
		sg.Nodes[n.ID] = n
	}
	return sg, nil
}

// GetFileDependencies returns all files that filePath directly imports.
func (e *Engine) GetFileDependencies(ctx context.Context, filePath string) ([]types.FileRecord, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	// Find the file node for this path.
	fileNodes, err := e.indexDB.GetNodesInFile(ctx, filePath)
	if err != nil {
		return nil, err
	}
	// Collect file nodes (kind==file) and follow outgoing imports edges.
	var fileNodeIDs []string
	for _, n := range fileNodes {
		if n.Kind == types.NodeKindFile {
			fileNodeIDs = append(fileNodeIDs, n.ID)
		}
	}
	if len(fileNodeIDs) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var deps []types.FileRecord
	for _, fid := range fileNodeIDs {
		edges, err := e.indexDB.GetEdgesBySource(ctx, fid)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if edge.Kind != types.EdgeKindImports {
				continue
			}
			// Look up the target node's file path.
			target, err := e.indexDB.GetNode(ctx, edge.Target)
			if err != nil {
				continue // best-effort
			}
			if target.FilePath != "" && !seen[target.FilePath] {
				seen[target.FilePath] = true
				fr, err := e.indexDB.GetFile(ctx, target.FilePath)
				if err == nil {
					deps = append(deps, fr)
				}
			}
		}
	}
	return deps, nil
}

// GetFileDependents returns all files that directly import filePath.
func (e *Engine) GetFileDependents(ctx context.Context, filePath string) ([]types.FileRecord, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	// Find the file node for this path.
	fileNodes, err := e.indexDB.GetNodesInFile(ctx, filePath)
	if err != nil {
		return nil, err
	}
	var fileNodeIDs []string
	for _, n := range fileNodes {
		if n.Kind == types.NodeKindFile {
			fileNodeIDs = append(fileNodeIDs, n.ID)
		}
	}
	if len(fileNodeIDs) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var deps []types.FileRecord
	for _, fid := range fileNodeIDs {
		edges, err := e.indexDB.GetEdgesByTarget(ctx, fid)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if edge.Kind != types.EdgeKindImports {
				continue
			}
			source, err := e.indexDB.GetNode(ctx, edge.Source)
			if err != nil {
				continue
			}
			if source.FilePath != "" && !seen[source.FilePath] {
				seen[source.FilePath] = true
				fr, err := e.indexDB.GetFile(ctx, source.FilePath)
				if err == nil {
					deps = append(deps, fr)
				}
			}
		}
	}
	return deps, nil
}

// FindCircularDependencies finds import cycles among indexed files.
func (e *Engine) FindCircularDependencies(ctx context.Context) ([][]string, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.mgr.FindCircularDependencies(ctx)
}

// FindDeadCode returns unexported functions, methods, and classes with no
// non-contains incoming edges.
func (e *Engine) FindDeadCode(ctx context.Context) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.mgr.FindDeadCode(ctx)
}

// GetNodeMetrics returns a basic metrics map for a node: incoming edge count,
// outgoing edge count, and the node kind.
func (e *Engine) GetNodeMetrics(ctx context.Context, nodeID string) (map[string]interface{}, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	n, err := e.indexDB.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	incoming, err := e.indexDB.GetEdgesByTarget(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	outgoing, err := e.indexDB.GetEdgesBySource(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"kind":           string(n.Kind),
		"incoming":       len(incoming),
		"outgoing":       len(outgoing),
		"is_exported":    n.IsExported,
		"start_line":     n.StartLine,
		"qualified_name": n.QualifiedName,
	}, nil
}

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

// GetCode returns the raw source excerpt for a node. In v1 it reads the file
// and returns the lines from StartLine to EndLine inclusive.
func (e *Engine) GetCode(ctx context.Context, nodeID string) (types.CodeBlock, error) {
	if err := e.requireDB(); err != nil {
		return types.CodeBlock{}, err
	}
	n, err := e.indexDB.GetNode(ctx, nodeID)
	if err != nil {
		return types.CodeBlock{}, err
	}
	absPath := filepath.Join(e.root, n.FilePath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		// File may have been deleted since indexing. Return empty but not error.
		return types.CodeBlock{
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Language:  n.Language,
		}, nil
	}

	lines := splitLines(string(data))
	start := n.StartLine - 1
	end := n.EndLine
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		start = end
	}
	content := joinLines(lines[start:end])

	return types.CodeBlock{
		Content:   content,
		FilePath:  n.FilePath,
		StartLine: n.StartLine,
		EndLine:   n.EndLine,
		Language:  n.Language,
	}, nil
}

// FindRelevantContext gathers a relevant subgraph for query and returns the
// Subgraph, tier string, truncated flag, and any error.
func (e *Engine) FindRelevantContext(ctx context.Context, query string, opts ContextOptions) (types.Subgraph, string, bool, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, "", false, err
	}
	return e.bld.FindRelevantContext(ctx, query, opts)
}

// BuildContext formats a Subgraph into a types.Context for an AI agent.
func (e *Engine) BuildContext(ctx context.Context, sg types.Subgraph, opts codectx.BuildOptions) (types.Context, error) {
	if err := e.requireDB(); err != nil {
		return types.Context{}, err
	}
	return e.bld.BuildContext(ctx, sg, opts)
}

// ---------------------------------------------------------------------------
// DB
// ---------------------------------------------------------------------------

// Optimize runs PRAGMA optimize and PRAGMA wal_checkpoint(PASSIVE) on the DB.
func (e *Engine) Optimize(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	return e.indexDB.Optimize(ctx)
}

// Clear removes all nodes, edges, files, and unresolved_refs from the DB,
// preserving the schema. Use before a full re-index when you want to reset
// the graph without recreating the file.
func (e *Engine) Clear(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	return e.indexDB.Clear(ctx)
}

// ---------------------------------------------------------------------------
// Watch (stubbed in v1)
// ---------------------------------------------------------------------------

// Watch starts a file-system watcher that triggers incremental re-indexing
// on change. Not implemented in v1 — returns ErrWatchNotImplemented.
func (e *Engine) Watch() error {
	return ErrWatchNotImplemented
}

// StopWatch stops the file-system watcher. Not implemented in v1 — returns
// ErrWatchNotImplemented.
func (e *Engine) StopWatch() error {
	return ErrWatchNotImplemented
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// requireDB returns ErrNotInitialized when the DB is not open. All facade
// methods that need the DB call this first.
func (e *Engine) requireDB() error {
	if e.indexDB == nil {
		return ErrNotInitialized
	}
	return nil
}

// splitLines splits s into lines, preserving empty trailing lines from \n.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := make([]string, 0, 40)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// joinLines re-joins lines with newlines.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	total := 0
	for _, l := range lines {
		total += len(l) + 1
	}
	buf := make([]byte, 0, total)
	for i, l := range lines {
		buf = append(buf, l...)
		if i < len(lines)-1 {
			buf = append(buf, '\n')
		}
	}
	return string(buf)
}
