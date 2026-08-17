package languages_test

// Java, C, C++, and C#. Every fixture here runs through the real grammar, so
// these also cover ABI and pool wiring, not only the configs.
//
// Each language repeats one shape: every declaration form reaches its intended
// node kind, imports and calls surface as references rather than edges, export
// status follows the language's own rule, and two runs agree.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Covers every declaration form, at all three visibilities.
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

func TestJava_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestJava_ClassExtracted(t *testing.T) {
	t.Parallel()
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

func TestJava_InterfaceExtracted(t *testing.T) {
	t.Parallel()
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

func TestJava_EnumExtracted(t *testing.T) {
	t.Parallel()
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

func TestJava_ImportsExtracted(t *testing.T) {
	t.Parallel()
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

func TestJava_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestJava_IsExported_PublicModifier(t *testing.T) {
	t.Parallel()
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

func TestJava_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// Covers both typedef forms and a static function beside a non-static one.
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

func TestC_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestC_StructExtracted(t *testing.T) {
	t.Parallel()
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

func TestC_EnumExtracted(t *testing.T) {
	t.Parallel()
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

func TestC_ImportsExtracted(t *testing.T) {
	t.Parallel()
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

func TestC_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestC_IsExported_StaticRule(t *testing.T) {
	t.Parallel()
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

func TestC_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// Covers all three aggregate types ResolveKind has to tell apart, plus a
// namespace, and both member and free functions.
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

func TestCpp_FunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageCpp)
	if !ok {
		t.Fatal("C++ not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), cppFixturePath, cppFixture, types.LanguageCpp)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "main")
	if fn == nil {
		t.Fatalf("main function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestCpp_ClassExtracted(t *testing.T) {
	t.Parallel()
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

func TestCpp_StructExtracted(t *testing.T) {
	t.Parallel()
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

func TestCpp_EnumExtracted(t *testing.T) {
	t.Parallel()
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

func TestCpp_ImportsExtracted(t *testing.T) {
	t.Parallel()
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

func TestCpp_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestCpp_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// Covers every declaration form, at all three visibilities.
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

func TestCSharp_FunctionExtracted(t *testing.T) {
	t.Parallel()
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

func TestCSharp_ClassExtracted(t *testing.T) {
	t.Parallel()
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

func TestCSharp_InterfaceExtracted(t *testing.T) {
	t.Parallel()
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

func TestCSharp_EnumExtracted(t *testing.T) {
	t.Parallel()
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

func TestCSharp_ImportsExtracted(t *testing.T) {
	t.Parallel()
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

func TestCSharp_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
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

func TestCSharp_IsExported_PublicModifier(t *testing.T) {
	t.Parallel()
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

func TestCSharp_NodeCountStable(t *testing.T) {
	t.Parallel()
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

// The two shapes the implicit-public fallback must separate: an unmarked class
// field, which is package-private, and an unmarked interface method, which is
// genuinely public. Both carry no modifier and no body.
const javaFieldVisibilityFixture = `public interface Runnable {
    void run();
}

class Box {
    int x;
}
`

const javaFieldVisibilityPath = "src/Box.java"

func TestJava_BareClassField_NotExported(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFieldVisibilityPath, javaFieldVisibilityFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	field := findNode(result.Nodes, types.NodeKindField, "x")
	if field == nil {
		t.Fatalf("field x not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if field.IsExported {
		t.Errorf("Java bare class field x: IsExported=true, want false (package-private field should not be exported)")
	}
}

func TestJava_InterfaceMethod_ImplicitlyPublic(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageJava)
	if !ok {
		t.Fatal("Java not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), javaFieldVisibilityPath, javaFieldVisibilityFixture, types.LanguageJava)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	method := findNode(result.Nodes, types.NodeKindMethod, "run")
	if method == nil {
		t.Fatalf("method run not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !method.IsExported {
		t.Errorf("Java interface method run: IsExported=false, want true (interface methods are implicitly public)")
	}
}

// The same pair as Java, where an unmarked class member defaults to private.
const csharpFieldVisibilityFixture = `public interface IRunnable {
    void Run();
}

class Box {
    int x;
}
`

const csharpFieldVisibilityPath = "src/Box.cs"

func TestCSharp_BareClassField_NotExported(t *testing.T) {
	t.Parallel()
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

func TestCSharp_InterfaceMethod_ImplicitlyPublic(t *testing.T) {
	t.Parallel()
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

func TestRegistry_For_CP8A_Languages(t *testing.T) {
	t.Parallel()
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
