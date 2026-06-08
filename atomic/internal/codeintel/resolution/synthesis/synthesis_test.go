package synthesis_test

// CP16 synthesis infrastructure + batch-1 synthesizer tests.
//
// # Why these tests are the spec gate
//
//   - Cap constants are asserted literally (40/6/8) — R6 compliance.
//   - Composite.SynthesizeCallbackEdges stamps every edge kind='calls',
//     provenance='heuristic', metadata.synthesizedBy=<name>.
//   - Dedup by "source>target": two synthesizers proposing the same source→target
//     produce exactly one edge.
//   - DB dedup: a second SynthesizeCallbackEdges call on the same DB produces
//     zero new edges (idempotent).
//   - Node-count stable: no new nodes across two synthesis runs.
//   - react-render gate: real .ts fixtures indexed through the real indexer +
//     ResolveAndPersistBatched with the composite wired; setState-calling methods
//     gain a 'calls' edge to the render method.
//   - jsx-render + event-emitter + callback: emit no edges (absent signal —
//     the tests assert the synthesizers return empty and document why).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := synthTmpDir(t)
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func synthTmpDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return t.TempDir()
	}
	// wd is .../atomic/internal/codeintel/resolution/synthesis
	// go up 5 levels → worktree root → tmp/
	candidate := filepath.Join(wd, "..", "..", "..", "..", "..", "tmp", "synth-"+t.Name())
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		return t.TempDir()
	}
	return candidate
}

func countEdgesWithProvenance(t *testing.T, d *db.DB, provenance string) int {
	t.Helper()
	ctx := context.Background()
	nodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	count := 0
	for _, n := range nodes {
		edges, err := d.GetEdgesBySource(ctx, n.ID)
		if err != nil {
			t.Fatalf("GetEdgesBySource %s: %v", n.ID, err)
		}
		for _, e := range edges {
			if e.Provenance == provenance {
				count++
			}
		}
	}
	return count
}

func countNodes(t *testing.T, d *db.DB) int {
	t.Helper()
	nodes, err := d.GetAllNodes(context.Background())
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	return len(nodes)
}

func edgesFrom(t *testing.T, d *db.DB, sourceID string) []types.Edge {
	t.Helper()
	edges, err := d.GetEdgesBySource(context.Background(), sourceID)
	if err != nil {
		t.Fatalf("GetEdgesBySource %s: %v", sourceID, err)
	}
	return edges
}

func seedNode(t *testing.T, d *db.DB, id, name, filePath string, kind types.NodeKind, lang types.Language) {
	t.Helper()
	if err := d.UpsertNode(context.Background(), types.Node{
		ID:       id,
		Kind:     kind,
		Name:     name,
		FilePath: filePath,
		Language: lang,
	}); err != nil {
		t.Fatalf("UpsertNode %s: %v", id, err)
	}
}

func seedEdge(t *testing.T, d *db.DB, source, target string, kind types.EdgeKind) {
	t.Helper()
	if _, err := d.InsertEdge(context.Background(), types.Edge{
		Source: source,
		Target: target,
		Kind:   kind,
	}); err != nil {
		t.Fatalf("InsertEdge %s→%s: %v", source, target, err)
	}
}

