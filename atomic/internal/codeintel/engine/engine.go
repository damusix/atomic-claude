// Package engine is the facade both the `atomic code` CLI and the MCP server
// compile against. New binds an engine to a project root but leaves it dormant;
// Init or Open must be called before any DB-backed method, and Close after.
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
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// ErrWatchNotImplemented is returned by the stubbed Watch and StopWatch.
var ErrWatchNotImplemented = errors.New("codeintel/engine: Watch not implemented in v1")

// ErrNotInitialized is returned by DB-backed methods before Init or Open.
var ErrNotInitialized = errors.New("codeintel/engine: not initialized; call Init or Open first")

// ContextOptions configures FindRelevantContext.
type ContextOptions = codectx.Options

// Engine wraps the db, pool, orchestrator, pipeline, graph manager, searcher,
// and context builder into one API. The zero value is not usable — use New or
// NewWithDBPath.
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
	// Diagnostics from the last ensureIndexer boot's ignore-config load, so the
	// CLI can report a degraded .claude/atomic.toml without failing the index.
	ignoreWarnings []string
}

// New creates an Engine bound to projectRoot, with the DB at the canonical
// repo-scope path. Close must still be called — the pool is acquired lazily.
func New(projectRoot string) (*Engine, error) {
	return &Engine{root: projectRoot}, nil
}

// NewWithDBPath scans projectRoot but stores the index at dbPath, decoupling
// the two. Go-only seam for realm federation, where the index lives outside the
// member repo; nothing in the DB records the source root.
func NewWithDBPath(projectRoot, dbPath string) (*Engine, error) {
	return &Engine{root: projectRoot, explicitDB: dbPath}, nil
}

func (e *Engine) indexPath() string {
	if e.explicitDB != "" {
		return e.explicitDB
	}
	return config.IndexDBPath(e.root)
}

func (e *Engine) indexDir() string {
	if e.explicitDB != "" {
		return filepath.Dir(e.explicitDB)
	}
	return config.IndexDir(e.root)
}

// Init creates the index directory and opens the database. Idempotent: an
// existing index is opened and migrated.
func (e *Engine) Init(ctx context.Context) error {
	if err := os.MkdirAll(e.indexDir(), 0o755); err != nil {
		return err
	}
	return e.open(ctx)
}

// Open opens an existing index, erroring when none is on disk.
func (e *Engine) Open(ctx context.Context) error {
	if !e.IsInitialized() {
		return errors.New("codeintel/engine: Open: index does not exist; call Init first")
	}
	return e.open(ctx)
}

func (e *Engine) open(ctx context.Context) error {
	if e.pool != nil {
		e.pool.Close()
		e.pool = nil
	}
	// orch holds the now-closed pool + db; drop it so ensureIndexer rebuilds
	// against the fresh DB.
	e.orch = nil
	if e.indexDB != nil {
		_ = e.indexDB.Close()
		e.indexDB = nil
	}

	database, err := db.Open(e.indexPath())
	if err != nil {
		return err
	}
	e.indexDB = database

	// The pool + orchestrator are deliberately not built here — see ensureIndexer.
	e.mgr = graph.NewManager(database)
	e.srch = search.New(database)
	e.bld = codectx.New(database)

	// The Registry is retained so ExtractFrameworkNodes can call ExtractAndPersist;
	// the pipeline only gets its narrower FrameworkRegistry view.
	reg := frameworks.NewRegistry(e.root, database)
	e.fwReg = reg
	synth := synthesis.Default(database)
	e.pipe = resolution.NewPipelineWithSeams(database, e.root, reg.FrameworkRegistry(), synth)

	return nil
}

// ensureIndexer lazily boots the extraction pool + orchestrator, called only by
// methods that parse source. The pool spins up one wazero runtime per CPU and
// compiles every tree-sitter grammar (~4.7 s CPU, ~1.9 GB peak RSS), so read-only
// queries must never trigger it. Idempotent while the pool is live.
func (e *Engine) ensureIndexer(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	if e.orch != nil {
		return nil
	}
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{})
	if err != nil {
		return err
	}
	e.pool = pool
	orch := indexer.NewOrchestrator(e.indexDB, pool)

	// Ignore config is read once per indexer boot, not per index call. A missing
	// file yields a nil matcher; a malformed one degrades to unfiltered indexing
	// rather than failing the run.
	cfg, warns, err := config.LoadRepoConfig(config.RepoConfigPath(e.root))
	var msgs []string
	if err != nil {
		msgs = append(msgs, fmt.Sprintf("%s: indexing proceeds unfiltered", err))
	} else {
		for _, w := range warns {
			msgs = append(msgs, w.Message)
		}
		if len(cfg.Code.Ignore) > 0 {
			matcher, mwarns := config.NewIgnoreMatcher(cfg.Code.Ignore)
			orch.SetIgnoreMatcher(matcher)
			for _, w := range mwarns {
				msgs = append(msgs, w.Message)
			}
		}
	}
	e.ignoreWarnings = msgs

	e.orch = orch
	return nil
}

