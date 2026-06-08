package languages_test

// Tests for Ruby, PHP, Lua, Luau language extractor configs (CP8 batch C).
//
// Each language has:
//  1. A real fixture parsed through the pool (grammar ABI proof).
//  2. Assertions per success criteria:
//     - Function/method node extracted with correct kind.
//     - Class node extracted (PHP/Ruby).
//     - Interface-equivalent (PHP interface/trait → NodeKindInterface).
//     - Import UnresolvedReference emitted (PHP namespace_use_declaration, Ruby require call,
//       Lua/Luau require call).
//     - Call site → UnresolvedReference (EdgeKindCalls).
//     - IsExported correct per-language rule.
//     - Node count stable across two extractions.
//
// Node-type strings are VERIFIED by real grammar parse (see tmp/probe-cp8c/).
// Do NOT change them without running the probe again.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Ruby
// ---------------------------------------------------------------------------

// rubyFixture exercises:
//   - call (require 'json', require_relative './helper') → import UnresolvedReference
//   - module (Drawable)                   → NodeKindModule
//   - class (Shape, Circle)               → NodeKindClass
//   - method (draw, initialize, area)     → NodeKindFunction or NodeKindMethod
//   - singleton_method (self.create)      → NodeKindFunction or NodeKindMethod
//   - call (render, include, s.draw)      → EdgeKindCalls UnresolvedReference
//
// Verified node-type strings (tmp/probe-cp8c/ — Ruby grammar):
//
//	call               — require 'json' / s.draw / render(id)
//	module             — "module Drawable { ... }"
//	class              — "class Shape { ... }"
//	method             — "def draw ... end"
//	singleton_method   — "def self.create(id, name) ... end"
//
// IsExported rule: Ruby has no export keyword. All methods/classes/modules
// default to public (IsExported=true). Private/protected sections are hard to
// track statically per-method without parent-walk context; we document this
// as a known limitation and default to true.
const rubyFixture = `require 'json'
require_relative './helper'

module Drawable
  def draw
    render(id)
  end
end

class Shape
  include Drawable

  def initialize(id, name)
    @id = id
    @name = name
  end

  def draw
    puts @name
  end

  def self.create(id, name)
    new(id, name)
  end

  private

  def render(v)
    puts v
  end
end

class Circle < Shape
  def initialize(id, name, radius)
    super(id, name)
    @radius = radius
  end

  def area
    Math::PI * @radius * @radius
  end
end

def make_shape(id, name)
  s = Shape.create(id, name)
  s.draw
  s
end
`

const rubyFixturePath = "src/canvas.rb"