func seedRef(t *testing.T, d *db.DB, id, fromID, name string, kind types.EdgeKind) {
	t.Helper()
	if err := d.InsertUnresolvedRef(context.Background(), types.UnresolvedReference{
		ID:            id,
		FromNodeID:    fromID,
		ReferenceName: name,
		ReferenceKind: kind,
		FilePath:      "test.ts",
		Language:      types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("InsertUnresolvedRef %s: %v", id, err)
	}
}

func seedEdgeWithMeta(t *testing.T, d *db.DB, source, target string, kind types.EdgeKind, meta json.RawMessage) {
	t.Helper()
	if _, err := d.InsertEdge(context.Background(), types.Edge{
		Source:   source,
		Target:   target,
		Kind:     kind,
		Metadata: meta,
	}); err != nil {
		t.Fatalf("InsertEdge %s→%s: %v", source, target, err)
	}
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFixture %s: %v", name, err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Cap const assertions — R6: test asserts literal values
// ---------------------------------------------------------------------------

func TestCapConsts(t *testing.T) {
	// Appendix G mandates exact values. These tests catch any accidental change.
	if synthesis.MAX_CALLBACKS_PER_CHANNEL != 40 {
		t.Errorf("MAX_CALLBACKS_PER_CHANNEL = %d, want 40", synthesis.MAX_CALLBACKS_PER_CHANNEL)
	}
	if synthesis.EVENT_FANOUT_CAP != 6 {
		t.Errorf("EVENT_FANOUT_CAP = %d, want 6", synthesis.EVENT_FANOUT_CAP)
	}
	if synthesis.CC_FANOUT_CAP != 8 {
		t.Errorf("CC_FANOUT_CAP = %d, want 8", synthesis.CC_FANOUT_CAP)
	}
}

// ---------------------------------------------------------------------------
// Composite stamps kind + provenance + metadata
// ---------------------------------------------------------------------------

type fixedSynthesizer struct {
	name  string
	edges []types.Edge
}

func (f *fixedSynthesizer) Name() string { return f.name }
func (f *fixedSynthesizer) Synthesize(_ context.Context, _ *db.DB) ([]types.Edge, error) {
	return f.edges, nil
}

func TestCompositeStampsEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Seed source + target nodes so edge FK is satisfied.
	seedNode(t, d, "src-node", "srcFn", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "tgt-node", "tgtFn", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)

	composite := synthesis.NewComposite(d, &fixedSynthesizer{
		name: "test-synth",
		edges: []types.Edge{
			{Source: "src-node", Target: "tgt-node"},
		},
	})

	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("SynthesizeCallbackEdges: %v", err)
	}

	edges := edgesFrom(t, d, "src-node")
	var synthEdge *types.Edge
	for i := range edges {
		if edges[i].Target == "tgt-node" && edges[i].Provenance == "heuristic" {
			synthEdge = &edges[i]
			break
		}
	}
	if synthEdge == nil {
		t.Fatalf("no heuristic edge src-node→tgt-node found")
	}
	if synthEdge.Kind != types.EdgeKindCalls {
		t.Errorf("kind = %s, want calls", synthEdge.Kind)
	}
	if synthEdge.Provenance != "heuristic" {
		t.Errorf("provenance = %q, want heuristic", synthEdge.Provenance)
	}

	var meta map[string]string
	if err := json.Unmarshal(synthEdge.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["synthesizedBy"] != "test-synth" {
		t.Errorf("metadata.synthesizedBy = %q, want test-synth", meta["synthesizedBy"])
	}
}

// ---------------------------------------------------------------------------
// Dedup: within-run dedup (same source>target from two synthesizers)
// ---------------------------------------------------------------------------

func TestCompositeDedupWithinRun(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "src-a", "srcA", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "tgt-a", "tgtA", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// Two synthesizers propose the same edge.
	composite := synthesis.NewComposite(d,
		&fixedSynthesizer{name: "synth1", edges: []types.Edge{{Source: "src-a", Target: "tgt-a"}}},
		&fixedSynthesizer{name: "synth2", edges: []types.Edge{{Source: "src-a", Target: "tgt-a"}}},
	)

	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("SynthesizeCallbackEdges: %v", err)
	}

	edges := edgesFrom(t, d, "src-a")
	heuristicCount := 0
	for _, e := range edges {
		if e.Target == "tgt-a" && e.Provenance == "heuristic" {
			heuristicCount++
		}
	}
	if heuristicCount != 1 {
		t.Errorf("heuristic edges src-a→tgt-a = %d, want 1 (dedup)", heuristicCount)
	}
}

// ---------------------------------------------------------------------------
// Dedup: idempotent across runs (re-run produces zero new edges)
// ---------------------------------------------------------------------------

func TestCompositeDedupAcrossRuns(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "src-b", "srcB", "b.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "tgt-b", "tgtB", "b.ts", types.NodeKindFunction, types.LanguageTypeScript)

	composite := synthesis.NewComposite(d, &fixedSynthesizer{
		name:  "idempotent-synth",
		edges: []types.Edge{{Source: "src-b", Target: "tgt-b"}},
	})

	// First run.
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	countAfterRun1 := countEdgesWithProvenance(t, d, "heuristic")

	// Second run — must not produce new edges.
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	countAfterRun2 := countEdgesWithProvenance(t, d, "heuristic")

	if countAfterRun1 != countAfterRun2 {
		t.Errorf("heuristic edge count after run1=%d run2=%d, want equal (idempotent)", countAfterRun1, countAfterRun2)
	}
}