// IgnoreWarnings returns diagnostics from the last indexer boot's ignore-config
// load. Empty when .claude/atomic.toml is absent or well-formed.
func (e *Engine) IgnoreWarnings() []string {
	return e.ignoreWarnings
}

// IgnorePatternInfo reports how many ignore patterns compiled, and from where.
// Reads the config directly rather than via the indexer, so a read-only status
// query does not pay ensureIndexer's pool-boot cost. count is 0 on any failure.
func (e *Engine) IgnorePatternInfo() (count int, path string) {
	path = config.RepoConfigPath(e.root)
	cfg, _, err := config.LoadRepoConfig(path)
	if err != nil || len(cfg.Code.Ignore) == 0 {
		return 0, path
	}
	matcher, _ := config.NewIgnoreMatcher(cfg.Code.Ignore)
	return matcher.PatternCount(), path
}

// IndexPath returns the database path this engine is actually bound to. Callers
// reporting the live index location must use this, not the package-level
// IndexPath: in realm scope the db lives outside the member repo.
func (e *Engine) IndexPath() string {
	return e.indexPath()
}

// IsInitialized reports whether the db file exists on disk. It says nothing
// about whether Init or Open has run in this session.
func (e *Engine) IsInitialized() bool {
	_, err := os.Stat(e.indexPath())
	return err == nil
}

// IndexPath returns the canonical repo-scope database path, for callers that
// need it without opening an engine. Wrong for realm scope — see the method.
func IndexPath(projectRoot string) string {
	return config.IndexDBPath(projectRoot)
}

// Close releases the DB connection and the pool. Idempotent.
func (e *Engine) Close() {
	if e.pool != nil {
		e.pool.Close()
		e.pool = nil
	}
	// orch references the pool just closed; drop it so a later ensureIndexer
	// rebuilds rather than reusing a dead pool.
	e.orch = nil
	if e.indexDB != nil {
		_ = e.indexDB.Close()
		e.indexDB = nil
	}
}

// ProjectRoot returns the absolute path of the project this engine manages.
func (e *Engine) ProjectRoot() string {
	return e.root
}

// Uninitialize deletes the index directory, returning the engine to its
// uninitialized state. The caller must ensure no reads or writes are in flight.
func (e *Engine) Uninitialize() error {
	e.Close()
	return os.RemoveAll(e.indexDir())
}

// IndexAll indexes all source files under the project root.
func (e *Engine) IndexAll(ctx context.Context) error {
	if err := e.ensureIndexer(ctx); err != nil {
		return err
	}
	return e.orch.IndexAll(ctx, e.root)
}

// SkippedFiles returns how many files the last index run could not read or stat,
// so the CLI can surface them rather than dropping them silently.
func (e *Engine) SkippedFiles() int {
	if e.orch == nil {
		return 0
	}
	return e.orch.SkippedFiles()
}

