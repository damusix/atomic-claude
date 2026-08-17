package synthesis_test

// CallbackSynthesizer tests, plus the zero-edge assertions for the two
// synthesizers whose signal is absent. Those tests exist deliberately: a
// synthesizer that emits nothing must be provably inert rather than quietly
// broken, and the assertion flips to real edges once the signal lands.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Seeds the graph state the real pipeline produces for
// `this.onData = handleData` / `this.onData(chunk)`. Manual seeding keeps the
// correlation logic under test rather than the pipeline that feeds it.
func TestCallbackSynthesizer_Unit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor", "constructor", "pipeline.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "process", "processChunk", "pipeline.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "handle-data", "handleData", "pipeline.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// The registration edge resolution produces.
	seedEdgeWithMeta(t, d, "ctor", "handle-data", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onData"]}`))

	// The invocation, which stays unresolved forever: "this.onData" is not a
	// node name.
	seedRefWithArgs(t, d, "ref-invoke-onData", "process", "this.onData", types.EdgeKindCalls, nil)

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1; edges=%v", len(edges), edges)
	}
	e := edges[0]
	if e.Source != "process" {
		t.Errorf("source=%q, want process (invoker method)", e.Source)
	}
	if e.Target != "handle-data" {
		t.Errorf("target=%q, want handle-data (registered callable node)", e.Target)
	}
	var meta map[string]string
	if err := json.Unmarshal(e.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["field"] != "onData" {
		t.Errorf("metadata.field=%q, want onData", meta["field"])
	}
}

// Some code styles invoke the field without a "this." prefix.
func TestCallbackSynthesizer_BareFieldName(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor2", "constructor", "a.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "run2", "run", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "cb2", "myCallback", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)

	seedEdgeWithMeta(t, d, "ctor2", "cb2", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:cb"]}`))

	seedRefWithArgs(t, d, "ref-bare-cb", "run2", "cb", types.EdgeKindCalls, nil)

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1 (bare field name)", len(edges))
	}
	if edges[0].Source != "run2" || edges[0].Target != "cb2" {
		t.Errorf("edge %s→%s, want run2→cb2", edges[0].Source, edges[0].Target)
	}
}

// A registration with nothing invoking it is not a dispatch.
func TestCallbackSynthesizer_NoEdgeWithoutCallRef(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor3", "constructor", "b.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "fn3", "doWork", "b.ts", types.NodeKindFunction, types.LanguageTypeScript)

	seedEdgeWithMeta(t, d, "ctor3", "fn3", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:worker"]}`))

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("got %d edges, want 0 (no invocation ref)", len(edges))
	}
}

// An invocation with nothing registered to the field has no target to reach.
func TestCallbackSynthesizer_NoEdgeWithoutRegistrationEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "invoker4", "runCallback", "c.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedRefWithArgs(t, d, "ref-cb4", "invoker4", "this.onDataCallback", types.EdgeKindCalls, nil)

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("got %d edges, want 0 (no registration edge = no signal)", len(edges))
	}
}

func TestCallbackSynthesizer_MultipleCallbacks(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor5", "constructor", "d.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "process5a", "processA", "d.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "process5b", "processB", "d.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "hdl5", "onHandler", "d.ts", types.NodeKindFunction, types.LanguageTypeScript)

	seedEdgeWithMeta(t, d, "ctor5", "hdl5", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onData"]}`))

	seedRefWithArgs(t, d, "ref-pA", "process5a", "this.onData", types.EdgeKindCalls, nil)
	seedRefWithArgs(t, d, "ref-pB", "process5b", "this.onData", types.EdgeKindCalls, nil)

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2 (one per invoker)", len(edges))
	}
	srcSet := map[string]bool{}
	for _, e := range edges {
		if e.Target != "hdl5" {
			t.Errorf("edge target=%q, want hdl5", e.Target)
		}
		srcSet[e.Source] = true
	}
	if !srcSet["process5a"] || !srcSet["process5b"] {
		t.Errorf("expected sources process5a and process5b, got %v", srcSet)
	}
}

// The per-channel cap holds when invokers outnumber it.
func TestCallbackSynthesizer_MaxCallbacksPerChannelCap(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor-cap", "constructor", "cap.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "hdl-cap", "handler", "cap.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedEdgeWithMeta(t, d, "ctor-cap", "hdl-cap", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onMsg"]}`))

	for i := 0; i < 45; i++ {
		id := nodeID("inv-cap", i)
		seedNode(t, d, id, id, "cap.ts", types.NodeKindMethod, types.LanguageTypeScript)
		seedRefWithArgs(t, d, "r-cap-"+id, id, "this.onMsg", types.EdgeKindCalls, nil)
	}

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != synthesis.MAX_CALLBACKS_PER_CHANNEL {
		t.Errorf("got %d edges, want MAX_CALLBACKS_PER_CHANNEL=%d", len(edges), synthesis.MAX_CALLBACKS_PER_CHANNEL)
	}
}

func TestCallbackSynthesizer_NoSelfLoop(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor-sl", "constructor", "sl.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "hdl-sl", "myHandler", "sl.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedEdgeWithMeta(t, d, "ctor-sl", "hdl-sl", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onEvent"]}`))

	seedRefWithArgs(t, d, "r-sl-ctor", "ctor-sl", "this.onEvent", types.EdgeKindCalls, nil)
	seedNode(t, d, "fn-sl", "doThing", "sl.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedRefWithArgs(t, d, "r-sl-fn", "fn-sl", "this.onEvent", types.EdgeKindCalls, nil)

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// A constructor legitimately invokes the field it registered (fire-once
	// init), so registrar-as-invoker is left unasserted; only the absence of a
	// literal self-loop is.
	for _, e := range edges {
		if e.Source == e.Target {
			t.Errorf("self-loop detected: %s→%s", e.Source, e.Target)
		}
	}
	if len(edges) == 0 {
		t.Errorf("expected at least one edge (doThing→hdl-sl)")
	}
}