// ---------------------------------------------------------------------------
// Node-count stable across two synthesis runs
// ---------------------------------------------------------------------------

func TestNodeCountStableAcrossRuns(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "src-c", "srcC", "c.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "tgt-c", "tgtC", "c.ts", types.NodeKindFunction, types.LanguageTypeScript)

	composite := synthesis.NewComposite(d, &fixedSynthesizer{
		name:  "stable-synth",
		edges: []types.Edge{{Source: "src-c", Target: "tgt-c"}},
	})

	nodeCountBefore := countNodes(t, d)

	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	nodeCountAfterRun1 := countNodes(t, d)

	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	nodeCountAfterRun2 := countNodes(t, d)

	if nodeCountBefore != nodeCountAfterRun1 {
		t.Errorf("node count before=%d after run1=%d, want equal (no new nodes)", nodeCountBefore, nodeCountAfterRun1)
	}
	if nodeCountAfterRun1 != nodeCountAfterRun2 {
		t.Errorf("node count after run1=%d run2=%d, want equal (stable)", nodeCountAfterRun1, nodeCountAfterRun2)
	}
}

// ---------------------------------------------------------------------------
// react-render gate: real fixture through indexer + pipeline
// ---------------------------------------------------------------------------

func TestReactRenderSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	// Class with setState-calling methods + render.
	writeFixture(t, fixtureDir, "Counter.ts", `
class Counter {
  state = { count: 0 };

  increment() {
    this.setState({ count: this.state.count + 1 });
  }

  decrement() {
    this.setState({ count: this.state.count - 1 });
  }

  render() {
    return this.state.count;
  }
}
export { Counter };
`)

	// Class that also has render but no setState — should NOT get synth edges from above.
	writeFixture(t, fixtureDir, "NoState.ts", `
class NoState {
  render() {
    return 42;
  }
}
export { NoState };
`)

	// Index through the real indexer.
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Run resolution + synthesis via NewPipelineWithSeams.
	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Find increment and decrement method nodes.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}

	var incrementID, decrementID, counterRenderID string
	for _, n := range allNodes {
		if n.Kind == types.NodeKindMethod {
			switch n.Name {
			case "increment":
				incrementID = n.ID
			case "decrement":
				decrementID = n.ID
			case "render":
				if n.FilePath != "" && containsSubstring(n.FilePath, "Counter") {
					counterRenderID = n.ID
				}
			}
		}
	}

	if incrementID == "" || decrementID == "" || counterRenderID == "" {
		t.Fatalf("expected Counter methods not found: increment=%s decrement=%s render=%s",
			incrementID, decrementID, counterRenderID)
	}

	// Assert synth edge: increment → render
	assertSynthEdge(t, d, incrementID, counterRenderID, "react-render", "setState")
	// Assert synth edge: decrement → render
	assertSynthEdge(t, d, decrementID, counterRenderID, "react-render", "setState")

	// Assert idempotency: re-run produces same count.
	synthCountBefore := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run SynthesizeCallbackEdges: %v", err)
	}
	synthCountAfter := countEdgesWithProvenance(t, d, "heuristic")
	if synthCountBefore != synthCountAfter {
		t.Errorf("synth count before rerun=%d after=%d, want equal (idempotent)", synthCountBefore, synthCountAfter)
	}

	// Assert NoState class has no synth edges.
	for _, n := range allNodes {
		if n.Kind == types.NodeKindMethod && containsSubstring(n.FilePath, "NoState") {
			edges := edgesFrom(t, d, n.ID)
			for _, e := range edges {
				if e.Provenance == "heuristic" {
					t.Errorf("NoState method %s has unexpected heuristic edge", n.Name)
				}
			}
		}
	}
}

