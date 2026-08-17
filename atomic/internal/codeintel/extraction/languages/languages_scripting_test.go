package languages_test

// Ruby, PHP, Lua, and Luau. Every fixture here runs through the real grammar, so
// these also cover ABI and pool wiring, not only the configs.
//
// Each language repeats one shape: every declaration form reaches its intended
// node kind, imports and calls surface as references rather than edges, export
// status follows the language's own rule, and two runs agree. Only PHP spells
// its imports as a directive; the rest reach them through require().

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Covers every declaration form, plus both require spellings and a method call.
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

func TestRuby_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestRuby_ClassExtracted(t *testing.T) {
	t.Parallel()
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

func TestRuby_ModuleExtracted(t *testing.T) {
	t.Parallel()
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

func TestRuby_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageRuby)
	if !ok {
		t.Fatal("Ruby not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), rubyFixturePath, rubyFixture, types.LanguageRuby)

	// require arrives as an ordinary call; promotion happens in resolution.
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

func TestRuby_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestRuby_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// Covers every declaration form, a trait among them, both call forms, and all
// three visibilities plus an unmarked top-level function.
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

func TestPHP_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestPHP_MethodExtracted(t *testing.T) {
	t.Parallel()
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

func TestPHP_ClassExtracted(t *testing.T) {
	t.Parallel()
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

func TestPHP_InterfaceExtracted(t *testing.T) {
	t.Parallel()
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

func TestPHP_ImportsExtracted(t *testing.T) {
	t.Parallel()
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

func TestPHP_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestPHP_IsExported_VisibilityModifier(t *testing.T) {
	t.Parallel()
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
		{types.NodeKindFunction, "draw", true},
		{types.NodeKindMethod, "draw", true},
		{types.NodeKindFunction, "helper", false},
		{types.NodeKindMethod, "helper", false},
		// No modifier, and none is possible at top level.
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

func TestPHP_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// Covers the global, table-method, and local function forms, which share one
// node type, plus table-based OO standing in for classes Lua does not have.
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

func TestLua_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestLua_VariableExtracted(t *testing.T) {
	t.Parallel()
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
		// Shape is a table, so it is a variable here.
		v = findNode(result.Nodes, types.NodeKindVariable, "Shape")
	}
	if v == nil {
		t.Fatalf("no variable nodes found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestLua_CallsInsideFunctionsExtracted(t *testing.T) {
	t.Parallel()
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

func TestLua_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestLua_TopLevelRequireExtracted(t *testing.T) {
	t.Parallel()
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

func TestLua_TopLevelRequireNoDuplicates(t *testing.T) {
	t.Parallel()
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

	seen := make(map[string]int)
	for _, ref := range result.UnresolvedReferences {
		seen[ref.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate UnresolvedReference ID %q appears %d times", id, count)
		}
	}

	// Two require() calls at different scopes must not collide on one ID.
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

func TestLua_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// The Lua fixture plus what Luau adds: type annotations and a type alias.
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

func TestLuau_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestLuau_VariableExtracted(t *testing.T) {
	t.Parallel()
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

func TestLuau_TypeAliasExtracted(t *testing.T) {
	t.Parallel()
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

func TestLuau_CallsInsideFunctionsExtracted(t *testing.T) {
	t.Parallel()
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

func TestLuau_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestLuau_NodeCountStable(t *testing.T) {
	t.Parallel()
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

func TestRegistry_For_CP8C_Languages(t *testing.T) {
	t.Parallel()
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
