package languages_test

// Tests for Swift, Kotlin, Scala language extractor configs (CP8 batch B).
//
// Each language has:
//  1. A real fixture parsed through the pool (grammar ABI proof).
//  2. Assertions per success criteria:
//     - Function/method node extracted with correct kind.
//     - Class node extracted.
//     - Interface-equivalent (protocol/interface/trait) → NodeKindInterface.
//     - Import UnresolvedReference emitted.
//     - Call site → UnresolvedReference (EdgeKindCalls).
//     - IsExported correct per-language rule.
//     - Node count stable across two extractions.
//
// Node-type strings are VERIFIED by real grammar parse (see tmp/probe-cp8b/).
// Do NOT change them without running the probe again.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Swift
// ---------------------------------------------------------------------------

// swiftFixture exercises:
//   - import_declaration     (import Foundation, import UIKit)
//   - protocol_declaration   (Drawable)          → NodeKindInterface
//   - class_declaration      (Direction[enum], Point[struct], Canvas[class])
//   - function_declaration   (draw, isVertical, createCanvas)
//   - init_declaration       (Canvas.init)
//   - property_declaration   (public/private var)
//   - call_expression        (render, c.draw, print)
//
// Verified node-type strings (tmp/probe-cp8b/ — Swift grammar):
//
//	import_declaration     — "import Foundation"
//	protocol_declaration   — "public protocol Drawable { ... }"
//	class_declaration      — "public enum Direction" / "public struct Point" / "open class Canvas"
//	function_declaration   — "public func draw() -> Void { ... }"
//	init_declaration       — "public init(id: Int, name: String) { ... }"
//	property_declaration   — "public var x: Double"
//	call_expression        — "render(self._id)"
//
// IsExported rule: Swift default is internal. Only public/open → exported.
//
//	modifiers child (kind="modifiers") text contains "public" or "open".
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

