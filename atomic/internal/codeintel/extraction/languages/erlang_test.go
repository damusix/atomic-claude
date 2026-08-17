package languages_test

// Every fixture here runs through the real grammar, so these also cover ABI and
// pool wiring, not only the config.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// One fixture covering every construct the config handles: a module, exported
// and unexported functions, a multi-clause function, a record, a behaviour, a
// macro, and both a local and a remote call.
const erlangFixture = `-module(mymod).
-behaviour(gen_server).
-export([add/2, sqrt_sum/2]).

-define(MAX_RETRIES, 3).

-record(state, {name, retries = ?MAX_RETRIES}).

%% Adds two numbers. Exported.
add(X, Y) ->
    X + Y.

%% Tail-recursive consumer. NOT exported.
loop(State) ->
    receive
        stop -> ok;
        _ -> loop(State)
    end.

%% Multi-clause function. NOT exported.
multi(0) ->
    zero;
multi(N) ->
    N.

%% Demonstrates remote call (math:sqrt) + local call (add). Exported.
sqrt_sum(A, B) ->
    SA = math:sqrt(A),
    SB = math:sqrt(B),
    add(SA, SB).
`

const erlangFixturePath = "src/mymod.erl"

func newErlangExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, lang, ok := languages.NewRegistry().For(types.LanguageErlang)
	if !ok {
		t.Fatal("Erlang not registered in languages.NewRegistry()")
	}
	return newExtractor(t, lang, cfg)
}

// extractErlang fails the test rather than returning extraction errors.
func extractErlang(t *testing.T) types.ExtractionResult {
	t.Helper()
	e := newErlangExtractor(t)
	result := e.Extract(context.Background(), erlangFixturePath, erlangFixture, types.LanguageErlang)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("extraction produced 0 nodes — regression: extractor must emit at least one node")
	}
	return result
}

// The module is Erlang's namespace, and resolution is keyed on it.
func TestErlang_ModuleExtracted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)
	mod := findNode(result.Nodes, types.NodeKindModule, "mymod")
	if mod == nil {
		t.Fatalf("module node 'mymod' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !mod.IsExported {
		t.Errorf("module node 'mymod' IsExported=false, want true")
	}
}

func TestErlang_ExportedFunctionExtracted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)

	add := findNode(result.Nodes, types.NodeKindFunction, "add")
	if add == nil {
		t.Fatalf("function node 'add' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !add.IsExported {
		t.Errorf("add IsExported=false, want true (add/2 is in -export)")
	}
	if add.Signature != "add/2" {
		t.Errorf("add Signature=%q, want %q", add.Signature, "add/2")
	}
}

func TestErlang_UnexportedFunctionExtracted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)

	loop := findNode(result.Nodes, types.NodeKindFunction, "loop")
	if loop == nil {
		t.Fatalf("function node 'loop' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if loop.IsExported {
		t.Errorf("loop IsExported=true, want false (loop/1 is not exported)")
	}
	if loop.Signature != "loop/1" {
		t.Errorf("loop Signature=%q, want %q", loop.Signature, "loop/1")
	}
}

// Each clause is its own fun_decl in the grammar, but they share one name and
// arity, which is the identity that has to survive.
func TestErlang_MultiClauseFunctionExtracted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)
	multi := findNode(result.Nodes, types.NodeKindFunction, "multi")
	if multi == nil {
		t.Fatalf("function node 'multi' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if multi.IsExported {
		t.Errorf("multi IsExported=true, want false")
	}
	if multi.Signature != "multi/1" {
		t.Errorf("multi Signature=%q, want %q", multi.Signature, "multi/1")
	}
}

func TestErlang_SqrtSumExportedWithArity(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)
	fn := findNode(result.Nodes, types.NodeKindFunction, "sqrt_sum")
	if fn == nil {
		t.Fatalf("function node 'sqrt_sum' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !fn.IsExported {
		t.Errorf("sqrt_sum IsExported=false, want true")
	}
	if fn.Signature != "sqrt_sum/2" {
		t.Errorf("sqrt_sum Signature=%q, want %q", fn.Signature, "sqrt_sum/2")
	}
}

