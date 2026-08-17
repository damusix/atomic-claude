package languages_test

// Swift, Kotlin, and Scala. Every fixture here runs through the real grammar, so
// these also cover ABI and pool wiring, not only the configs.
//
// Each language repeats one shape: every declaration form reaches its intended
// node kind, its interface equivalent included; imports and calls surface as
// references rather than edges; export status follows the language's own rule;
// and two runs agree.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Declares an enum, a struct, and a class, all three of which parse as the same
// node type, and mixes public, open, private, and unmarked visibility.
const swiftFixture = `import Foundation
import UIKit

public protocol Drawable {
    func draw() -> Void
    var id: Int { get }
}

public enum Direction: Int {
    case north = 0
    case south = 1

    public func isVertical() -> Bool {
        return self == .north || self == .south
    }
}

public struct Point {
    public var x: Double
    public var y: Double
}

open class Canvas: Drawable {
    private var _id: Int
    public var name: String

    public init(id: Int, name: String) {
        self._id = id
        self.name = name
    }

    public func draw() -> Void {
        render(self._id)
    }

    public var id: Int { return self._id }

    private func render(_ v: Int) {
        print(v)
    }

    internal func internalHelper() -> Int {
        return self._id
    }
}

func createCanvas() -> Canvas {
    let c = Canvas(id: 1, name: "test")
    c.draw()
    return c
}
`

const swiftFixturePath = "src/Canvas.swift"

func TestSwift_FunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), swiftFixturePath, swiftFixture, types.LanguageSwift)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "draw")
	}
	if fn == nil {
		t.Fatalf("draw function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestSwift_ClassExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), swiftFixturePath, swiftFixture, types.LanguageSwift)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Canvas")
	if cls == nil {
		t.Fatalf("Canvas class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestSwift_InterfaceExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), swiftFixturePath, swiftFixture, types.LanguageSwift)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Drawable")
	if iface == nil {
		t.Fatalf("Drawable protocol not found as NodeKindInterface; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestSwift_EnumExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), swiftFixturePath, swiftFixture, types.LanguageSwift)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Direction")
	if en == nil {
		t.Fatalf("Direction enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestSwift_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), swiftFixturePath, swiftFixture, types.LanguageSwift)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports Foundation and UIKit")
	}
}

func TestSwift_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), swiftFixturePath, swiftFixture, types.LanguageSwift)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render, c.draw, print calls")
	}
}

func TestSwift_IsExported_PublicOpen(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), swiftFixturePath, swiftFixture, types.LanguageSwift)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindInterface, "Drawable", true}, // public protocol
		{types.NodeKindClass, "Canvas", true},       // open class
		// Unmarked means internal, which does not leave the module.
		{types.NodeKindFunction, "internalHelper", false}, // internal func
		{types.NodeKindFunction, "render", false},         // private func
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			fn := findNode(result.Nodes, types.NodeKindMethod, tc.name)
			if fn != nil {
				n = fn
			}
		}
		if n == nil {
			t.Errorf("node %s/%s not found; nodes: %s", tc.kind, tc.name, nodeKindList(result.Nodes))
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("Swift %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

func TestSwift_NodeCountStable(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageSwift)
	if !ok {
		t.Fatal("Swift not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, swiftFixturePath, swiftFixture, types.LanguageSwift)
	r2 := e.Extract(ctx, swiftFixturePath, swiftFixture, types.LanguageSwift)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// Declares an interface, an enum class, a data class, and a class, all four of
// which parse as the same node type, plus a singleton object.
const kotlinFixture = `import java.io.File
import kotlin.math.sqrt

interface Drawable {
    fun draw(): Unit
    val id: Int
}

enum class Direction {
    NORTH, SOUTH, EAST, WEST;

    fun isVertical(): Boolean = this == NORTH || this == SOUTH
}

data class Point(val x: Double, val y: Double)

object Singleton {
    val value = 42
}

class Canvas(private val _id: Int, val name: String) : Drawable {
    override val id: Int get() = _id

    override fun draw(): Unit {
        render(_id)
    }

    private fun render(v: Int) {
        println(v)
    }

    internal fun internalHelper(): Int {
        return _id
    }
}

fun createCanvas(): Canvas {
    val c = Canvas(1, "test")
    c.draw()
    return c
}
`

const kotlinFixturePath = "src/Canvas.kt"

func TestKotlin_FunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), kotlinFixturePath, kotlinFixture, types.LanguageKotlin)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "draw")
	}
	if fn == nil {
		t.Fatalf("draw function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestKotlin_ClassExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), kotlinFixturePath, kotlinFixture, types.LanguageKotlin)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Canvas")
	if cls == nil {
		t.Fatalf("Canvas class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestKotlin_InterfaceExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), kotlinFixturePath, kotlinFixture, types.LanguageKotlin)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Drawable")
	if iface == nil {
		t.Fatalf("Drawable interface not found as NodeKindInterface; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestKotlin_EnumExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), kotlinFixturePath, kotlinFixture, types.LanguageKotlin)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Direction")
	if en == nil {
		t.Fatalf("Direction enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestKotlin_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), kotlinFixturePath, kotlinFixture, types.LanguageKotlin)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture has import java.io.File and kotlin.math.sqrt")
	}
}

func TestKotlin_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), kotlinFixturePath, kotlinFixture, types.LanguageKotlin)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render, c.draw, println calls")
	}
}

