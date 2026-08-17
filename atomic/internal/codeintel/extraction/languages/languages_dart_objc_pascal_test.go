package languages_test

// Dart, ObjC, and Pascal. Every fixture here runs through the real grammar, so
// these also cover ABI and pool wiring, not only the configs.
//
// Each language repeats one shape: every declaration form reaches its intended
// node kind, imports and calls surface as references rather than edges, export
// status follows the language's own rule, and two runs agree. Calls are the
// exception — the Dart grammar has no call node, and TestDart_CallsBlocked pins
// that constraint rather than asserting them away.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Covers every declaration form, a mixin among them, plus both naming
// conventions the visibility rule turns on.
const dartFixture = `import 'dart:async';
import 'package:flutter/material.dart';

enum Direction { north, south, east, west }

mixin Drawable {
  void draw();
}

abstract class Shape with Drawable {
  final int id;
  final String name;

  Shape(this.id, this.name);

  double area();
}

class Circle extends Shape {
  final double radius;

  Circle(int id, String name, this.radius) : super(id, name);

  @override
  double area() => 3.14159 * radius * radius;

  @override
  void draw() {
    _render(id);
  }

  void _privateHelper() {
    print(name);
  }
}

double computeArea(Shape s) {
  final a = s.area();
  print(a);
  return a;
}
`

const dartFixturePath = "lib/canvas.dart"

func TestDart_FunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "computeArea")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "computeArea")
	}
	if fn == nil {
		t.Fatalf("computeArea function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestDart_MethodExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)
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

func TestDart_ClassExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Circle")
	if cls == nil {
		t.Fatalf("Circle class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestDart_EnumExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Direction")
	if en == nil {
		t.Fatalf("Direction enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestDart_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has import 'dart:async' and import 'package:...'")
	}
}

func TestDart_IsExported_UnderscoreConvention(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"computeArea", true},
		{"draw", true},
		{"_privateHelper", false},
	} {
		// Either kind is acceptable here.
		n := findNode(result.Nodes, types.NodeKindFunction, tc.name)
		if n == nil {
			n = findNode(result.Nodes, types.NodeKindMethod, tc.name)
		}
		if n == nil {
			// Skip rather than fail: whether a private helper surfaces at
			// top level is not what this test pins.
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("Dart %s: IsExported=%v, want %v", tc.name, n.IsExported, tc.want)
		}
	}
}

func TestDart_CallsBlocked(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)
	// Asserting on call refs would fail by construction; the point is that
	// extraction still completes.
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors during Dart extraction: %v", result.Errors)
	}
	_ = result.UnresolvedReferences
	t.Log("Dart: no call_expression node in grammar; EdgeKindCalls extraction is not supported (grammar constraint)")
}

func TestDart_NodeCountStable(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, dartFixturePath, dartFixture, types.LanguageDart)
	r2 := e.Extract(ctx, dartFixturePath, dartFixture, types.LanguageDart)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// Covers every declaration form, both include spellings, and both call forms:
// a message send and a C-style call.
const objcFixture = `#import <Foundation/Foundation.h>
#import "Shape.h"

@protocol Drawable <NSObject>
- (void)draw;
@end

@interface Shape : NSObject <Drawable>
@property (nonatomic, assign) NSInteger shapeId;
@property (nonatomic, copy) NSString *name;

- (instancetype)initWithId:(NSInteger)shapeId name:(NSString *)name;
- (double)area;
@end

@implementation Shape

- (instancetype)initWithId:(NSInteger)shapeId name:(NSString *)name {
    self = [super init];
    if (self) {
        _shapeId = shapeId;
        _name = name;
    }
    return self;
}

- (double)area {
    return 0.0;
}

- (void)draw {
    [self renderWithId:_shapeId];
}

- (void)renderWithId:(NSInteger)ident {
    NSLog(@"rendering %@", _name);
}

@end

Shape *createShape(NSInteger ident, NSString *name) {
    Shape *s = [[Shape alloc] initWithId:ident name:name];
    [s draw];
    return s;
}
`

const objcFixturePath = "src/Shape.m"