// Records are Erlang's data structure, and field resolution is keyed on them.
func TestErlang_RecordExtracted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)
	rec := findNode(result.Nodes, types.NodeKindStruct, "state")
	if rec == nil {
		t.Fatalf("record node 'state' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !rec.IsExported {
		t.Errorf("record 'state' IsExported=false, want true (records are always public)")
	}
}

// Macro constants are indexed as named bindings so references to them resolve.
func TestErlang_MacroExtracted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)
	macro := findNode(result.Nodes, types.NodeKindVariable, "MAX_RETRIES")
	if macro == nil {
		t.Fatalf("macro node 'MAX_RETRIES' not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// A behaviour is a module-level dependency, and the import edge is how the
// resolution layer learns which behaviours a module implements.
func TestErlang_BehaviourImportEmitted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)
	importCount := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importCount == 0 {
		t.Fatalf("no EdgeKindImports references emitted; expected at least one for -behaviour(gen_server)")
	}
	found := false
	for _, ref := range result.UnresolvedReferences {
		if ref.ReferenceKind == types.EdgeKindImports && ref.ReferenceName == "gen_server" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("EdgeKindImports reference 'gen_server' not found in UnresolvedReferences")
	}
}

// Without call refs the callers and callees queries return nothing.
func TestErlang_LocalCallExtracted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)
	callCount := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callCount == 0 {
		t.Fatalf("no EdgeKindCalls references emitted; expected at least one local call")
	}
}

