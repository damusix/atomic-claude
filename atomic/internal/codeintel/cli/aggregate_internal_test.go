package cli

// Internal tests for aggregateSymbolGraph — the fix for callers/callees/impact
// dropping results when a symbol name maps to more than one definition.
//
// WHY this exists: `callers $proc` returned nothing on a real repo even though
// 37 caller edges existed, because the query used only nodes[0] — and the first
// `$proc` node (an accessor with zero callers) shadowed the second `$proc`
// definition that owned all the callers. A symbol name routinely maps to several
// nodes (overloads, interface + impl, two classes with a same-named method), so
// the query must aggregate across every match, not just the first.

import (
	"errors"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// TestAggregateSymbolGraph_MergesCallersOnNonFirstMatch is the direct regression
// gate: the callers live ONLY on the second matching node. The old nodes[0]-only
// query dropped them; aggregateSymbolGraph must union all matches.
func TestAggregateSymbolGraph_MergesCallersOnNonFirstMatch(t *testing.T) {
	nodes := []types.Node{{ID: "proc-1"}, {ID: "proc-2"}}
	perNode := map[string]types.Subgraph{
		// First match: a definition with no callers (the shadowing accessor).
		"proc-1": {
			Nodes: map[string]types.Node{"proc-1": {ID: "proc-1"}},
			Roots: []string{"proc-1"},
		},
		// Second match: the definition that actually owns the callers.
		"proc-2": {
			Nodes: map[string]types.Node{
				"proc-2": {ID: "proc-2"},
				"caller": {ID: "caller", Name: "claim"},
			},
			Edges: []types.Edge{{Source: "caller", Target: "proc-2", Kind: types.EdgeKindCalls, Line: 23}},
			Roots: []string{"proc-2"},
		},
	}

	calls := 0
	got, err := aggregateSymbolGraph(nodes, func(id string) (types.Subgraph, error) {
		calls++
		return perNode[id], nil
	})
	if err != nil {
		t.Fatalf("aggregateSymbolGraph: %v", err)
	}
	if calls != 2 {
		t.Errorf("query must run for every match: got %d calls, want 2", calls)
	}
	if _, ok := got.Nodes["caller"]; !ok {
		t.Error("caller node from the second match was dropped — aggregation must union ALL matches, not just nodes[0]")
	}
	if len(got.Edges) != 1 {
		t.Errorf("merged subgraph must keep the caller edge: got %d edges, want 1", len(got.Edges))
	}
	if len(got.Roots) != 2 {
		t.Errorf("both matched definitions must appear as roots: got %d, want 2", len(got.Roots))
	}
}

// TestAggregateSymbolGraph_DedupsSharedEdges proves an edge reachable from more
// than one root is emitted once (no double-counting when matches overlap).
func TestAggregateSymbolGraph_DedupsSharedEdges(t *testing.T) {
	nodes := []types.Node{{ID: "a"}, {ID: "b"}}
	shared := types.Edge{Source: "x", Target: "y", Kind: types.EdgeKindCalls, Line: 3}
	perNode := map[string]types.Subgraph{
		"a": {Nodes: map[string]types.Node{"x": {ID: "x"}, "y": {ID: "y"}}, Edges: []types.Edge{shared}, Roots: []string{"a"}},
		"b": {Nodes: map[string]types.Node{"x": {ID: "x"}, "y": {ID: "y"}}, Edges: []types.Edge{shared}, Roots: []string{"b"}},
	}

	got, err := aggregateSymbolGraph(nodes, func(id string) (types.Subgraph, error) {
		return perNode[id], nil
	})
	if err != nil {
		t.Fatalf("aggregateSymbolGraph: %v", err)
	}
	if len(got.Edges) != 1 {
		t.Errorf("the same edge from two roots must dedup to 1, got %d", len(got.Edges))
	}
}

// TestAggregateSymbolGraph_PropagatesError proves an error from any per-node
// query is surfaced, not swallowed (a partial graph would be misleading).
func TestAggregateSymbolGraph_PropagatesError(t *testing.T) {
	nodes := []types.Node{{ID: "a"}, {ID: "b"}}
	_, err := aggregateSymbolGraph(nodes, func(id string) (types.Subgraph, error) {
		if id == "b" {
			return types.Subgraph{}, errors.New("boom")
		}
		return types.Subgraph{Nodes: map[string]types.Node{"a": {ID: "a"}}, Roots: []string{"a"}}, nil
	})
	if err == nil {
		t.Error("error from a per-node query must propagate")
	}
}
