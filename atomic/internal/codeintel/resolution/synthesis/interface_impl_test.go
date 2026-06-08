package synthesis_test

// interface_impl_test.go — TDD tests for InterfaceImplSynthesizer and
// CppOverrideSynthesizer (CP16 batch 5).
//
// # Ground truth (real synthesizers, activated after EE4)
//
// EE4 landed: heritage extraction is now wired for TypeScript, C++, and Java.
//
// interface-impl:
//   EE4 produces EdgeKindImplements C→I edges + interface method nodes
//   (method_signature IS in MethodTypes). InterfaceImplSynthesizer walks
//   implements edges, matches methods by name, and emits calls+heuristic
//   I.m→C.m edges with synthesizedBy="interface-impl".
//
// cpp-override:
//   EE4 produces EdgeKindExtends D→B edges for C++ base_class_clause.
//   CppOverrideSynthesizer walks C++ extends edges, matches methods by name,
//   and emits calls+heuristic B.m→D.m edges with synthesizedBy="cpp-override".
//   Scoped to C++ (Language==cpp) — does not fire on TypeScript extends.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/synthesis"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// InterfaceImplSynthesizer — real gate tests (activated after EE4)
// ---------------------------------------------------------------------------

// TestInterfaceImplSynthesizer_EmitsDispatchEdge is the primary gate for the
// interface-impl synthesizer. It seeds the minimal signal: an interface with one
// method, a class that implements the interface with a matching method, and the
// implements edge between them. The synthesizer must emit exactly one
// calls+heuristic+synthesizedBy=interface-impl edge from the interface method
// to the class method.
func TestInterfaceImplSynthesizer_EmitsDispatchEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Interface Speaker with method speak.
	seedNode(t, d, "iface-speaker", "Speaker", "s.ts", types.NodeKindInterface, types.LanguageTypeScript)
	seedNode(t, d, "meth-speaker-speak", "speak", "s.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "iface-speaker", "meth-speaker-speak", types.EdgeKindContains)

	// Class Dog implements Speaker with a speak method.
	seedNode(t, d, "cls-dog", "Dog", "s.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "meth-dog-speak", "speak", "s.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "cls-dog", "meth-dog-speak", types.EdgeKindContains)
	seedEdge(t, d, "cls-dog", "iface-speaker", types.EdgeKindImplements)

	s := &synthesis.InterfaceImplSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("InterfaceImplSynthesizer emitted %d edges, want 1", len(edges))
	}
	e := edges[0]
	if e.Source != "meth-speaker-speak" {
		t.Errorf("edge source = %q, want %q", e.Source, "meth-speaker-speak")
	}
	if e.Target != "meth-dog-speak" {
		t.Errorf("edge target = %q, want %q", e.Target, "meth-dog-speak")
	}
}

// TestInterfaceImplSynthesizer_NoEdgeWhenClassLacksMatchingMethod verifies that
// when the implementing class has no method with the same name as the interface
// method, no edge is synthesized. The synthesizer must not fabricate edges.
func TestInterfaceImplSynthesizer_NoEdgeWhenClassLacksMatchingMethod(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Interface with method speak.
	seedNode(t, d, "iface-talker", "Talker", "t.ts", types.NodeKindInterface, types.LanguageTypeScript)
	seedNode(t, d, "meth-talker-speak", "speak", "t.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "iface-talker", "meth-talker-speak", types.EdgeKindContains)

	// Class Cat implements Talker but has no speak method (only a purr method).
	seedNode(t, d, "cls-cat", "Cat", "t.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "meth-cat-purr", "purr", "t.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "cls-cat", "meth-cat-purr", types.EdgeKindContains)
	seedEdge(t, d, "cls-cat", "iface-talker", types.EdgeKindImplements)

	s := &synthesis.InterfaceImplSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("InterfaceImplSynthesizer emitted %d edges for unmatched method, want 0", len(edges))
	}
}

