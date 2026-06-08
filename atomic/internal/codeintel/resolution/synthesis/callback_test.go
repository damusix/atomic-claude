package synthesis_test

// callback_test.go — TDD tests for CallbackSynthesizer (CP16 batch 4).
//
// Ground truth (probe 2026-06-05): indexing a TypeScript class with
// `this.onData = handleData` and `this.onData(chunk)` produces:
//
//   After extraction, before resolution:
//     ref: kind=references  name="handleData"  args=["field:onData"]   (EE3)
//     ref: kind=calls       name="this.onData" args=[]                 (EE2-less call)
//
//   After resolution:
//     static edge: constructor → handleData node  kind=references  meta={"refArgs":["field:onData"]}
//     unresolved ref remaining: kind=calls  name="this.onData"  (no "onData" node → unresolved)
//
// Correlation strategy:
//   1. Walk all static references edges whose Metadata.refArgs[0] starts with "field:".
//      Extract fieldName = "onData", callableTargetID = "handleData node".
//   2. Scan unresolved_refs for calls-kind refs where ReferenceName ends with ".fieldName"
//      or equals fieldName.  The FromNodeID of such refs = the invoking method.
//   3. Synthesize calls+heuristic edge: invokerMethod → callableTargetID
//      Metadata: {synthesizedBy:"callback", field:fieldName}
//   4. Cap: MAX_CALLBACKS_PER_CHANNEL (40) per field channel.
//   5. Dedup + idempotent (handled by Composite).
//
// closure-collection (Swift/Kotlin): documented gap.
//   ABSENT SIGNAL: .append(closure) captures the callee ("handlers.append")
//   but NOT the closure argument as an identifier — EE2 only records string-literal
//   args, not closure-block arguments. Without knowing which callable was appended,
//   no source→target correlation is derivable. Zero edges; zero fake.
//
// flutter-build (Dart setState→build): documented gap.
//   ABSENT SIGNAL: Dart grammar has no call_expression node — CallTypes is empty
//   for Dart. setState calls are not captured as unresolved refs. The `increment`
//   method shows zero unresolved refs in a real probe. Zero edges; zero fake.
//
// WHY document gaps: appendix G / BRIEF mandate honest stubs over fabricated
// edges. A documented stub is a machine-readable promise that the synthesizer
// will emit edges once the missing signal is available.

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

// ---------------------------------------------------------------------------
// Unit: seeded DB — callback with real EE3 static edge + call unresolved_ref
// ---------------------------------------------------------------------------

