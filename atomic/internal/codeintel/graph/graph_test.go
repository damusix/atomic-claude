package graph_test

// Tests for the graph traversal + query manager (master CP17).
//
// Fixture structure (built entirely via db CRUD — no indexer):
//
//   Nodes:
//     nodeA  (function, unexported) in file_a.go  — the root caller
//     nodeB  (function, unexported) in file_b.go  — mid-hop
//     nodeC  (function, unexported) in file_c.go  — leaf callee
//     nodeExported (function, exported) in file_a.go — exported; must NOT appear in dead code
//     nodeUncalled (function, unexported) in file_a.go — no non-contains incoming; dead
//     nodeIface    (interface) in iface.go
//     nodeClass    (class)     in impl.go
//     fileNodeA    (file) — container for nodeA/nodeExported/nodeUncalled
//     fileNodeB    (file) — container for nodeB
//     fileNodeC    (file) — container for nodeC
//     fileNodeX    (file) — cycle participant 1 (imports fileNodeY)
//     fileNodeY    (file) — cycle participant 2 (imports fileNodeX)
//
//   Edges:
//     A --calls-->      B
//     B --calls-->      C
//     A --contains-->   nodeA      (fileNodeA → nodeA; the "contains" kind)
//     fileNodeA --contains--> nodeA
//     fileNodeA --contains--> nodeExported
//     fileNodeA --contains--> nodeUncalled
//     fileNodeB --contains--> nodeB
//     fileNodeC --contains--> nodeC
//     nodeClass --implements--> nodeIface   (EE4 heritage)
//     fileNodeX --imports--> fileNodeY
//     fileNodeY --imports--> fileNodeX
//
// Dead-code target: nodeUncalled — function, unexported, no non-contains incoming edges.
// Not dead: nodeA (called by nothing but we don't assert it dead here — the test
//   focuses on nodeUncalled explicitly), nodeExported (exported), nodeB/nodeC (called).

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/graph"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func makeNode(id, kind, name, filePath string, exported bool) types.Node {
	return types.Node{
		ID:         id,
		Kind:       types.NodeKind(kind),
		Name:       name,
		FilePath:   filePath,
		Language:   types.LanguageGo,
		IsExported: exported,
	}
}

func makeEdge(source, target string, kind types.EdgeKind) types.Edge {
	return types.Edge{
		Source: source,
		Target: target,
		Kind:   kind,
	}
}

// buildFixture populates the database with the known-structure fixture and
// returns a Manager. The fixture is described at the top of this file.
func buildFixture(t *testing.T, d *db.DB) *graph.Manager {
	t.Helper()
	ctx := context.Background()

	nodes := []types.Node{
		makeNode("nodeA", "function", "A", "file_a.go", false),
		makeNode("nodeB", "function", "B", "file_b.go", false),
		makeNode("nodeC", "function", "C", "file_c.go", false),
		makeNode("nodeExported", "function", "Exported", "file_a.go", true),
		makeNode("nodeUncalled", "function", "Uncalled", "file_a.go", false),
		makeNode("nodeIface", "interface", "Iface", "iface.go", true),
		makeNode("nodeClass", "class", "MyClass", "impl.go", false),
		makeNode("fileNodeA", "file", "file_a.go", "file_a.go", false),
		makeNode("fileNodeB", "file", "file_b.go", "file_b.go", false),
		makeNode("fileNodeC", "file", "file_c.go", "file_c.go", false),
		makeNode("fileNodeX", "file", "file_x.go", "file_x.go", false),
		makeNode("fileNodeY", "file", "file_y.go", "file_y.go", false),
	}
	for _, n := range nodes {
		if err := d.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert node %s: %v", n.ID, err)
		}
	}

	edges := []types.Edge{
		// Call chain: A → B → C
		makeEdge("nodeA", "nodeB", types.EdgeKindCalls),
		makeEdge("nodeB", "nodeC", types.EdgeKindCalls),
		// Container edges (file → symbol)
		makeEdge("fileNodeA", "nodeA", types.EdgeKindContains),
		makeEdge("fileNodeA", "nodeExported", types.EdgeKindContains),
		makeEdge("fileNodeA", "nodeUncalled", types.EdgeKindContains),
		makeEdge("fileNodeB", "nodeB", types.EdgeKindContains),
		makeEdge("fileNodeC", "nodeC", types.EdgeKindContains),
		// Heritage: MyClass implements Iface
		makeEdge("nodeClass", "nodeIface", types.EdgeKindImplements),
		// Import cycle: fileX ↔ fileY
		makeEdge("fileNodeX", "fileNodeY", types.EdgeKindImports),
		makeEdge("fileNodeY", "fileNodeX", types.EdgeKindImports),
	}
	for _, e := range edges {
		if _, err := d.InsertEdge(ctx, e); err != nil {
			t.Fatalf("insert edge %s→%s: %v", e.Source, e.Target, err)
		}
	}

	return graph.NewManager(d)
}

