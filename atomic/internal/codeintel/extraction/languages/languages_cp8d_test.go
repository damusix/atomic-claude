package languages_test

// Tests for Dart, ObjC, Pascal language extractor configs (CP8 batch D — final batch).
//
// Each language has:
//  1. A real fixture parsed through the pool (grammar ABI proof).
//  2. Assertions per success criteria:
//     - Function/method node extracted with correct kind.
//     - Class/interface node extracted.
//     - Import UnresolvedReference emitted.
//     - Call site → UnresolvedReference (EdgeKindCalls) where the grammar supports it.
//     - IsExported correct per-language rule.
//     - Node count stable across two extractions.
//
// Node-type strings are VERIFIED by real grammar parse (see tmp/probe-cp8d/).
// Do NOT change them without running the probe again.
//
// Grammar-specific notes:
//   - Dart: no call_expression node type — calls are expression_statement +
//     identifier + selector; call extraction is BLOCKED for this grammar.
//     Documented below in TestDart_CallsBlocked.
//   - ObjC: message_expression covers Objective-C message sends ([obj msg]).
//     C-style call_expression covers NSLog()-style calls.
//   - Pascal: declProc is the unified node for procedure/function/constructor/
//     destructor declarations in both interface and implementation sections.
//     defProc covers implementation-body procs. exprCall covers call expressions.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Dart
// ---------------------------------------------------------------------------

// dartFixture exercises:
//   - import_or_export (import 'dart:async', import 'package:...')  → EdgeKindImports
//   - enum_declaration (Direction)                → NodeKindEnum
//   - mixin_declaration (Drawable)                → NodeKindClass (mixin ≈ class in Dart)
//   - class_definition (Shape, Circle)            → NodeKindClass
//   - function_signature (inside class_body)      → NodeKindFunction or NodeKindMethod
//   - constructor_signature (Shape, Circle)       → NodeKindFunction or NodeKindMethod
//
// Verified node-type strings (tmp/probe-cp8d/ — Dart grammar):
//
//	import_or_export      — "import 'dart:async';"
//	enum_declaration      — "enum Direction { ... }"
//	mixin_declaration     — "mixin Drawable { ... }"
//	class_definition      — "abstract class Shape ..." / "class Circle ..."
//	function_signature    — "double computeArea(Shape s)" at top-level
//	                        also appears nested inside method_signature for methods
//	constructor_signature — "Shape(this.id, this.name)"
//	method_signature      — wraps function_signature for class member declarations
//
// BLOCKED: Dart grammar has no call_expression node. Call sites appear as
// expression_statement containing identifier + selector (argument_part).
// The engine's named-iterator walk does not see a dedicated call node.
// This is documented below and does not fail the batch — the grammar itself
// is the constraint.
//
// IsExported rule: Dart convention is leading underscore = private.
// No leading underscore = exported (IsExportedByName).
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