// The whole path, real fixture through the real pipeline.
func TestCallbackSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	writeFixture(t, fixtureDir, "pipeline.ts", `
class DataPipeline {
  constructor() {
    this.onData = handleData;
    this.onError = handleError;
  }

  processChunk(chunk: any) {
    this.onData(chunk);
  }

  fail(err: any) {
    this.onError(err);
  }
}

function handleData(d: any) {
  console.log("data", d);
}

function handleError(e: any) {
  console.log("error", e);
}
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
	if _, _, err := pipe.ResolveAndPersistBatched(ctx, 0, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	var processChunkID, failID, handleDataID, handleErrorID string
	for _, n := range allNodes {
		switch n.Name {
		case "processChunk":
			processChunkID = n.ID
		case "fail":
			failID = n.ID
		case "handleData":
			handleDataID = n.ID
		case "handleError":
			handleErrorID = n.ID
		}
	}
	if processChunkID == "" || failID == "" || handleDataID == "" || handleErrorID == "" {
		t.Fatalf("expected nodes not found: processChunk=%s fail=%s handleData=%s handleError=%s",
			processChunkID, failID, handleDataID, handleErrorID)
	}

	assertCallbackEdge(t, d, processChunkID, handleDataID, "onData")
	assertCallbackEdge(t, d, failID, handleErrorID, "onError")

	synthBefore := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	synthAfter := countEdgesWithProvenance(t, d, "heuristic")
	if synthBefore != synthAfter {
		t.Errorf("idempotent: before=%d after=%d, want equal", synthBefore, synthAfter)
	}

	nodesBefore := countNodes(t, d)
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("second re-run: %v", err)
	}
	nodesAfter := countNodes(t, d)
	if nodesBefore != nodesAfter {
		t.Errorf("node count: before=%d after=%d, want equal", nodesBefore, nodesAfter)
	}
}

func assertCallbackEdge(t *testing.T, d *db.DB, sourceID, targetID, fieldName string) {
	t.Helper()
	edges := edgesFrom(t, d, sourceID)
	for _, e := range edges {
		if e.Target != targetID || e.Provenance != "heuristic" {
			continue
		}
		if e.Kind != types.EdgeKindCalls {
			t.Errorf("callback edge kind=%s, want calls", e.Kind)
			return
		}
		var meta map[string]string
		if err := json.Unmarshal(e.Metadata, &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if meta["synthesizedBy"] != "callback" {
			t.Errorf("synthesizedBy=%q, want callback", meta["synthesizedBy"])
		}
		if meta["field"] != fieldName {
			t.Errorf("field=%q, want %q", meta["field"], fieldName)
		}
		return // found
	}
	t.Errorf("no heuristic calls edge %s→%s (synthesizedBy=callback field=%s)", sourceID, targetID, fieldName)
}

// A Swift/Kotlin `.append(closure)` records the callee but not the closure's
// identity, so there is no target to correlate against and no edge to emit.
// Flip this to real edges once identifier arguments are captured.
func TestClosureCollectionSynthesizer_GapDocumented(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Exactly what real Swift source produces today.
	seedNode(t, d, "evt-mgr", "EventManager", "event.swift", types.NodeKindClass, types.LanguageSwift)
	seedNode(t, d, "fn-addHandler", "addHandler", "event.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-fireAll", "fireAll", "event.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-handleData", "handleData", "event.swift", types.NodeKindFunction, types.LanguageSwift)

	seedRefWithArgs(t, d, "r-append", "fn-addHandler", "handlers.append", types.EdgeKindCalls, nil)
	seedRefWithArgs(t, d, "r-forEach", "fn-fireAll", "handlers.forEach", types.EdgeKindCalls, nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("ClosureCollectionSynthesizer.Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("ClosureCollectionSynthesizer produced %d edges, want 0 (gap: EE2 does not capture closure-block arguments; no closure identity to correlate)", len(edges))
	}
}

// The Dart grammar exposes no call_expression, so setState never becomes a
// ref and there is nothing to correlate with build. Flip this to real edges
// once Dart call extraction exists.
func TestFlutterBuildSynthesizer_GapDocumented(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "counter-state", "CounterState", "counter.dart", types.NodeKindClass, types.LanguageDart)
	seedNode(t, d, "fn-increment", "increment", "counter.dart", types.NodeKindFunction, types.LanguageDart)
	seedNode(t, d, "fn-build", "build", "counter.dart", types.NodeKindFunction, types.LanguageDart)

	// No setState ref is seeded because the real graph never contains one.

	s := &synthesis.FlutterBuildSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("FlutterBuildSynthesizer.Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("FlutterBuildSynthesizer produced %d edges, want 0 (gap: Dart grammar has no call_expression node; setState calls not captured as unresolved refs)", len(edges))
	}
}
