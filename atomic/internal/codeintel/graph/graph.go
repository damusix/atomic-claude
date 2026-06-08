// Package graph implements the traversal engine and query manager over the
// resolved code-intelligence graph (master CP17).
//
// The package builds on the db query layer:
//   - db.GetEdgesBySource / db.GetEdgesByTarget for edge lookup
//   - db.GetNodesByIds for batched neighbor hydration (never N+1)
//
// # BFS batching contract (appendix I)
//
// Each frontier level is processed in one pass:
//  1. Collect all neighbor node-ids from the edge rows returned for the
//     current frontier.
//  2. Call db.GetNodesByIds once per frontier level (already 500-chunked
//     inside the db layer).
//  3. Expand the next frontier from the newly hydrated nodes.
//
// This guarantees O(depth) round-trips to the database, not O(nodes).
//
// # Edge priority sort
//
// Edges at each frontier are sorted by kind priority before expansion:
//
//	contains(0) < calls(1) < everything-else(2)
//
// This ordering ensures container-first descent (a file/class is expanded
// before its callers) and is load-bearing for deterministic BFS expansion.
//
// # Determinism
//
// Subgraph.Nodes is a map. Any code that serialises or renders a Subgraph
// must use types.SubgraphSortedNodes — never range over the map directly.
package graph

import (
	"context"
	"fmt"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// edgePriority returns the expansion priority for a given edge kind.
// Lower is higher priority: contains(0) < calls(1) < everything-else(2).
func edgePriority(k types.EdgeKind) int {
	switch k {
	case types.EdgeKindContains:
		return 0
	case types.EdgeKindCalls:
		return 1
	default:
		return 2
	}
}

// sortEdgesByPriority sorts edges in-place: contains < calls < other.
func sortEdgesByPriority(edges []types.Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		return edgePriority(edges[i].Kind) < edgePriority(edges[j].Kind)
	})
}

// callerCalleeKinds are the edge kinds followed for GetCallers/GetCallees.
var callerCalleeKinds = map[types.EdgeKind]bool{
	types.EdgeKindCalls:      true,
	types.EdgeKindReferences: true,
	types.EdgeKindImports:    true,
}

// heritageKinds are the edge kinds followed for GetTypeHierarchy.
var heritageKinds = map[types.EdgeKind]bool{
	types.EdgeKindExtends:    true,
	types.EdgeKindImplements: true,
}

// containerKinds are node kinds considered containers for GetImpactRadius.
// Container nodes first descend into children via outgoing contains edges
// before expanding impact, to avoid climbing to the parent then re-expanding
// siblings.
var containerKinds = map[types.NodeKind]bool{
	types.NodeKindFile:      true,
	types.NodeKindModule:    true,
	types.NodeKindClass:     true,
	types.NodeKindStruct:    true,
	types.NodeKindInterface: true,
	types.NodeKindNamespace: true,
	types.NodeKindTrait:     true,
	types.NodeKindProtocol:  true,
}

// deadCodeKinds are the node kinds checked by FindDeadCode.
var deadCodeKinds = map[types.NodeKind]bool{
	types.NodeKindFunction: true,
	types.NodeKindMethod:   true,
	types.NodeKindClass:    true,
}

// Manager holds the db handle and exposes the graph query operations.
// It is the primary entry point for CP19 (context builder) and the CP20+
// engine facade.
type Manager struct {
	db *db.DB
}

// NewManager creates a Manager backed by the given database.
func NewManager(d *db.DB) *Manager {
	return &Manager{db: d}
}

// ---------------------------------------------------------------------------
// GetCallees
// ---------------------------------------------------------------------------

// GetCallees returns all nodes reachable from startID via outgoing
// calls|references|imports edges, up to maxDepth hops.
// maxDepth=0 applies the default depth of 1.
func (m *Manager) GetCallees(ctx context.Context, startID string, maxDepth int) (types.Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 1
	}
	return m.bfsOutgoing(ctx, startID, maxDepth, callerCalleeKinds)
}

// ---------------------------------------------------------------------------
// GetCallers
// ---------------------------------------------------------------------------

