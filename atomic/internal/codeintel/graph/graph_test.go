package graph_test

// The shared fixture, built through db CRUD rather than the indexer:
//
//	nodeA --calls--> nodeB --calls--> nodeC
//	nodeClass --implements--> nodeIface
//	fileNodeX --imports--> fileNodeY --imports--> fileNodeX   (cycle)
//	fileNodeA/B/C --contains--> their symbols
//
// nodeExported is exported and nodeUncalled has no non-contains incoming edge,
// so dead-code queries must return the second and never the first.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/graph"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

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
		makeEdge("nodeA", "nodeB", types.EdgeKindCalls),
		makeEdge("nodeB", "nodeC", types.EdgeKindCalls),
		makeEdge("fileNodeA", "nodeA", types.EdgeKindContains),
		makeEdge("fileNodeA", "nodeExported", types.EdgeKindContains),
		makeEdge("fileNodeA", "nodeUncalled", types.EdgeKindContains),
		makeEdge("fileNodeB", "nodeB", types.EdgeKindContains),
		makeEdge("fileNodeC", "nodeC", types.EdgeKindContains),
		makeEdge("nodeClass", "nodeIface", types.EdgeKindImplements),
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

func nodeIDs(nodes []types.Node) map[string]bool {
	m := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		m[n.ID] = true
	}
	return m
}

func subgraphNodeIDs(sg types.Subgraph) map[string]bool {
	m := make(map[string]bool, len(sg.Nodes))
	for id := range sg.Nodes {
		m[id] = true
	}
	return m
}

// assertEdgeEndpointsResolve checks the invariant serve's renderSubgraph relies
// on: an endpoint missing from sg.Nodes renders as a raw node ID.
func assertEdgeEndpointsResolve(t *testing.T, sg types.Subgraph) {
	t.Helper()
	for _, e := range sg.Edges {
		if _, ok := sg.Nodes[e.Source]; !ok {
			t.Errorf("edge %s--%s-->%s: source %q does not resolve in Subgraph.Nodes", e.Source, e.Kind, e.Target, e.Source)
		}
		if _, ok := sg.Nodes[e.Target]; !ok {
			t.Errorf("edge %s--%s-->%s: target %q does not resolve in Subgraph.Nodes", e.Source, e.Kind, e.Target, e.Target)
		}
	}
}

func TestGetCallees_Depth1(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

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

	sg, err := m.GetCallees(ctx, "nodeA", 2)
	if err != nil {
		t.Fatalf("GetCallees: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeB"] || !ids["nodeC"] {
		t.Errorf("expected nodeB and nodeC in callees(A, 2), got %v", ids)
	}
}

func TestGetCallers_Depth1(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

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

	sg, err := m.GetCallers(ctx, "nodeC", 2)
	if err != nil {
		t.Fatalf("GetCallers: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeB"] || !ids["nodeA"] {
		t.Errorf("expected nodeA and nodeB in callers(C, 2), got %v", ids)
	}
}

func TestGetImpactRadius_ExcludesContains(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	// fileNodeB contains nodeB, but contains is excluded from the radius.
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

	sg, err := m.GetImpactRadius(ctx, "nodeB", 0)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeA"] {
		t.Errorf("expected nodeA in impact(B) with default depth")
	}
}

// Regression: impactBFS hydrates only its neighbors, so the container path —
// one impactBFS per child — used to drop every child from Subgraph.Nodes.
func TestGetImpactRadius_EveryEdgeEndpointResolves(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// The caller→method1 edge is the incoming non-contains edge whose endpoint
	// must come back hydrated.
	nodes := []types.Node{
		makeNode("classContainer", "class", "MyContainer", "container.go", false),
		makeNode("method1", "method", "Method1", "container.go", false),
		makeNode("method2", "method", "Method2", "container.go", false),
		makeNode("externalCaller", "function", "ExternalCaller", "caller.go", false),
	}
	for _, n := range nodes {
		if err := d.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.ID, err)
		}
	}
	edges := []types.Edge{
		makeEdge("classContainer", "method1", types.EdgeKindContains),
		makeEdge("classContainer", "method2", types.EdgeKindContains),
		makeEdge("externalCaller", "method1", types.EdgeKindCalls),
	}
	for _, e := range edges {
		if _, err := d.InsertEdge(ctx, e); err != nil {
			t.Fatalf("insert edge %s->%s: %v", e.Source, e.Target, err)
		}
	}

	m := graph.NewManager(d)
	sg, err := m.GetImpactRadius(ctx, "classContainer", 3)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}

	ids := subgraphNodeIDs(sg)
	if !ids["method1"] {
		t.Errorf("expected method1 (container child, edge endpoint) hydrated in Nodes, got %v", ids)
	}
	if !ids["externalCaller"] {
		t.Errorf("expected externalCaller in Nodes, got %v", ids)
	}
	assertEdgeEndpointsResolve(t, sg)
}

// The non-container path and the childless-container fallthrough: the start node
// is always an endpoint of the first-level edges, so it must be hydrated too.
func TestGetImpactRadius_NonContainerStartNodeHydrated(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	sg, err := m.GetImpactRadius(ctx, "nodeB", 3)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeB"] {
		t.Errorf("expected start node nodeB hydrated in Nodes, got %v", ids)
	}
	assertEdgeEndpointsResolve(t, sg)
}

func TestFindPath_ReachableAtoC(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

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

	sg, err := m.FindPath(ctx, "nodeA", "nodeC", []types.EdgeKind{types.EdgeKindImports})
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if len(sg.Nodes) != 0 {
		t.Errorf("expected empty path with imports-only filter, got %v", subgraphNodeIDs(sg))
	}
}