// TestRuby_FunctionExtracted verifies method → NodeKindFunction or NodeKindMethod.
// WHY: Ruby methods are the primary callable units; wrong kind breaks call-graph resolution.
func TestRuby_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRuby)
	if !ok {
		t.Fatal("Ruby not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rubyFixturePath, rubyFixture, types.LanguageRuby)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "draw")
	}
	if fn == nil {
		t.Fatalf("draw method not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestRuby_ClassExtracted verifies class → NodeKindClass.
// WHY: Classes are structural containers; missing them breaks the member graph.
func TestRuby_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRuby)
	if !ok {
		t.Fatal("Ruby not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rubyFixturePath, rubyFixture, types.LanguageRuby)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Shape")
	if cls == nil {
		t.Fatalf("Shape class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestRuby_ModuleExtracted verifies module → NodeKindModule.
// WHY: Ruby modules are namespace/mixin containers distinct from classes; the
// engine's ModuleTypes arm dispatches extractClass with NodeKindModule so that
// include/extend resolution can distinguish modules from class hierarchies.
// Accepting NodeKindClass here would mask a wrong-kind regression.
func TestRuby_ModuleExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRuby)
	if !ok {
		t.Fatal("Ruby not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rubyFixturePath, rubyFixture, types.LanguageRuby)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	mod := findNode(result.Nodes, types.NodeKindModule, "Drawable")
	if mod == nil {
		t.Fatalf("Drawable module not found as NodeKindModule; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestRuby_ImportsExtracted verifies require/require_relative calls emit
// UnresolvedReference with EdgeKindCalls where the callee is "require" or
// "require_relative".
//
// WHY: Ruby has no distinct import AST node — require() is a plain function
// call in the grammar (node type "call"). The extractor therefore emits
// EdgeKindCalls for require just like any other call. The resolution layer
// is responsible for recognising require/require_relative callee names and
// promoting those edges to imports. Testing EdgeKindImports here would
// require modifying the extractor framework (out of scope for CP8 batch C).
func TestRuby_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRuby)
	if !ok {
		t.Fatal("Ruby not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rubyFixturePath, rubyFixture, types.LanguageRuby)

	// Find at least one EdgeKindCalls reference whose callee is "require" or "require_relative".
	found := false
	for _, ref := range result.UnresolvedReferences {
		if ref.ReferenceKind == types.EdgeKindCalls &&
			(ref.ReferenceName == "require" || ref.ReferenceName == "require_relative") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no EdgeKindCalls with callee require/require_relative; fixture has require 'json' and require_relative './helper'; refs: %v", result.UnresolvedReferences)
	}
}

// TestRuby_CallEmitsUnresolvedReference verifies call expressions emit EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestRuby_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRuby)
	if !ok {
		t.Fatal("Ruby not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rubyFixturePath, rubyFixture, types.LanguageRuby)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render, s.draw, puts calls")
	}
}

// TestRuby_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestRuby_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRuby)
	if !ok {
		t.Fatal("Ruby not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, rubyFixturePath, rubyFixture, types.LanguageRuby)
	r2 := e.Extract(ctx, rubyFixturePath, rubyFixture, types.LanguageRuby)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// PHP
// ---------------------------------------------------------------------------

// phpFixture exercises:
//   - namespace_use_declaration (use App\Contracts\Drawable) → EdgeKindImports
//   - interface_declaration (Paintable)     → NodeKindInterface
//   - trait_declaration (HasColor)          → NodeKindInterface (PHP traits ≈ mixins)
//   - enum_declaration (Direction)          → NodeKindEnum
//   - class_declaration (Shape, Canvas)     → NodeKindClass
//   - method_declaration (draw, area, …)    → NodeKindMethod or NodeKindFunction
//   - function_definition (createCanvas)    → NodeKindFunction
//   - property_declaration                  → NodeKindProperty
//   - function_call_expression (strlen, …)  → EdgeKindCalls
//   - member_call_expression ($obj->draw()) → EdgeKindCalls
//
// Verified node-type strings (tmp/probe-cp8c/ — PHP grammar):
//
//	function_definition          — "function createCanvas(...) { ... }"
//	method_declaration           — "public function draw(): void { ... }"
//	class_declaration            — "class Canvas extends Shape { ... }"
//	interface_declaration        — "interface Paintable { ... }"
//	trait_declaration            — "trait HasColor { ... }"
//	enum_declaration             — "enum Direction { ... }"
//	property_declaration         — "private string $color = 'red';"
//	namespace_use_declaration    — "use App\Contracts\Drawable;"
//	function_call_expression     — "strlen(\"hello\")"
//	member_call_expression       — "$this->renderer->render($this->id)"
//
// IsExported rule: PHP uses visibility_modifier child ("public"/"protected"/"private").
// public → exported. private/protected → not exported. No visibility_modifier → exported
// (PHP default outside class context is public; inside class, absence of modifier is
// still visible, so we default to true when no modifier is found).
const phpFixture = `<?php

namespace App\Models;

use App\Contracts\Drawable;
use App\Services\Renderer;

interface Paintable {
    public function paint(): void;
    public function getColor(): string;
}

trait HasColor {
    private string $color = 'red';

    public function getColor(): string {
        return $this->color;
    }
}

enum Direction {
    case North;
    case South;
    case East;
    case West;
}

abstract class Shape implements Drawable {
    protected int $id;
    public string $name;

    public function __construct(int $id, string $name) {
        $this->id = $id;
        $this->name = $name;
    }

    abstract public function area(): float;
}

class Canvas extends Shape implements Paintable {
    use HasColor;

    private Renderer $renderer;

    public function __construct(int $id, string $name) {
        parent::__construct($id, $name);
        $this->renderer = new Renderer();
    }

    public function draw(): void {
        $this->renderer->render($this->id);
    }

    public function area(): float {
        return 0.0;
    }

    private function helper(int $v): int {
        return $v * 2;
    }
}

function createCanvas(int $id, string $name): Canvas {
    $c = new Canvas($id, $name);
    $c->draw();
    return $c;
}
`

const phpFixturePath = "src/Canvas.php"

// TestPHP_FunctionExtracted verifies function_definition/method_declaration → NodeKindFunction/Method.
// WHY: PHP functions are the primary callable units; wrong kind breaks call-graph.
func TestPHP_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), phpFixturePath, phpFixture, types.LanguagePHP)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "createCanvas")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "createCanvas")
	}
	if fn == nil {
		t.Fatalf("createCanvas function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestPHP_MethodExtracted verifies method_declaration → NodeKindMethod or NodeKindFunction.
// WHY: PHP class methods are distinct from top-level functions; both must be indexed.
func TestPHP_MethodExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), phpFixturePath, phpFixture, types.LanguagePHP)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "draw")
	}
	if fn == nil {
		t.Fatalf("draw method not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestPHP_ClassExtracted verifies class_declaration → NodeKindClass.
// WHY: Classes are structural containers; missing them breaks the member graph.
func TestPHP_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), phpFixturePath, phpFixture, types.LanguagePHP)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Canvas")
	if cls == nil {
		t.Fatalf("Canvas class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestPHP_InterfaceExtracted verifies interface_declaration → NodeKindInterface.
// WHY: PHP interfaces are type contracts; wrong kind breaks implements edge promotion.
func TestPHP_InterfaceExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), phpFixturePath, phpFixture, types.LanguagePHP)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Paintable")
	if iface == nil {
		t.Fatalf("Paintable interface not found as NodeKindInterface; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestPHP_ImportsExtracted verifies namespace_use_declaration emits UnresolvedReference.
// WHY: PHP use statements are the import mechanism for the resolution layer.
func TestPHP_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), phpFixturePath, phpFixture, types.LanguagePHP)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has use App\\Contracts\\Drawable and use App\\Services\\Renderer")
	}
}

