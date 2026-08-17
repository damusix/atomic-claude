package types_test

import (
	"bytes"
	"encoding/json"
	"sort"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Adding or removing a kind breaks the on-disk data model, so the count is
// pinned to force the change through this gate.
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
		types.NodeKindStage,
		types.NodeKindStream,
		types.NodeKindTask,
		types.NodeKindModel,
		types.NodeKindFileFormat,
		types.NodeKindMacro,
		types.NodeKindScript,
		types.NodeKindPackage,
	}

	if len(types.AllNodeKinds) != 39 {
		t.Errorf("AllNodeKinds: got %d entries, want 39", len(types.AllNodeKinds))
	}

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

// The gate on the json.RawMessage convention: opaque bytes must round-trip
// unmutated. Replacing RawMessage with typed structs breaks this.
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

// The gate on the stable-sort contract every serialisation path depends on:
// output must not vary with Go's map iteration order.
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

	const rounds = 50
	var baseline []byte
	for i := 0; i < rounds; i++ {
		nodes := types.SubgraphSortedNodes(sg)

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

	nodes := types.SubgraphSortedNodes(sg)
	wantIDs := []string{"function:aaa", "function:mmm", "function:zzz"}
	for i, n := range nodes {
		if n.ID != wantIDs[i] {
			t.Errorf("position %d: got ID %q, want %q", i, n.ID, wantIDs[i])
		}
	}
}

// A symbol name mapping to several definitions must surface the relationships
// on every one of them, not just the first.
func TestMergeSubgraphs_UnionsDedupsAndUnionsRoots(t *testing.T) {
	shared := types.Edge{Source: "caller", Target: "proc", Kind: types.EdgeKindCalls, Line: 7}
	sgs := []types.Subgraph{
		{
			Nodes: map[string]types.Node{"proc": {ID: "proc"}},
			Roots: []string{"proc"},
		},
		{
			Nodes: map[string]types.Node{"proc": {ID: "proc"}, "caller": {ID: "caller"}},
			Edges: []types.Edge{shared, shared}, // same edge twice → must collapse to one
			Roots: []string{"proc", "proc2"},
		},
	}

	got := types.MergeSubgraphs(sgs)

	if len(got.Nodes) != 2 {
		t.Errorf("nodes union: got %d, want 2 (proc, caller)", len(got.Nodes))
	}
	if _, ok := got.Nodes["caller"]; !ok {
		t.Error("caller node from the second subgraph was dropped")
	}
	if len(got.Edges) != 1 {
		t.Errorf("duplicate edges must dedup: got %d, want 1", len(got.Edges))
	}
	if len(got.Roots) != 2 {
		t.Errorf("roots dedup: got %d, want 2 (proc, proc2)", len(got.Roots))
	}

	empty := types.MergeSubgraphs(nil)
	if empty.Nodes == nil {
		t.Error("MergeSubgraphs(nil) must return a usable (non-nil) Nodes map")
	}
}