// GetCallers returns all nodes that reach startID via incoming
// calls|references|imports edges, up to maxDepth hops.
// maxDepth=0 applies the default depth of 1.
func (m *Manager) GetCallers(ctx context.Context, startID string, maxDepth int) (types.Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 1
	}
	return m.bfsIncoming(ctx, startID, maxDepth, callerCalleeKinds)
}

// ---------------------------------------------------------------------------
// GetImpactRadius
// ---------------------------------------------------------------------------

// GetImpactRadius returns all nodes that transitively depend on startID via
// any incoming edge kind EXCEPT contains. The impact radius is computed
// recursively: container nodes first descend into their children via outgoing
// contains edges before expanding impact, which avoids climbing to the parent
// and re-expanding siblings.
//
// maxDepth=0 applies the default depth of 3.
func (m *Manager) GetImpactRadius(ctx context.Context, startID string, maxDepth int) (types.Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	visited := make(map[string]bool)
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}

	// Seed the start node.
	startNode, err := m.db.GetNode(ctx, startID)
	if err != nil {
		return sg, err
	}
	// For container kinds: first descend into children via contains outgoing,
	// then expand their impact. This avoids the parent→sibling bleed.
	if containerKinds[startNode.Kind] {
		childEdges, err := m.db.GetEdgesBySource(ctx, startID)
		if err != nil {
			return sg, err
		}
		sortEdgesByPriority(childEdges)
		childIDs := make([]string, 0)
		for _, e := range childEdges {
			if e.Kind == types.EdgeKindContains {
				childIDs = append(childIDs, e.Target)
			}
		}
		if len(childIDs) > 0 {
			childNodes, err := m.db.GetNodesByIds(ctx, childIDs)
			if err != nil {
				return sg, err
			}
			for _, cn := range childNodes {
				if visited[cn.ID] {
					continue
				}
				childSG, err := m.impactBFS(ctx, cn.ID, maxDepth, visited)
				if err != nil {
					return sg, err
				}
				for id, n := range childSG.Nodes {
					sg.Nodes[id] = n
				}
				sg.Edges = append(sg.Edges, childSG.Edges...)
			}
			return sg, nil
		}
		// F-45: childless container — fall through to impactBFS on the start node
		// itself so real incoming non-contains edges are not silently ignored.
	}

	childSG, err := m.impactBFS(ctx, startID, maxDepth, visited)
	if err != nil {
		return sg, err
	}
	return childSG, nil
}

// impactBFS performs a BFS over incoming edges except contains.
// It accumulates results into the returned Subgraph and marks visited IDs.
func (m *Manager) impactBFS(ctx context.Context, startID string, maxDepth int, visited map[string]bool) (types.Subgraph, error) {
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}
	if visited[startID] {
		return sg, nil
	}

	frontier := []string{startID}
	visited[startID] = true

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		// Collect all incoming edges for the current frontier.
		var allEdges []types.Edge
		for _, id := range frontier {
			edges, err := m.db.GetEdgesByTarget(ctx, id)
			if err != nil {
				return sg, err
			}
			sortEdgesByPriority(edges)
			for _, e := range edges {
				// Exclude contains — it's the container relationship, not dependency.
				if e.Kind != types.EdgeKindContains {
					allEdges = append(allEdges, e)
					sg.Edges = append(sg.Edges, e)
				}
			}
		}

		// Batch-fetch all new neighbor nodes in one call.
		neighborIDs := make([]string, 0, len(allEdges))
		seen := make(map[string]bool)
		for _, e := range allEdges {
			if !visited[e.Source] && !seen[e.Source] {
				neighborIDs = append(neighborIDs, e.Source)
				seen[e.Source] = true
			}
		}
		if len(neighborIDs) == 0 {
			break
		}

		neighbors, err := m.db.GetNodesByIds(ctx, neighborIDs)
		if err != nil {
			return sg, err
		}

		nextFrontier := make([]string, 0, len(neighbors))
		for _, n := range neighbors {
			if visited[n.ID] {
				continue
			}
			visited[n.ID] = true
			sg.Nodes[n.ID] = n
			nextFrontier = append(nextFrontier, n.ID)
		}
		frontier = nextFrontier
	}

	return sg, nil
}