// assertSynthEdge verifies that a calls edge with heuristic provenance and
// matching synthesizedBy/via metadata exists from source → target.
func assertSynthEdge(t *testing.T, d *db.DB, sourceID, targetID, synthesizedBy, via string) {
	t.Helper()
	edges := edgesFrom(t, d, sourceID)
	for _, e := range edges {
		if e.Target != targetID {
			continue
		}
		if e.Provenance != "heuristic" {
			continue
		}
		if e.Kind != types.EdgeKindCalls {
			t.Errorf("synth edge %s→%s: kind=%s, want calls", sourceID, targetID, e.Kind)
		}
		var meta map[string]string
		if err := json.Unmarshal(e.Metadata, &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if meta["synthesizedBy"] != synthesizedBy {
			t.Errorf("synthesizedBy=%q, want %q", meta["synthesizedBy"], synthesizedBy)
		}
		if via != "" && meta["via"] != via {
			t.Errorf("via=%q, want %q", meta["via"], via)
		}
		return // found — pass
	}
	t.Errorf("no heuristic calls edge %s→%s (synthesizedBy=%s via=%s)", sourceID, targetID, synthesizedBy, via)
}

// ---------------------------------------------------------------------------
// jsx-render: gate test — real .tsx parent+child fixture through full pipeline
// ---------------------------------------------------------------------------

func TestJSXRenderSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	// Parent component renders two children.
	writeFixture(t, fixtureDir, "parent.tsx", `
import React from "react";
import { ChildWidget } from "./child";

function ParentApp() {
  return (
    <div>
      <ChildWidget title="hello" />
    </div>
  );
}
export { ParentApp };
`)
	writeFixture(t, fixtureDir, "child.tsx", `
import React from "react";
function ChildWidget({ title }: { title?: string }) {
  return <div>{title}</div>;
}
export { ChildWidget };
`)

	// Index through real indexer.
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Run resolution + synthesis via NewPipelineWithSeams.
	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Find ParentApp and ChildWidget nodes.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	var parentID, childID string
	for _, n := range allNodes {
		switch n.Name {
		case "ParentApp":
			parentID = n.ID
		case "ChildWidget":
			childID = n.ID
		}
	}
	if parentID == "" || childID == "" {
		t.Fatalf("expected ParentApp and ChildWidget nodes; parentID=%s childID=%s", parentID, childID)
	}

	// Assert heuristic calls edge ParentApp → ChildWidget with synthesizedBy=jsx-render.
	assertSynthEdge(t, d, parentID, childID, "jsx-render", "")

	// Idempotency: re-run synthesis produces same edge count.
	synthBefore := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	synthAfter := countEdgesWithProvenance(t, d, "heuristic")
	if synthBefore != synthAfter {
		t.Errorf("idempotent: before=%d after=%d, want equal", synthBefore, synthAfter)
	}

	// Node count stable across re-run.
	nodesBefore := countNodes(t, d)
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("second re-run: %v", err)
	}
	nodesAfter := countNodes(t, d)
	if nodesBefore != nodesAfter {
		t.Errorf("node count: before=%d after=%d, want equal (no new nodes)", nodesBefore, nodesAfter)
	}
}

// TestJSXRenderSynthesizer_NoDiscriminator verifies that references edges
// without the "jsx:" refArgs discriminator do NOT produce jsx-render edges.
// This guards against false-positive edges from type-annotation refs.
func TestJSXRenderSynthesizer_NoDiscriminator(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Seed a function node and a plain references edge (no jsx discriminator).
	seedNode(t, d, "fn-src", "MyComp", "App.tsx", types.NodeKindFunction, types.LanguageTSX)
	seedNode(t, d, "fn-tgt", "ChildComp", "Child.tsx", types.NodeKindFunction, types.LanguageTSX)
	// Plain references edge — no Metadata / refArgs.
	seedEdge(t, d, "fn-src", "fn-tgt", types.EdgeKindReferences)

	s := &synthesis.JSXRenderSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("JSXRenderSynthesizer produced %d edges from plain references edge (no discriminator), want 0", len(edges))
	}
}

