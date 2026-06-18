package languages_test

// Erlang extraction tests.
//
// Fixture: a representative Erlang source module exercising all constructs the
// extractor must handle — module attribute, exported + unexported functions with
// arity, a record, a behaviour declaration, a -define macro, a remote call, and
// local recursive calls.
//
// Probe used to verify node types:
//   tmp/probe-erlang/main.go  + tmp/probe-erlang/fields.go
//
// Key node-type/field shapes confirmed by the probe:
//
//	module_attribute  .name → atom
//	behaviour_attribute .name → atom
//	record_decl       .name → atom; named children include record_field
//	pp_define         first named child → macro_lhs; macro_lhs .name → var
//	fun_decl          .clause → function_clause
//	function_clause   .name → atom; .args → expr_args (arity = NamedChildCount)
//	call              first named child → atom (callee name)
//	remote            → remote_module + call (inner call carries the fn name)

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// erlangFixture is a representative Erlang source file (.erl).
// It includes:
//   - -module, -behaviour, -export declarations
//   - -define(MAX_RETRIES, 3) macro
//   - -record(state, {name, retries}) with two fields
//   - add/2 (exported, 2 args), loop/1 (unexported, 1 arg)
//   - multi/1 (multi-clause: pattern-match on 0 and N)
//   - remote call to math:sqrt(Z) inside sqrt_sum/2
//   - local call to add/2 inside sqrt_sum/2
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

// newErlangExtractor returns a configured TreeSitterExtractor for Erlang.
func newErlangExtractor(t *testing.T) *extraction.TreeSitterExtractor {
	t.Helper()
	cfg, lang, ok := languages.NewRegistry().For(types.LanguageErlang)
	if !ok {
		t.Fatal("Erlang not registered in languages.NewRegistry()")
	}
	return newExtractor(t, lang, cfg)
}

// extractErlang runs the Erlang extractor on erlangFixture and fails fast if
// there are extraction errors.
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

// ---------------------------------------------------------------------------
// Module
// ---------------------------------------------------------------------------