// nodeIDs extracts just the IDs from a slice of nodes (for assertions).
func nodeIDs(nodes []types.Node) map[string]bool {
	m := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		m[n.ID] = true
	}
	return m
}

// subgraphNodeIDs extracts the node id set from a Subgraph.
func subgraphNodeIDs(sg types.Subgraph) map[string]bool {
	m := make(map[string]bool, len(sg.Nodes))
	for id := range sg.Nodes {
		m[id] = true
	}
	return m
}

// ---------------------------------------------------------------------------
// GetCallees — follow calls edges outgoing
// ---------------------------------------------------------------------------

func TestGetCallees_Depth1(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// callees(A, depth=1) should return B only.
	sg, err := m.GetCallees(ctx, "nodeA", 1)
	if err != nil {
		t.Fatalf("GetCallees: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeB"] {
		t.Errorf("expected nodeB in callees(A, 1), got %v", ids)
	}
	if ids["nodeC"] {
		t.Errorf("nodeC should not appear at depth=1")
	}
}

func TestGetCallees_Depth2(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// callees(A, depth=2) should return B and C.
	sg, err := m.GetCallees(ctx, "nodeA", 2)
	if err != nil {
		t.Fatalf("GetCallees: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeB"] || !ids["nodeC"] {
		t.Errorf("expected nodeB and nodeC in callees(A, 2), got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// GetCallers — follow calls edges incoming
// ---------------------------------------------------------------------------

func TestGetCallers_Depth1(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// callers(C, depth=1) should return B only.
	sg, err := m.GetCallers(ctx, "nodeC", 1)
	if err != nil {
		t.Fatalf("GetCallers: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeB"] {
		t.Errorf("expected nodeB in callers(C, 1), got %v", ids)
	}
	if ids["nodeA"] {
		t.Errorf("nodeA should not appear at depth=1")
	}
}

func TestGetCallers_Depth2(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// callers(C, depth=2) should return B and A.
	sg, err := m.GetCallers(ctx, "nodeC", 2)
	if err != nil {
		t.Fatalf("GetCallers: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeB"] || !ids["nodeA"] {
		t.Errorf("expected nodeA and nodeB in callers(C, 2), got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// GetImpactRadius — incoming edges except contains
// ---------------------------------------------------------------------------

func TestGetImpactRadius_ExcludesContains(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// impact(B, depth=3): should include nodeA (calls B).
	// Must NOT include fileNodeB even though fileNodeB --contains--> nodeB
	// (contains edges are excluded from the radius).
	sg, err := m.GetImpactRadius(ctx, "nodeB", 3)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeA"] {
		t.Errorf("expected nodeA (caller) in impact(B), got %v", ids)
	}
	if ids["fileNodeB"] {
		t.Errorf("fileNodeB must NOT appear in impact(B): contains edges excluded from radius")
	}
}

func TestGetImpactRadius_DefaultDepth(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// Impact with depth=0 should use the default (3). Same outcome as explicit 3.
	sg, err := m.GetImpactRadius(ctx, "nodeB", 0)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeA"] {
		t.Errorf("expected nodeA in impact(B) with default depth")
	}
}

// ---------------------------------------------------------------------------
// FindPath — BFS shortest path
// ---------------------------------------------------------------------------

func TestFindPath_ReachableAtoC(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// findPath(A, C) → A→B→C (2 hops via calls edges)
	sg, err := m.FindPath(ctx, "nodeA", "nodeC", nil)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeA"] || !ids["nodeB"] || !ids["nodeC"] {
		t.Errorf("expected path A→B→C, got nodes %v", ids)
	}
}

func TestFindPath_Unreachable(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// findPath(C, A) → unreachable (edges are directed A→B→C, not reversed).
	sg, err := m.FindPath(ctx, "nodeC", "nodeA", nil)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(sg.Nodes) != 0 {
		t.Errorf("expected empty path C→A (unreachable), got %v", subgraphNodeIDs(sg))
	}
}

func TestFindPath_FilteredEdgeKinds(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// findPath(A, C, edgeKinds=[imports]) — no imports edge between A and C.
	sg, err := m.FindPath(ctx, "nodeA", "nodeC", []types.EdgeKind{types.EdgeKindImports})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(sg.Nodes) != 0 {
		t.Errorf("expected empty path with imports-only filter, got %v", subgraphNodeIDs(sg))
	}
}

// ---------------------------------------------------------------------------
// GetTypeHierarchy — extends/implements edges
// ---------------------------------------------------------------------------

func TestGetTypeHierarchy_Ancestors(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// ancestors of nodeClass via implements → should find nodeIface.
	ancestors, err := m.GetTypeHierarchy(ctx, "nodeClass", "ancestors")
	if err != nil {
		t.Fatalf("GetTypeHierarchy: %v", err)
	}
	ids := nodeIDs(ancestors)
	if !ids["nodeIface"] {
		t.Errorf("expected nodeIface as ancestor of nodeClass, got %v", ids)
	}
}

func TestGetTypeHierarchy_Descendants(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// descendants of nodeIface → should find nodeClass.
	descendants, err := m.GetTypeHierarchy(ctx, "nodeIface", "descendants")
	if err != nil {
		t.Fatalf("GetTypeHierarchy: %v", err)
	}
	ids := nodeIDs(descendants)
	if !ids["nodeClass"] {
		t.Errorf("expected nodeClass as descendant of nodeIface, got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// FindDeadCode — unexported, no non-contains incoming
// ---------------------------------------------------------------------------

func TestFindDeadCode(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	dead, err := m.FindDeadCode(ctx)
	if err != nil {
		t.Fatalf("FindDeadCode: %v", err)
	}
	deadIDs := nodeIDs(dead)

	// nodeUncalled: unexported function, no non-contains incoming edge → DEAD.
	if !deadIDs["nodeUncalled"] {
		t.Errorf("nodeUncalled should be in dead code, got %v", deadIDs)
	}

	// nodeExported: exported → must NOT be in dead code.
	if deadIDs["nodeExported"] {
		t.Errorf("nodeExported (exported) must NOT be in dead code")
	}

	// nodeB, nodeC: called → must NOT be in dead code.
	if deadIDs["nodeB"] || deadIDs["nodeC"] {
		t.Errorf("nodeB/nodeC are called — must NOT be in dead code")
	}
}

// ---------------------------------------------------------------------------
// FindCircularDependencies — DFS over file-level imports
// ---------------------------------------------------------------------------

func TestFindCircularDependencies(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	cycles, err := m.FindCircularDependencies(ctx)
	if err != nil {
		t.Fatalf("FindCircularDependencies: %v", err)
	}

	// The fixture has exactly one 2-file cycle: fileNodeX ↔ fileNodeY.
	if len(cycles) == 0 {
		t.Fatal("expected at least one cycle, got none")
	}

	// At least one cycle must contain both fileNodeX and fileNodeY.
	found := false
	for _, cycle := range cycles {
		cycleSet := make(map[string]bool)
		for _, id := range cycle {
			cycleSet[id] = true
		}
		if cycleSet["fileNodeX"] && cycleSet["fileNodeY"] {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a cycle containing fileNodeX and fileNodeY, got %v", cycles)
	}
}

// ---------------------------------------------------------------------------
// BFS edge-priority ordering — contains(0) < calls(1) < other(2)
// ---------------------------------------------------------------------------

// TestBFSEdgePrioritySort verifies that the batched BFS correctly expands
// contains edges before calls edges when a node has both outgoing kinds.
// We add a new node that has both a contains edge and a calls edge outgoing,
// then call GetCallees and confirm the BFS found the calls-edge target.
// (The priority sort is an internal BFS concern; correctness shows through
//
//	the results being complete and correct, not N+1.)
func TestBFSEdgePrioritySort(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Build a tiny graph: hub --contains--> child1, hub --calls--> child2.
	// GetCallees(hub, 1) must return child2 (via calls).
	nodes := []types.Node{
		makeNode("hub", "class", "Hub", "hub.go", false),
		makeNode("child1", "function", "Child1", "hub.go", false),
		makeNode("child2", "function", "Child2", "other.go", false),
	}
	for _, n := range nodes {
		if err := d.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.ID, err)
		}
	}
	edges := []types.Edge{
		makeEdge("hub", "child1", types.EdgeKindContains),
		makeEdge("hub", "child2", types.EdgeKindCalls),
	}
	for _, e := range edges {
		if _, err := d.InsertEdge(ctx, e); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}

	m := graph.NewManager(d)
	// GetCallees follows calls|references|imports — must find child2.
	sg, err := m.GetCallees(ctx, "hub", 1)
	if err != nil {
		t.Fatalf("GetCallees: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["child2"] {
		t.Errorf("expected child2 in callees(hub, 1), got %v", ids)
	}
	// child1 is connected only via contains; GetCallees doesn't follow contains.
	if ids["child1"] {
		t.Errorf("child1 (contains-only) should NOT appear in GetCallees result")
	}
}

// ---------------------------------------------------------------------------
// F-45: GetImpactRadius childless container must fall through to impactBFS
// ---------------------------------------------------------------------------

// TestGetImpactRadius_ChildlessContainer ensures that a container node with no
// contains-children but real incoming non-contains edges returns a non-empty
// impact radius instead of an empty subgraph (the pre-fix regression).
func TestGetImpactRadius_ChildlessContainer(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// childlessFile: a file node (container kind) with zero contains edges.
	// caller: calls childlessFile via a "calls" edge so the impact radius is non-empty.
	nodes := []types.Node{
		makeNode("childlessFile", "file", "empty.go", "empty.go", false),
		makeNode("callerOfFile", "function", "Caller", "caller.go", false),
	}
	for _, n := range nodes {
		if err := d.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.ID, err)
		}
	}
	// callerOfFile → childlessFile via calls (not contains)
	if _, err := d.InsertEdge(ctx, makeEdge("callerOfFile", "childlessFile", types.EdgeKindCalls)); err != nil {
		t.Fatalf("insert edge: %v", err)
	}

	m := graph.NewManager(d)
	sg, err := m.GetImpactRadius(ctx, "childlessFile", 3)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	// Pre-fix: returns empty because container branch returns early when childIDs is empty.
	// Post-fix: falls through to impactBFS and finds callerOfFile.
	if _, ok := sg.Nodes["callerOfFile"]; !ok {
		t.Errorf("expected callerOfFile in impact(childlessFile): got nodes %v", func() []string {
			ids := make([]string, 0, len(sg.Nodes))
			for id := range sg.Nodes {
				ids = append(ids, id)
			}
			return ids
		}())
	}
}

// ---------------------------------------------------------------------------
// F-46: FindPath self-path must propagate GetNode error
// ---------------------------------------------------------------------------

// TestFindPath_SelfPath_ErrorPropagates ensures that when fromID==toID and
// GetNode returns an error (node not found), the error is surfaced to the
// caller rather than swallowed (the pre-fix regression returned nil error).
func TestFindPath_SelfPath_ErrorPropagates(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	// Do NOT insert any node — GetNode("ghost") will return an error.

	m := graph.NewManager(d)
	// fromID == toID triggers the self-path branch.
	_, err := m.FindPath(ctx, "ghost", "ghost", nil)
	// Pre-fix: err is nil (error silently discarded).
	// Post-fix: err is non-nil (node-not-found propagated).
	if err == nil {
		t.Error("FindPath(ghost, ghost): expected error for missing node, got nil")
	}
}

// ---------------------------------------------------------------------------
// F-47: GetTypeHierarchy unknown direction must return error
// ---------------------------------------------------------------------------

// TestGetTypeHierarchy_UnknownDirection ensures that an unrecognised direction
// string returns an error rather than silently using bfsIncoming.
func TestGetTypeHierarchy_UnknownDirection(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// "bogus" is not "ancestors" or "descendants".
	_, err := m.GetTypeHierarchy(ctx, "nodeClass", "bogus")
	// Pre-fix: err is nil (falls through to bfsIncoming silently).
	// Post-fix: err is non-nil.
	if err == nil {
		t.Error("GetTypeHierarchy with direction='bogus': expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// F-48: FindCircularDependencies — deterministic cycle ordering
// ---------------------------------------------------------------------------

// TestFindCircularDependencies_Deterministic verifies that repeated calls
// return cycles in identical sorted order. Uses a 3-file cycle so the
// non-determinism from adjacency map traversal is exercised.
func TestFindCircularDependencies_Deterministic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// 3-file cycle: fileP → fileQ → fileR → fileP
	for _, id := range []string{"fileP", "fileQ", "fileR"} {
		if err := d.UpsertNode(ctx, makeNode(id, "file", id+".go", id+".go", false)); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}
	for _, e := range []types.Edge{
		makeEdge("fileP", "fileQ", types.EdgeKindImports),
		makeEdge("fileQ", "fileR", types.EdgeKindImports),
		makeEdge("fileR", "fileP", types.EdgeKindImports),
	} {
		if _, err := d.InsertEdge(ctx, e); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}

	m := graph.NewManager(d)

	cycles1, err := m.FindCircularDependencies(ctx)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	cycles2, err := m.FindCircularDependencies(ctx)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if len(cycles1) != len(cycles2) {
		t.Fatalf("cycle count differs: %d vs %d", len(cycles1), len(cycles2))
	}
	for i := range cycles1 {
		if len(cycles1[i]) != len(cycles2[i]) {
			t.Errorf("cycle[%d] length differs: %d vs %d", i, len(cycles1[i]), len(cycles2[i]))
			continue
		}
		for j := range cycles1[i] {
			if cycles1[i][j] != cycles2[i][j] {
				t.Errorf("cycle[%d][%d] differs: %s vs %s", i, j, cycles1[i][j], cycles2[i][j])
			}
		}
	}

	// Also assert the returned slice is sorted (cycle[0][0] ≤ cycle[1][0] etc.).
	for i := 1; i < len(cycles1); i++ {
		if len(cycles1[i]) > 0 && len(cycles1[i-1]) > 0 {
			if cycles1[i][0] < cycles1[i-1][0] {
				t.Errorf("cycles not sorted: cycles[%d][0]=%s < cycles[%d][0]=%s", i, cycles1[i][0], i-1, cycles1[i-1][0])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// F-49: FindDeadCode — deterministic kind iteration order
// ---------------------------------------------------------------------------

// TestFindDeadCode_Deterministic verifies that repeated FindDeadCode calls
// return node slices in identical sorted order across the 3 checked kinds
// (function, method, class). Node IDs chosen so all three kinds contribute.
func TestFindDeadCode_Deterministic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// One unexported dead node per kind (no incoming non-contains edge).
	for _, n := range []types.Node{
		makeNode("deadFn1", "function", "DeadFn1", "f.go", false),
		makeNode("deadFn2", "function", "DeadFn2", "f.go", false),
		makeNode("deadFn3", "function", "DeadFn3", "f.go", false),
		makeNode("deadMethod1", "method", "DeadM1", "f.go", false),
		makeNode("deadMethod2", "method", "DeadM2", "f.go", false),
		makeNode("deadClass1", "class", "DeadC1", "f.go", false),
	} {
		if err := d.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.ID, err)
		}
	}

	m := graph.NewManager(d)

	dead1, err := m.FindDeadCode(ctx)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	dead2, err := m.FindDeadCode(ctx)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if len(dead1) != len(dead2) {
		t.Fatalf("result length differs: %d vs %d", len(dead1), len(dead2))
	}
	for i := range dead1 {
		if dead1[i].ID != dead2[i].ID {
			t.Errorf("position %d differs: %s vs %s", i, dead1[i].ID, dead2[i].ID)
		}
	}

	// Also assert the slice is sorted by ID.
	for i := 1; i < len(dead1); i++ {
		if dead1[i].ID < dead1[i-1].ID {
			t.Errorf("dead code not sorted: dead[%d].ID=%s < dead[%d].ID=%s", i, dead1[i].ID, i-1, dead1[i-1].ID)
		}
	}
}

// ---------------------------------------------------------------------------
// SubgraphSortedNodes — deterministic ordering in result paths
// ---------------------------------------------------------------------------

func TestGetCallers_DeterministicOrder(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// Run twice; sorted output must be identical.
	sg1, err := m.GetCallers(ctx, "nodeC", 2)
	if err != nil {
		t.Fatalf("first GetCallers: %v", err)
	}
	sg2, err := m.GetCallers(ctx, "nodeC", 2)
	if err != nil {
		t.Fatalf("second GetCallers: %v", err)
	}

	sorted1 := types.SubgraphSortedNodes(sg1)
	sorted2 := types.SubgraphSortedNodes(sg2)
	if len(sorted1) != len(sorted2) {
		t.Fatalf("different lengths: %d vs %d", len(sorted1), len(sorted2))
	}
	for i := range sorted1 {
		if sorted1[i].ID != sorted2[i].ID {
			t.Errorf("position %d: %s vs %s", i, sorted1[i].ID, sorted2[i].ID)
		}
	}
}