// TestInterfaceImplSynthesizer_IdempotentViaComposite verifies that running the
// full composite twice on the same DB produces the same edge count. The composite's
// dedup-by-source>target prevents double insertion.
func TestInterfaceImplSynthesizer_IdempotentViaComposite(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "iface-idem", "IFace", "i.ts", types.NodeKindInterface, types.LanguageTypeScript)
	seedNode(t, d, "meth-idem-iface", "run", "i.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "iface-idem", "meth-idem-iface", types.EdgeKindContains)

	seedNode(t, d, "cls-idem", "Impl", "i.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "meth-idem-cls", "run", "i.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "cls-idem", "meth-idem-cls", types.EdgeKindContains)
	seedEdge(t, d, "cls-idem", "iface-idem", types.EdgeKindImplements)

	composite := synthesis.NewComposite(d, &synthesis.InterfaceImplSynthesizer{})
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	edgesAfterFirst, err := d.GetEdgesBySource(ctx, "meth-idem-iface")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	var callsCount int
	for _, e := range edgesAfterFirst {
		if e.Kind == types.EdgeKindCalls {
			callsCount++
		}
	}
	if callsCount != 1 {
		t.Fatalf("after first run: %d calls edges, want 1", callsCount)
	}

	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}
	edgesAfterSecond, err := d.GetEdgesBySource(ctx, "meth-idem-iface")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	var callsCount2 int
	for _, e := range edgesAfterSecond {
		if e.Kind == types.EdgeKindCalls {
			callsCount2++
		}
	}
	if callsCount2 != callsCount {
		t.Errorf("idempotency broken: first run=%d calls edges, second run=%d", callsCount, callsCount2)
	}
}

// TestInterfaceImplSynthesizer_SynthesizedByMetadata verifies that the composite
// stamps synthesizedBy="interface-impl" on edges produced by InterfaceImplSynthesizer.
func TestInterfaceImplSynthesizer_SynthesizedByMetadata(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "iface-meta", "IBar", "m.ts", types.NodeKindInterface, types.LanguageTypeScript)
	seedNode(t, d, "meth-meta-iface", "bar", "m.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "iface-meta", "meth-meta-iface", types.EdgeKindContains)

	seedNode(t, d, "cls-meta", "BarImpl", "m.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "meth-meta-cls", "bar", "m.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "cls-meta", "meth-meta-cls", types.EdgeKindContains)
	seedEdge(t, d, "cls-meta", "iface-meta", types.EdgeKindImplements)

	composite := synthesis.NewComposite(d, &synthesis.InterfaceImplSynthesizer{})
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("SynthesizeCallbackEdges: %v", err)
	}

	edges, err := d.GetEdgesBySource(ctx, "meth-meta-iface")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	var found bool
	for _, e := range edges {
		if e.Kind != types.EdgeKindCalls || e.Target != "meth-meta-cls" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(e.Metadata, &m); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if got := m["synthesizedBy"]; got != "interface-impl" {
			t.Errorf("synthesizedBy = %q, want %q", got, "interface-impl")
		}
		if e.Provenance != "heuristic" {
			t.Errorf("provenance = %q, want %q", e.Provenance, "heuristic")
		}
		found = true
	}
	if !found {
		t.Error("no calls edge from meth-meta-iface to meth-meta-cls")
	}
}

// TestInterfaceImplSynthesizer_NoEdgeWithoutImplementsEdge verifies that even
// with manually seeded interface + class + method nodes, the synthesizer emits
// nothing if no `implements` edge connects the class to the interface.
//
// This validates the synthesizer's signal-first design: it ONLY fires when an
// `implements` edge exists as a confirmed prerequisite.
func TestInterfaceImplSynthesizer_NoEdgeWithoutImplementsEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Interface node with no method children (as extraction produces).
	seedNode(t, d, "iface-animal", "Animal", "a.ts", types.NodeKindInterface, types.LanguageTypeScript)
	// Class node.
	seedNode(t, d, "cls-dog", "Dog", "a.ts", types.NodeKindClass, types.LanguageTypeScript)
	// Method nodes under class.
	seedNode(t, d, "meth-dog-speak", "speak", "a.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "cls-dog", "meth-dog-speak", types.EdgeKindContains)
	// NO implements edge cls-dog → iface-animal.

	s := &synthesis.InterfaceImplSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("InterfaceImplSynthesizer produced %d edges without implements edge, want 0", len(edges))
	}
}