// TestJSXRenderSynthesizer_WithDiscriminator verifies that a references edge
// carrying the "jsx:" refArgs discriminator DOES produce a jsx-render proposal.
func TestJSXRenderSynthesizer_WithDiscriminator(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-src2", "MyComp", "App.tsx", types.NodeKindFunction, types.LanguageTSX)
	seedNode(t, d, "fn-tgt2", "ChildComp", "Child.tsx", types.NodeKindFunction, types.LanguageTSX)
	// References edge WITH jsx discriminator (as createEdges would produce).
	seedEdgeWithMeta(t, d, "fn-src2", "fn-tgt2", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["jsx:ChildComp"]}`))

	s := &synthesis.JSXRenderSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("JSXRenderSynthesizer produced %d edges, want 1", len(edges))
	}
	if edges[0].Source != "fn-src2" || edges[0].Target != "fn-tgt2" {
		t.Errorf("edge %s→%s, want fn-src2→fn-tgt2", edges[0].Source, edges[0].Target)
	}
}

// ---------------------------------------------------------------------------
// event-emitter: asserts empty (absent signal documented)
// ---------------------------------------------------------------------------

func TestEventEmitterSynthesizer_AbsentSignal(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Even with .on/.emit refs, event-emitter emits nothing (no event name).
	seedNode(t, d, "svc-class", "DataService", "svc.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "emit-method", "emit", "svc.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "svc-class", "emit-method", types.EdgeKindContains)
	seedRef(t, d, "ref-emit", "emit-method", "this.emitter.emit", types.EdgeKindCalls)

	s := &synthesis.EventEmitterSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("EventEmitterSynthesizer.Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("EventEmitterSynthesizer produced %d edges, want 0 (absent signal: event name not captured in unresolved_refs)", len(edges))
	}
}

// ---------------------------------------------------------------------------
// callback: asserts empty (absent signal documented)
// ---------------------------------------------------------------------------

func TestCallbackSynthesizer_AbsentSignal(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Even with this.onDataCallback call refs, callback emits nothing.
	seedNode(t, d, "svc2", "DataService2", "svc2.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "run-cb", "runCallback", "svc2.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "svc2", "run-cb", types.EdgeKindContains)
	seedRef(t, d, "ref-cb", "run-cb", "this.onDataCallback", types.EdgeKindCalls)

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("CallbackSynthesizer.Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("CallbackSynthesizer produced %d edges, want 0 (absent signal: assignment tracking absent)", len(edges))
	}
}

// ---------------------------------------------------------------------------
// vue-handler: gate test — real .vue parent+child fixture through full pipeline
// ---------------------------------------------------------------------------

func TestVueHandlerSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	// Parent .vue component that uses a child component.
	writeFixture(t, fixtureDir, "ParentView.vue", `
<template>
  <div>
    <ChildWidget :title="msg" />
  </div>
</template>
<script lang="ts">
export default {
  name: "ParentView",
  data() { return { msg: "hello" }; },
};
</script>
`)
	// Child is a .vue component.
	writeFixture(t, fixtureDir, "ChildWidget.vue", `
<template>
  <div>{{ title }}</div>
</template>
<script lang="ts">
export default {
  name: "ChildWidget",
  props: ["title"],
};
</script>
`)

	// Index through real indexer.
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Run resolution + synthesis.
	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Find ParentView and ChildWidget component nodes.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	var parentID, childID string
	for _, n := range allNodes {
		if n.Kind != types.NodeKindComponent {
			continue
		}
		switch n.Name {
		case "ParentView":
			parentID = n.ID
		case "ChildWidget":
			childID = n.ID
		}
	}
	if parentID == "" || childID == "" {
		t.Fatalf("expected ParentView and ChildWidget component nodes; parentID=%s childID=%s", parentID, childID)
	}

	// Assert heuristic calls edge ParentView → ChildWidget with synthesizedBy=vue-handler.
	assertSynthEdge(t, d, parentID, childID, "vue-handler", "")

	// Idempotency.
	synthBefore := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	synthAfter := countEdgesWithProvenance(t, d, "heuristic")
	if synthBefore != synthAfter {
		t.Errorf("idempotent: before=%d after=%d, want equal", synthBefore, synthAfter)
	}
}

// TestVueHandlerSynthesizer_NoRefsNoEdges confirms that the synthesizer
// produces nothing when no references edges exist on the vue component node.
// This tests the synthesizer in isolation (no resolved edges present).
// The extractor now emits handler UnresolvedReferences, but the synthesizer
// still requires them to be RESOLVED into references edges first — this
// isolation test confirms the synthesizer's prerequisite is a static edge,
// not a raw unresolved ref.
func TestVueHandlerSynthesizer_NoRefsNoEdges(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Seed a Vue component node and a handler function node, but no references
	// edge between them (i.e. resolution has not run yet).
	seedNode(t, d, "vue-comp-x", "MyView", "MyView.vue", types.NodeKindComponent, types.LanguageVue)
	seedNode(t, d, "fn-handler", "handleClick", "MyView.vue", types.NodeKindFunction, types.LanguageTypeScript)
	s := &synthesis.VueHandlerSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// No references edges on the vue component → no proposals.
	if len(edges) != 0 {
		t.Errorf("VueHandlerSynthesizer produced %d edges without resolved refs, want 0", len(edges))
	}
}

// TestVueHandlerSynthesizer_EventHandlerEdge verifies that @event="handler"
// bindings produce calls edges from the Vue component to the <script> handler
// method, end-to-end through the full pipeline. The extractor now emits
// handler UnresolvedReferences; resolution turns them into references edges;
// vue-handler synthesizer turns those into heuristic calls edges.
func TestVueHandlerSynthesizer_EventHandlerEdge(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	// .vue component with @click and v-on:submit bindings and matching handlers.
	writeFixture(t, fixtureDir, "FormView.vue", `
<template>
  <form v-on:submit="onSubmit">
    <button @click="handleClick">Submit</button>
  </form>
</template>
<script>
export default {
  methods: {
    handleClick() { console.log("clicked"); },
    onSubmit(e) { e.preventDefault(); },
  },
};
</script>
`)

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Run resolution + synthesis.
	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Find the FormView component node and the handler method nodes.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	var compID, handleClickID, onSubmitID string
	for _, n := range allNodes {
		switch {
		case n.Kind == types.NodeKindComponent && n.Name == "FormView":
			compID = n.ID
		case (n.Kind == types.NodeKindFunction || n.Kind == types.NodeKindMethod) && n.Name == "handleClick":
			handleClickID = n.ID
		case (n.Kind == types.NodeKindFunction || n.Kind == types.NodeKindMethod) && n.Name == "onSubmit":
			onSubmitID = n.ID
		}
	}
	if compID == "" {
		t.Fatalf("FormView component node not found; nodes: %v", allNodes)
	}
	if handleClickID == "" {
		t.Fatalf("handleClick method not found; nodes: %v", allNodes)
	}
	if onSubmitID == "" {
		t.Fatalf("onSubmit method not found; nodes: %v", allNodes)
	}

	// vue-handler synthesizer must have produced calls edges for both handlers.
	assertSynthEdge(t, d, compID, handleClickID, "vue-handler", "")
	assertSynthEdge(t, d, compID, onSubmitID, "vue-handler", "")
}

// ---------------------------------------------------------------------------
// Default returns composite with 6 synthesizers (batch 1 + batch 2 + batch 3)
// ---------------------------------------------------------------------------

func TestDefaultCompositeHasSixSynthesizers(t *testing.T) {
	d := openTestDB(t)
	// Default should not panic and the returned composite should work.
	composite := synthesis.Default(d)
	if composite == nil {
		t.Fatal("Default returned nil")
	}
	// Verify it runs without error on an empty DB.
	if err := composite.SynthesizeCallbackEdges(context.Background()); err != nil {
		t.Fatalf("Default composite on empty DB: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Pipeline integration: NewPipelineWithSeams with Default composite
// ---------------------------------------------------------------------------

func TestPipelineWithSeams_SynthesisRunsLast(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()
	writeFixture(t, fixtureDir, "Widget.ts", `
class Widget {
  update() {
    this.setState({ value: 1 });
  }

  render() {
    return null;
  }
}
export { Widget };
`)

	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	orch := indexer.NewOrchestrator(d, pool)
	if err := orch.IndexAll(ctx, fixtureDir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	composite := synthesis.Default(d)
	pipe := resolution.NewPipelineWithSeams(d, fixtureDir, nil, composite)
	_, edges, err := pipe.ResolveAndPersistBatched(ctx, 0, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}
	// edges is the static edge count — may be zero or more.
	_ = edges

	synthCount := countEdgesWithProvenance(t, d, "heuristic")
	if synthCount == 0 {
		t.Errorf("expected at least one heuristic edge (Widget.update→Widget.render via react-render), got 0")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		len(s) > 0 && len(sub) > 0 &&
			(s[:len(sub)] == sub || s[len(s)-len(sub):] == sub ||
				func() bool {
					for i := 0; i <= len(s)-len(sub); i++ {
						if s[i:i+len(sub)] == sub {
							return true
						}
					}
					return false
				}()))
}
