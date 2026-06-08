package languages_test

// Tests for Java, C, C++, C# language extractor configs (CP8 batch A).
//
// Each language has:
//   1. A real fixture parsed through the pool (grammar ABI proof).
//   2. Assertions per success criteria:
//      - Function/method node extracted with correct kind.
//      - Class/struct node extracted.
//      - Interface (Java, C#) or enum (C, C++) extracted with correct kind.
//      - Import UnresolvedReference emitted.
//      - Call site → UnresolvedReference (EdgeKindCalls).
//      - IsExported correct per-language rule.
//      - Node count stable across two extractions.
//
// Node-type strings are VERIFIED by real grammar parse (see tmp/probe-cp8a/).
// Do NOT change them without running the probe again.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Java
// ---------------------------------------------------------------------------

// javaFixture exercises:
//   - import_declaration  (java.util.List, java.io.IOException)
//   - interface_declaration  (Drawable)
//   - enum_declaration  (Direction)
//   - class_declaration  (Canvas — public, implements Drawable; Main)
//   - method_declaration  (public draw / private getId / package-private helper)
//   - field_declaration  (public/private/protected fields)
//   - method_invocation  (render(), c.draw(), System.out.println())
//   - object_creation_expression  (new Canvas(...))
//
// Verified node-type strings (tmp/probe-cp8a/ — Java grammar):
//
//	import_declaration         — "import java.util.List;"
//	interface_declaration      — "public interface Drawable { ... }"
//	enum_declaration           — "public enum Direction { ... }"
//	class_declaration          — "public class Canvas ..." / "public class Main ..."
//	method_declaration         — "public void draw() {}" / "int getId() {}"
//	field_declaration          — "private int id;" / "public String name;"
//	method_invocation          — "render(this.id)" / "c.draw()" / "System.out.println(...)"
//	object_creation_expression — "new Canvas(1, \"test\")"
//
// Name field: 'name' works on class_declaration, interface_declaration,
//
//	enum_declaration, method_declaration (verified by probe4).
//
// IsExported rule: 'modifiers' named child contains "public".
const javaFixture = `import java.util.List;
import java.io.IOException;

public interface Drawable {
    void draw();
    int getId();
}

public enum Direction {
    NORTH, SOUTH, EAST, WEST;

    public boolean isVertical() {
        return this == NORTH || this == SOUTH;
    }
}

public class Canvas implements Drawable {
    private int id;
    public String name;

    public Canvas(int id, String name) {
        this.id = id;
        this.name = name;
    }

    @Override
    public void draw() {
        render(this.id);
    }

    @Override
    public int getId() {
        return this.id;
    }

    int packageHelper() {
        return this.id * 2;
    }
}

public class Main {
    public static void main(String[] args) {
        Canvas c = new Canvas(1, "test");
        c.draw();
        System.out.println(c.getId());
    }
}
`

const javaFixturePath = "src/Canvas.java"