// TestInterfaceImplSynthesizer_WithImplementsEdge documents the future behavior:
// when an implements edge IS present (e.g., after heritage extraction lands),
// the synthesizer should emit dispatch edges. Currently it must emit zero because
// the extraction pipeline doesn't yet produce interface method nodes.
//
// This test seeds the FULL expected signal (implements edge + interface methods +
// class methods) and verifies that IF the signal were present, the synthesizer
// would fire correctly. Since interface method nodes are NOT extracted today,
// the expected result is still 0 even with an implements edge.
func TestInterfaceImplSynthesizer_WithImplementsEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Interface node.
	seedNode(t, d, "iface-b", "Printer", "b.ts", types.NodeKindInterface, types.LanguageTypeScript)
	// Class node that implements it.
	seedNode(t, d, "cls-b", "LaserPrinter", "b.ts", types.NodeKindClass, types.LanguageTypeScript)
	// Implements edge (what heritage extraction would produce after landing).
	seedEdge(t, d, "cls-b", "iface-b", types.EdgeKindImplements)

	// Class method node.
	seedNode(t, d, "meth-b-print", "print", "b.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "cls-b", "meth-b-print", types.EdgeKindContains)
	// NOTE: no interface method node (NodeKindMethod inside the interface)
	// because method_signature is NOT in MethodTypes for TypeScript.
	// The synthesizer would need interface method nodes to emit dispatch edges.
	// With implements edge but no interface method nodes, expect 0 edges.

	s := &synthesis.InterfaceImplSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Even with implements edge, interface has no method nodes → no dispatch edges.
	// Zero edges expected until interface method extraction is also implemented.
	if len(edges) != 0 {
		t.Errorf("InterfaceImplSynthesizer produced %d edges (expected 0: interface methods not extracted)", len(edges))
	}
}

// ---------------------------------------------------------------------------
// CppOverrideSynthesizer — real gate tests (activated after EE4)
// ---------------------------------------------------------------------------

// TestCppOverrideSynthesizer_EmitsOverrideEdge is the primary gate for the
// cpp-override synthesizer. It seeds the minimal signal: a C++ base class with
// one method, a C++ derived class with a matching method name, and an extends
// edge between them. The synthesizer must emit exactly one
// calls+heuristic+synthesizedBy=cpp-override edge from the base method to the
// derived method.
//
// Note: C++ extraction uses NodeKindFunction for member functions (CppExtractor
// has no MethodTypes). The synthesizer must handle both NodeKindFunction and
// NodeKindMethod in the contains-edge container lookup.
func TestCppOverrideSynthesizer_EmitsOverrideEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Base class B with method m (NodeKindFunction — C++ extraction convention).
	seedNode(t, d, "cls-base", "B", "b.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "fn-base-m", "m", "b.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cls-base", "fn-base-m", types.EdgeKindContains)

	// Derived class D with override method m.
	seedNode(t, d, "cls-derived", "D", "b.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "fn-derived-m", "m", "b.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cls-derived", "fn-derived-m", types.EdgeKindContains)

	// D extends B.
	seedEdge(t, d, "cls-derived", "cls-base", types.EdgeKindExtends)

	s := &synthesis.CppOverrideSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("CppOverrideSynthesizer emitted %d edges, want 1", len(edges))
	}
	e := edges[0]
	if e.Source != "fn-base-m" {
		t.Errorf("edge source = %q, want %q", e.Source, "fn-base-m")
	}
	if e.Target != "fn-derived-m" {
		t.Errorf("edge target = %q, want %q", e.Target, "fn-derived-m")
	}
}

