package languages_test

// TypeScript, JavaScript, Python, and Rust, plus the registry that resolves them.
// Every fixture here runs through the real grammar, so these also cover ABI and
// pool wiring, not only the configs.
//
// Each language repeats one shape: every declaration form reaches its intended
// node kind, imports and calls surface as references rather than edges, export
// status follows the language's own rule, and two runs agree.

import (
	"context"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

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

func TestRegistry_For_KnownLanguages(t *testing.T) {
	t.Parallel()
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
		// A registered but empty config would resolve without extracting.
		if len(cfg.FunctionTypes) == 0 {
			t.Errorf("For(%q): FunctionTypes is empty", tc.lang)
		}
	}
}

func TestRegistry_For_Unknown(t *testing.T) {
	t.Parallel()
	reg := languages.NewRegistry()
	_, _, ok := reg.For(types.LanguageSvelte)
	if ok {
		t.Errorf("For(svelte) returned ok=true, want false (svelte is not in the registry)")
	}
}

// Covers every declaration form, exported and not, plus both import styles.
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

func TestTypeScript_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestTypeScript_ClassExtracted(t *testing.T) {
	t.Parallel()
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

func TestTypeScript_InterfaceExtracted(t *testing.T) {
	t.Parallel()
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

func TestTypeScript_ImportsExtracted(t *testing.T) {
	t.Parallel()
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

func TestTypeScript_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestTypeScript_IsExported_ExportedSymbolsDetected(t *testing.T) {
	t.Parallel()
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

func TestTypeScript_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// Regression guard: a package name was once the last path segment, so several
// scoped or subpath packages collapsed onto one indistinguishable node.
func TestTypeScript_PackageImportKeepsFullSpecifierName(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	src := "import { Server } from '@hapi/hapi';\n"
	result := e.Extract(context.Background(), "src/app.ts", src, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	imp := findNode(result.Nodes, types.NodeKindImport, "hapi")
	if imp == nil {
		t.Fatalf("import node not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if imp.Name != "@hapi/hapi" {
		t.Errorf("import node Name = %q, want %q (full specifier, not basename)", imp.Name, "@hapi/hapi")
	}
}

// The other half of that rule: a relative import keeps its basename, since it
// resolves to a real file node anyway.
func TestTypeScript_RelativeImportUsesBasenameName(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	src := "import { x } from './utils/context.ts';\n"
	result := e.Extract(context.Background(), "src/app.ts", src, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	imp := findNode(result.Nodes, types.NodeKindImport, "context.ts")
	if imp == nil {
		t.Fatalf("import node not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if imp.Name != "context.ts" {
		t.Errorf("import node Name = %q, want %q (basename for relative import)", imp.Name, "context.ts")
	}
}

// The TypeScript fixture minus what JavaScript has no syntax for.
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// The JavaScript half of the import-naming rule; both share importNodeName.
func TestJavaScript_PackageImportKeepsFullSpecifierName(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	src := "import { Server } from '@hapi/hapi';\n"
	result := e.Extract(context.Background(), "src/app.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	imp := findNode(result.Nodes, types.NodeKindImport, "hapi")
	if imp == nil {
		t.Fatalf("import node not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if imp.Name != "@hapi/hapi" {
		t.Errorf("import node Name = %q, want %q (full specifier, not basename)", imp.Name, "@hapi/hapi")
	}
}

// As above, for a relative specifier.
func TestJavaScript_RelativeImportUsesBasenameName(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	src := "import { x } from './utils/context.js';\n"
	result := e.Extract(context.Background(), "src/app.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	imp := findNode(result.Nodes, types.NodeKindImport, "context.js")
	if imp == nil {
		t.Fatalf("import node not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if imp.Name != "context.js" {
		t.Errorf("import node Name = %q, want %q (basename for relative import)", imp.Name, "context.js")
	}
}

// Covers both import forms and both sides of the underscore convention.
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestPython_IsExported_UnderscoreConvention(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// Covers all three aggregate types ResolveKind has to tell apart, methods
// reached only by descent into an impl block, a macro invocation, and both
// visibilities.
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
	t.Parallel()
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
	t.Parallel()
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

func TestRust_TraitExtractedAsInterface(t *testing.T) {
	t.Parallel()
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

func TestRust_MacroInvocationEmitsCall(t *testing.T) {
	t.Parallel()
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

	// A macro invocation is the one call form unique to Rust.
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
	t.Parallel()
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

func TestRust_IsExported_PubKeyword(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// Regression guard: the engine once honored only the interface and type-alias
// answers from ResolveKind and stored everything else, enums included, as a
// struct.
func TestRust_EnumExtractedAsEnum(t *testing.T) {
	t.Parallel()
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
	wrongNode := findNode(result.Nodes, types.NodeKindStruct, "Direction")
	if wrongNode != nil {
		t.Errorf("Direction was also/instead found as NodeKindStruct; should be NodeKindEnum only")
	}
}

// Puts a default export and a named one beside an unexported function.
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

// The form that defeats any text-lookback approach: the declaration starts just
// past "default ", so nothing before it says "export".
func TestTypeScript_ExportDefault_IsExported(t *testing.T) {
	t.Parallel()
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

// The same three shapes in JavaScript.
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

// An exported const needs a node of its own for references to it to resolve;
// with VariableTypes unwired the declaration is walked through but never minted.
func TestTypeScript_VariableExtracted(t *testing.T) {
	t.Parallel()
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

	vY := findNode(result.Nodes, types.NodeKindVariable, "y")
	if vY == nil {
		t.Fatalf("variable y not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if vY.IsExported {
		t.Errorf("variable y: IsExported=true, want false (it is non-exported 'const y')")
	}
}

// As above, in JavaScript.
func TestJavaScript_VariableExtracted(t *testing.T) {
	t.Parallel()
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

	vY := findNode(result.Nodes, types.NodeKindVariable, "y")
	if vY == nil {
		t.Fatalf("variable y not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if vY.IsExported {
		t.Errorf("variable y: IsExported=true, want false (it is non-exported 'const y')")
	}
}

// As above, in JavaScript.
func TestJavaScript_ExportDefault_IsExported(t *testing.T) {
	t.Parallel()
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

// A namespace body is not a function scope, so its state survives suppression.
// Were that net ever widened, namespace-scoped state would vanish silently.
func TestTypeScript_NamespaceConstKept(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	const src = `namespace N { const X = 1; }`
	result := e.Extract(context.Background(), "src/ns.ts", src, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "X"); n == nil {
		t.Errorf("namespace-scoped const X not found as a variable node; nodes: %s", nodeKindList(result.Nodes))
	}
}

// A for-of binding is a bare identifier, never wrapped in a declaration, so
// VariableTypes has never matched it. Pinned as behavior rather than left as a
// grammar note, since nothing else would catch it starting to mint nodes.
func TestTypeScript_ForOfBindingNeverMinted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTypeScript)
	if !ok {
		t.Fatal("TypeScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	const src = `for (const x of y) { console.log(x); }`
	result := e.Extract(context.Background(), "src/forof.ts", src, types.LanguageTypeScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "x"); n != nil {
		t.Errorf("for-of binding \"x\" minted as a variable node (want: never reachable via VariableTypes); nodes: %s", nodeKindList(result.Nodes))
	}
}

// TSX copies the TypeScript config and extends it, so it inherits suppression
// without an edit of its own. Also pins that suppressing a declaration does not
// stop the walk: a JSX ref in its initializer is still harvested, as a call
// would be.
func TestTSX_InheritsFunctionScopeTypes(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageTSX)
	if !ok {
		t.Fatal("TSX not registered")
	}
	e := newExtractor(t, extLang, cfg)
	const src = `export const moduleConst = 1;

items.forEach((item) => {
  const inCallback = item;
  const suppressedWithJSX = <Widget item={item} />;
});
`
	result := e.Extract(context.Background(), "src/widget.tsx", src, types.LanguageTSX)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "moduleConst"); n == nil {
		t.Errorf("moduleConst not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if n := findNode(result.Nodes, types.NodeKindVariable, "inCallback"); n != nil {
		t.Errorf("inCallback minted in .tsx (want suppressed — proves FunctionScopeTypes did NOT inherit); nodes: %s", nodeKindList(result.Nodes))
	}
	if n := findNode(result.Nodes, types.NodeKindVariable, "suppressedWithJSX"); n != nil {
		t.Errorf("suppressedWithJSX minted (want suppressed); nodes: %s", nodeKindList(result.Nodes))
	}

	foundWidget := false
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindReferences && strings.Contains(r.ReferenceName, "Widget") {
			foundWidget = true
		}
	}
	if !foundWidget {
		t.Errorf("expected a references ref for <Widget/> inside the suppressed declaration's initializer; refs: %v", result.UnresolvedReferences)
	}
}

// The JavaScript half of scope suppression: a module-scope const is kept, a
// callback-scoped one dropped, and the initializer's call harvested either way.
func TestJavaScript_FunctionScopeSuppression(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJavaScript)
	if !ok {
		t.Fatal("JavaScript not registered")
	}
	e := newExtractor(t, extLang, cfg)
	const src = `export const moduleConst = 1;

items.forEach((item) => {
  const inCallback = item;
  helperCall(inCallback);
});
`
	result := e.Extract(context.Background(), "src/scope.js", src, types.LanguageJavaScript)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	if n := findNode(result.Nodes, types.NodeKindVariable, "moduleConst"); n == nil {
		t.Errorf("moduleConst not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if n := findNode(result.Nodes, types.NodeKindVariable, "inCallback"); n != nil {
		t.Errorf("inCallback minted (want suppressed — scopeDepth 1); nodes: %s", nodeKindList(result.Nodes))
	}

	found := false
	for _, r := range result.UnresolvedReferences {
		if r.ReferenceKind == types.EdgeKindCalls && r.ReferenceName == "helperCall" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected calls ref \"helperCall\" from the suppressed declaration's body; refs: %v", result.UnresolvedReferences)
	}
}

// Python wires no VariableTypes, which puts it structurally out of reach of
// scope suppression. These exact counts were confirmed unchanged across that
// change and are pinned so the language cannot be drawn into it by accident.
func TestPython_ByteIdenticalAfterScopeSuppression(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePython)
	if !ok {
		t.Fatal("Python not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pyFixturePath, pyFixture, types.LanguagePython)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.Nodes) != 12 {
		t.Errorf("Python node count = %d, want 12 (byte-identical to pre-change)", len(result.Nodes))
	}
	if len(result.Edges) != 11 {
		t.Errorf("Python edge count = %d, want 11 (byte-identical to pre-change)", len(result.Edges))
	}
	if len(result.UnresolvedReferences) != 7 {
		t.Errorf("Python unresolved-ref count = %d, want 7 (byte-identical to pre-change)", len(result.UnresolvedReferences))
	}
}