// ---------------------------------------------------------------------------
// FindPath
// ---------------------------------------------------------------------------

// FindPath returns the shortest path between fromID and toID via BFS over
// outgoing edges. edgeKinds restricts which edge kinds are followed; nil means
// all kinds. Returns an empty Subgraph when no path exists.
func (m *Manager) FindPath(ctx context.Context, fromID, toID string, edgeKinds []types.EdgeKind) (types.Subgraph, error) {
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}
	if fromID == toID {
		// Trivially reachable — but propagate any GetNode error (F-46).
		n, err := m.db.GetNode(ctx, fromID)
		if err != nil {
			return sg, err
		}
		sg.Nodes[n.ID] = n
		sg.Roots = []string{n.ID}
		return sg, nil
	}

	kindFilter := make(map[types.EdgeKind]bool)
	for _, k := range edgeKinds {
		kindFilter[k] = true
	}

	// parent maps each visited node id to the id of the predecessor on the
	// shortest-path tree.
	parent := map[string]string{fromID: ""}
	frontier := []string{fromID}
	found := false

	for len(frontier) > 0 && !found {
		// Collect all outgoing edges for the frontier.
		var allEdges []types.Edge
		for _, id := range frontier {
			edges, err := m.db.GetEdgesBySource(ctx, id)
			if err != nil {
				return sg, err
			}
			sortEdgesByPriority(edges)
			for _, e := range edges {
				if len(kindFilter) > 0 && !kindFilter[e.Kind] {
					continue
				}
				if _, visited := parent[e.Target]; !visited {
					allEdges = append(allEdges, e)
				}
			}
		}

		// Batch-fetch neighbors.
		neighborIDs := make([]string, 0, len(allEdges))
		edgeByTarget := make(map[string]types.Edge)
		for _, e := range allEdges {
			if _, visited := parent[e.Target]; !visited {
				if _, dup := edgeByTarget[e.Target]; !dup {
					neighborIDs = append(neighborIDs, e.Target)
					edgeByTarget[e.Target] = e
				}
			}
		}
		if len(neighborIDs) == 0 {
			break
		}

		neighbors, err := m.db.GetNodesByIds(ctx, neighborIDs)
		if err != nil {
			return sg, err
		}

		nextFrontier := make([]string, 0, len(neighbors))
		for _, n := range neighbors {
			e := edgeByTarget[n.ID]
			parent[n.ID] = e.Source
			nextFrontier = append(nextFrontier, n.ID)
			if n.ID == toID {
				found = true
				// Don't break — let the batch finish so we register the parent.
			}
		}
		frontier = nextFrontier
	}

	if !found {
		return sg, nil
	}

	// Reconstruct path from toID back to fromID.
	path := []string{}
	cur := toID
	for cur != "" {
		path = append(path, cur)
		cur = parent[cur]
	}
	// Reverse to get fromID → toID order.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	// Hydrate path nodes.
	pathNodes, err := m.db.GetNodesByIds(ctx, path)
	if err != nil {
		return sg, err
	}
	for _, n := range pathNodes {
		sg.Nodes[n.ID] = n
	}
	sg.Roots = []string{fromID}
	return sg, nil
}

// ---------------------------------------------------------------------------
// GetTypeHierarchy
// ---------------------------------------------------------------------------

// GetTypeHierarchy returns all ancestors or descendants of startID via
// extends|implements edges. direction must be "ancestors" or "descendants".
//
//   - "ancestors": follow outgoing extends/implements edges (what startID
//     extends/implements).
//   - "descendants": follow incoming extends/implements edges (what extends/
//     implements startID).
func (m *Manager) GetTypeHierarchy(ctx context.Context, startID string, direction string) ([]types.Node, error) {
	if direction == "ancestors" {
		sg, err := m.bfsOutgoing(ctx, startID, 0, heritageKinds)
		if err != nil {
			return nil, err
		}
		return types.SubgraphSortedNodes(sg), nil
	}
	if direction == "descendants" {
		sg, err := m.bfsIncoming(ctx, startID, 0, heritageKinds)
		if err != nil {
			return nil, err
		}
		return types.SubgraphSortedNodes(sg), nil
	}
	// F-47: unknown direction — return explicit error rather than silently guessing.
	return nil, fmt.Errorf("graph: GetTypeHierarchy: unknown direction %q (want \"ancestors\" or \"descendants\")", direction)
}

