package types_test

import (
	"bytes"
	"encoding/json"
	"sort"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Const-set contract tests (appendix C gate)
// ---------------------------------------------------------------------------

// TestNodeKindCount asserts that AllNodeKinds contains exactly 31 entries
// matching the appendix C verbatim list. Any addition or removal breaks the
// on-disk data model and should be a deliberate, test-gated change.
func TestNodeKindCount(t *testing.T) {
	want := []types.NodeKind{
		types.NodeKindFile,
		types.NodeKindModule,
		types.NodeKindClass,
		types.NodeKindStruct,
		types.NodeKindInterface,
		types.NodeKindTrait,
		types.NodeKindProtocol,
		types.NodeKindFunction,
		types.NodeKindMethod,
		types.NodeKindProperty,
		types.NodeKindField,
		types.NodeKindVariable,
		types.NodeKindConstant,
		types.NodeKindEnum,
		types.NodeKindEnumMember,
		types.NodeKindTypeAlias,
		types.NodeKindNamespace,
		types.NodeKindParameter,
		types.NodeKindImport,
		types.NodeKindExport,
		types.NodeKindRoute,
		types.NodeKindComponent,
		types.NodeKindTable,
		types.NodeKindView,
		types.NodeKindColumn,
		types.NodeKindProcedure,
		types.NodeKindTrigger,
		types.NodeKindConstraint,
		types.NodeKindIndex,
		types.NodeKindSequence,
		types.NodeKindPolicy,
	}

	if len(types.AllNodeKinds) != 31 {
		t.Errorf("AllNodeKinds: got %d entries, want 31", len(types.AllNodeKinds))
	}

	// Build a lookup set from the exported slice.
	got := make(map[types.NodeKind]bool, len(types.AllNodeKinds))
	for _, k := range types.AllNodeKinds {
		got[k] = true
	}

	for _, k := range want {
		if !got[k] {
			t.Errorf("AllNodeKinds: missing %q", k)
		}
	}

	if len(types.AllNodeKinds) > len(want) {
		wantSet := make(map[types.NodeKind]bool, len(want))
		for _, k := range want {
			wantSet[k] = true
		}
		for _, k := range types.AllNodeKinds {
			if !wantSet[k] {
				t.Errorf("AllNodeKinds: extra entry %q not in appendix C", k)
			}
		}
	}
}

// TestEdgeKindCount asserts exactly 13 EdgeKind entries per appendix C.
// CP5 added EdgeKindWrites for routine→table mutation targets.
func TestEdgeKindCount(t *testing.T) {
	want := []types.EdgeKind{
		types.EdgeKindContains,
		types.EdgeKindCalls,
		types.EdgeKindImports,
		types.EdgeKindExports,
		types.EdgeKindExtends,
		types.EdgeKindImplements,
		types.EdgeKindReferences,
		types.EdgeKindTypeOf,
		types.EdgeKindReturns,
		types.EdgeKindInstantiates,
		types.EdgeKindOverrides,
		types.EdgeKindDecorates,
		types.EdgeKindWrites,
	}

	if len(types.AllEdgeKinds) != 13 {
		t.Errorf("AllEdgeKinds: got %d entries, want 13", len(types.AllEdgeKinds))
	}

	got := make(map[types.EdgeKind]bool, len(types.AllEdgeKinds))
	for _, k := range types.AllEdgeKinds {
		got[k] = true
	}

	for _, k := range want {
		if !got[k] {
			t.Errorf("AllEdgeKinds: missing %q", k)
		}
	}

	if len(types.AllEdgeKinds) > len(want) {
		wantSet := make(map[types.EdgeKind]bool, len(want))
		for _, k := range want {
			wantSet[k] = true
		}
		for _, k := range types.AllEdgeKinds {
			if !wantSet[k] {
				t.Errorf("AllEdgeKinds: extra entry %q not in appendix C", k)
			}
		}
	}
}

// TestLanguageCount asserts exactly 32 Language entries per appendix C.
func TestLanguageCount(t *testing.T) {
	want := []types.Language{
		types.LanguageTypeScript,
		types.LanguageJavaScript,
		types.LanguageTSX,
		types.LanguageJSX,
		types.LanguagePython,
		types.LanguageGo,
		types.LanguageRust,
		types.LanguageJava,
		types.LanguageC,
		types.LanguageCpp,
		types.LanguageCSharp,
		types.LanguagePHP,
		types.LanguageRuby,
		types.LanguageSwift,
		types.LanguageKotlin,
		types.LanguageDart,
		types.LanguageSvelte,
		types.LanguageVue,
		types.LanguageLiquid,
		types.LanguagePascal,
		types.LanguageScala,
		types.LanguageLua,
		types.LanguageLuau,
		types.LanguageObjC,
		types.LanguageYAML,
		types.LanguageTwig,
		types.LanguageXML,
		types.LanguageProperties,
		types.LanguageUnknown,
		types.LanguageSQL,
		types.LanguageElixir,
		types.LanguageErlang,
	}

	if len(types.AllLanguages) != 32 {
		t.Errorf("AllLanguages: got %d entries, want 32", len(types.AllLanguages))
	}

	got := make(map[types.Language]bool, len(types.AllLanguages))
	for _, l := range types.AllLanguages {
		got[l] = true
	}

	for _, l := range want {
		if !got[l] {
			t.Errorf("AllLanguages: missing %q", l)
		}
	}

	if len(types.AllLanguages) > len(want) {
		wantSet := make(map[types.Language]bool, len(want))
		for _, l := range want {
			wantSet[l] = true
		}
		for _, l := range types.AllLanguages {
			if !wantSet[l] {
				t.Errorf("AllLanguages: extra entry %q not in appendix C", l)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// JSON-in-TEXT round-trip test (json.RawMessage convention)
// ---------------------------------------------------------------------------

// TestNodeJSONInTextRoundTrip proves that json.RawMessage fields in Node
// survive a marshal→unmarshal cycle without mutation. The convention chosen is
// json.RawMessage (opaque bytes); this test is the contract gate for that
// choice. If typed structs replace RawMessage, this test must be updated.
func TestNodeJSONInTextRoundTrip(t *testing.T) {
	decorators := json.RawMessage(`["@Controller","@Get"]`)
	typeParams := json.RawMessage(`["T","U"]`)

	original := types.Node{
		ID:             "function:abc123",
		Kind:           types.NodeKindFunction,
		Name:           "myFunc",
		QualifiedName:  "pkg::myFunc",
		FilePath:       "src/main.go",
		Language:       types.LanguageGo,
		StartLine:      10,
		EndLine:        20,
		StartColumn:    0,
		EndColumn:      1,
		IsExported:     true,
		Decorators:     decorators,
		TypeParameters: typeParams,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded types.Node
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if string(decoded.Decorators) != string(original.Decorators) {
		t.Errorf("Decorators mismatch after round-trip: got %s, want %s",
			decoded.Decorators, original.Decorators)
	}
	if string(decoded.TypeParameters) != string(original.TypeParameters) {
		t.Errorf("TypeParameters mismatch after round-trip: got %s, want %s",
			decoded.TypeParameters, original.TypeParameters)
	}

	// nil RawMessage survives too (represents SQL NULL).
	nullNode := types.Node{ID: "file:src/main.go", Kind: types.NodeKindFile}
	data2, err := json.Marshal(nullNode)
	if err != nil {
		t.Fatalf("json.Marshal null RawMessage: %v", err)
	}
	var decoded2 types.Node
	if err := json.Unmarshal(data2, &decoded2); err != nil {
		t.Fatalf("json.Unmarshal null RawMessage: %v", err)
	}
	if decoded2.Decorators != nil {
		t.Errorf("nil Decorators should survive as null, got %s", decoded2.Decorators)
	}
}

// ---------------------------------------------------------------------------
// Subgraph determinism test
// ---------------------------------------------------------------------------

// TestSubgraphSortedNodes proves that SubgraphSortedNodes returns the same
// ordered slice regardless of Go's non-deterministic map iteration. This is
// the stable-sort contract gate for any serialization path that iterates the
// Subgraph.Nodes map.
func TestSubgraphSortedNodes(t *testing.T) {
	sg := types.Subgraph{
		Nodes: map[string]types.Node{
			"function:zzz": {ID: "function:zzz", Name: "z"},
			"function:aaa": {ID: "function:aaa", Name: "a"},
			"function:mmm": {ID: "function:mmm", Name: "m"},
		},
		Edges: []types.Edge{},
		Roots: []string{"function:aaa"},
	}

	// Run many times to expose non-determinism if the sort is missing.
	const rounds = 50
	var baseline []byte
	for i := 0; i < rounds; i++ {
		nodes := types.SubgraphSortedNodes(sg)

		// Verify sort order.
		if !sort.SliceIsSorted(nodes, func(a, b int) bool {
			return nodes[a].ID < nodes[b].ID
		}) {
			t.Fatalf("round %d: SubgraphSortedNodes not sorted by ID", i)
		}

		data, err := json.Marshal(nodes)
		if err != nil {
			t.Fatalf("round %d: json.Marshal: %v", i, err)
		}
		if baseline == nil {
			baseline = data
		} else if !bytes.Equal(baseline, data) {
			t.Fatalf("round %d: non-deterministic output\nbaseline: %s\ngot:      %s",
				i, baseline, data)
		}
	}

	// Verify the expected ID ordering.
	nodes := types.SubgraphSortedNodes(sg)
	wantIDs := []string{"function:aaa", "function:mmm", "function:zzz"}
	for i, n := range nodes {
		if n.ID != wantIDs[i] {
			t.Errorf("position %d: got ID %q, want %q", i, n.ID, wantIDs[i])
		}
	}
}
