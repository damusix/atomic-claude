package languages_test

// Tests for the TS, JS, Python, Rust configs and the Registry.
//
// Each test:
//   1. Extracts a real fixture through the pool (proves grammar ABI ok).
//   2. Asserts the declared success criteria:
//      - At least one function node extracted.
//      - At least one class/struct/trait node extracted.
//      - At least one import UnresolvedReference emitted.
//      - At least one call site → UnresolvedReference (EdgeKindCalls).
//      - IsExported correct per-language.
//      - Node count stable across two extractions.
//
// Rust additionally: trait → NodeKindInterface OR impl method, macro_invocation
// call emits UnresolvedReference.
//
// Registry: For() returns the correct config for all 5 languages; unknown → ok=false.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newExtractor(t *testing.T, lang extraction.Lang, cfg extraction.LanguageExtractor) *extraction.TreeSitterExtractor {
	t.Helper()
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return extraction.NewTreeSitterExtractor(pool, lang, cfg)
}

func findNode(nodes []types.Node, kind types.NodeKind, namePart string) *types.Node {
	for i := range nodes {
		if nodes[i].Kind == kind && strings.Contains(nodes[i].Name, namePart) {
			return &nodes[i]
		}
	}
	return nil
}

func countUnresolved(refs []types.UnresolvedReference, kind types.EdgeKind) int {
	n := 0
	for _, r := range refs {
		if r.ReferenceKind == kind {
			n++
		}
	}
	return n
}

func nodeKindList(nodes []types.Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(string(n.Kind))
		sb.WriteByte(':')
		sb.WriteString(n.Name)
		sb.WriteByte(' ')
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Registry tests
// ---------------------------------------------------------------------------

// TestRegistry_For_KnownLanguages verifies that For() resolves all 5 registered
// languages to non-zero configs.
// WHY: The registry is the single resolution point for CP10; if any language is
// missing, the orchestrator will silently skip files of that language.
func TestRegistry_For_KnownLanguages(t *testing.T) {
	reg := languages.NewRegistry()
	tests := []struct {
		lang     types.Language
		wantLang extraction.Lang
	}{
		{types.LanguageGo, extraction.LangGo},
		{types.LanguageTypeScript, extraction.LangTypeScript},
		{types.LanguageJavaScript, extraction.LangJavaScript},
		{types.LanguagePython, extraction.LangPython},
		{types.LanguageRust, extraction.LangRust},
	}
	for _, tc := range tests {
		cfg, lang, ok := reg.For(tc.lang)
		if !ok {
			t.Errorf("For(%q) returned ok=false, want true", tc.lang)
			continue
		}
		if lang != tc.wantLang {
			t.Errorf("For(%q) Lang = %d, want %d", tc.lang, lang, tc.wantLang)
		}
		// Sanity: returned config must have at least FunctionTypes populated.
		if len(cfg.FunctionTypes) == 0 {
			t.Errorf("For(%q): FunctionTypes is empty", tc.lang)
		}
	}
}

// TestRegistry_For_Unknown verifies that an unregistered language returns ok=false.
// WHY: Callers must be able to skip unsupported languages without panicking.
func TestRegistry_For_Unknown(t *testing.T) {
	reg := languages.NewRegistry()
	_, _, ok := reg.For(types.LanguageSvelte)
	if ok {
		t.Errorf("For(svelte) returned ok=true, want false (svelte is not in the registry)")
	}
}

// ---------------------------------------------------------------------------
// TypeScript
// ---------------------------------------------------------------------------

// Verified node types (from tmp/probe-lang-details probe2.go):
//
//	Top-level: import_statement, export_statement (wraps class_declaration,
//	function_declaration, interface_declaration, type_alias_declaration,
//	enum_declaration, lexical_declaration with arrow_function)
//	Named-iterator sees: interface_declaration, class_declaration,
//	function_declaration, type_alias_declaration, method_definition,
//	call_expression, enum_declaration, lexical_declaration
const tsFixture = `import { EventEmitter } from "events";
import defaultExport from "./defaults";

export interface Emittable {
    on(event: string, listener: () => void): void;
}

export type EventName = string;

export enum LogLevel {
    Debug = 0,
    Info,
    Warn,
    Error,
}

export class MyEmitter implements Emittable {
    private count: number = 0;
    on(event: string, listener: () => void): void {
        doThing(event, listener);
    }
}

export function createEmitter(name: string): MyEmitter {
    const e = new MyEmitter();
    e.on("start", () => {});
    return e;
}
`

const tsFixturePath = "src/emitter.ts"

// TestTypeScript_FunctionExtracted asserts functions are extracted as NodeKindFunction.
// WHY: Functions are the primary call targets; if missing, call-edge resolution fails.
func TestTypeScript_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsFixturePath, tsFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "createEmitter")
	if fn == nil {
		t.Fatalf("createEmitter function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestTypeScript_ClassExtracted asserts classes are extracted as NodeKindClass.
// WHY: Classes are the structural containers; missing them breaks the member graph.
func TestTypeScript_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsFixturePath, tsFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "MyEmitter")
	if cls == nil {
		t.Fatalf("MyEmitter class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestTypeScript_InterfaceExtracted asserts TS interfaces emit NodeKindInterface.
// WHY: Interfaces are the type-contract nodes; wrong kind breaks resolution edge promotion.
func TestTypeScript_InterfaceExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsFixturePath, tsFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Emittable")
	if iface == nil {
		t.Fatalf("Emittable interface not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestTypeScript_ImportsExtracted asserts import statements emit UnresolvedReferences.
// WHY: Imports are the starting point for the resolution layer's import resolver.
func TestTypeScript_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsFixturePath, tsFixture, types.LanguageTypeScript)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports events and ./defaults")
	}
}

// TestTypeScript_CallEmitsUnresolvedReference asserts call expressions emit calls refs.
// WHY: Calls must NOT emit edges directly — resolution layer owns that.
func TestTypeScript_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsFixturePath, tsFixture, types.LanguageTypeScript)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has doThing() and e.on() calls")
	}
}