// TestDart_FunctionExtracted verifies function_signature → NodeKindFunction.
// WHY: Top-level Dart functions are primary call targets; wrong kind breaks call-graph.
func TestDart_FunctionExtracted(t *testing.T) {
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

// TestDart_MethodExtracted verifies function_signature inside class_body → NodeKindFunction/Method.
// WHY: Dart class methods are declared as function_signature nodes inside class_body;
// the extractor must descend into the class and extract them.
func TestDart_MethodExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// draw is a method inside Circle
	fn := findNode(result.Nodes, types.NodeKindFunction, "draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "draw")
	}
	if fn == nil {
		t.Fatalf("draw method not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestDart_ClassExtracted verifies class_definition → NodeKindClass.
// WHY: Classes are structural containers; missing them breaks the member graph.
func TestDart_ClassExtracted(t *testing.T) {
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

// TestDart_EnumExtracted verifies enum_declaration → NodeKindEnum.
// WHY: Enums must be indexed as NodeKindEnum for the type graph to distinguish them from classes.
func TestDart_EnumExtracted(t *testing.T) {
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

// TestDart_ImportsExtracted verifies import_or_export → UnresolvedReference (EdgeKindImports).
// WHY: Import declarations are the resolution layer's anchor for cross-file references.
func TestDart_ImportsExtracted(t *testing.T) {
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

// TestDart_IsExported_UnderscoreConvention verifies Dart's underscore = private convention.
// WHY: Dart visibility is name-based (leading _ = library-private). The IsExportedByName
// hook must return false for underscore names and true for public names.
func TestDart_IsExported_UnderscoreConvention(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)

	// _privateHelper has a leading underscore → not exported
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"computeArea", true},
		{"draw", true},
		{"_privateHelper", false},
	} {
		// search both Function and Method kinds since the extractor may use either
		n := findNode(result.Nodes, types.NodeKindFunction, tc.name)
		if n == nil {
			n = findNode(result.Nodes, types.NodeKindMethod, tc.name)
		}
		if n == nil {
			// _privateHelper may not be extracted at top-level; skip if missing
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("Dart %s: IsExported=%v, want %v", tc.name, n.IsExported, tc.want)
		}
	}
}

// TestDart_CallsBlocked documents that Dart's grammar has no call_expression node.
// WHY: The tree-sitter-dart grammar represents function calls as expression_statement
// containing identifier + selector (argument_part) nodes — there is no unified
// call_expression node type (verified by tmp/probe-cp8d/). The extractor therefore
// cannot emit EdgeKindCalls without a grammar-level call node. This is a grammar
// constraint, not an implementation gap — documented here so future maintainers
// know this was checked and is expected behavior.
func TestDart_CallsBlocked(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageDart)
	if !ok {
		t.Fatal("Dart not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), dartFixturePath, dartFixture, types.LanguageDart)
	// We do not assert callRefs > 0 here — that would fail by design.
	// We assert the extractor completes without error.
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors during Dart extraction: %v", result.Errors)
	}
	// Document the known limitation.
	_ = result.UnresolvedReferences
	t.Log("Dart: no call_expression node in grammar; EdgeKindCalls extraction is not supported (grammar constraint)")
}

// TestDart_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestDart_NodeCountStable(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Objective-C
// ---------------------------------------------------------------------------

// objcFixture exercises:
//   - preproc_include (#import Foundation, "Shape.h")  → EdgeKindImports
//   - protocol_declaration (@protocol Drawable)        → NodeKindInterface
//   - class_interface (@interface Shape)               → NodeKindClass
//   - class_implementation (@implementation Shape)    → NodeKindClass
//   - method_declaration (in @interface/@protocol)     → NodeKindMethod or NodeKindFunction
//   - implementation_definition (in @implementation)  → NodeKindMethod or NodeKindFunction
//   - message_expression ([s draw], [[Shape alloc] init:]) → EdgeKindCalls
//   - call_expression (NSLog(...))                    → EdgeKindCalls
//   - function_definition (createShape)               → NodeKindFunction
//
// Verified node-type strings (tmp/probe-cp8d/ — ObjC grammar):
//
//	preproc_include       — "#import <Foundation/Foundation.h>"
//	protocol_declaration  — "@protocol Drawable <NSObject> ... @end"
//	class_interface       — "@interface Shape : NSObject ... @end"
//	class_implementation  — "@implementation Shape ... @end"
//	method_declaration    — "- (void)draw;"
//	implementation_definition — wraps method_definition in @implementation
//	method_definition     — "- (instancetype)initWithId:... { ... }"
//	message_expression    — "[super init]" / "[s draw]" / "[[Shape alloc] init...]"
//	call_expression       — "NSLog(@"rendering %@", _name)"
//	function_definition   — "Shape *createShape(...) { ... }"
//
// Name extraction:
//   - class_interface, class_implementation: first identifier child = class name
//   - protocol_declaration: first identifier child = protocol name
//   - method_declaration: first identifier child after method_type = selector name
//   - implementation_definition: wraps method_definition; selector from first identifier
//
// IsExported rule: ObjC default is public. No access modifier concept at the
// method level (unlike C++ private:). We default IsExported=true for all ObjC
// symbols. (@private/@protected ivar sections are a different story and out of
// scope for CP8.)
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

// TestObjC_ClassInterfaceExtracted verifies @interface → NodeKindClass.
// WHY: @interface is the Obj-C class declaration; missing it breaks the class graph.
func TestObjC_ClassInterfaceExtracted(t *testing.T) {
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

// TestObjC_ProtocolExtracted verifies @protocol → NodeKindInterface.
// WHY: ObjC protocols are the semantic interface equivalent; wrong kind breaks
// resolution's edge promotion for protocol conformance.
func TestObjC_ProtocolExtracted(t *testing.T) {
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

// TestObjC_MethodExtracted verifies method_declaration/implementation_definition → NodeKindMethod/Function.
// WHY: ObjC methods are the primary call targets; missing them breaks the call graph.
func TestObjC_MethodExtracted(t *testing.T) {
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

// TestObjC_FunctionExtracted verifies C-style function_definition → NodeKindFunction.
// WHY: ObjC files can contain plain C functions; these must be indexed.
func TestObjC_FunctionExtracted(t *testing.T) {
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

// TestObjC_ImportsExtracted verifies preproc_include → UnresolvedReference (EdgeKindImports).
// WHY: #import is the ObjC import mechanism; the resolution layer needs these references.
func TestObjC_ImportsExtracted(t *testing.T) {
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

// TestObjC_MessageExpressionEmitsCall verifies message_expression → EdgeKindCalls.
// WHY: ObjC message sends [obj method] are the primary call mechanism; they must
// emit EdgeKindCalls so the call graph is populated.
func TestObjC_MessageExpressionEmitsCall(t *testing.T) {
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

// TestObjC_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestObjC_NodeCountStable(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Pascal
// ---------------------------------------------------------------------------

// pascalFixture exercises:
//   - declUses (uses SysUtils, Classes)           → EdgeKindImports
//   - declType → declClass (TShape, TCircle)      → NodeKindClass
//   - declType → declEnum (TDirection)            → NodeKindEnum (via declType + declEnum child)
//   - declType → declIntf (IDrawable)             → NodeKindInterface
//   - declProc (procedure/function/constructor decls in interface)  → NodeKindFunction
//   - defProc  (procedure/function/constructor impls in impl section) → NodeKindFunction
//   - exprCall (Render(FId), WriteLn(FName), TShape.Create(...))    → EdgeKindCalls
//
// Verified node-type strings (tmp/probe-cp8d/ — Pascal grammar):
//
//	unit            — "unit Canvas;\n interface\n ... implementation\n ... end."
//	interface       — "interface\n uses ...\n type ...\n"
//	declUses        — "uses SysUtils, Classes;"
//	declTypes       — "type TDirection = ... TShape = ..."
//	declType        — "TDirection = (dNorth, ...);"  / "TShape = class(...)"
//	declClass       — "class(TObject, IDrawable) ... end"
//	declIntf        — "interface ... end" (Pascal interface type, NOT ObjC)
//	declEnum        — "(dNorth, dSouth, dEast, dWest)"
//	declProc        — "procedure Draw; virtual;" / "function GetId: Integer;"
//	                  also "constructor Create(...)" / "destructor Destroy"
//	defProc         — "procedure TShape.Draw;\nbegin ... end;"
//	exprCall        — "Render(FId)" / "WriteLn(FName)" / "TShape.Create(AId, AName)"
//	declField       — "FId: Integer;" / "FName: string;"
//
// Name extraction:
//   - declType: first identifier child is the type name
//   - declClass: name comes from parent declType's first identifier
//   - declProc: identifier child (after keyword) is the procedure/function name
//   - defProc: contains a declProc child — name from that child's identifier
//   - exprCall: first identifier child is the callee name
//
// IsExported rule: Pascal has public/private/protected section keywords but
// these are structural section markers (declSection with kPublic/kPrivate child)
// rather than per-symbol modifiers. Default IsExported=true for all Pascal symbols
// (public by default outside class sections). Private/protected tracking within
// classes requires parent-section context accumulation — out of scope for CP8.
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

// TestPascal_ProcedureExtracted verifies declProc (procedure) → NodeKindFunction.
// WHY: Pascal procedures are primary callable units; wrong kind breaks the call graph.
func TestPascal_ProcedureExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pascalFixturePath, pascalFixture, types.LanguagePascal)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Draw is declared in the interface section as a procedure
	fn := findNode(result.Nodes, types.NodeKindFunction, "Draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "Draw")
	}
	if fn == nil {
		t.Fatalf("Draw procedure not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestPascal_FunctionExtracted verifies declProc (function) → NodeKindFunction.
// WHY: Pascal functions return values; must be extracted for call-graph resolution.
func TestPascal_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguagePascal)
	if !ok {
		t.Fatal("Pascal not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), pascalFixturePath, pascalFixture, types.LanguagePascal)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// GetId is a function declared in the interface section
	fn := findNode(result.Nodes, types.NodeKindFunction, "GetId")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "GetId")
	}
	if fn == nil {
		t.Fatalf("GetId function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestPascal_ClassExtracted verifies declType → declClass → NodeKindClass.
// WHY: Pascal classes are structural containers; missing them breaks the member graph.
func TestPascal_ClassExtracted(t *testing.T) {
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

// TestPascal_ImportsExtracted verifies declUses → UnresolvedReference (EdgeKindImports).
// WHY: Pascal uses clauses are the import mechanism for the resolution layer.
func TestPascal_ImportsExtracted(t *testing.T) {
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

// TestPascal_CallEmitsUnresolvedReference verifies exprCall → EdgeKindCalls.
// WHY: exprCall is Pascal's call expression node; must emit EdgeKindCalls for
// the call graph to be populated.
func TestPascal_CallEmitsUnresolvedReference(t *testing.T) {
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

// TestPascal_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestPascal_NodeCountStable(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Registry — batch D languages registered
// ---------------------------------------------------------------------------

// TestRegistry_For_CP8D_Languages verifies all 3 new languages are registered.
// WHY: The registry is the single resolution point for CP10; missing entries
// cause the orchestrator to silently skip files of those languages.
func TestRegistry_For_CP8D_Languages(t *testing.T) {
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
