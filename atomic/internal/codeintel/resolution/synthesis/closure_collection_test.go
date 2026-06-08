package synthesis_test

// closure_collection_test.go — TDD tests for ClosureCollectionSynthesizer (EE5 activated).
//
// # What this synthesizer does
//
// After EE5, `handlers.append(handler)` emits a calls-kind unresolved ref with
// Arguments containing "arg:handler". This lets the synthesizer:
//  1. Find all .append(handler) refs and extract (receiver="handlers", handlerName="handler").
//  2. Resolve handlerName to a node in the DB via GetNodesByName.
//  3. Find all .forEach refs with the same receiver on the same enclosing function scope.
//  4. Emit a calls+heuristic edge: forEach-enclosing-fn → handler-node.
//
// Cap: CC_FANOUT_CAP = 8 (per receiver channel).
//
// # Gap documented
//
// Anonymous closures passed to .append (e.g. Swift trailing { ... } or Kotlin { x -> ... })
// produce no "arg:" identifier → no edge. This is honest: the handler identity
// is not available in the graph. Only named-identifier handler args produce edges.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// seedRefCC seeds an unresolved ref for closure-collection tests. Same as
// seedRefWithArgs but named for clarity.
func seedRefCC(t *testing.T, d *db.DB, id, fromID, name string, args []string) {
	t.Helper()
	if err := d.InsertUnresolvedRef(context.Background(), types.UnresolvedReference{
		ID:            id,
		FromNodeID:    fromID,
		ReferenceName: name,
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "test.swift",
		Language:      types.LanguageSwift,
		Arguments:     args,
	}); err != nil {
		t.Fatalf("InsertUnresolvedRef %s: %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// TestClosureCollectionSynthesizer_BasicCorrelation
// ---------------------------------------------------------------------------

// TestClosureCollectionSynthesizer_BasicCorrelation is the canonical EE5 use case:
// handlers.append(onLogin) + handlers.forEach { $0() } → one calls edge from
// the forEach-enclosing fn to the onLogin function node.
// synthesizedBy = "closure-collection".
func TestClosureCollectionSynthesizer_BasicCorrelation(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Two function nodes: setup (calls append), fire (calls forEach).
	seedNode(t, d, "fn-setup", "setup", "app.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-fire", "fire", "app.swift", types.NodeKindFunction, types.LanguageSwift)
	// The handler function that was appended.
	seedNode(t, d, "fn-onLogin", "onLogin", "app.swift", types.NodeKindFunction, types.LanguageSwift)

	// handlers.append(onLogin) from setup fn; EE5 captures "arg:onLogin".
	seedRefCC(t, d, "ref-append", "fn-setup", "handlers.append", []string{"arg:onLogin"})
	// handlers.forEach { $0() } from fire fn; no identifier arg.
	seedRefCC(t, d, "ref-forEach", "fn-fire", "handlers.forEach", nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1; edges=%v", len(edges), edges)
	}
	e := edges[0]
	if e.Source != "fn-fire" {
		t.Errorf("Source = %q, want fn-fire (forEach enclosing fn)", e.Source)
	}
	if e.Target != "fn-onLogin" {
		t.Errorf("Target = %q, want fn-onLogin (handler node)", e.Target)
	}
}

// TestClosureCollectionSynthesizer_SynthesizedByTag verifies that the Composite
// stamps synthesizedBy="closure-collection" on the edge metadata.
func TestClosureCollectionSynthesizer_SynthesizedByTag(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-reg", "register", "svc.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-invoke", "invoke", "svc.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-handler", "handleEvent", "svc.swift", types.NodeKindFunction, types.LanguageSwift)

	seedRefCC(t, d, "ref-a", "fn-reg", "callbacks.append", []string{"arg:handleEvent"})
	seedRefCC(t, d, "ref-f", "fn-invoke", "callbacks.forEach", nil)

	// Use the Composite so synthesizedBy is stamped.
	comp := synthesis.NewComposite(d, &synthesis.ClosureCollectionSynthesizer{})
	if err := comp.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("SynthesizeCallbackEdges: %v", err)
	}

	// Find the edge.
	allNodes, err := d.GetAllNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllNodes: %v", err)
	}
	var found *types.Edge
	for _, n := range allNodes {
		edges, err := d.GetEdgesBySource(ctx, n.ID)
		if err != nil {
			t.Fatalf("GetEdgesBySource: %v", err)
		}
		for i, e := range edges {
			if e.Source == "fn-invoke" && e.Target == "fn-handler" {
				found = &edges[i]
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		t.Fatalf("expected an edge from fn-invoke to fn-handler, none found")
	}
	if found.Kind != types.EdgeKindCalls {
		t.Errorf("Kind = %q, want calls", found.Kind)
	}
	if found.Provenance != "heuristic" {
		t.Errorf("Provenance = %q, want heuristic", found.Provenance)
	}
	var meta map[string]any
	if err := json.Unmarshal(found.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["synthesizedBy"] != "closure-collection" {
		t.Errorf("metadata.synthesizedBy = %v, want closure-collection", meta["synthesizedBy"])
	}
}

// TestClosureCollectionSynthesizer_NoEdgeWhenHandlerNotInGraph verifies that when
// the handler identifier from "arg:handler" cannot be resolved to a node (e.g.
// anonymous closure), no edge is emitted — honest gap behavior.
func TestClosureCollectionSynthesizer_NoEdgeWhenHandlerNotInGraph(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-reg2", "registerAnon", "svc.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-invoke2", "invokeAnon", "svc.swift", types.NodeKindFunction, types.LanguageSwift)
	// Note: "anonClosure" node is NOT in the DB (simulates an anonymous closure).

	seedRefCC(t, d, "ref-anon-append", "fn-reg2", "handlers2.append", []string{"arg:anonClosure"})
	seedRefCC(t, d, "ref-anon-forEach", "fn-invoke2", "handlers2.forEach", nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges (handler not in graph), got %d: %v", len(edges), edges)
	}
}

// TestClosureCollectionSynthesizer_NoEdgeWhenNoForEach verifies that an append
// with a known handler but no matching forEach in the graph emits no edge.
func TestClosureCollectionSynthesizer_NoEdgeWhenNoForEach(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-reg3", "registerOnly", "svc.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-h3", "myHandler", "svc.swift", types.NodeKindFunction, types.LanguageSwift)

	// .append(myHandler) but NO matching .forEach.
	seedRefCC(t, d, "ref-append3", "fn-reg3", "handlers3.append", []string{"arg:myHandler"})

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges (no forEach), got %d", len(edges))
	}
}

// TestClosureCollectionSynthesizer_NoEdgeWhenStringOnlyArgs verifies that string
// args (EE2, no "arg:" prefix) do not confuse the synthesizer — only "arg:"
// entries identify closure/handler identifiers.
func TestClosureCollectionSynthesizer_NoEdgeWhenStringOnlyArgs(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-str-setup", "strSetup", "svc.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-str-fire", "strFire", "svc.swift", types.NodeKindFunction, types.LanguageSwift)

	// .append("login") — string arg, not an identifier. Should not produce a CC edge.
	seedRefCC(t, d, "ref-str-append", "fn-str-setup", "handlers4.append", []string{"login"})
	seedRefCC(t, d, "ref-str-forEach", "fn-str-fire", "handlers4.forEach", nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges (string arg, not ident), got %d", len(edges))
	}
}

// TestClosureCollectionSynthesizer_FanoutCapRespected verifies that
// CC_FANOUT_CAP (8) limits the number of forEach-caller→handler edges per
// receiver channel when many handlers are appended.
func TestClosureCollectionSynthesizer_FanoutCapRespected(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-cap-setup", "capSetup", "svc.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-cap-fire", "capFire", "svc.swift", types.NodeKindFunction, types.LanguageSwift)

	// Append 12 handlers (> CC_FANOUT_CAP=8). Only 8 edges should be emitted.
	handlerArgs := make([]string, 12)
	for i := 0; i < 12; i++ {
		name := "handler" + string(rune('A'+i))
		id := "fn-cap-h-" + string(rune('A'+i))
		seedNode(t, d, id, name, "svc.swift", types.NodeKindFunction, types.LanguageSwift)
		handlerArgs[i] = "arg:" + name
	}
	seedRefCC(t, d, "ref-cap-append", "fn-cap-setup", "capHandlers.append", handlerArgs)
	seedRefCC(t, d, "ref-cap-forEach", "fn-cap-fire", "capHandlers.forEach", nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) > synthesis.CC_FANOUT_CAP {
		t.Errorf("got %d edges, want ≤ CC_FANOUT_CAP (%d)", len(edges), synthesis.CC_FANOUT_CAP)
	}
}

// TestClosureCollectionSynthesizer_KotlinEachAlso verifies that .each() (Kotlin)
// as well as .forEach() triggers the forEach-caller side.
func TestClosureCollectionSynthesizer_KotlinEachAlso(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-kt-setup", "ktSetup", "app.kt", types.NodeKindFunction, types.LanguageKotlin)
	seedNode(t, d, "fn-kt-fire", "ktFire", "app.kt", types.NodeKindFunction, types.LanguageKotlin)
	seedNode(t, d, "fn-kt-h", "ktHandler", "app.kt", types.NodeKindFunction, types.LanguageKotlin)

	seedRefCC(t, d, "ref-kt-append", "fn-kt-setup", "listeners.add", []string{"arg:ktHandler"})
	seedRefCC(t, d, "ref-kt-each", "fn-kt-fire", "listeners.forEach", nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	if edges[0].Target != "fn-kt-h" {
		t.Errorf("Target = %q, want fn-kt-h", edges[0].Target)
	}
}

// TestClosureCollectionSynthesizer_ReceiverIsolation verifies that two distinct
// receiver collections are not cross-correlated.
func TestClosureCollectionSynthesizer_ReceiverIsolation(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-iso-setup", "isoSetup", "app.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-iso-fire", "isoFire", "app.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-iso-h", "isoHandler", "app.swift", types.NodeKindFunction, types.LanguageSwift)

	// "channelA.append(isoHandler)" but forEach is on "channelB" — different receiver.
	seedRefCC(t, d, "ref-iso-append", "fn-iso-setup", "channelA.append", []string{"arg:isoHandler"})
	seedRefCC(t, d, "ref-iso-forEach", "fn-iso-fire", "channelB.forEach", nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges (different receivers), got %d", len(edges))
	}
}

// TestClosureCollectionSynthesizer_AnonymousClosureGapDocumented verifies that
// a call with no "arg:" entry (anonymous closure passed to append) produces no
// edge. This is the documented gap: anonymous closures have no extractable identity.
func TestClosureCollectionSynthesizer_AnonymousClosureGapDocumented(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "fn-anon-reg", "anonReg", "app.swift", types.NodeKindFunction, types.LanguageSwift)
	seedNode(t, d, "fn-anon-fire", "anonFire", "app.swift", types.NodeKindFunction, types.LanguageSwift)

	// .append() with no arguments at all — anonymous trailing closure { ... }.
	seedRefCC(t, d, "ref-anon2-append", "fn-anon-reg", "obs.append", nil)
	seedRefCC(t, d, "ref-anon2-forEach", "fn-anon-fire", "obs.forEach", nil)

	s := &synthesis.ClosureCollectionSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges (anonymous closure — honest gap), got %d", len(edges))
	}
}