// ---------------------------------------------------------------------------
// FindDeadCode
// ---------------------------------------------------------------------------

// FindDeadCode returns nodes of kind function|method|class that have no
// non-contains incoming edges and IsExported=false.
func (m *Manager) FindDeadCode(ctx context.Context) ([]types.Node, error) {
	var dead []types.Node

	// F-49: iterate kinds in sorted order so DB-call sequence is reproducible.
	sortedKinds := make([]types.NodeKind, 0, len(deadCodeKinds))
	for kind := range deadCodeKinds {
		sortedKinds = append(sortedKinds, kind)
	}
	sort.Slice(sortedKinds, func(i, j int) bool { return sortedKinds[i] < sortedKinds[j] })

	for _, kind := range sortedKinds {
		nodes, err := m.db.GetNodesByKind(ctx, kind)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			if n.IsExported {
				continue
			}
			// Check for any non-contains incoming edge.
			incoming, err := m.db.GetEdgesByTarget(ctx, n.ID)
			if err != nil {
				return nil, err
			}
			hasRealIncoming := false
			for _, e := range incoming {
				if e.Kind != types.EdgeKindContains {
					hasRealIncoming = true
					break
				}
			}
			if !hasRealIncoming {
				dead = append(dead, n)
			}
		}
	}

	// Sort for determinism.
	sort.Slice(dead, func(i, j int) bool {
		return dead[i].ID < dead[j].ID
	})
	return dead, nil
}

// ---------------------------------------------------------------------------
// FindCircularDependencies
// ---------------------------------------------------------------------------

// FindCircularDependencies finds cycles in file-level imports edges using DFS
// with a recursion stack. Returns a slice of cycles, where each cycle is an
// ordered list of node IDs forming the cycle.
func (m *Manager) FindCircularDependencies(ctx context.Context) ([][]string, error) {
	// Gather all file nodes.
	fileNodes, err := m.db.GetNodesByKind(ctx, types.NodeKindFile)
	if err != nil {
		return nil, err
	}

	// Build adjacency list: fileID → []fileID (via imports edges only).
	adj := make(map[string][]string, len(fileNodes))
	for _, fn := range fileNodes {
		edges, err := m.db.GetEdgesBySource(ctx, fn.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if e.Kind == types.EdgeKindImports {
				adj[fn.ID] = append(adj[fn.ID], e.Target)
			}
		}
		if _, ok := adj[fn.ID]; !ok {
			adj[fn.ID] = nil
		}
	}
	// F-48: sort each neighbor list so DFS visits neighbors in deterministic order.
	for id := range adj {
		sort.Strings(adj[id])
	}

	var cycles [][]string
	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	stackPos := make(map[string]int) // node → position in current DFS path
	path := make([]string, 0)

	var dfs func(id string)
	dfs = func(id string) {
		visited[id] = true
		onStack[id] = true
		stackPos[id] = len(path)
		path = append(path, id)

		for _, neighbor := range adj[id] {
			if !visited[neighbor] {
				dfs(neighbor)
			} else if onStack[neighbor] {
				// Found a cycle: extract it from the path.
				start := stackPos[neighbor]
				cycle := make([]string, len(path)-start)
				copy(cycle, path[start:])
				cycles = append(cycles, cycle)
			}
		}

		path = path[:len(path)-1]
		onStack[id] = false
		delete(stackPos, id)
	}

	// Sort file node IDs for deterministic DFS order.
	fileIDs := make([]string, 0, len(fileNodes))
	for _, fn := range fileNodes {
		fileIDs = append(fileIDs, fn.ID)
	}
	sort.Strings(fileIDs)

	for _, id := range fileIDs {
		if !visited[id] {
			dfs(id)
		}
	}

	// F-48: sort the returned cycles slice for reproducible output.
	sort.Slice(cycles, func(i, j int) bool {
		ci, cj := cycles[i], cycles[j]
		for k := 0; k < len(ci) && k < len(cj); k++ {
			if ci[k] != cj[k] {
				return ci[k] < cj[k]
			}
		}
		return len(ci) < len(cj)
	})

	return cycles, nil
}