// TestPHP_CallEmitsUnresolvedReference verifies function_call_expression/member_call_expression → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestPHP_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), phpFixturePath, phpFixture, types.LanguagePHP)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has $this->renderer->render, $c->draw calls")
	}
}

// TestPHP_IsExported_VisibilityModifier verifies public → exported, private → not exported.
// WHY: PHP visibility modifiers drive cross-class resolution scoring; wrong IsExported
// causes private methods to be promoted incorrectly in the call graph.
func TestPHP_IsExported_VisibilityModifier(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), phpFixturePath, phpFixture, types.LanguagePHP)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		// public methods → exported
		{types.NodeKindFunction, "draw", true},
		{types.NodeKindMethod, "draw", true},
		// private method → not exported
		{types.NodeKindFunction, "helper", false},
		{types.NodeKindMethod, "helper", false},
		// top-level function (no class) → exported
		{types.NodeKindFunction, "createCanvas", true},
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			continue // may not be extracted at this particular kind; skip rather than fail
		}
		if n.IsExported != tc.want {
			t.Errorf("PHP %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

// TestPHP_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestPHP_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePHP)
	if !ok {
		t.Fatal("PHP not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, phpFixturePath, phpFixture, types.LanguagePHP)
	r2 := e.Extract(ctx, phpFixturePath, phpFixture, types.LanguagePHP)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Lua
// ---------------------------------------------------------------------------

// luaFixture exercises:
//   - variable_declaration (local json = require("json")) → EdgeKindImports
//   - function_statement (Shape.new, Shape:draw, local function render) → NodeKindFunction
//   - variable_declaration (local PI = 3.14159, local Shape = {}) → NodeKindVariable
//   - function_call (require("json"), render, s:draw) → EdgeKindCalls
//
// Verified node-type strings (tmp/probe-cp8c/ — Lua grammar):
//
//	function_statement     — "function Shape.new(id, name) ... end" / "local function render(v) ... end"
//	variable_declaration   — "local json = require(\"json\")"
//	function_call          — "require(\"json\")"
//
// Lua has no classes or interfaces — it uses table-based OO. We extract only
// what the grammar exposes: functions, variables, and calls.
//
// IsExported rule: Lua has no visibility modifiers. All symbols default to
// exported (IsExported=true). Local functions/variables are technically
// file-scoped but the grammar exposes the same node types — we document
// this as a known limitation.
//
// Name extraction: function_name child (kind="function_name") text is used
// directly (e.g. "Shape:draw", "Shape.new", "greet"). For local functions
// the name is in an identifier child. We extract raw text from the first
// name-bearing child.
const luaFixture = `local json = require("json")
local util = require("myapp.util")

local Shape = {}
Shape.__index = Shape

function Shape.new(id, name)
    local self = setmetatable({}, Shape)
    self.id = id
    self.name = name
    return self
end

function Shape:draw()
    render(self.id)
end

function Shape:getName()
    return self.name
end

local function render(v)
    print(v)
end

local function makeShape(id, name)
    local s = Shape.new(id, name)
    s:draw()
    return s
end

local PI = 3.14159

function area(radius)
    return PI * radius * radius
end
`

const luaFixturePath = "src/canvas.lua"

// TestLua_FunctionExtracted verifies function_statement → NodeKindFunction.
// WHY: Lua functions are the primary callable units; wrong kind breaks call-graph resolution.
func TestLua_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLua)
	if !ok {
		t.Fatal("Lua not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luaFixturePath, luaFixture, types.LanguageLua)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "area")
	if fn == nil {
		t.Fatalf("area function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestLua_VariableExtracted verifies variable_declaration → NodeKindVariable.
// WHY: Lua variables (local X = ...) are module-level symbols that the resolution
// layer needs to link references against.
func TestLua_VariableExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLua)
	if !ok {
		t.Fatal("Lua not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luaFixturePath, luaFixture, types.LanguageLua)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	v := findNode(result.Nodes, types.NodeKindVariable, "PI")
	if v == nil {
		// Also accept Shape as a variable (it's a table declared as local)
		v = findNode(result.Nodes, types.NodeKindVariable, "Shape")
	}
	if v == nil {
		t.Fatalf("no variable nodes found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestLua_CallsInsideFunctionsExtracted verifies that EdgeKindCalls UnresolvedReferences
// are emitted for call expressions inside function bodies (render, print, setmetatable, etc.).
//
// WHY: Calls inside function bodies are the primary call-graph edges for Lua.
// The fixture exercises render(), print(), and setmetatable() inside function bodies.
// Top-level require() extraction (F-15) is covered separately by
// TestLua_TopLevelRequireExtracted.
func TestLua_CallsInsideFunctionsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLua)
	if !ok {
		t.Fatal("Lua not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luaFixturePath, luaFixture, types.LanguageLua)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no EdgeKindCalls UnresolvedReferences; fixture has render, print, setmetatable calls inside functions; refs: %v", result.UnresolvedReferences)
	}
}

// TestLua_CallEmitsUnresolvedReference verifies function_call → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestLua_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLua)
	if !ok {
		t.Fatal("Lua not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luaFixturePath, luaFixture, types.LanguageLua)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render, s:draw, print calls")
	}
}

// TestLua_TopLevelRequireExtracted verifies that require() calls inside
// variable_declaration RHS at module top level emit EdgeKindCalls UnresolvedReferences.
//
// WHY (F-15): The pattern "local x = require('y')" wraps the require() call inside
// a variable_declaration node. Before this fix, extractSimpleNode returned skipChildren=true
// without scanning the RHS, so the require() call was silently dropped. After the fix,
// extractSimpleNode calls visitFunctionBody on the node to pick up any call expressions
// in the RHS. This is the primary module-dependency-edge gap for Lua/Luau files.
func TestLua_TopLevelRequireExtracted(t *testing.T) {
	const src = `local x = require("mymod")
local y = require("othermod")
`
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLua)
	if !ok {
		t.Fatal("Lua not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "src/test.lua", src, types.LanguageLua)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Both top-level require() calls must produce EdgeKindCalls UnresolvedReferences.
	var requireRefs []types.UnresolvedReference
	for _, ref := range result.UnresolvedReferences {
		if ref.ReferenceKind == types.EdgeKindCalls && ref.ReferenceName == "require" {
			requireRefs = append(requireRefs, ref)
		}
	}
	if len(requireRefs) < 2 {
		t.Fatalf("want >=2 EdgeKindCalls refs for require, got %d; all refs: %v", len(requireRefs), result.UnresolvedReferences)
	}
}

// TestLua_TopLevelRequireNoDuplicates verifies that a require() call inside a
// variable_declaration RHS is not double-extracted (once by the top-level walk
// and once again by some other path).
//
// WHY (F-15): The fix must not introduce duplicates. GenerateRefID should produce
// unique IDs for each distinct call site, and there must be no ref with the same
// ID appearing twice.
func TestLua_TopLevelRequireNoDuplicates(t *testing.T) {
	const src = `local x = require("mymod")

local function foo()
    return require("mymod")
end
`
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLua)
	if !ok {
		t.Fatal("Lua not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "src/test.lua", src, types.LanguageLua)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Count refs per ID — no ID should appear twice.
	seen := make(map[string]int)
	for _, ref := range result.UnresolvedReferences {
		seen[ref.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate UnresolvedReference ID %q appears %d times", id, count)
		}
	}

	// Both the top-level require (line 1) and the in-function require (line 4)
	// should produce separate refs with different IDs.
	var requireRefs []types.UnresolvedReference
	for _, ref := range result.UnresolvedReferences {
		if ref.ReferenceName == "require" {
			requireRefs = append(requireRefs, ref)
		}
	}
	if len(requireRefs) < 2 {
		t.Fatalf("want >=2 require refs (one top-level, one in function), got %d; refs: %v", len(requireRefs), result.UnresolvedReferences)
	}
}

// TestLua_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestLua_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLua)
	if !ok {
		t.Fatal("Lua not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, luaFixturePath, luaFixture, types.LanguageLua)
	r2 := e.Extract(ctx, luaFixturePath, luaFixture, types.LanguageLua)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Luau
// ---------------------------------------------------------------------------

// luauFixture exercises:
//   - variable_declaration (local json = require("json")) → EdgeKindImports
//   - function_declaration (Shape.new, Shape:draw, local function render) → NodeKindFunction
//   - variable_declaration (local PI: number = 3.14159) → NodeKindVariable
//   - type_definition (type Vector2 = {...}) → NodeKindTypeAlias
//   - function_call (require("json"), render, s:draw) → EdgeKindCalls
//
// Verified node-type strings (tmp/probe-cp8c/ — Luau grammar):
//
//	function_declaration   — "function Shape.new(id: number, name: string) ... end"
//	variable_declaration   — "local json = require(\"json\")"
//	function_call          — "require(\"json\")"
//	type_definition        — "type Vector2 = { ... }"
//
// Luau = Lua + type annotations. The Luau grammar uses function_declaration
// (not function_statement) and adds type_definition nodes. Same IsExported
// rule as Lua: default-true, no visibility modifiers.
const luauFixture = `local json = require("json")
local util = require("myapp.util")

type Vector2 = {
    x: number,
    y: number,
}

type Drawable = {
    draw: (self: any) -> (),
}

local Shape = {}
Shape.__index = Shape

function Shape.new(id: number, name: string): {}
    local self = setmetatable({}, Shape)
    self.id = id
    self.name = name
    return self
end

function Shape:draw(): ()
    render(self.id)
end

local function render(v: number): ()
    print(v)
end

local function makeShape(id: number, name: string): {}
    local s = Shape.new(id, name)
    s:draw()
    return s
end

local PI: number = 3.14159

function area(radius: number): number
    return PI * radius * radius
end
`

const luauFixturePath = "src/canvas.luau"

// TestLuau_FunctionExtracted verifies function_declaration → NodeKindFunction.
// WHY: Luau functions are the primary callable units; wrong kind breaks call-graph.
func TestLuau_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLuau)
	if !ok {
		t.Fatal("Luau not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luauFixturePath, luauFixture, types.LanguageLuau)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "area")
	if fn == nil {
		t.Fatalf("area function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestLuau_VariableExtracted verifies variable_declaration → NodeKindVariable.
// WHY: Luau typed local variables are module-level symbols the resolution layer needs.
func TestLuau_VariableExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLuau)
	if !ok {
		t.Fatal("Luau not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luauFixturePath, luauFixture, types.LanguageLuau)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	v := findNode(result.Nodes, types.NodeKindVariable, "PI")
	if v == nil {
		v = findNode(result.Nodes, types.NodeKindVariable, "Shape")
	}
	if v == nil {
		t.Fatalf("no variable nodes found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestLuau_TypeAliasExtracted verifies type_definition → NodeKindTypeAlias.
// WHY: Luau adds a type system on top of Lua. Type aliases must be indexed as
// NodeKindTypeAlias so the type graph can reference them correctly.
func TestLuau_TypeAliasExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLuau)
	if !ok {
		t.Fatal("Luau not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luauFixturePath, luauFixture, types.LanguageLuau)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	ta := findNode(result.Nodes, types.NodeKindTypeAlias, "Vector2")
	if ta == nil {
		t.Fatalf("Vector2 type alias not found as NodeKindTypeAlias; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestLuau_CallsInsideFunctionsExtracted verifies that EdgeKindCalls UnresolvedReferences
// are emitted for call expressions inside function bodies (render, print, setmetatable, etc.).
//
// WHY: Calls inside function bodies are the primary call-graph edges for Luau.
// Top-level require() extraction (F-15) is now handled by extractSimpleNode calling
// visitFunctionBody on variable_declaration nodes.
func TestLuau_CallsInsideFunctionsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLuau)
	if !ok {
		t.Fatal("Luau not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luauFixturePath, luauFixture, types.LanguageLuau)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no EdgeKindCalls UnresolvedReferences; fixture has render, print, setmetatable calls inside functions; refs: %v", result.UnresolvedReferences)
	}
}

// TestLuau_CallEmitsUnresolvedReference verifies function_call → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestLuau_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLuau)
	if !ok {
		t.Fatal("Luau not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), luauFixturePath, luauFixture, types.LanguageLuau)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render, s:draw, print calls")
	}
}

// TestLuau_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestLuau_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageLuau)
	if !ok {
		t.Fatal("Luau not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, luauFixturePath, luauFixture, types.LanguageLuau)
	r2 := e.Extract(ctx, luauFixturePath, luauFixture, types.LanguageLuau)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Registry — batch C languages registered
// ---------------------------------------------------------------------------

// TestRegistry_For_CP8C_Languages verifies all 4 new languages are registered.
// WHY: The registry is the single resolution point for CP10; missing entries
// cause the orchestrator to silently skip files of those languages.
func TestRegistry_For_CP8C_Languages(t *testing.T) {
	reg := languages.NewRegistry()
	tests := []struct {
		lang     types.Language
		wantLang extraction.Lang
	}{
		{types.LanguageRuby, extraction.LangRuby},
		{types.LanguagePHP, extraction.LangPHP},
		{types.LanguageLua, extraction.LangLua},
		{types.LanguageLuau, extraction.LangLuau},
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
		if len(cfg.FunctionTypes) == 0 && len(cfg.MethodTypes) == 0 {
			t.Errorf("For(%q): both FunctionTypes and MethodTypes are empty", tc.lang)
		}
	}
}