// TestErlang_ModuleExtracted asserts that -module(mymod). emits a NodeKindModule.
// WHY: The module is the top-level namespace; resolution depends on it.
func TestErlang_ModuleExtracted(t *testing.T) {
	result := extractErlang(t)
	mod := findNode(result.Nodes, types.NodeKindModule, "mymod")
	if mod == nil {
		t.Fatalf("module node 'mymod' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !mod.IsExported {
		t.Errorf("module node 'mymod' IsExported=false, want true")
	}
}

// ---------------------------------------------------------------------------
// Functions + arity
// ---------------------------------------------------------------------------

// TestErlang_ExportedFunctionExtracted asserts add/2 is extracted and marked exported.
// WHY: add/2 is in -export([add/2, sqrt_sum/2]); IsExported must reflect that.
func TestErlang_ExportedFunctionExtracted(t *testing.T) {
	result := extractErlang(t)

	add := findNode(result.Nodes, types.NodeKindFunction, "add")
	if add == nil {
		t.Fatalf("function node 'add' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !add.IsExported {
		t.Errorf("add IsExported=false, want true (add/2 is in -export)")
	}
	// Arity captured in Signature.
	if add.Signature != "add/2" {
		t.Errorf("add Signature=%q, want %q", add.Signature, "add/2")
	}
}

// TestErlang_UnexportedFunctionExtracted asserts loop/1 is extracted but NOT exported.
// WHY: loop/1 is not in -export([...]); IsExported must be false.
func TestErlang_UnexportedFunctionExtracted(t *testing.T) {
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

// TestErlang_MultiClauseFunctionExtracted asserts multi/1 is extracted with correct arity.
// WHY: Multi-clause functions produce separate fun_decl nodes in the grammar;
// the extractor must produce at least one function node for the shared name,
// and the Signature must carry the arity identity contract ("multi/1").
func TestErlang_MultiClauseFunctionExtracted(t *testing.T) {
	result := extractErlang(t)
	multi := findNode(result.Nodes, types.NodeKindFunction, "multi")
	if multi == nil {
		t.Fatalf("function node 'multi' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	// multi is not exported; confirm.
	if multi.IsExported {
		t.Errorf("multi IsExported=true, want false")
	}
	// Arity captured in Signature.
	if multi.Signature != "multi/1" {
		t.Errorf("multi Signature=%q, want %q", multi.Signature, "multi/1")
	}
}

// TestErlang_SqrtSumExportedWithArity asserts sqrt_sum/2 is exported with correct arity.
// WHY: sqrt_sum/2 is in -export([...]); IsExported must be true; Signature must be "sqrt_sum/2".
func TestErlang_SqrtSumExportedWithArity(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Record
// ---------------------------------------------------------------------------

// TestErlang_RecordExtracted asserts -record(state, {name, retries}) → NodeKindStruct.
// WHY: Records are the primary data structure in Erlang; callers index them for
// field resolution. Missing record nodes break struct-field resolution.
func TestErlang_RecordExtracted(t *testing.T) {
	result := extractErlang(t)
	rec := findNode(result.Nodes, types.NodeKindStruct, "state")
	if rec == nil {
		t.Fatalf("record node 'state' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !rec.IsExported {
		t.Errorf("record 'state' IsExported=false, want true (records are always public)")
	}
}

// ---------------------------------------------------------------------------
// Macro (define)
// ---------------------------------------------------------------------------

// TestErlang_MacroExtracted asserts -define(MAX_RETRIES, 3) → NodeKindVariable.
// WHY: Macro constants are indexed as named bindings for reference tracking.
func TestErlang_MacroExtracted(t *testing.T) {
	result := extractErlang(t)
	macro := findNode(result.Nodes, types.NodeKindVariable, "MAX_RETRIES")
	if macro == nil {
		t.Fatalf("macro node 'MAX_RETRIES' not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Imports / behaviour
// ---------------------------------------------------------------------------

// TestErlang_BehaviourImportEmitted asserts -behaviour(gen_server) emits an import edge.
// WHY: Behaviour declarations are module-level dependencies; the resolution layer
// uses import edges to discover which OTP behaviours a module implements.
func TestErlang_BehaviourImportEmitted(t *testing.T) {
	result := extractErlang(t)
	importCount := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importCount == 0 {
		t.Fatalf("no EdgeKindImports references emitted; expected at least one for -behaviour(gen_server)")
	}
	// Verify the gen_server reference exists.
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

// ---------------------------------------------------------------------------
// Calls
// ---------------------------------------------------------------------------

// TestErlang_LocalCallExtracted asserts a local call (add/loop) emits EdgeKindCalls.
// WHY: Call edges drive the call-graph; without them, callers/callees queries are empty.
func TestErlang_LocalCallExtracted(t *testing.T) {
	result := extractErlang(t)
	callCount := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callCount == 0 {
		t.Fatalf("no EdgeKindCalls references emitted; expected at least one local call")
	}
}

// ---------------------------------------------------------------------------
// Stability
// ---------------------------------------------------------------------------

// TestErlang_ExtractionStable asserts the same fixture produces the same node count
// on two successive extractions.
// WHY: Non-deterministic output (e.g. from map iteration or state leakage) would
// silently corrupt the index on re-indexing.
func TestErlang_ExtractionStable(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// export_all
// ---------------------------------------------------------------------------

// erlangExportAllFixture is an Erlang module that uses -compile(export_all)
// with no -export([...]) list. All functions must be marked exported.
const erlangExportAllFixture = `-module(mymod_all).
-compile(export_all).

foo(X) -> X.
bar(X, Y) -> X + Y.
`

// TestErlang_ExportAll_FunctionsAreExported asserts that -compile(export_all)
// causes all fun_decl nodes to be marked IsExported=true, even without an
// explicit -export([...]) list.
// WHY: OTP test and umbrella modules commonly use -compile(export_all) as a
// shorthand to export everything. Without this short-circuit, none of their
// functions would appear in the symbol graph as exported, silently hiding the
// public surface from callers queries.
func TestErlang_ExportAll_FunctionsAreExported(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Record fields
// ---------------------------------------------------------------------------

// TestErlang_RecordFieldsEmitted asserts that -record(state, {name, retries})
// emits NodeKindField nodes for each field.
// WHY: FieldTypes is wired (record_field → NodeKindField); if the framework
// does not walk into record_decl children and match record_field nodes, field
// resolution callers will silently get no field symbols.
func TestErlang_RecordFieldsEmitted(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Export false-positive regression (Bug #4)
// ---------------------------------------------------------------------------

// erlangFalsePositiveFixture is an Erlang module that exports foo/1 explicitly
// but references bar/0 only as "fun bar/0" in a lookup table — a pattern
// identical to the rabbit_amqqueue.erl false-positive case. bar/0 must NOT be
// marked exported.
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

// TestErlang_FunRefDoesNotFalsePositiveExport asserts that a function referenced
// only via "fun bar/0" in the source is NOT marked exported=1.
// WHY: The previous implementation used strings.Contains(source, "bar/0") which
// matched the "fun bar/0" expression and produced a false positive. The fix parses
// -export([…]) attributes into a set and checks set membership instead.
func TestErlang_FunRefDoesNotFalsePositiveExport(t *testing.T) {
	e := newErlangExtractor(t)
	result := e.Extract(context.Background(), "src/mymod_fp.erl", erlangFalsePositiveFixture, types.LanguageErlang)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected extraction errors: %v", result.Errors)
	}

	// foo/1 must be exported (it is in -export([foo/1])).
	foo := findNode(result.Nodes, types.NodeKindFunction, "foo")
	if foo == nil {
		t.Fatalf("function node 'foo' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !foo.IsExported {
		t.Errorf("foo IsExported=false, want true (foo/1 is in -export)")
	}

	// bar/0 must NOT be exported — it only appears as "fun bar/0", not in -export.
	bar := findNode(result.Nodes, types.NodeKindFunction, "bar")
	if bar == nil {
		t.Fatalf("function node 'bar' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if bar.IsExported {
		t.Errorf("bar IsExported=true, want false (bar/0 is only referenced as 'fun bar/0', not in -export) — false-positive regression")
	}

	// table/0 must also NOT be exported.
	table := findNode(result.Nodes, types.NodeKindFunction, "table")
	if table == nil {
		t.Fatalf("function node 'table' not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if table.IsExported {
		t.Errorf("table IsExported=true, want false (table/0 is not in -export)")
	}
}

// erlangExportAllCommentFixture has "export_all" only in a comment — NOT in a
// -compile attribute. Functions must NOT be marked exported as a result.
const erlangExportAllCommentFixture = `-module(mymod_ea_comment).
%% Note: we intentionally do NOT use -compile(export_all) here.

foo(X) -> X.
`

// TestErlang_ExportAllInCommentDoesNotExport asserts that the literal string
// "export_all" appearing only in a comment does NOT cause all functions to be
// marked exported.
// WHY: The previous erlangHasExportAll used strings.Contains(source, "export_all")
// which would have matched the comment. The fix anchors to the -compile(...) form.
func TestErlang_ExportAllInCommentDoesNotExport(t *testing.T) {
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

// erlangExportAllListFixture uses the bracketed -compile([…, export_all]) form.
const erlangExportAllListFixture = `-module(mymod_ea_list).
-compile([debug_info, export_all]).

baz(X, Y) -> X + Y.
qux() -> ok.
`

// TestErlang_ExportAllListForm asserts that -compile([debug_info, export_all])
// causes all functions to be marked exported.
// WHY: OTP modules commonly bundle compile options in a list; the export_all
// option must be recognized in that list form, not only as a bare atom.
func TestErlang_ExportAllListForm(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// TestErlang_RegistryEntry asserts LanguageErlang is registered with LangErlang.
// WHY: A missing registry entry means .erl/.hrl files are silently skipped by the
// orchestrator during indexing.
func TestErlang_RegistryEntry(t *testing.T) {
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