// ---------------------------------------------------------------------------
// Internal BFS helpers
// ---------------------------------------------------------------------------

// bfsOutgoing performs a BFS following outgoing edges of the allowed kinds
// from startID, up to maxDepth hops. maxDepth=0 means unlimited.
// Returns a Subgraph of all visited nodes (excluding the start node itself).
func (m *Manager) bfsOutgoing(ctx context.Context, startID string, maxDepth int, allowedKinds map[types.EdgeKind]bool) (types.Subgraph, error) {
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}
	visited := map[string]bool{startID: true}
	frontier := []string{startID}

	for depth := 0; (maxDepth == 0 || depth < maxDepth) && len(frontier) > 0; depth++ {
		var allEdges []types.Edge
		for _, id := range frontier {
			edges, err := m.db.GetEdgesBySource(ctx, id)
			if err != nil {
				return sg, err
			}
			sortEdgesByPriority(edges)
			for _, e := range edges {
				if len(allowedKinds) == 0 || allowedKinds[e.Kind] {
					if !visited[e.Target] {
						allEdges = append(allEdges, e)
						sg.Edges = append(sg.Edges, e)
					}
				}
			}
		}

		// Batch-hydrate all neighbor nodes in one call per frontier level.
		neighborIDs := make([]string, 0, len(allEdges))
		seen := make(map[string]bool)
		for _, e := range allEdges {
			if !visited[e.Target] && !seen[e.Target] {
				neighborIDs = append(neighborIDs, e.Target)
				seen[e.Target] = true
			}
		}
		if len(neighborIDs) == 0 {
			break
		}

		neighbors, err := m.db.GetNodesByIds(ctx, neighborIDs)
		if err != nil {
			return sg, err
		}

		nextFrontier := make([]string, 0, len(neighbors))
		for _, n := range neighbors {
			if visited[n.ID] {
				continue
			}
			visited[n.ID] = true
			sg.Nodes[n.ID] = n
			nextFrontier = append(nextFrontier, n.ID)
		}
		frontier = nextFrontier
	}

	return sg, nil
}

// bfsIncoming performs a BFS following incoming edges of the allowed kinds
// into startID, up to maxDepth hops. maxDepth=0 means unlimited.
// Returns a Subgraph of all visited nodes (excluding the start node itself).
func (m *Manager) bfsIncoming(ctx context.Context, startID string, maxDepth int, allowedKinds map[types.EdgeKind]bool) (types.Subgraph, error) {
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}
	visited := map[string]bool{startID: true}
	frontier := []string{startID}

	for depth := 0; (maxDepth == 0 || depth < maxDepth) && len(frontier) > 0; depth++ {
		var allEdges []types.Edge
		for _, id := range frontier {
			edges, err := m.db.GetEdgesByTarget(ctx, id)
			if err != nil {
				return sg, err
			}
			sortEdgesByPriority(edges)
			for _, e := range edges {
				if len(allowedKinds) == 0 || allowedKinds[e.Kind] {
					if !visited[e.Source] {
						allEdges = append(allEdges, e)
						sg.Edges = append(sg.Edges, e)
					}
				}
			}
		}

		// Batch-hydrate all neighbor nodes in one call per frontier level.
		neighborIDs := make([]string, 0, len(allEdges))
		seen := make(map[string]bool)
		for _, e := range allEdges {
			if !visited[e.Source] && !seen[e.Source] {
				neighborIDs = append(neighborIDs, e.Source)
				seen[e.Source] = true
			}
		}
		if len(neighborIDs) == 0 {
			break
		}

		neighbors, err := m.db.GetNodesByIds(ctx, neighborIDs)
		if err != nil {
			return sg, err
		}

		nextFrontier := make([]string, 0, len(neighbors))
		for _, n := range neighbors {
			if visited[n.ID] {
				continue
			}
			visited[n.ID] = true
			sg.Nodes[n.ID] = n
			nextFrontier = append(nextFrontier, n.ID)
		}
		frontier = nextFrontier
	}

	return sg, nil
}