// Non-deterministic output, from map iteration or leaked state, would corrupt
// the index silently on every re-index.
func TestErlang_ExtractionStable(t *testing.T) {
	t.Parallel()
	e := newErlangExtractor(t)
	ctx := context.Background()
	r1 := e.Extract(ctx, erlangFixturePath, erlangFixture, types.LanguageErlang)
	r2 := e.Extract(ctx, erlangFixturePath, erlangFixture, types.LanguageErlang)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("non-deterministic extraction: first=%d nodes, second=%d nodes",
			len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("non-deterministic references: first=%d, second=%d",
			len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// -compile(export_all) with no -export list at all.
const erlangExportAllFixture = `-module(mymod_all).
-compile(export_all).

foo(X) -> X.
bar(X, Y) -> X + Y.
`

// OTP test and umbrella modules lean on this shorthand, and without the
// short-circuit their entire public surface reads as unexported.
func TestErlang_ExportAll_FunctionsAreExported(t *testing.T) {
	t.Parallel()
	e := newErlangExtractor(t)
	result := e.Extract(context.Background(), "src/mymod_all.erl", erlangExportAllFixture, types.LanguageErlang)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	foo := findNode(result.Nodes, types.NodeKindFunction, "foo")
	if foo == nil {
		t.Fatalf("function node 'foo' not found")
	}
	if !foo.IsExported {
		t.Errorf("foo IsExported=false, want true (-compile(export_all) must export all functions)")
	}

	bar := findNode(result.Nodes, types.NodeKindFunction, "bar")
	if bar == nil {
		t.Fatalf("function node 'bar' not found")
	}
	if !bar.IsExported {
		t.Errorf("bar IsExported=false, want true (-compile(export_all) must export all functions)")
	}
}

// Record fields only surface if the walk descends into the record declaration,
// so this pins the descent, not just the config entry.
func TestErlang_RecordFieldsEmitted(t *testing.T) {
	t.Parallel()
	result := extractErlang(t)

	nameField := findNode(result.Nodes, types.NodeKindField, "name")
	if nameField == nil {
		t.Fatalf("field node 'name' not found; nodes: %s", nodeKindList(result.Nodes))
	}

	retriesField := findNode(result.Nodes, types.NodeKindField, "retries")
	if retriesField == nil {
		t.Fatalf("field node 'retries' not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// Exports foo/1, and mentions bar/0 only as a "fun bar/0" reference in a table.
const erlangFalsePositiveFixture = `-module(mymod_fp).
-export([foo/1]).

%% Exported — in -export list.
foo(X) -> X.

%% Not exported — only referenced as a fun expression, never in -export.
bar() -> ok.

%% A lookup table that holds "fun bar/0" — the source of the false positive.
table() ->
    [{check, fun bar/0}].
`

// Regression guard: export status was once a substring search over the source,
// which any "fun name/arity" reference satisfied.
func TestErlang_FunRefDoesNotFalsePositiveExport(t *testing.T) {
	t.Parallel()
	e := newErlangExtractor(t)
	result := e.Extract(context.Background(), "src/mymod_fp.erl", erlangFalsePositiveFixture, types.LanguageErlang)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	foo := findNode(result.Nodes, types.NodeKindFunction, "foo")
	if foo == nil {
		t.Fatalf("function node 'foo' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !foo.IsExported {
		t.Errorf("foo IsExported=false, want true (foo/1 is in -export)")
	}

	bar := findNode(result.Nodes, types.NodeKindFunction, "bar")
	if bar == nil {
		t.Fatalf("function node 'bar' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if bar.IsExported {
		t.Errorf("bar IsExported=true, want false (bar/0 is only referenced as 'fun bar/0', not in -export) — false-positive regression")
	}

	table := findNode(result.Nodes, types.NodeKindFunction, "table")
	if table == nil {
		t.Fatalf("function node 'table' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if table.IsExported {
		t.Errorf("table IsExported=true, want false (table/0 is not in -export)")
	}
}

// "export_all" appears only inside a comment, never in a -compile attribute.
const erlangExportAllCommentFixture = `-module(mymod_ea_comment).
%% Note: we intentionally do NOT use -compile(export_all) here.

foo(X) -> X.
`

// Regression guard: the export_all check was once a substring search, which a
// comment mentioning it satisfied.
func TestErlang_ExportAllInCommentDoesNotExport(t *testing.T) {
	t.Parallel()
	e := newErlangExtractor(t)
	result := e.Extract(context.Background(), "src/mymod_ea_comment.erl", erlangExportAllCommentFixture, types.LanguageErlang)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	foo := findNode(result.Nodes, types.NodeKindFunction, "foo")
	if foo == nil {
		t.Fatalf("function node 'foo' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if foo.IsExported {
		t.Errorf("foo IsExported=true, want false ('export_all' only in comment, not in -compile) — false-positive regression")
	}
}

// The bracketed -compile([…]) form.
const erlangExportAllListFixture = `-module(mymod_ea_list).
-compile([debug_info, export_all]).

baz(X, Y) -> X + Y.
qux() -> ok.
`

// OTP modules bundle compile options in a list, so the bare-atom form is not
// the only one that has to be recognized.
func TestErlang_ExportAllListForm(t *testing.T) {
	t.Parallel()
	e := newErlangExtractor(t)
	result := e.Extract(context.Background(), "src/mymod_ea_list.erl", erlangExportAllListFixture, types.LanguageErlang)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	baz := findNode(result.Nodes, types.NodeKindFunction, "baz")
	if baz == nil {
		t.Fatalf("function node 'baz' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !baz.IsExported {
		t.Errorf("baz IsExported=false, want true (-compile([…, export_all]) must export all functions)")
	}

	qux := findNode(result.Nodes, types.NodeKindFunction, "qux")
	if qux == nil {
		t.Fatalf("function node 'qux' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !qux.IsExported {
		t.Errorf("qux IsExported=false, want true (-compile([…, export_all]) must export all functions)")
	}
}

// A missing registry entry makes the orchestrator skip the language's files
// silently rather than fail.
func TestErlang_RegistryEntry(t *testing.T) {
	t.Parallel()
	reg := languages.NewRegistry()
	cfg, lang, ok := reg.For(types.LanguageErlang)
	if !ok {
		t.Fatal("LanguageErlang not in registry — orchestrator will silently skip .erl files")
	}
	if lang != extraction.LangErlang {
		t.Errorf("registry Lang = %d, want LangErlang (%d)", lang, extraction.LangErlang)
	}
	if len(cfg.FunctionTypes) == 0 {
		t.Errorf("FunctionTypes is empty — function extraction will produce nothing")
	}
}