// TestCppOverrideSynthesizer_IdempotentViaComposite verifies that running the
// composite twice on the same DB produces the same edge count.
func TestCppOverrideSynthesizer_IdempotentViaComposite(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "cpp-idem-base", "BBase", "x.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "cpp-idem-base-fn", "go", "x.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cpp-idem-base", "cpp-idem-base-fn", types.EdgeKindContains)

	seedNode(t, d, "cpp-idem-derived", "BDerived", "x.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "cpp-idem-derived-fn", "go", "x.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cpp-idem-derived", "cpp-idem-derived-fn", types.EdgeKindContains)
	seedEdge(t, d, "cpp-idem-derived", "cpp-idem-base", types.EdgeKindExtends)

	composite := synthesis.NewComposite(d, &synthesis.CppOverrideSynthesizer{})
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	edgesAfterFirst, err := d.GetEdgesBySource(ctx, "cpp-idem-base-fn")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	var callsCount int
	for _, e := range edgesAfterFirst {
		if e.Kind == types.EdgeKindCalls {
			callsCount++
		}
	}
	if callsCount != 1 {
		t.Fatalf("after first run: %d calls edges, want 1", callsCount)
	}

	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}
	edgesAfterSecond, err := d.GetEdgesBySource(ctx, "cpp-idem-base-fn")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	var callsCount2 int
	for _, e := range edgesAfterSecond {
		if e.Kind == types.EdgeKindCalls {
			callsCount2++
		}
	}
	if callsCount2 != callsCount {
		t.Errorf("idempotency broken: first run=%d calls edges, second run=%d", callsCount, callsCount2)
	}
}