// TestCallbackSynthesizer_Unit is the core unit test. It manually seeds the
// graph state that the real extraction+resolution pipeline produces for a
// `this.onData = handleData` / `this.onData(chunk)` pattern, then asserts that
// CallbackSynthesizer emits the correct edge.
//
// WHY manual seeding: lets us assert the correlation logic without the full
// pipeline setup overhead, and makes the expected input/output explicit.
func TestCallbackSynthesizer_Unit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Nodes: the constructor (registers), the processChunk method (invokes),
	// and the handleData function (the target callable).
	seedNode(t, d, "ctor", "constructor", "pipeline.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "process", "processChunk", "pipeline.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "handle-data", "handleData", "pipeline.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// Static edge (EE3 registration): constructor → handleData with refArgs=["field:onData"]
	// This is what the real pipeline produces after resolving the EE3 ref.
	seedEdgeWithMeta(t, d, "ctor", "handle-data", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onData"]}`))

	// Unresolved ref (invocation): processChunk calls this.onData (unresolved because
	// "this.onData" is not a node name — it stays in unresolved_refs after resolution).
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
	// Metadata must carry field name.
	var meta map[string]string
	if err := json.Unmarshal(e.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["field"] != "onData" {
		t.Errorf("metadata.field=%q, want onData", meta["field"])
	}
}

// TestCallbackSynthesizer_BareFieldName verifies that an invocation ref
// with a bare field name (e.g. "onData" rather than "this.onData") is also
// correlated.  Some code styles call the handler without "this." prefix.
func TestCallbackSynthesizer_BareFieldName(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor2", "constructor", "a.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "run2", "run", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedNode(t, d, "cb2", "myCallback", "a.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// Registration: constructor stores myCallback in this.cb
	seedEdgeWithMeta(t, d, "ctor2", "cb2", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:cb"]}`))

	// Invocation: bare "cb" without "this."
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

// TestCallbackSynthesizer_NoEdgeWithoutCallRef verifies that a field-assignment
// registration edge without any corresponding invocation unresolved_ref produces
// no synthesized edge.
func TestCallbackSynthesizer_NoEdgeWithoutCallRef(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor3", "constructor", "b.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "fn3", "doWork", "b.ts", types.NodeKindFunction, types.LanguageTypeScript)

	// Registration edge exists but no corresponding invocation.
	seedEdgeWithMeta(t, d, "ctor3", "fn3", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:worker"]}`))
	// No invocation ref for "worker" or "this.worker".

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("got %d edges, want 0 (no invocation ref)", len(edges))
	}
}

// TestCallbackSynthesizer_NoEdgeWithoutRegistrationEdge verifies that an
// invocation call ref without any field-assignment registration edge produces
// no synthesized edge. The old stub behavior was correct for this case.
func TestCallbackSynthesizer_NoEdgeWithoutRegistrationEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "invoker4", "runCallback", "c.ts", types.NodeKindMethod, types.LanguageTypeScript)
	// Only an invocation ref — no EE3 registration static edge.
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

// TestCallbackSynthesizer_MultipleCallbacks verifies multiple callbacks on the
// same channel from multiple invokers each produce an edge.
func TestCallbackSynthesizer_MultipleCallbacks(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// One constructor, two methods that both invoke onData, one handler.
	seedNode(t, d, "ctor5", "constructor", "d.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "process5a", "processA", "d.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "process5b", "processB", "d.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "hdl5", "onHandler", "d.ts", types.NodeKindFunction, types.LanguageTypeScript)

	seedEdgeWithMeta(t, d, "ctor5", "hdl5", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onData"]}`))

	// Two distinct invocation refs from two methods.
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

// TestCallbackSynthesizer_MaxCallbacksPerChannelCap verifies that when more
// than MAX_CALLBACKS_PER_CHANNEL invokers reference the same field channel,
// only MAX_CALLBACKS_PER_CHANNEL edges are emitted (cap = 40).
func TestCallbackSynthesizer_MaxCallbacksPerChannelCap(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor-cap", "constructor", "cap.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "hdl-cap", "handler", "cap.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedEdgeWithMeta(t, d, "ctor-cap", "hdl-cap", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onMsg"]}`))

	// Seed 45 distinct invokers (> MAX_CALLBACKS_PER_CHANNEL = 40).
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

// TestCallbackSynthesizer_NoSelfLoop verifies that if the registering method
// (ctor) also calls the field, no self-loop edge is emitted for it.
func TestCallbackSynthesizer_NoSelfLoop(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "ctor-sl", "constructor", "sl.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "hdl-sl", "myHandler", "sl.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedEdgeWithMeta(t, d, "ctor-sl", "hdl-sl", types.EdgeKindReferences,
		json.RawMessage(`{"refArgs":["field:onEvent"]}`))

	// constructor also invokes this.onEvent (self-loop candidate: ctor-sl → hdl-sl).
	seedRefWithArgs(t, d, "r-sl-ctor", "ctor-sl", "this.onEvent", types.EdgeKindCalls, nil)
	// Another method too.
	seedNode(t, d, "fn-sl", "doThing", "sl.ts", types.NodeKindFunction, types.LanguageTypeScript)
	seedRefWithArgs(t, d, "r-sl-fn", "fn-sl", "this.onEvent", types.EdgeKindCalls, nil)

	s := &synthesis.CallbackSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Should have edge from doThing→hdl-sl, but whether ctor-sl→hdl-sl is emitted
	// depends on whether we treat registrars as invokers. In the callback pattern
	// it is valid for the constructor to also call the field (e.g. fire-once init).
	// So we only assert at least 1 edge (doThing→hdl-sl) and that no source==target.
	for _, e := range edges {
		if e.Source == e.Target {
			t.Errorf("self-loop detected: %s→%s", e.Source, e.Target)
		}
	}
	if len(edges) == 0 {
		t.Errorf("expected at least one edge (doThing→hdl-sl)")
	}
}

// ---------------------------------------------------------------------------
// Gate: real fixture through full indexer + pipeline
// ---------------------------------------------------------------------------

// TestCallbackSynthesizer_Gate indexes a real TypeScript fixture through the
// full pipeline and asserts calls+heuristic edges from the invoking methods →
// the registered callable nodes.
func TestCallbackSynthesizer_Gate(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	fixtureDir := t.TempDir()

	// The fixture matches the probe output: DataPipeline stores handleData in
	// this.onData, handleError in this.onError, then invokes both.
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

	// Find nodes.
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

	// Assert: processChunk → handleData (via field:onData)
	assertCallbackEdge(t, d, processChunkID, handleDataID, "onData")
	// Assert: fail → handleError (via field:onError)
	assertCallbackEdge(t, d, failID, handleErrorID, "onError")

	// Idempotency.
	synthBefore := countEdgesWithProvenance(t, d, "heuristic")
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	synthAfter := countEdgesWithProvenance(t, d, "heuristic")
	if synthBefore != synthAfter {
		t.Errorf("idempotent: before=%d after=%d, want equal", synthBefore, synthAfter)
	}

	// Node count stable.
	nodesBefore := countNodes(t, d)
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("second re-run: %v", err)
	}
	nodesAfter := countNodes(t, d)
	if nodesBefore != nodesAfter {
		t.Errorf("node count: before=%d after=%d, want equal", nodesBefore, nodesAfter)
	}
}

// assertCallbackEdge asserts a calls+heuristic edge with synthesizedBy=callback
// and the expected field name in metadata.
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

// ---------------------------------------------------------------------------
// closure-collection: documented gap test
// ---------------------------------------------------------------------------

// TestClosureCollectionSynthesizer_GapDocumented asserts that the
// ClosureCollectionSynthesizer emits zero edges because the append-callable
// signal is absent from the Swift/Kotlin extraction pipeline.
//
// ABSENT SIGNAL: Swift/Kotlin .append(closure) emits a call ref with
// ReferenceName="handlers.append" and Arguments=[] (EE2 only records
// string-literal args, not closure-block args). Without the closure's identity
// in Arguments, there is no source→target correlation. No edges, zero fake.
//
// When EE2 is extended to capture identifier arguments (not just string literals),
// this test should be updated to assert real edges and the synthesizer activated.
func TestClosureCollectionSynthesizer_GapDocumented(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Seed Swift-like call refs for handlers.append and handlers.forEach.
	// These are exactly what a real Swift probe produces (see probe 2026-06-05).
	seedNode(t, d, "evt-mgr", "EventManager", "event.swift", types.NodeKindClass, types.LanguageSwift)
	seedNode(t, d, "fn-addHandler", "addHandler", "event.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-fireAll", "fireAll", "event.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-handleData", "handleData", "event.swift", types.NodeKindFunction, types.LanguageSwift)

	// .append ref: no identifier arg captured.
	seedRefWithArgs(t, d, "r-append", "fn-addHandler", "handlers.append", types.EdgeKindCalls, nil)
	// .forEach ref: no identifier arg.
	seedRefWithArgs(t, d, "r-forEach", "fn-fireAll", "handlers.forEach", types.EdgeKindCalls, nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("ClosureCollectionSynthesizer.Synthesize: %v", err)
	}
	// ABSENT SIGNAL: EE2 does not capture closure/identifier args from .append().
	// Without the closure identity, no correlation is possible. Zero edges expected.
	if len(edges) != 0 {
		t.Errorf("ClosureCollectionSynthesizer produced %d edges, want 0 (gap: EE2 does not capture closure-block arguments; no closure identity to correlate)", len(edges))
	}
}

// ---------------------------------------------------------------------------
// flutter-build: documented gap test
// ---------------------------------------------------------------------------

// TestFlutterBuildSynthesizer_GapDocumented asserts that the
// FlutterBuildSynthesizer emits zero edges because the Dart call extraction
// pipeline is blocked.
//
// ABSENT SIGNAL: The Dart grammar tree-sitter binding has no call_expression
// node (documented in dart.go and TestDart_CallsBlocked). CallTypes is empty
// for Dart. No setState calls are captured as unresolved refs; therefore the
// synthesizer has no invocation signal to correlate with the build method.
// Zero edges, zero fake.
//
// When the Dart grammar is upgraded to expose call_expression nodes (or an
// alternative extraction strategy captures setState), this test should be
// updated to assert real edges and the synthesizer activated.
func TestFlutterBuildSynthesizer_GapDocumented(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Seed Dart-like nodes as a real Flutter State subclass would produce.
	// The real probe (2026-06-05) shows zero unresolved refs for increment().
	seedNode(t, d, "counter-state", "CounterState", "counter.dart", types.NodeKindClass, types.LanguageDart)
	seedNode(t, d, "fn-increment", "increment", "counter.dart", types.NodeKindFunction, types.LanguageDart)
	seedNode(t, d, "fn-build", "build", "counter.dart", types.NodeKindFunction, types.LanguageDart)

	// No setState call ref — because Dart has no call_expression in its grammar.
	// If there were a setState ref, it would be: seedRefWithArgs(..., "setState", EdgeKindCalls, ...)
	// but the real graph has zero such refs.

	s := &synthesis.FlutterBuildSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("FlutterBuildSynthesizer.Synthesize: %v", err)
	}
	// ABSENT SIGNAL: Dart grammar has no call_expression → setState not captured.
	// Zero setState refs → zero edges. Zero fake.
	if len(edges) != 0 {
		t.Errorf("FlutterBuildSynthesizer produced %d edges, want 0 (gap: Dart grammar has no call_expression node; setState calls not captured as unresolved refs)", len(edges))
	}
}