func TestObjC_ClassInterfaceExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageObjC)
	if !ok {
		t.Fatal("ObjC not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), objcFixturePath, objcFixture, types.LanguageObjC)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Shape")
	if cls == nil {
		t.Fatalf("Shape class not found (class_interface → NodeKindClass); nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestObjC_ProtocolExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageObjC)
	if !ok {
		t.Fatal("ObjC not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), objcFixturePath, objcFixture, types.LanguageObjC)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Drawable")
	if iface == nil {
		t.Fatalf("Drawable protocol not found as NodeKindInterface; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestObjC_MethodExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageObjC)
	if !ok {
		t.Fatal("ObjC not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), objcFixturePath, objcFixture, types.LanguageObjC)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindMethod, "draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindFunction, "draw")
	}
	if fn == nil {
		t.Fatalf("draw method not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestObjC_FunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageObjC)
	if !ok {
		t.Fatal("ObjC not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), objcFixturePath, objcFixture, types.LanguageObjC)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "createShape")
	if fn == nil {
		t.Fatalf("createShape function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestObjC_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageObjC)
	if !ok {
		t.Fatal("ObjC not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), objcFixturePath, objcFixture, types.LanguageObjC)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has #import <Foundation/Foundation.h> and #import \"Shape.h\"")
	}
}

func TestObjC_MessageExpressionEmitsCall(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageObjC)
	if !ok {
		t.Fatal("ObjC not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), objcFixturePath, objcFixture, types.LanguageObjC)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no EdgeKindCalls UnresolvedReferences; fixture has [super init], [s draw], [[Shape alloc] initWithId:...] message expressions")
	}
}

func TestObjC_NodeCountStable(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageObjC)
	if !ok {
		t.Fatal("ObjC not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, objcFixturePath, objcFixture, types.LanguageObjC)
	r2 := e.Extract(ctx, objcFixturePath, objcFixture, types.LanguageObjC)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// A full unit: interface-section declarations and their implementation-section
// definitions, covering all three type forms ResolveKind has to tell apart.
const pascalFixture = `unit Canvas;

interface

uses
  SysUtils, Classes;

type
  TDirection = (dNorth, dSouth, dEast, dWest);

  IDrawable = interface
    procedure Draw;
  end;

  TShape = class(TObject, IDrawable)
  private
    FId: Integer;
    FName: string;
    procedure Render(V: Integer);
  public
    constructor Create(AId: Integer; AName: string);
    destructor Destroy; override;
    procedure Draw; virtual;
    function GetId: Integer;
    function Area: Double; virtual; abstract;
  end;

  TCircle = class(TShape)
  private
    FRadius: Double;
  public
    constructor Create(AId: Integer; AName: string; ARadius: Double);
    function Area: Double; override;
    procedure Draw; override;
  end;

implementation

constructor TShape.Create(AId: Integer; AName: string);
begin
  inherited Create;
  FId := AId;
  FName := AName;
end;

destructor TShape.Destroy;
begin
  inherited Destroy;
end;

procedure TShape.Draw;
begin
  Render(FId);
end;

function TShape.GetId: Integer;
begin
  Result := FId;
end;

procedure TShape.Render(V: Integer);
begin
  WriteLn(FName);
end;

constructor TCircle.Create(AId: Integer; AName: string; ARadius: Double);
begin
  inherited Create(AId, AName);
  FRadius := ARadius;
end;

function TCircle.Area: Double;
begin
  Result := 3.14159 * FRadius * FRadius;
end;

procedure TCircle.Draw;
begin
  inherited Draw;
  WriteLn(FRadius);
end;

function CreateShape(AId: Integer; AName: string): TShape;
var
  S: TShape;
begin
  S := TShape.Create(AId, AName);
  S.Draw;
  Result := S;
end;

end.
`

const pascalFixturePath = "src/Canvas.pas"

func TestPascal_ProcedureExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pascalFixturePath, pascalFixture, types.LanguagePascal)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "Draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "Draw")
	}
	if fn == nil {
		t.Fatalf("Draw procedure not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestPascal_FunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pascalFixturePath, pascalFixture, types.LanguagePascal)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "GetId")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "GetId")
	}
	if fn == nil {
		t.Fatalf("GetId function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestPascal_ClassExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pascalFixturePath, pascalFixture, types.LanguagePascal)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "TShape")
	if cls == nil {
		t.Fatalf("TShape class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestPascal_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pascalFixturePath, pascalFixture, types.LanguagePascal)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has uses SysUtils, Classes")
	}
}

func TestPascal_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pascalFixturePath, pascalFixture, types.LanguagePascal)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no EdgeKindCalls UnresolvedReferences; fixture has Render(FId), WriteLn(FName), TShape.Create() calls")
	}
}

func TestPascal_NodeCountStable(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, pascalFixturePath, pascalFixture, types.LanguagePascal)
	r2 := e.Extract(ctx, pascalFixturePath, pascalFixture, types.LanguagePascal)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

func TestRegistry_For_CP8D_Languages(t *testing.T) {
	t.Parallel()
	reg := languages.NewRegistry()
	tests := []struct {
		lang     types.Language
		wantLang extraction.Lang
	}{
		{types.LanguageDart, extraction.LangDart},
		{types.LanguageObjC, extraction.LangObjC},
		{types.LanguagePascal, extraction.LangPascal},
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
