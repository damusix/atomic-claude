package languages

// Scala language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8b/ — Scala grammar):
//
//	Top-level (direct children of compilation_unit):
//	  import_declaration   — "import scala.collection.mutable.ListBuffer"
//	  trait_definition     — "trait Drawable { ... }"         → NodeKindInterface
//	  enum_definition      — "enum Direction { ... }"         → NodeKindEnum (Scala 3)
//	  class_definition     — "case class Point(x: Double, y: Double)"
//	  object_definition    — "object Singleton { ... }"       → NodeKindClass
//	  function_definition  — "def createCanvas(): Canvas = { ... }"
//
//	Named-iterator also sees (inside bodies):
//	  function_definition  — "override def draw(): Unit = { ... }"
//	  function_declaration — (abstract method stubs in traits)
//	  call_expression      — "render(_id)"
//	  instance_expression  — "new Canvas(1, \"test\")"
//
// Node-type disambiguation:
//   - trait_definition   → InterfaceTypes  → NodeKindInterface
//   - enum_definition    → EnumTypes       → NodeKindEnum
//   - class_definition   → ClassTypes      → NodeKindClass
//   - object_definition  → ClassTypes      → NodeKindClass  (singletons)
//   No ResolveKind needed — all types are distinct node kinds.
//
// IsExported rule: Scala default is public. private/protected modifiers in
// a "modifiers" named child → not exported.
//
// Name field: "name" works on all definition node types (probe confirmed).
//
// Import path: Scala import_declaration contains identifier child nodes
// separated by ".". We collect them and join to form the path.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// ScalaExtractor returns the LanguageExtractor config for Scala source files (.scala).
//
// Node-type strings are verified by parsing real Scala via the wazero binding
// (see tmp/probe-cp8b/main.go, probe2.go, and probe3.go).
func ScalaExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// trait_definition is the semantic interface equivalent in Scala.
		InterfaceTypes: extraction.TypeSet("trait_definition"),

		// enum_definition covers Scala 3 enums (NOT enum_specifier — that is a C/C++ node).
		EnumTypes: extraction.TypeSet("enum_definition"),

		// class_definition covers regular classes, case classes, and abstract classes.
		// object_definition covers singleton objects (Scala's replacement for static members).
		ClassTypes: extraction.TypeSet("class_definition", "object_definition"),

		// function_definition covers concrete method and function definitions.
		// function_declaration covers abstract method signatures (in traits / abstract classes).
		FunctionTypes: extraction.TypeSet("function_definition", "function_declaration"),

		// import_declaration: "import scala.collection.mutable.ListBuffer".
		ImportTypes: extraction.TypeSet("import_declaration"),

		// call_expression: "render(_id)", "c.draw()", "println(v)".
		CallTypes: extraction.TypeSet("call_expression"),

		// instance_expression: "new Canvas(1, \"test\")" → instantiation.
		InstantiationTypes: extraction.TypeSet("instance_expression"),

		// Field names in the Scala grammar (verified by probe_body.go).
		// "body" resolves to the block node on function_definition nodes (probe confirmed).
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "class_parameters",

		// IsExported: Scala default is public. private/protected → not exported.
		IsExported: scalaIsExported,

		// ExtractImport collects identifier children and joins with ".".
		ExtractImport: scalaExtractImport,
	}
}

// scalaIsExported reports whether a Scala node is exported (public by default).
//
// Scala's default visibility is public. Only "private" and "protected" restrict
// visibility:
//   - "private"             — only accessible within the enclosing class/object.
//   - "private[pkg]"        — accessible within package but not outside.
//   - "protected"           — accessible to subclasses.
//
// All of these reduce cross-module resolution scope; we treat them as not-exported.
func scalaIsExported(ctx context.Context, node sitter.Node, source string) bool {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return true
	}
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		kind, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if kind != "modifiers" {
			continue
		}
		sb, _ := ch.StartByte(ctx)
		eb, _ := ch.EndByte(ctx)
		if int(eb) <= len(source) {
			text := source[sb:eb]
			if strings.Contains(text, "private") || strings.Contains(text, "protected") {
				return false
			}
		}
		// Found modifiers but none restrict export → public.
		return true
	}
	// No modifiers child → Scala default is public.
	return true
}

// scalaExtractImport extracts the import path from an import_declaration node.
//
// Scala import declarations may be:
//
//	import scala.collection.mutable.ListBuffer  → name="ListBuffer", path="scala.collection.mutable.ListBuffer"
//	import java.io.{File, InputStream}          → name="io",         path="java.io"
//	import scala._                              → name="scala",      path="scala"
//
// We use source text extraction: strip "import " prefix, then handle braced
// and wildcard forms.
func scalaExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "import_declaration" {
		return "", ""
	}
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])
	text = strings.TrimPrefix(text, "import ")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}

	// Handle braced selectors: "scala.collection.{List, Set}" → "scala.collection".
	if idx := strings.Index(text, ".{"); idx >= 0 {
		text = text[:idx]
	}

	// Handle wildcard: "scala._" → "scala".
	text = strings.TrimSuffix(text, "._")
	text = strings.TrimSuffix(text, ".*")

	path = text
	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}