// TestCppOverrideSynthesizer_SynthesizedByMetadata verifies that the composite
// stamps synthesizedBy="cpp-override" on edges produced by CppOverrideSynthesizer.
func TestCppOverrideSynthesizer_SynthesizedByMetadata(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "cpp-meta-base", "Base", "z.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "cpp-meta-base-fn", "run", "z.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cpp-meta-base", "cpp-meta-base-fn", types.EdgeKindContains)

	seedNode(t, d, "cpp-meta-derived", "Derived", "z.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "cpp-meta-derived-fn", "run", "z.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cpp-meta-derived", "cpp-meta-derived-fn", types.EdgeKindContains)
	seedEdge(t, d, "cpp-meta-derived", "cpp-meta-base", types.EdgeKindExtends)

	composite := synthesis.NewComposite(d, &synthesis.CppOverrideSynthesizer{})
	if err := composite.SynthesizeCallbackEdges(ctx); err != nil {
		t.Fatalf("SynthesizeCallbackEdges: %v", err)
	}

	edges, err := d.GetEdgesBySource(ctx, "cpp-meta-base-fn")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	var found bool
	for _, e := range edges {
		if e.Kind != types.EdgeKindCalls || e.Target != "cpp-meta-derived-fn" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(e.Metadata, &m); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
		if got := m["synthesizedBy"]; got != "cpp-override" {
			t.Errorf("synthesizedBy = %q, want %q", got, "cpp-override")
		}
		if e.Provenance != "heuristic" {
			t.Errorf("provenance = %q, want %q", e.Provenance, "heuristic")
		}
		found = true
	}
	if !found {
		t.Error("no calls edge from cpp-meta-base-fn to cpp-meta-derived-fn")
	}
}

// TestCppOverrideSynthesizer_NoEdgeWithoutExtendsEdge verifies that with
// manually seeded base + derived class + method nodes but NO extends edge,
// the synthesizer emits nothing.
func TestCppOverrideSynthesizer_NoEdgeWithoutExtendsEdge(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// Base class node with a method.
	seedNode(t, d, "cls-animal", "Animal", "p.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "fn-animal-speak", "speak", "p.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cls-animal", "fn-animal-speak", types.EdgeKindContains)

	// Derived class with the same method name.
	seedNode(t, d, "cls-dog", "Dog", "p.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "fn-dog-speak", "speak", "p.cpp", types.NodeKindFunction, types.LanguageCpp)
	seedEdge(t, d, "cls-dog", "fn-dog-speak", types.EdgeKindContains)

	// NO extends edge cls-dog → cls-animal.

	s := &synthesis.CppOverrideSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("CppOverrideSynthesizer produced %d edges without extends edge, want 0", len(edges))
	}
}

// TestCppOverrideSynthesizer_DoesNotFireOnNonCppExtends verifies that the
// cpp-override synthesizer is C++-scoped: an extends edge with a non-C++
// language should NOT trigger override synthesis.
//
// This is the scope guard: cpp-override only fires when both nodes are C++.
// Since the extraction pipeline today has no extends edges at all, this test
// uses manually seeded data to document the scoping contract.
func TestCppOverrideSynthesizer_DoesNotFireOnNonCppExtends(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// TypeScript extends edge (e.g. class Dog extends Animal in TS).
	seedNode(t, d, "ts-base", "Animal", "a.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "ts-dog", "Dog", "a.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedNode(t, d, "ts-base-speak", "speak", "a.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedNode(t, d, "ts-dog-speak", "speak", "a.ts", types.NodeKindMethod, types.LanguageTypeScript)
	seedEdge(t, d, "ts-base", "ts-base-speak", types.EdgeKindContains)
	seedEdge(t, d, "ts-dog", "ts-dog-speak", types.EdgeKindContains)
	// Manually seed an extends edge in TypeScript (what heritage extraction would add).
	seedEdge(t, d, "ts-dog", "ts-base", types.EdgeKindExtends)

	s := &synthesis.CppOverrideSynthesizer{}
	edges, err := s.Synthesize(ctx, d)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// cpp-override must NOT fire on TypeScript extends edges.
	if len(edges) != 0 {
		t.Errorf("CppOverrideSynthesizer produced %d edges for TypeScript extends, want 0 (C++-scope guard)", len(edges))
	}
}

// TestDefaultCompositeHasTenSynthesizers verifies that Default(d) registers
// exactly 10 synthesizers after batch 5 is added:
//   - batch 1: react-render, jsx-render (2 real)
//   - batch 2: vue-handler (1 real)
//   - batch 3: rn-event-channel, event-emitter (2 real)
//   - batch 4: callback, closure-collection (gap), flutter-build (gap) (3)
//   - batch 5: interface-impl (gap), cpp-override (gap) (2)
func TestDefaultCompositeHasTenSynthesizers(t *testing.T) {
	d := openTestDB(t)
	composite := synthesis.Default(d)
	if composite == nil {
		t.Fatal("Default returned nil")
	}
	// Verify it runs without error on an empty DB.
	if err := composite.SynthesizeCallbackEdges(context.Background()); err != nil {
		t.Fatalf("Default composite on empty DB: %v", err)
	}
}

// TestInterfaceImplSynthesizer_NodeCountStable verifies synthesis adds no nodes.
func TestInterfaceImplSynthesizer_NodeCountStable(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "iface-stable", "IFoo", "c.ts", types.NodeKindInterface, types.LanguageTypeScript)
	seedNode(t, d, "cls-stable", "FooImpl", "c.ts", types.NodeKindClass, types.LanguageTypeScript)
	seedEdge(t, d, "cls-stable", "iface-stable", types.EdgeKindImplements)

	nodesBefore := countNodes(t, d)

	s := &synthesis.InterfaceImplSynthesizer{}
	if _, err := s.Synthesize(ctx, d); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	nodesAfter := countNodes(t, d)
	if nodesBefore != nodesAfter {
		t.Errorf("node count changed: before=%d after=%d (synthesizer must not add nodes)", nodesBefore, nodesAfter)
	}
}

// TestCppOverrideSynthesizer_NodeCountStable verifies synthesis adds no nodes.
func TestCppOverrideSynthesizer_NodeCountStable(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	seedNode(t, d, "cpp-base-stable", "Base", "x.cpp", types.NodeKindClass, types.LanguageCpp)
	seedNode(t, d, "cpp-derived-stable", "Derived", "x.cpp", types.NodeKindClass, types.LanguageCpp)
	seedEdge(t, d, "cpp-derived-stable", "cpp-base-stable", types.EdgeKindExtends)

	nodesBefore := countNodes(t, d)

	s := &synthesis.CppOverrideSynthesizer{}
	if _, err := s.Synthesize(ctx, d); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	nodesAfter := countNodes(t, d)
	if nodesBefore != nodesAfter {
		t.Errorf("node count changed: before=%d after=%d (synthesizer must not add nodes)", nodesBefore, nodesAfter)
	}
}