// ExtractFrameworkNodes runs the framework route-extraction seam and returns the
// resulting route-node count. Must run after IndexAll/Sync and before
// ResolveReferences, so route nodes and handler refs are present for resolution.
func (e *Engine) ExtractFrameworkNodes(ctx context.Context) (int, error) {
	if err := e.ensureIndexer(ctx); err != nil {
		return 0, err
	}

	// Scanning through e.orch applies the same ignore matcher the generic
	// extractor uses, so no route node appears for a file the index hides.
	absPaths, err := e.orch.ScanFiles(e.root)
	if err != nil {
		return 0, fmt.Errorf("codeintel/engine: ExtractFrameworkNodes: scan: %w", err)
	}

	// Paths must be relative to match the generic extractor's convention, or route
	// nodes get file_path values inconsistent with every other node in the DB.
	files := make([]frameworks.FileInput, 0, len(absPaths))
	for _, abs := range absPaths {
		rel, err := filepath.Rel(e.root, abs)
		if err != nil {
			rel = abs
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		files = append(files, frameworks.FileInput{Path: rel, Content: string(data)})
	}

	if err := e.fwReg.ExtractAndPersist(ctx, files); err != nil {
		return 0, fmt.Errorf("codeintel/engine: ExtractFrameworkNodes: %w", err)
	}

	routes, err := e.indexDB.GetNodesByKind(ctx, types.NodeKindRoute)
	if err != nil {
		return 0, fmt.Errorf("codeintel/engine: ExtractFrameworkNodes: count routes: %w", err)
	}
	return len(routes), nil
}

// IndexFiles indexes exactly the listed absolute paths. Paths with no recognized
// extension are skipped, matching IndexAll.
func (e *Engine) IndexFiles(ctx context.Context, paths []string) error {
	if err := e.ensureIndexer(ctx); err != nil {
		return err
	}
	return e.orch.IndexPaths(ctx, e.root, paths)
}

// Sync re-indexes files that have changed since the last index run.
func (e *Engine) Sync(ctx context.Context) error {
	if err := e.ensureIndexer(ctx); err != nil {
		return err
	}
	return e.orch.Sync(ctx, e.root)
}

// ResolveReferences turns unresolved references into edges.
func (e *Engine) ResolveReferences(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	_, _, err := e.pipe.ResolveAndPersistBatched(ctx, resolution.DefaultBatchSize, nil)
	return err
}

// ResolveReferencesProfiled resolves with an optional callback fired after each
// sub-phase (warm → match → synth), so callers can flush a profile line before
// the next starts. emit may be nil; the returned profile is always complete.
func (e *Engine) ResolveReferencesProfiled(ctx context.Context, emit resolution.PhaseEmitFunc) (resolution.ResolveProfile, error) {
	if err := e.requireDB(); err != nil {
		return resolution.ResolveProfile{}, err
	}
	prof, _, err := e.pipe.ResolveAndPersistBatched(ctx, resolution.DefaultBatchSize, emit)
	return prof, err
}

// GetDetectedFrameworks names the framework resolvers active in the project root.
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

// IsIndexing always returns false; no background indexer exists yet.
func (e *Engine) IsIndexing() bool {
	return false
}

// ExtractFromSource would extract nodes from an in-memory source string without
// persisting them, for preview and diff tools. Not implemented.
func (e *Engine) ExtractFromSource(ctx context.Context, filename, source string) (types.ExtractionResult, error) {
	if err := e.requireDB(); err != nil {
		return types.ExtractionResult{}, err
	}
	return types.ExtractionResult{}, errors.New("codeintel/engine: ExtractFromSource not implemented in v1")
}

// GetLastIndexedAt returns the newest IndexedAt across all files, or "" if none.
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

// GetStats returns node/edge/file counts, the by-kind breakdown, and the last
// indexed timestamp.
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

// GetTopRouteFile returns the file holding the most route nodes, or "" if there
// are none. The MCP explore tool uses it to seed the flow graph.
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

// GetAllNodes returns every node. A full table scan, meant for one bulk read per
// request (full-graph export), never for per-node querying.
func (e *Engine) GetAllNodes(ctx context.Context) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetAllNodes(ctx)
}

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

// GetAllEdges returns every edge; the GetAllNodes caveat applies.
func (e *Engine) GetAllEdges(ctx context.Context) ([]types.Edge, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.indexDB.GetAllEdges(ctx)
}

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

// GetContext returns the immediate neighbourhood of nodeID: a depth-1
// GetCallers + GetCallees expansion, merged.
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

// Traverse BFS-walks from nodeID up to maxDepth hops. direction must be
// "outgoing" or "incoming".
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

// FindUsages is GetCallers at depth 1, flattened to a sorted list.
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

// GetAncestors returns what nodeID extends or implements, as a Subgraph.
func (e *Engine) GetAncestors(ctx context.Context, nodeID string, maxDepth int) (types.Subgraph, error) {
	if err := e.requireDB(); err != nil {
		return types.Subgraph{}, err
	}
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

// GetChildren returns what extends or implements nodeID, as a Subgraph.
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
		edges, err := e.indexDB.GetEdgesBySource(ctx, fid)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if edge.Kind != types.EdgeKindImports {
				continue
			}
			target, err := e.indexDB.GetNode(ctx, edge.Target)
			if err != nil {
				continue
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
// incoming edge other than contains.
func (e *Engine) FindDeadCode(ctx context.Context) ([]types.Node, error) {
	if err := e.requireDB(); err != nil {
		return nil, err
	}
	return e.mgr.FindDeadCode(ctx)
}

// GetNodeMetrics returns edge counts and identity fields for one node.
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

// GetCode returns the node's source lines, StartLine to EndLine inclusive.
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
		// The file may have been deleted since indexing; that is not an error.
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

// FindRelevantContext gathers a subgraph for query, returning it with the search
// tier that produced it and whether diversity capping truncated the result.
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

// Optimize runs PRAGMA optimize and PRAGMA wal_checkpoint(PASSIVE) on the DB.
func (e *Engine) Optimize(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	return e.indexDB.Optimize(ctx)
}

// Clear empties every table but keeps the schema, resetting the graph without
// recreating the file.
func (e *Engine) Clear(ctx context.Context) error {
	if err := e.requireDB(); err != nil {
		return err
	}
	return e.indexDB.Clear(ctx)
}

// Watch would start a file-system watcher driving incremental re-indexing.
// Not implemented.
func (e *Engine) Watch() error {
	return ErrWatchNotImplemented
}

// StopWatch would stop the file-system watcher. Not implemented.
func (e *Engine) StopWatch() error {
	return ErrWatchNotImplemented
}

func (e *Engine) requireDB() error {
	if e.indexDB == nil {
		return ErrNotInitialized
	}
	return nil
}

// splitLines drops the trailing empty element strings.Split leaves after a
// final newline.
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