// TestTypeScript_IsExported_ExportedSymbolsDetected asserts exported symbols have IsExported=true.
// WHY: IsExported drives +10 scoring bonus in resolution; missing it degrades cross-file resolution.
func TestTypeScript_IsExported_ExportedSymbolsDetected(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsFixturePath, tsFixture, types.LanguageTypeScript)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindClass, "MyEmitter", true},
		{types.NodeKindFunction, "createEmitter", true},
		{types.NodeKindInterface, "Emittable", true},
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("%s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

// TestTypeScript_NodeCountStable asserts node count is deterministic.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestTypeScript_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, tsFixturePath, tsFixture, types.LanguageTypeScript)
	r2 := e.Extract(ctx, tsFixturePath, tsFixture, types.LanguageTypeScript)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// JavaScript
// ---------------------------------------------------------------------------

// Verified node types (from tmp/probe-lang-details probe2.go):
//
//	Top-level: import_statement, export_statement (wraps class_declaration,
//	function_declaration, lexical_declaration with arrow_function), lexical_declaration
//	Named-iterator sees: class_declaration, function_declaration, method_definition,
//	call_expression, lexical_declaration, arrow_function
const jsFixture = `import { EventEmitter } from 'events';
const path = require('path');

export class MyEmitter {
    on(event, listener) {
        doThing(event, listener);
    }
}

export function createEmitter(name) {
    const e = new MyEmitter();
    e.on('start', function() {});
    return e;
}

const helper = (x) => x * 2;
`

const jsFixturePath = "src/emitter.js"

func TestJavaScript_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsFixturePath, jsFixture, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "createEmitter")
	if fn == nil {
		t.Fatalf("createEmitter function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestJavaScript_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsFixturePath, jsFixture, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "MyEmitter")
	if cls == nil {
		t.Fatalf("MyEmitter class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestJavaScript_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsFixturePath, jsFixture, types.LanguageJavaScript)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports events module")
	}
}

func TestJavaScript_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsFixturePath, jsFixture, types.LanguageJavaScript)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has doThing() and e.on() calls")
	}
}

func TestJavaScript_IsExported_ExportedSymbolsDetected(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsFixturePath, jsFixture, types.LanguageJavaScript)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindClass, "MyEmitter", true},
		{types.NodeKindFunction, "createEmitter", true},
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("%s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

func TestJavaScript_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, jsFixturePath, jsFixture, types.LanguageJavaScript)
	r2 := e.Extract(ctx, jsFixturePath, jsFixture, types.LanguageJavaScript)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Python
