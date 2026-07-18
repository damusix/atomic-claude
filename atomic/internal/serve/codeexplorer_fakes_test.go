package serve_test

// codeexplorer_fakes_test.go — shared CodeEngine test doubles for the
// /api/code/* JSON handler tests (api_code_test.go). Extracted from the
// pre-cutover codeexplorer_test.go (HTML /code/* handler tests, deleted at
// cutover) since api_code_test.go depends on these seams.

import (
	"context"
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── Fake CodeEngine ─────────────────────────────────────────────────────────

// fakeCodeEngine is a stub CodeEngine for tests.
type fakeCodeEngine struct {
	node          types.Node
	nodesByName   []types.Node
	callers       types.Subgraph
	callees       types.Subgraph
	impact        types.Subgraph
	files         []types.FileRecord
	nodesInFile   []types.Node
	nodesByKind   map[types.NodeKind][]types.Node
	allNodes      []types.Node
	allEdges      []types.Edge
	nodeErr       error
	callersErr    error
	calleesErr    error
	impactErr     error
	subgraphDepth int // last depth passed to callers/callees/impact
}

func (f *fakeCodeEngine) SearchNodes(_ context.Context, _ types.SearchOptions) ([]types.SearchResult, error) {
	return nil, nil
}
func (f *fakeCodeEngine) GetNode(_ context.Context, _ string) (types.Node, error) {
	return f.node, f.nodeErr
}
func (f *fakeCodeEngine) GetNodesByName(_ context.Context, _ string, _ types.NodeKind) ([]types.Node, error) {
	return f.nodesByName, f.nodeErr
}
func (f *fakeCodeEngine) GetCallers(_ context.Context, _ string, depth int) (types.Subgraph, error) {
	f.subgraphDepth = depth
	return f.callers, f.callersErr
}
func (f *fakeCodeEngine) GetCallees(_ context.Context, _ string, depth int) (types.Subgraph, error) {
	f.subgraphDepth = depth
	return f.callees, f.calleesErr
}
func (f *fakeCodeEngine) GetImpactRadius(_ context.Context, _ string, depth int) (types.Subgraph, error) {
	f.subgraphDepth = depth
	return f.impact, f.impactErr
}
func (f *fakeCodeEngine) GetFiles(_ context.Context) ([]types.FileRecord, error) {
	return f.files, nil
}
func (f *fakeCodeEngine) GetNodesInFile(_ context.Context, _ string) ([]types.Node, error) {
	return f.nodesInFile, nil
}
func (f *fakeCodeEngine) GetNodesByKind(_ context.Context, kind types.NodeKind) ([]types.Node, error) {
	return f.nodesByKind[kind], nil
}
func (f *fakeCodeEngine) GetOutgoingEdges(_ context.Context, _ string) ([]types.Edge, error) {
	return nil, nil
}
func (f *fakeCodeEngine) GetAllNodes(_ context.Context) ([]types.Node, error) {
	return f.allNodes, f.nodeErr
}
func (f *fakeCodeEngine) GetAllEdges(_ context.Context) ([]types.Edge, error) {
	return f.allEdges, f.nodeErr
}
func (f *fakeCodeEngine) Close() {}

// fakeProviderFor wraps a fake engine as an EngineProvider.
func fakeProviderFor(eng serve.CodeEngine) serve.EngineProvider {
	return func(_ context.Context, _, _ string) (serve.CodeEngine, error) {
		return eng, nil
	}
}

// richFakeCodeEngine supports schema testing with outgoing edges per node.
type richFakeCodeEngine struct {
	tableNodes     []types.Node
	viewNodes      []types.Node
	procedureNodes []types.Node
	nodes          map[string]types.Node
	outgoingEdges  map[string][]types.Edge
}

func (r *richFakeCodeEngine) SearchNodes(_ context.Context, _ types.SearchOptions) ([]types.SearchResult, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetNode(_ context.Context, id string) (types.Node, error) {
	if n, ok := r.nodes[id]; ok {
		return n, nil
	}
	return types.Node{}, fmt.Errorf("node not found: %s", id)
}
func (r *richFakeCodeEngine) GetNodesByName(_ context.Context, _ string, _ types.NodeKind) ([]types.Node, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetCallers(_ context.Context, _ string, _ int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (r *richFakeCodeEngine) GetCallees(_ context.Context, _ string, _ int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (r *richFakeCodeEngine) GetImpactRadius(_ context.Context, _ string, _ int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (r *richFakeCodeEngine) GetFiles(_ context.Context) ([]types.FileRecord, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetNodesInFile(_ context.Context, _ string) ([]types.Node, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetNodesByKind(_ context.Context, kind types.NodeKind) ([]types.Node, error) {
	switch kind {
	case types.NodeKindTable:
		return r.tableNodes, nil
	case types.NodeKindView:
		return r.viewNodes, nil
	case types.NodeKindProcedure:
		return r.procedureNodes, nil
	}
	return nil, nil
}
func (r *richFakeCodeEngine) GetOutgoingEdges(_ context.Context, nodeID string) ([]types.Edge, error) {
	return r.outgoingEdges[nodeID], nil
}
func (r *richFakeCodeEngine) GetAllNodes(_ context.Context) ([]types.Node, error) { return nil, nil }
func (r *richFakeCodeEngine) GetAllEdges(_ context.Context) ([]types.Edge, error) { return nil, nil }
func (r *richFakeCodeEngine) Close()                                              {}