// TestJava_FunctionExtracted verifies method_declaration → NodeKindMethod.
// WHY: Java methods are the primary call targets; wrong kind breaks call-graph
// edge promotion during resolution.
func TestJava_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFixturePath, javaFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindMethod, "draw")
	if fn == nil {
		t.Fatalf("draw method not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestJava_ClassExtracted verifies class_declaration → NodeKindClass.
// WHY: Classes are the structural containers; missing them breaks the member graph.
func TestJava_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFixturePath, javaFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Canvas")
	if cls == nil {
		t.Fatalf("Canvas class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestJava_InterfaceExtracted verifies interface_declaration → NodeKindInterface.
// WHY: Interfaces are the type-contract nodes; wrong kind breaks implements edge promotion.
func TestJava_InterfaceExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFixturePath, javaFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Drawable")
	if iface == nil {
		t.Fatalf("Drawable interface not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestJava_EnumExtracted verifies enum_declaration → NodeKindEnum.
// WHY: Enums are typed constant sets; extracting as struct would break query correctness.
func TestJava_EnumExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFixturePath, javaFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Direction")
	if en == nil {
		t.Fatalf("Direction enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestJava_ImportsExtracted verifies import_declaration emits UnresolvedReference.
// WHY: Imports are the starting point for the resolution layer's import resolver.
func TestJava_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFixturePath, javaFixture, types.LanguageJava)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports java.util.List and java.io.IOException")
	}
}

// TestJava_CallEmitsUnresolvedReference verifies method_invocation → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestJava_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFixturePath, javaFixture, types.LanguageJava)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render(), c.draw(), System.out.println()")
	}
}

// TestJava_IsExported_PublicModifier verifies public=exported, non-public=not exported.
// WHY: Java's access control is explicit; public = exported. Wrong IsExported means
// the +10 resolution scoring bonus applies to package-private symbols.
func TestJava_IsExported_PublicModifier(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFixturePath, javaFixture, types.LanguageJava)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindClass, "Canvas", true},          // public class Canvas
		{types.NodeKindInterface, "Drawable", true},    // public interface Drawable
		{types.NodeKindEnum, "Direction", true},        // public enum Direction
		{types.NodeKindMethod, "draw", true},           // public void draw()
		{types.NodeKindMethod, "packageHelper", false}, // package-private (no modifier)
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("Java %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

// TestJava_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestJava_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, javaFixturePath, javaFixture, types.LanguageJava)
	r2 := e.Extract(ctx, javaFixturePath, javaFixture, types.LanguageJava)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// C
// ---------------------------------------------------------------------------

// cFixture exercises:
//   - preproc_include   (#include <stdio.h>, etc.)
//   - type_definition   (typedef struct → Point; typedef enum → Color)
//   - function_definition  (static int helper / int add / void process / int main)
//   - call_expression   (printf, helper, add, process)
//
// Verified node-type strings (tmp/probe-cp8a/ — C grammar):
//
//	preproc_include        — "#include <stdio.h>"
//	type_definition        — "typedef struct { ... } Point;"
//	struct_specifier       — "struct { int x; int y; }"  (inside type_definition)
//	enum_specifier         — "enum { RED, GREEN, BLUE }" (inside type_definition)
//	function_definition    — "static int helper(...)" / "int add(...)"
//	call_expression        — "printf(...)" / "helper(p->x)"
//
// Name extraction for function_definition:
//
//	function_definition.ChildByFieldName("declarator") = function_declarator
//	function_declarator first-named-child = identifier (the function name)
//
// Name extraction for type_definition (typedef struct/enum):
//
//	type_definition last-named-child = type_identifier (the typedef alias name)
//
// IsExported rule: top-level non-static symbols are exported.
//
//	Absence of a storage_class_specifier named child = exported.
//	Presence of storage_class_specifier with text "static" = not exported.
const cFixture = `#include <stdio.h>
#include <stdlib.h>

typedef struct {
    int x;
    int y;
} Point;

typedef enum {
    RED,
    GREEN,
    BLUE
} Color;

static int helper(int x) {
    return x * 2;
}

int add(int a, int b) {
    return a + b;
}

void process(Point* p) {
    printf("%d %d\n", p->x, p->y);
    helper(p->x);
}

int main(void) {
    Point p;
    p.x = 1;
    p.y = 2;
    int result = add(p.x, p.y);
    process(&p);
    return 0;
}
`

const cFixturePath = "src/shapes.c"

// TestC_FunctionExtracted verifies function_definition → NodeKindFunction.
// WHY: C functions are the primary callable units; wrong kind breaks call-graph.
func TestC_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageC)
	if !ok {
		t.Fatal("C not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cFixturePath, cFixture, types.LanguageC)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "add")
	if fn == nil {
		t.Fatalf("add function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestC_StructExtracted verifies typedef struct → NodeKindStruct.
// WHY: Struct types are the primary data containers in C; missing them breaks member resolution.
func TestC_StructExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageC)
	if !ok {
		t.Fatal("C not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cFixturePath, cFixture, types.LanguageC)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	st := findNode(result.Nodes, types.NodeKindStruct, "Point")
	if st == nil {
		t.Fatalf("Point struct not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestC_EnumExtracted verifies typedef enum → NodeKindEnum.
// WHY: C enums model typed constants; storing as struct breaks semantic correctness.
func TestC_EnumExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageC)
	if !ok {
		t.Fatal("C not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cFixturePath, cFixture, types.LanguageC)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Color")
	if en == nil {
		t.Fatalf("Color enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestC_ImportsExtracted verifies preproc_include emits UnresolvedReference.
// WHY: #include is the C import mechanism; the resolution layer uses it to resolve headers.
func TestC_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageC)
	if !ok {
		t.Fatal("C not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cFixturePath, cFixture, types.LanguageC)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has #include <stdio.h> and <stdlib.h>")
	}
}

// TestC_CallEmitsUnresolvedReference verifies call_expression → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestC_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageC)
	if !ok {
		t.Fatal("C not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cFixturePath, cFixture, types.LanguageC)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has printf, helper, add, process calls")
	}
}

// TestC_IsExported_StaticRule verifies static functions are NOT exported; non-static are.
// WHY: C uses static for translation-unit-private; wrong IsExported corrupts link-resolution.
func TestC_IsExported_StaticRule(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageC)
	if !ok {
		t.Fatal("C not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cFixturePath, cFixture, types.LanguageC)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindFunction, "add", true},     // non-static: exported
		{types.NodeKindFunction, "process", true}, // non-static: exported
		{types.NodeKindFunction, "helper", false}, // static: not exported
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("C %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

// TestC_NodeCountStable verifies deterministic extraction.
func TestC_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageC)
	if !ok {
		t.Fatal("C not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, cFixturePath, cFixture, types.LanguageC)
	r2 := e.Extract(ctx, cFixturePath, cFixture, types.LanguageC)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// C++
// ---------------------------------------------------------------------------

// cppFixture exercises:
//   - preproc_include    (#include <iostream>, etc.)
//   - class_specifier    (Shape — abstract; Circle — concrete)
//   - struct_specifier   (Point)
//   - enum_specifier     (Color — scoped enum class)
//   - namespace_definition  (geometry)
//   - function_definition   (inside class + top-level)
//   - call_expression    (c.area(), geometry::distance(), Circle::unit(), etc.)
//
// Verified node-type strings (tmp/probe-cp8a/ — C++ grammar):
//
//	class_specifier      — "class Shape { ... }" / "class Circle : public Shape { ... }"
//	struct_specifier     — "struct Point { ... }"
//	enum_specifier       — "enum class Color { ... }"
//	namespace_definition — "namespace geometry { ... }"
//	function_definition  — "double area() const { ... }" (inside class body)
//	preproc_include      — "#include <iostream>"
//	call_expression      — "c.area()" / "Circle::unit()"
//
// Kind disambiguation (ResolveKind):
//
//	class_specifier  → NodeKindClass  (default for StructTypes)
//	struct_specifier → NodeKindStruct (returned by ResolveKind)
//	enum_specifier   → NodeKindEnum   (returned by ResolveKind)
//
// Name: class_specifier / struct_specifier have 'name' field → type_identifier.
//
//	function_definition: ResolveBody → function_declarator; fallback finds identifier.
//
// IsExported rule: C++ has no export keyword. Rule: top-level symbols and
// public-section members are treated as potentially exported. Implementation:
// all symbols are exported=true (C++ visibility is enforced by the linker and
// access specifiers, not by a dedicated grammar node). Document this simplification.
const cppFixture = `#include <iostream>
#include <vector>

class Shape {
public:
    virtual double area() const = 0;
    virtual ~Shape() {}
};

struct Point {
    double x;
    double y;

    Point(double xx, double yy) : x(xx), y(yy) {}
};

class Circle : public Shape {
private:
    Point center;
    double radius;

public:
    Circle(Point c, double r) : center(c), radius(r) {}

    double area() const override {
        return 3.14159 * radius * radius;
    }

    static Circle unit() {
        return Circle(Point(0.0, 0.0), 1.0);
    }
};

enum class Color {
    Red,
    Green,
    Blue
};

namespace geometry {
    double distance(const Point& a, const Point& b) {
        double dx = a.x - b.x;
        double dy = a.y - b.y;
        return dx * dx + dy * dy;
    }
}

int main() {
    Circle c = Circle::unit();
    std::cout << c.area() << std::endl;
    return 0;
}
`

const cppFixturePath = "src/shapes.cpp"

// TestCpp_FunctionExtracted verifies function_definition → NodeKindFunction.
// WHY: C++ functions are the primary callable units; wrong kind breaks call-graph.
func TestCpp_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cppFixturePath, cppFixture, types.LanguageCpp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// main() is a top-level function
	fn := findNode(result.Nodes, types.NodeKindFunction, "main")
	if fn == nil {
		t.Fatalf("main function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCpp_ClassExtracted verifies class_specifier → NodeKindClass.
// WHY: C++ classes are structural containers; missing them breaks member resolution.
func TestCpp_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cppFixturePath, cppFixture, types.LanguageCpp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Shape")
	if cls == nil {
		t.Fatalf("Shape class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCpp_StructExtracted verifies struct_specifier → NodeKindStruct (via ResolveKind).
// WHY: C++ structs are structurally distinct from classes; wrong kind breaks field resolution.
func TestCpp_StructExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cppFixturePath, cppFixture, types.LanguageCpp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	st := findNode(result.Nodes, types.NodeKindStruct, "Point")
	if st == nil {
		t.Fatalf("Point struct not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCpp_EnumExtracted verifies enum_specifier → NodeKindEnum (via ResolveKind).
// WHY: C++ enums (including scoped enum class) must produce NodeKindEnum, not NodeKindStruct.
func TestCpp_EnumExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cppFixturePath, cppFixture, types.LanguageCpp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Color")
	if en == nil {
		t.Fatalf("Color enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCpp_ImportsExtracted verifies preproc_include emits UnresolvedReference.
// WHY: #include is C++'s import mechanism; the resolution layer uses these paths.
func TestCpp_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cppFixturePath, cppFixture, types.LanguageCpp)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has #include <iostream> and <vector>")
	}
}

// TestCpp_CallEmitsUnresolvedReference verifies call_expression → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestCpp_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cppFixturePath, cppFixture, types.LanguageCpp)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has c.area(), Circle::unit() calls")
	}
}

// TestCpp_NodeCountStable verifies deterministic extraction.
func TestCpp_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, cppFixturePath, cppFixture, types.LanguageCpp)
	r2 := e.Extract(ctx, cppFixturePath, cppFixture, types.LanguageCpp)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// C#
// ---------------------------------------------------------------------------

// csharpFixture exercises:
//   - using_directive     (using System; using System.Collections.Generic;)
//   - namespace_declaration  (namespace MyApp { ... })
//   - interface_declaration  (IDrawable)
//   - enum_declaration    (Direction)
//   - class_declaration   (Canvas — public; Program — package-private)
//   - method_declaration  (public Draw / private static Render / void Main)
//   - property_declaration (Name { get; set; })
//   - field_declaration   (private int _id; protected List<string> Items;)
//   - invocation_expression  (Render(_id), Console.WriteLine(...))
//   - object_creation_expression  (new List<string>(), new Canvas(...))
//
// Verified node-type strings (tmp/probe-cp8a/ — C# grammar):
//
//	using_directive              — "using System;"
//	namespace_declaration        — "namespace MyApp { ... }"
//	interface_declaration        — "public interface IDrawable { ... }"
//	enum_declaration             — "public enum Direction { ... }"
//	class_declaration            — "public class Canvas : IDrawable { ... }"
//	method_declaration           — "public void Draw() {}" / "private static void Render(...)"
//	property_declaration         — "public string Name { get; set; }"
//	field_declaration            — "private int _id;"
//	invocation_expression        — "Render(_id)" / "Console.WriteLine(...)"
//	object_creation_expression   — "new Canvas(1, \"test\")"
//
// Name field: 'name' works on class_declaration, interface_declaration,
//
//	enum_declaration, method_declaration, namespace_declaration (verified by probe4).
//
// IsExported rule: 'modifier' named child contains "public" → exported.
const csharpFixture = `using System;
using System.Collections.Generic;

namespace MyApp
{
    public interface IDrawable
    {
        void Draw();
        int GetId();
    }

    public enum Direction
    {
        North,
        South,
        East,
        West
    }

    public class Canvas : IDrawable
    {
        private int _id;
        public string Name { get; set; }
        protected List<string> Items;

        public Canvas(int id, string name)
        {
            _id = id;
            Name = name;
            Items = new List<string>();
        }

        public void Draw()
        {
            Render(_id);
        }

        public int GetId()
        {
            return _id;
        }

        private static void Render(int id)
        {
            Console.WriteLine(id);
        }

        void PackageMethod()
        {
        }
    }

    class Program
    {
        static void Main(string[] args)
        {
            Canvas c = new Canvas(1, "test");
            c.Draw();
            Console.WriteLine(c.GetId());
        }
    }
}
`

const csharpFixturePath = "src/Canvas.cs"

// TestCSharp_FunctionExtracted verifies method_declaration → NodeKindMethod.
// WHY: C# methods are the primary callable units; wrong kind breaks call-graph.
func TestCSharp_FunctionExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFixturePath, csharpFixture, types.LanguageCSharp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindMethod, "Draw")
	if fn == nil {
		t.Fatalf("Draw method not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCSharp_ClassExtracted verifies class_declaration → NodeKindClass.
// WHY: Classes are structural containers; missing them breaks the member graph.
func TestCSharp_ClassExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFixturePath, csharpFixture, types.LanguageCSharp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Canvas")
	if cls == nil {
		t.Fatalf("Canvas class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCSharp_InterfaceExtracted verifies interface_declaration → NodeKindInterface.
// WHY: Interfaces are the type-contract nodes; wrong kind breaks implements edge promotion.
func TestCSharp_InterfaceExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFixturePath, csharpFixture, types.LanguageCSharp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "IDrawable")
	if iface == nil {
		t.Fatalf("IDrawable interface not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCSharp_EnumExtracted verifies enum_declaration → NodeKindEnum.
// WHY: C# enums are typed constant sets; storing as struct breaks semantic correctness.
func TestCSharp_EnumExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFixturePath, csharpFixture, types.LanguageCSharp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Direction")
	if en == nil {
		t.Fatalf("Direction enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// TestCSharp_ImportsExtracted verifies using_directive emits UnresolvedReference.
// WHY: using directives are C#'s import mechanism; the resolution layer uses these.
func TestCSharp_ImportsExtracted(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFixturePath, csharpFixture, types.LanguageCSharp)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has 'using System' and 'using System.Collections.Generic'")
	}
}

// TestCSharp_CallEmitsUnresolvedReference verifies invocation_expression → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestCSharp_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFixturePath, csharpFixture, types.LanguageCSharp)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has Render(), c.Draw(), Console.WriteLine()")
	}
}

// TestCSharp_IsExported_PublicModifier verifies public=exported, non-public=not exported.
// WHY: C# access control is explicit; public = exported. Wrong IsExported corrupts scoring.
func TestCSharp_IsExported_PublicModifier(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFixturePath, csharpFixture, types.LanguageCSharp)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindClass, "Canvas", true},          // public class Canvas
		{types.NodeKindInterface, "IDrawable", true},   // public interface IDrawable
		{types.NodeKindEnum, "Direction", true},        // public enum Direction
		{types.NodeKindMethod, "Draw", true},           // public void Draw()
		{types.NodeKindMethod, "PackageMethod", false}, // no modifier (package-private)
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("C# %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

// TestCSharp_NodeCountStable verifies deterministic extraction.
func TestCSharp_NodeCountStable(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, csharpFixturePath, csharpFixture, types.LanguageCSharp)
	r2 := e.Extract(ctx, csharpFixturePath, csharpFixture, types.LanguageCSharp)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// ---------------------------------------------------------------------------
// Registry — extended for 4 new languages
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// F-13: implicit-public heuristic must NOT mark bare class fields as exported
// ---------------------------------------------------------------------------

// javaFieldVisibilityFixture has:
//   - a bare (package-private) field "int x;" inside a class — must be IsExported=false
//   - an interface method "void run();" — must be IsExported=true (legitimate implicit-public)
//
// WHY: javaIsExported's "no modifiers AND no body → implicitly public" rule was
// designed for interface abstract methods, which are genuinely public. A bare class
// field has no modifiers AND no body but is package-private in Java — IsExported=true
// is wrong and skews the +10 ScoreExported bonus in resolution toward hidden fields.
const javaFieldVisibilityFixture = `public interface Runnable {
    void run();
}

class Box {
    int x;
}
`

const javaFieldVisibilityPath = "src/Box.java"

// TestJava_BareClassField_NotExported verifies that a bare (no-modifier) field
// inside a class is IsExported=false, not implicitly public.
// This test must FAIL before the F-13 fix and PASS after.
func TestJava_BareClassField_NotExported(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFieldVisibilityPath, javaFieldVisibilityFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Field "x" in class Box has no access modifier → package-private in Java → not exported.
	field := findNode(result.Nodes, types.NodeKindField, "x")
	if field == nil {
		t.Fatalf("field x not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if field.IsExported {
		t.Errorf("Java bare class field x: IsExported=true, want false (package-private field should not be exported)")
	}
}

// TestJava_InterfaceMethod_ImplicitlyPublic verifies that an interface method with
// no modifiers and no body is still IsExported=true (the legitimate case).
// This test must PASS before and after the F-13 fix — regression guard.
func TestJava_InterfaceMethod_ImplicitlyPublic(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFieldVisibilityPath, javaFieldVisibilityFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// "void run();" in an interface — no modifiers, no body → implicitly public.
	method := findNode(result.Nodes, types.NodeKindMethod, "run")
	if method == nil {
		t.Fatalf("method run not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !method.IsExported {
		t.Errorf("Java interface method run: IsExported=false, want true (interface methods are implicitly public)")
	}
}

// csharpFieldVisibilityFixture has:
//   - a bare (no-modifier) field "int x;" inside a class — must be IsExported=false
//   - an interface method "void Run();" — must be IsExported=true
//
// WHY: same as Java — C# class members without an access modifier default to
// private, not public. The implicit-public fallback only applies to interface members.
const csharpFieldVisibilityFixture = `public interface IRunnable {
    void Run();
}

class Box {
    int x;
}
`

const csharpFieldVisibilityPath = "src/Box.cs"

// TestCSharp_BareClassField_NotExported verifies that a bare (no-modifier) field
// inside a class is IsExported=false.
// This test must FAIL before the F-13 fix and PASS after.
func TestCSharp_BareClassField_NotExported(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFieldVisibilityPath, csharpFieldVisibilityFixture, types.LanguageCSharp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	field := findNode(result.Nodes, types.NodeKindField, "x")
	if field == nil {
		t.Fatalf("field x not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if field.IsExported {
		t.Errorf("C# bare class field x: IsExported=true, want false (no-modifier class field is private in C#)")
	}
}

// TestCSharp_InterfaceMethod_ImplicitlyPublic verifies that an interface method with
// no modifiers and no body is IsExported=true (the legitimate case).
// Regression guard — must pass before and after F-13 fix.
func TestCSharp_InterfaceMethod_ImplicitlyPublic(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCSharp)
	if !ok {
		t.Fatal("C# not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), csharpFieldVisibilityPath, csharpFieldVisibilityFixture, types.LanguageCSharp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	method := findNode(result.Nodes, types.NodeKindMethod, "Run")
	if method == nil {
		t.Fatalf("method Run not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !method.IsExported {
		t.Errorf("C# interface method Run: IsExported=false, want true (interface methods are implicitly public)")
	}
}

// ---------------------------------------------------------------------------
// Registry — extended for 4 new languages
// ---------------------------------------------------------------------------

// TestRegistry_For_CP8A_Languages verifies all 4 new languages are registered.
// WHY: The registry is the single resolution point for CP10; missing entries
// cause the orchestrator to silently skip files of those languages.
func TestRegistry_For_CP8A_Languages(t *testing.T) {
	reg := languages.NewRegistry()
	tests := []struct {
		lang     types.Language
		wantLang extraction.Lang
	}{
		{types.LanguageJava, extraction.LangJava},
		{types.LanguageC, extraction.LangC},
		{types.LanguageCpp, extraction.LangCpp},
		{types.LanguageCSharp, extraction.LangCSharp},
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