func TestGetTypeHierarchy_Ancestors(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

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

	descendants, err := m.GetTypeHierarchy(ctx, "nodeIface", "descendants")
	if err != nil {
		t.Fatalf("GetTypeHierarchy: %v", err)
	}
	ids := nodeIDs(descendants)
	if !ids["nodeClass"] {
		t.Errorf("expected nodeClass as descendant of nodeIface, got %v", ids)
	}
}

func TestFindDeadCode(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	dead, err := m.FindDeadCode(ctx)
	if err != nil {
		t.Fatalf("FindDeadCode: %v", err)
	}
	deadIDs := nodeIDs(dead)

	if !deadIDs["nodeUncalled"] {
		t.Errorf("nodeUncalled should be in dead code, got %v", deadIDs)
	}

	if deadIDs["nodeExported"] {
		t.Errorf("nodeExported (exported) must NOT be in dead code")
	}

	if deadIDs["nodeB"] || deadIDs["nodeC"] {
		t.Errorf("nodeB/nodeC are called — must NOT be in dead code")
	}
}

func TestFindCircularDependencies(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	cycles, err := m.FindCircularDependencies(ctx)
	if err != nil {
		t.Fatalf("FindCircularDependencies: %v", err)
	}

	if len(cycles) == 0 {
		t.Fatal("expected at least one cycle, got none")
	}

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

// The priority sort is internal, so it is observed indirectly: a hub with both a
// contains and a calls edge must still yield a complete callee result.
func TestBFSEdgePrioritySort(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

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
	sg, err := m.GetCallees(ctx, "hub", 1)
	if err != nil {
		t.Fatalf("GetCallees: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["child2"] {
		t.Errorf("expected child2 in callees(hub, 1), got %v", ids)
	}
	if ids["child1"] {
		t.Errorf("child1 (contains-only) should NOT appear in GetCallees result")
	}
}

// A container with no children must still fall through to impactBFS rather than
// returning early with an empty subgraph.
func TestGetImpactRadius_ChildlessContainer(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	nodes := []types.Node{
		makeNode("childlessFile", "file", "empty.go", "empty.go", false),
		makeNode("callerOfFile", "function", "Caller", "caller.go", false),
	}
	for _, n := range nodes {
		if err := d.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.ID, err)
		}
	}
	if _, err := d.InsertEdge(ctx, makeEdge("callerOfFile", "childlessFile", types.EdgeKindCalls)); err != nil {
		t.Fatalf("insert edge: %v", err)
	}

	m := graph.NewManager(d)
	sg, err := m.GetImpactRadius(ctx, "childlessFile", 3)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
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

// The fromID==toID shortcut must still surface a node-not-found error.
func TestFindPath_SelfPath_ErrorPropagates(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	m := graph.NewManager(d)
	_, err := m.FindPath(ctx, "ghost", "ghost", nil)
	if err == nil {
		t.Error("FindPath(ghost, ghost): expected error for missing node, got nil")
	}
}

// An unrecognised direction must error rather than silently pick bfsIncoming.
func TestGetTypeHierarchy_UnknownDirection(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	_, err := m.GetTypeHierarchy(ctx, "nodeClass", "bogus")
	if err == nil {
		t.Error("GetTypeHierarchy with direction='bogus': expected error, got nil")
	}
}

// Three files, so adjacency-map iteration order actually varies between runs.
func TestFindCircularDependencies_Deterministic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

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

	for i := 1; i < len(cycles1); i++ {
		if len(cycles1[i]) > 0 && len(cycles1[i-1]) > 0 {
			if cycles1[i][0] < cycles1[i-1][0] {
				t.Errorf("cycles not sorted: cycles[%d][0]=%s < cycles[%d][0]=%s", i, cycles1[i][0], i-1, cycles1[i-1][0])
			}
		}
	}
}

// Node IDs are chosen so all three dead-code kinds contribute, exercising the
// kind-iteration order FindDeadCode has to keep stable.
func TestFindDeadCode_Deterministic(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

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

	for i := 1; i < len(dead1); i++ {
		if dead1[i].ID < dead1[i-1].ID {
			t.Errorf("dead code not sorted: dead[%d].ID=%s < dead[%d].ID=%s", i, dead1[i].ID, i-1, dead1[i-1].ID)
		}
	}
}

// First-level edges name startID, so it must resolve in Nodes. serve papers over
// a hollow endpoint by substituting the root, but CLI and MCP callers do not.
func TestGetCallers_StartNodeHydrated(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	sg, err := m.GetCallers(ctx, "nodeC", 2)
	if err != nil {
		t.Fatalf("GetCallers: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeC"] {
		t.Errorf("expected start node nodeC hydrated in Nodes, got %v", ids)
	}
	assertEdgeEndpointsResolve(t, sg)
}

func TestGetCallees_StartNodeHydrated(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

	sg, err := m.GetCallees(ctx, "nodeA", 2)
	if err != nil {
		t.Fatalf("GetCallees: %v", err)
	}
	ids := subgraphNodeIDs(sg)
	if !ids["nodeA"] {
		t.Errorf("expected start node nodeA hydrated in Nodes, got %v", ids)
	}
	assertEdgeEndpointsResolve(t, sg)
}

func TestGetCallers_DeterministicOrder(t *testing.T) {
	d := openTestDB(t)
	m := buildFixture(t, d)
	ctx := context.Background()

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