// ---------------------------------------------------------------------------

// Verified node types (from tmp/probe-lang-details):
//
//	Top-level: import_statement, import_from_statement, class_definition,
//	function_definition, expression_statement
//	Named-iterator sees: class_definition, function_definition, call,
//	import_statement, import_from_statement, assignment
//
// IsExported convention: NOT leading underscore (Python has no export keyword).
const pyFixture = `import os
import sys
from typing import Protocol
from pathlib import Path

class Drawable(Protocol):
    def draw(self) -> None: ...

class Canvas:
    def __init__(self):
        self.items = []
    def draw(self) -> None:
        render()

def make_canvas() -> Canvas:
    c = Canvas()
    c.draw()
    return c

def _private_helper():
    pass

PUBLIC_CONST = 42
`

const pyFixturePath = "src/canvas.py"

func TestPython_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePython)
	if !ok {
		t.Fatal("Python not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pyFixturePath, pyFixture, types.LanguagePython)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "make_canvas")
	if fn == nil {
		t.Fatalf("make_canvas function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestPython_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePython)
	if !ok {
		t.Fatal("Python not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pyFixturePath, pyFixture, types.LanguagePython)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Canvas")
	if cls == nil {
		t.Fatalf("Canvas class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestPython_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePython)
	if !ok {
		t.Fatal("Python not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pyFixturePath, pyFixture, types.LanguagePython)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports os, sys, typing, pathlib")
	}
}

func TestPython_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePython)
	if !ok {
		t.Fatal("Python not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pyFixturePath, pyFixture, types.LanguagePython)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render() and Canvas() calls")
	}
}

// TestPython_IsExported_UnderscoreConvention verifies the underscore convention:
// public names (no leading _) are exported; _private names are not.
// WHY: Python has no export keyword — the convention must be correctly implemented.
func TestPython_IsExported_UnderscoreConvention(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePython)
	if !ok {
		t.Fatal("Python not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pyFixturePath, pyFixture, types.LanguagePython)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindClass, "Canvas", true},
		{types.NodeKindFunction, "make_canvas", true},
		{types.NodeKindFunction, "_private_helper", false},
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("%s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

func TestPython_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePython)
	if !ok {
		t.Fatal("Python not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, pyFixturePath, pyFixture, types.LanguagePython)
	r2 := e.Extract(ctx, pyFixturePath, pyFixture, types.LanguagePython)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

// Verified node types (from tmp/probe-lang-details + tmp/probe19):
//
//	Top-level: use_declaration, struct_item, enum_item, trait_item,
//	impl_item, function_item, macro_invocation
//	Named-iterator sees: struct_item, enum_item, trait_item, impl_item,
//	function_item, use_declaration, call_expression, macro_invocation,
//	function_signature_item (trait method signatures)
//
// Key distinctive nodes:
//   - trait_item → NodeKindInterface (ResolveKind hook)
//   - impl_item → descend into member function_items
//   - macro_invocation (println!, vec!) → call UnresolvedReference
//   - pub keyword → IsExported
const rustFixture = `use std::collections::HashMap;
use std::fmt::Display;

pub struct Point {
    pub x: i32,
    pub y: i32,
}

struct Internal {
    value: i32,
}

pub enum Direction {
    North,
    South,
    East,
    West,
}

pub trait Shape {
    fn area(&self) -> f64;
    fn perimeter(&self) -> f64;
}

impl Shape for Point {
    fn area(&self) -> f64 {
        compute(self.x, self.y)
    }
    fn perimeter(&self) -> f64 {
        0.0
    }
}

pub fn main() {
    let p = Point { x: 1, y: 2 };
    println!("{}", p.area());
    let v = vec![1, 2, 3];
    _ = v;
}

fn helper(x: i32) -> i32 {
    x + 1
}
`

const rustFixturePath = "src/shapes.rs"

func TestRust_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rustFixturePath, rustFixture, types.LanguageRust)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "main")
	if fn == nil {
		t.Fatalf("main function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestRust_StructExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rustFixturePath, rustFixture, types.LanguageRust)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	st := findNode(result.Nodes, types.NodeKindStruct, "Point")
	if st == nil {
		t.Fatalf("Point struct not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestRust_TraitExtractedAsInterface asserts trait_item → NodeKindInterface.
// WHY: Rust traits are the semantic equivalent of interfaces; wrong kind breaks
// resolution's edge promotion (calls→instantiates, extends→implements).
func TestRust_TraitExtractedAsInterface(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rustFixturePath, rustFixture, types.LanguageRust)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	trait := findNode(result.Nodes, types.NodeKindInterface, "Shape")
	if trait == nil {
		t.Fatalf("Shape trait not found as NodeKindInterface; nodes: %s", nodeKindList(result.Nodes))
	}
	if trait.Kind != types.NodeKindInterface {
		t.Errorf("Shape trait Kind=%q, want %q", trait.Kind, types.NodeKindInterface)
	}
}

// TestRust_MacroInvocationEmitsCall asserts macro invocations (println!, vec!) emit
// UnresolvedReferences with EdgeKindCalls.
// WHY: Macros are the primary "call-like" operation in Rust; if they don't emit
// call references, the call graph is silently incomplete for Rust codebases.
func TestRust_MacroInvocationEmitsCall(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rustFixturePath, rustFixture, types.LanguageRust)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has println!, vec!, and compute() calls")
	}

	// Specifically check that a macro call was recorded.
	var refNames []string
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls {
			refNames = append(refNames, r.ReferenceName)
		}
	}
	foundMacro := false
	for _, n := range refNames {
		if strings.Contains(n, "println") || strings.Contains(n, "vec") {
			foundMacro = true
			break
		}
	}
	if !foundMacro {
		t.Errorf("expected macro call (println! or vec!) in UnresolvedReferences; got: %v", refNames)
	}
}

func TestRust_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rustFixturePath, rustFixture, types.LanguageRust)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture uses std::collections::HashMap")
	}
}

// TestRust_IsExported_PubKeyword asserts pub items are exported, non-pub are not.
// WHY: Rust's visibility is explicit; pub = exported. Wrong IsExported means the
// +10 resolution scoring bonus applies to private items.
func TestRust_IsExported_PubKeyword(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rustFixturePath, rustFixture, types.LanguageRust)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindStruct, "Point", true},     // pub struct Point
		{types.NodeKindStruct, "Internal", false}, // non-pub struct
		{types.NodeKindFunction, "main", true},    // pub fn main
		{types.NodeKindFunction, "helper", false}, // non-pub fn helper
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("%s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

func TestRust_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, rustFixturePath, rustFixture, types.LanguageRust)
	r2 := e.Extract(ctx, rustFixturePath, rustFixture, types.LanguageRust)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// TestRust_EnumExtractedAsEnum asserts that enum_item nodes → NodeKindEnum, not NodeKindStruct.
// WHY: rustResolveKind returns NodeKindEnum for enum_item but the engine previously
// fell through to extractStruct for any ResolveKind value other than Interface/TypeAlias.
// This means pub enum Direction would be stored as a struct, breaking semantic graph correctness.
func TestRust_EnumExtractedAsEnum(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRust)
	if !ok {
		t.Fatal("Rust not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rustFixturePath, rustFixture, types.LanguageRust)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Direction")
	if en == nil {
		t.Fatalf("Direction enum not found as NodeKindEnum; nodes: %s", nodeKindList(result.Nodes))
	}
	if en.Kind != types.NodeKindEnum {
		t.Errorf("Direction Kind=%q, want %q", en.Kind, types.NodeKindEnum)
	}
	// Confirm it is NOT stored as a struct.
	wrongNode := findNode(result.Nodes, types.NodeKindStruct, "Direction")
	if wrongNode != nil {
		t.Errorf("Direction was also/instead found as NodeKindStruct; should be NodeKindEnum only")
	}
}

// ---------------------------------------------------------------------------
// TypeScript — export default / export class
// ---------------------------------------------------------------------------

// tsExportDefaultFixture exercises export default function and export class
// alongside a non-exported function to verify IsExported correctness.
const tsExportDefaultFixture = `export default function defaultFn() {
    return 1;
}

export function namedExport() {
    return 2;
}

export class ExportedClass {
    method() {}
}

function notExported() {
    return 3;
}
`

const tsExportDefaultFixturePath = "src/exports.ts"

// TestTypeScript_ExportDefault_IsExported asserts that export default function → IsExported=true.
// WHY: The 8-byte text lookback sees "default " (not "export ") for export default declarations,
// so a text-scan approach misses them. AST-based detection via export_statement parent must catch all forms.
func TestTypeScript_ExportDefault_IsExported(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), tsExportDefaultFixturePath, tsExportDefaultFixture, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindFunction, "defaultFn", true},    // export default function
		{types.NodeKindFunction, "namedExport", true},  // export function
		{types.NodeKindClass, "ExportedClass", true},   // export class
		{types.NodeKindFunction, "notExported", false}, // not exported
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("TS %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// JavaScript — export default / export class
// ---------------------------------------------------------------------------

// jsExportDefaultFixture exercises export default function and export class
// alongside a non-exported function.
const jsExportDefaultFixture = `export default function defaultFn() {
    return 1;
}

export function namedExport() {
    return 2;
}

export class ExportedClass {
    method() {}
}

function notExported() {
    return 3;
}
`

const jsExportDefaultFixturePath = "src/exports.js"

// ---------------------------------------------------------------------------
// TypeScript — variable extraction (const/let/var)
// ---------------------------------------------------------------------------

// TestTypeScript_VariableExtracted asserts top-level const/let/var declarations are
// extracted as NodeKindVariable with the correct name and IsExported status.
// WHY: export const X = 1 must produce a variable node so the resolution layer can
// link references to X. Without VariableTypes wired, the lexical_declaration is
// descended into but never emitted as a node, breaking the semantic graph.
func TestTypeScript_VariableExtracted(t *testing.T) {
	const src = `export const X = 1;
const y = 2;
let z = "hello";
`
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "src/vars.ts", src, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// export const X → NodeKindVariable named "X", IsExported=true
	vX := findNode(result.Nodes, types.NodeKindVariable, "X")
	if vX == nil {
		t.Fatalf("variable X not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if vX.Name != "X" {
		t.Errorf("variable X: Name=%q, want %q", vX.Name, "X")
	}
	if !vX.IsExported {
		t.Errorf("variable X: IsExported=false, want true (it is 'export const X')")
	}

	// const y → NodeKindVariable named "y", IsExported=false
	vY := findNode(result.Nodes, types.NodeKindVariable, "y")
	if vY == nil {
		t.Fatalf("variable y not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if vY.IsExported {
		t.Errorf("variable y: IsExported=true, want false (it is non-exported 'const y')")
	}
}

// ---------------------------------------------------------------------------
// JavaScript — variable extraction (const/let/var)
// ---------------------------------------------------------------------------

// TestJavaScript_VariableExtracted asserts top-level const/let/var declarations are
// extracted as NodeKindVariable with the correct name and IsExported status.
// WHY: Same reason as TS — export const X must produce a variable node for the graph.
func TestJavaScript_VariableExtracted(t *testing.T) {
	const src = `export const X = 1;
const y = 2;
let z = "hello";
`
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "src/vars.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// export const X → NodeKindVariable named "X", IsExported=true
	vX := findNode(result.Nodes, types.NodeKindVariable, "X")
	if vX == nil {
		t.Fatalf("variable X not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if vX.Name != "X" {
		t.Errorf("variable X: Name=%q, want %q", vX.Name, "X")
	}
	if !vX.IsExported {
		t.Errorf("variable X: IsExported=false, want true (it is 'export const X')")
	}

	// const y → NodeKindVariable named "y", IsExported=false
	vY := findNode(result.Nodes, types.NodeKindVariable, "y")
	if vY == nil {
		t.Fatalf("variable y not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if vY.IsExported {
		t.Errorf("variable y: IsExported=true, want false (it is non-exported 'const y')")
	}
}

// TestJavaScript_ExportDefault_IsExported asserts that export default function → IsExported=true.
// WHY: Same as TS — the 8-byte lookback sees "default ", not "export ", for export default.
func TestJavaScript_ExportDefault_IsExported(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), jsExportDefaultFixturePath, jsExportDefaultFixture, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindFunction, "defaultFn", true},    // export default function
		{types.NodeKindFunction, "namedExport", true},  // export function
		{types.NodeKindClass, "ExportedClass", true},   // export class
		{types.NodeKindFunction, "notExported", false}, // not exported
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("JS %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}
