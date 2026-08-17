// Package graph traverses the resolved code-intelligence graph.
//
// Two invariants hold across every traversal here:
//
// Each frontier level hydrates its neighbors with a single db.GetNodesByIds
// call, so a walk costs O(depth) database round-trips rather than O(nodes).
//
// Subgraph.Nodes is a map, so anything that serialises or renders a Subgraph
// must go through types.SubgraphSortedNodes rather than ranging it directly.
package graph

import (
	"context"
	"fmt"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// edgePriority orders frontier expansion contains < calls < everything else, so
// a container is descended into before its callers. Load-bearing: without it,
// BFS expansion order is not deterministic.
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

func sortEdgesByPriority(edges []types.Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		return edgePriority(edges[i].Kind) < edgePriority(edges[j].Kind)
	})
}

var callerCalleeKinds = map[types.EdgeKind]bool{
	types.EdgeKindCalls:      true,
	types.EdgeKindReferences: true,
	types.EdgeKindImports:    true,
}

var heritageKinds = map[types.EdgeKind]bool{
	types.EdgeKindExtends:    true,
	types.EdgeKindImplements: true,
}

// containerKinds get impact expanded from their children rather than themselves,
// which stops the walk climbing to the parent and re-expanding every sibling.
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

var deadCodeKinds = map[types.NodeKind]bool{
	types.NodeKindFunction: true,
	types.NodeKindMethod:   true,
	types.NodeKindClass:    true,
}

// Manager exposes the graph query operations to the context builder and the
// engine facade.
type Manager struct {
	db *db.DB
}

// NewManager creates a Manager backed by the given database.
func NewManager(d *db.DB) *Manager {
	return &Manager{db: d}
}

// GetCallees returns nodes reachable from startID via outgoing
// calls|references|imports edges within maxDepth hops (0 means 1). The start
// node is included: first-hop edges name it, so it must resolve in Nodes.
func (m *Manager) GetCallees(ctx context.Context, startID string, maxDepth int) (types.Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 1
	}
	startNode, err := m.db.GetNode(ctx, startID)
	if err != nil {
		return types.Subgraph{Nodes: make(map[string]types.Node)}, err
	}
	sg, err := m.bfsOutgoing(ctx, startID, maxDepth, callerCalleeKinds)
	if err != nil {
		return sg, err
	}
	sg.Nodes[startID] = startNode
	return sg, nil
}

// GetCallers is GetCallees over incoming edges.
func (m *Manager) GetCallers(ctx context.Context, startID string, maxDepth int) (types.Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 1
	}
	startNode, err := m.db.GetNode(ctx, startID)
	if err != nil {
		return types.Subgraph{Nodes: make(map[string]types.Node)}, err
	}
	sg, err := m.bfsIncoming(ctx, startID, maxDepth, callerCalleeKinds)
	if err != nil {
		return sg, err
	}
	sg.Nodes[startID] = startNode
	return sg, nil
}

// GetImpactRadius returns nodes that transitively depend on startID via any
// incoming edge kind except contains, within maxDepth hops (0 means 3).
func (m *Manager) GetImpactRadius(ctx context.Context, startID string, maxDepth int) (types.Subgraph, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	visited := make(map[string]bool)
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}

	startNode, err := m.db.GetNode(ctx, startID)
	if err != nil {
		return sg, err
	}
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
				// impactBFS hydrates only its neighbors, never its own start node.
				sg.Nodes[cn.ID] = cn
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
		// A childless container falls through, so its own incoming edges are not
		// silently ignored.
	}

	childSG, err := m.impactBFS(ctx, startID, maxDepth, visited)
	if err != nil {
		return sg, err
	}
	childSG.Nodes[startID] = startNode
	return childSG, nil
}

// impactBFS walks incoming edges except contains, hydrating neighbors only —
// the caller is responsible for putting startID into the returned Nodes map.
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
		var allEdges []types.Edge
		for _, id := range frontier {
			edges, err := m.db.GetEdgesByTarget(ctx, id)
			if err != nil {
				return sg, err
			}
			sortEdgesByPriority(edges)
			for _, e := range edges {
				// contains is containment, not dependency.
				if e.Kind != types.EdgeKindContains {
					allEdges = append(allEdges, e)
					sg.Edges = append(sg.Edges, e)
				}
			}
		}

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

// FindPath returns the shortest outgoing path from fromID to toID, or an empty
// Subgraph if none exists. A nil edgeKinds follows every kind.
func (m *Manager) FindPath(ctx context.Context, fromID, toID string, edgeKinds []types.EdgeKind) (types.Subgraph, error) {
	sg := types.Subgraph{
		Nodes: make(map[string]types.Node),
	}
	if fromID == toID {
		// Trivially reachable, but a nonexistent node must still error.
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

	// parent maps a visited node to its predecessor on the shortest-path tree.
	parent := map[string]string{fromID: ""}
	frontier := []string{fromID}
	found := false

	for len(frontier) > 0 && !found {
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
				// No break: the rest of the batch must still register parents.
				found = true
			}
		}
		frontier = nextFrontier
	}

	if !found {
		return sg, nil
	}

	// Walk parents back from toID, then reverse into fromID → toID order.
	path := []string{}
	cur := toID
	for cur != "" {
		path = append(path, cur)
		cur = parent[cur]
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

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

// GetTypeHierarchy walks extends|implements edges outgoing for "ancestors" or
// incoming for "descendants"; any other direction is an error.
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
	return nil, fmt.Errorf("graph: GetTypeHierarchy: unknown direction %q (want \"ancestors\" or \"descendants\")", direction)
}

// FindDeadCode returns unexported function|method|class nodes whose only
// incoming edges are contains.
func (m *Manager) FindDeadCode(ctx context.Context) ([]types.Node, error) {
	var dead []types.Node

	// Sorted so the DB-call sequence is reproducible.
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

	sort.Slice(dead, func(i, j int) bool {
		return dead[i].ID < dead[j].ID
	})
	return dead, nil
}

// FindCircularDependencies returns each import cycle among file nodes as an
// ordered list of node IDs, found by DFS over a recursion stack.
func (m *Manager) FindCircularDependencies(ctx context.Context) ([][]string, error) {
	fileNodes, err := m.db.GetNodesByKind(ctx, types.NodeKindFile)
	if err != nil {
		return nil, err
	}

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
	// Sorted so DFS visits neighbors deterministically.
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

	// Sorted so DFS roots are visited deterministically.
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

// bfsOutgoing walks outgoing edges of the allowed kinds from startID, maxDepth=0
// meaning unlimited. The start node is excluded from the returned Nodes.
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

// bfsIncoming is bfsOutgoing over incoming edges.
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