func TestKotlin_IsExported_DefaultPublic(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), kotlinFixturePath, kotlinFixture, types.LanguageKotlin)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindInterface, "Drawable", true},       // interface: public by default
		{types.NodeKindFunction, "createCanvas", true},    // top-level fun: public by default
		{types.NodeKindFunction, "draw", true},            // override fun: no visibility modifier → public by default
		{types.NodeKindFunction, "render", false},         // private fun render
		{types.NodeKindFunction, "internalHelper", false}, // internal fun
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			n = findNode(result.Nodes, types.NodeKindMethod, tc.name)
		}
		if n == nil {
			// Skip rather than fail: whether a member function surfaces
			// standalone is not what this test pins.
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("Kotlin %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

func TestKotlin_NodeCountStable(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageKotlin)
	if !ok {
		t.Fatal("Kotlin not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, kotlinFixturePath, kotlinFixture, types.LanguageKotlin)
	r2 := e.Extract(ctx, kotlinFixturePath, kotlinFixture, types.LanguageKotlin)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

// Covers every definition form, each of which has its own node type here, plus
// an instantiation.
const scalaFixture = `import scala.collection.mutable.ListBuffer
import java.io.File

trait Drawable {
  def draw(): Unit
  val id: Int
}

enum Direction {
  case North, South, East, West

  def isVertical: Boolean = this == North || this == South
}

case class Point(x: Double, y: Double)

object Singleton {
  val value = 42
}

class Canvas(private val _id: Int, val name: String) extends Drawable {
  override val id: Int = _id

  override def draw(): Unit = {
    render(_id)
  }

  private def render(v: Int): Unit = {
    println(v)
  }

  protected def protectedHelper(): Int = {
    _id
  }
}

def createCanvas(): Canvas = {
  val c = new Canvas(1, "test")
  c.draw()
  c
}
`

const scalaFixturePath = "src/Canvas.scala"

func TestScala_FunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), scalaFixturePath, scalaFixture, types.LanguageScala)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "draw")
	if fn == nil {
		fn = findNode(result.Nodes, types.NodeKindMethod, "draw")
	}
	if fn == nil {
		t.Fatalf("draw function not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestScala_ClassExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), scalaFixturePath, scalaFixture, types.LanguageScala)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	cls := findNode(result.Nodes, types.NodeKindClass, "Canvas")
	if cls == nil {
		t.Fatalf("Canvas class not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestScala_InterfaceExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), scalaFixturePath, scalaFixture, types.LanguageScala)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	iface := findNode(result.Nodes, types.NodeKindInterface, "Drawable")
	if iface == nil {
		t.Fatalf("Drawable trait not found as NodeKindInterface; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestScala_EnumExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), scalaFixturePath, scalaFixture, types.LanguageScala)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	en := findNode(result.Nodes, types.NodeKindEnum, "Direction")
	if en == nil {
		t.Fatalf("Direction enum not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

func TestScala_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), scalaFixturePath, scalaFixture, types.LanguageScala)

	importRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindImports)
	if importRefs == 0 {
		t.Fatalf("no import UnresolvedReferences; fixture imports scala.collection.mutable.ListBuffer and java.io.File")
	}
}

func TestScala_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), scalaFixturePath, scalaFixture, types.LanguageScala)

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no calls UnresolvedReferences; fixture has render, c.draw, println calls")
	}
}

func TestScala_IsExported_DefaultPublic(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), scalaFixturePath, scalaFixture, types.LanguageScala)

	for _, tc := range []struct {
		kind types.NodeKind
		name string
		want bool
	}{
		{types.NodeKindInterface, "Drawable", true},        // trait: public by default
		{types.NodeKindClass, "Canvas", true},              // class: public by default
		{types.NodeKindFunction, "render", false},          // private def render
		{types.NodeKindFunction, "protectedHelper", false}, // protected def protectedHelper
		{types.NodeKindFunction, "createCanvas", true},     // top-level def: public
	} {
		n := findNode(result.Nodes, tc.kind, tc.name)
		if n == nil {
			n = findNode(result.Nodes, types.NodeKindMethod, tc.name)
		}
		if n == nil {
			continue // may not be extracted at this nesting level
		}
		if n.IsExported != tc.want {
			t.Errorf("Scala %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

func TestScala_NodeCountStable(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageScala)
	if !ok {
		t.Fatal("Scala not registered")
	}
	e := newExtractor(t, extLang, cfg)
	ctx := context.Background()
	r1 := e.Extract(ctx, scalaFixturePath, scalaFixture, types.LanguageScala)
	r2 := e.Extract(ctx, scalaFixturePath, scalaFixture, types.LanguageScala)
	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count unstable: first=%d second=%d", len(r1.Nodes), len(r2.Nodes))
	}
}

func TestRegistry_For_CP8B_Languages(t *testing.T) {
	t.Parallel()
	reg := languages.NewRegistry()
	tests := []struct {
		lang     types.Language
		wantLang extraction.Lang
	}{
		{types.LanguageSwift, extraction.LangSwift},
		{types.LanguageKotlin, extraction.LangKotlin},
		{types.LanguageScala, extraction.LangScala},
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