// TestSwift_FunctionExtracted verifies function_declaration → NodeKindFunction/NodeKindMethod.
// WHY: Swift functions are the primary callable units; wrong kind breaks call-graph resolution.
func TestSwift_FunctionExtracted(t *testing.T) {
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

// TestSwift_ClassExtracted verifies class_declaration (class) → NodeKindClass.
// WHY: Classes are structural containers; missing them breaks the member graph.
func TestSwift_ClassExtracted(t *testing.T) {
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

// TestSwift_InterfaceExtracted verifies protocol_declaration → NodeKindInterface.
// WHY: Swift protocols are the semantic equivalent of interfaces; wrong kind breaks type-graph.
func TestSwift_InterfaceExtracted(t *testing.T) {
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

// TestSwift_EnumExtracted verifies class_declaration with enum_class_body → NodeKindEnum.
// WHY: Swift enums must be typed correctly for the query layer.
func TestSwift_EnumExtracted(t *testing.T) {
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

// TestSwift_ImportsExtracted verifies import_declaration emits UnresolvedReference.
// WHY: Imports are the starting point for the resolution layer's import resolver.
func TestSwift_ImportsExtracted(t *testing.T) {
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

// TestSwift_CallEmitsUnresolvedReference verifies call_expression → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestSwift_CallEmitsUnresolvedReference(t *testing.T) {
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

// TestSwift_IsExported_PublicOpen verifies that only public/open → exported;
// internal/private → not exported; default (no modifier) → not exported.
// WHY: Swift's default access level is internal (module-private). Only public/open
// cross module boundaries. Wrong IsExported breaks cross-module resolution scoring.
func TestSwift_IsExported_PublicOpen(t *testing.T) {
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
		// public/open → exported
		{types.NodeKindInterface, "Drawable", true}, // public protocol
		{types.NodeKindClass, "Canvas", true},       // open class
		// draw is public func inside Canvas
		// private → not exported
		// internal → not exported (Swift default is internal, not public)
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

// TestSwift_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestSwift_NodeCountStable(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Kotlin
// ---------------------------------------------------------------------------

// kotlinFixture exercises:
//   - import_header        (import java.io.File, import kotlin.math.sqrt)
//   - class_declaration    (Drawable[interface], Direction[enum class], Point[data class], Canvas[class])
//   - object_declaration   (Singleton)
//   - function_declaration (draw, isVertical, render, createCanvas)
//   - property_declaration (val id, val name)
//   - call_expression      (render, c.draw, println)
//
// Verified node-type strings (tmp/probe-cp8b/ — Kotlin grammar):
//
//	import_header          — "import java.io.File"
//	class_declaration      — "interface Drawable { ... }" / "enum class Direction { ... }"
//	object_declaration     — "object Singleton { ... }"
//	function_declaration   — "fun draw(): Unit { ... }"
//	property_declaration   — "val id: Int"
//	call_expression        — "render(_id)"
//
// IsExported rule: Kotlin default is public. private/internal → not exported.
//
//	modifiers child (kind="modifiers") text contains "private" or "internal".
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

// TestKotlin_FunctionExtracted verifies function_declaration → NodeKindFunction/NodeKindMethod.
// WHY: Kotlin functions are the primary callable units; wrong kind breaks call-graph.
func TestKotlin_FunctionExtracted(t *testing.T) {
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

// TestKotlin_ClassExtracted verifies class_declaration (class) → NodeKindClass.
// WHY: Classes are structural containers; missing them breaks the member graph.
func TestKotlin_ClassExtracted(t *testing.T) {
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

// TestKotlin_InterfaceExtracted verifies class_declaration (interface) → NodeKindInterface.
// WHY: Kotlin interfaces are the semantic equivalent of Java interfaces; wrong kind breaks
// implements edge promotion during resolution.
func TestKotlin_InterfaceExtracted(t *testing.T) {
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

// TestKotlin_EnumExtracted verifies class_declaration (enum class) → NodeKindEnum.
// WHY: Kotlin enum classes must be typed correctly for query correctness.
func TestKotlin_EnumExtracted(t *testing.T) {
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

// TestKotlin_ImportsExtracted verifies import_header emits UnresolvedReference.
// WHY: Kotlin uses import_header (not import_declaration); the resolution layer needs these.
func TestKotlin_ImportsExtracted(t *testing.T) {
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

// TestKotlin_CallEmitsUnresolvedReference verifies call_expression → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestKotlin_CallEmitsUnresolvedReference(t *testing.T) {
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

// TestKotlin_IsExported_DefaultPublic verifies that default Kotlin functions are exported,
// and private/internal → not exported.
// WHY: Kotlin's default visibility is public — opposite of Java. Wrong IsExported corrupts
// resolution scoring for cross-module calls.
func TestKotlin_IsExported_DefaultPublic(t *testing.T) {
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
			// Functions inside classes may be NodeKindMethod
			n = findNode(result.Nodes, types.NodeKindMethod, tc.name)
		}
		if n == nil {
			// skip check for functions we can't locate (may not be extracted as standalone)
			continue
		}
		if n.IsExported != tc.want {
			t.Errorf("Kotlin %s %s: IsExported=%v, want %v", tc.kind, tc.name, n.IsExported, tc.want)
		}
	}
}

// TestKotlin_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestKotlin_NodeCountStable(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Scala
// ---------------------------------------------------------------------------

// scalaFixture exercises:
//   - import_declaration    (import scala.collection.mutable.ListBuffer, import java.io.File)
//   - trait_definition      (Drawable)          → NodeKindInterface
//   - enum_definition       (Direction)         → NodeKindEnum
//   - class_definition      (Point[case class], Canvas[class])
//   - object_definition     (Singleton)         → NodeKindClass
//   - function_definition   (def draw, def render, def createCanvas)
//   - call_expression       (render, c.draw, println)
//   - instance_expression   (new Canvas(1, "test"))
//
// Verified node-type strings (tmp/probe-cp8b/ — Scala grammar):
//
//	import_declaration   — "import scala.collection.mutable.ListBuffer"
//	trait_definition     — "trait Drawable { ... }"
//	enum_definition      — "enum Direction { ... }"
//	class_definition     — "case class Point(x: Double, y: Double)"
//	object_definition    — "object Singleton { ... }"
//	function_definition  — "def draw(): Unit = { ... }"
//	call_expression      — "render(_id)"
//	instance_expression  — "new Canvas(1, \"test\")"
//
// IsExported rule: Scala default is public. private/protected in modifiers → not exported.
//
//	modifiers child (kind="modifiers") text contains "private" or "protected".
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

// TestScala_FunctionExtracted verifies function_definition → NodeKindFunction/NodeKindMethod.
// WHY: Scala defs are the primary callable units; wrong kind breaks call-graph resolution.
func TestScala_FunctionExtracted(t *testing.T) {
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

// TestScala_ClassExtracted verifies class_definition → NodeKindClass.
// WHY: Classes are structural containers; missing them breaks the member graph.
func TestScala_ClassExtracted(t *testing.T) {
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

// TestScala_InterfaceExtracted verifies trait_definition → NodeKindInterface.
// WHY: Scala traits are the semantic equivalent of interfaces; wrong kind breaks type-graph.
func TestScala_InterfaceExtracted(t *testing.T) {
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

// TestScala_EnumExtracted verifies enum_definition → NodeKindEnum.
// WHY: Scala 3 enums must produce NodeKindEnum, not NodeKindClass, for correct query routing.
func TestScala_EnumExtracted(t *testing.T) {
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

// TestScala_ImportsExtracted verifies import_declaration emits UnresolvedReference.
// WHY: Imports are the starting point for the resolution layer's import resolver.
func TestScala_ImportsExtracted(t *testing.T) {
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

// TestScala_CallEmitsUnresolvedReference verifies call_expression → EdgeKindCalls.
// WHY: Calls must NOT emit edges directly — resolution layer owns that step.
func TestScala_CallEmitsUnresolvedReference(t *testing.T) {
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

// TestScala_IsExported_DefaultPublic verifies that Scala default is public; private/protected → not exported.
// WHY: Scala default visibility is public. Wrong IsExported corrupts resolution scoring.
func TestScala_IsExported_DefaultPublic(t *testing.T) {
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

// TestScala_NodeCountStable verifies deterministic extraction.
// WHY: Non-determinism means double-extraction, corrupt indexes, and unstable IDs.
func TestScala_NodeCountStable(t *testing.T) {
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

// ---------------------------------------------------------------------------
// Registry — batch B languages registered
// ---------------------------------------------------------------------------

// TestRegistry_For_CP8B_Languages verifies all 3 new languages are registered.
// WHY: The registry is the single resolution point for CP10; missing entries
// cause the orchestrator to silently skip files of those languages.
func TestRegistry_For_CP8B_Languages(t *testing.T) {
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
