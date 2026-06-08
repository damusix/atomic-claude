package languages

// Swift language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8b/ — Swift grammar):
//
//	Top-level (direct children of source_file):
//	  import_declaration     — "import Foundation"
//	  protocol_declaration   — "public protocol Drawable { ... }"
//	  class_declaration      — covers enum / struct / class (disambiguated by ResolveKind)
//	  function_declaration   — "func createCanvas() -> Canvas { ... }"
//
//	Named-iterator also sees (inside bodies):
//	  init_declaration       — "public init(id: Int, name: String) { ... }"
//	  function_declaration   — "public func draw() -> Void { ... }"
//	  property_declaration   — "public var x: Double"
//	  call_expression        — "render(self._id)"
//
// Type-kind disambiguation (ResolveKind on class_declaration):
//   - Has "enum_class_body" child → NodeKindEnum
//   - Otherwise (struct or class, both have "class_body") → NodeKindClass
//   - struct_declaration is NOT a separate node type in this grammar;
//     "public struct Point" parses as class_declaration with class_body.
//
// protocol_declaration is placed directly in InterfaceTypes — no ResolveKind needed.
//
// IsExported rule: Swift default is internal (module-private). Only public/open cross
// module boundaries. The modifiers container (kind="modifiers") text contains "public"
// or "open". Accessed via named-child scan (ChildByFieldName returns empty — probe confirmed).
//
// Name field: "name" works on protocol_declaration; for class_declaration the first
// type_identifier named child is used as a fallback (probe confirmed).

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// SwiftExtractor returns the LanguageExtractor config for Swift source files (.swift).
//
// Node-type strings are verified by parsing real Swift via the wazero binding
// (see tmp/probe-cp8b/main.go and tmp/probe-cp8b/probe2.go).
func SwiftExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// protocol_declaration is the semantic interface equivalent in Swift.
		// It has its own distinct node type, so no ResolveKind is needed.
		InterfaceTypes: extraction.TypeSet("protocol_declaration"),

		// class_declaration covers enum, struct, and class — disambiguated by ResolveKind.
		// Placed in StructTypes so the engine calls ResolveKind before deciding.
		StructTypes: extraction.TypeSet("class_declaration"),

		// function_declaration covers top-level and member functions.
		// init_declaration covers initializers (constructors).
		FunctionTypes: extraction.TypeSet("function_declaration", "init_declaration"),

		// property_declaration covers stored and computed properties.
		PropertyTypes: extraction.TypeSet("property_declaration"),

		// import_declaration: "import Foundation", "import UIKit".
		ImportTypes: extraction.TypeSet("import_declaration"),

		// call_expression: "render(self._id)", "c.draw()", "print(v)".
		CallTypes: extraction.TypeSet("call_expression"),

		// Field names in the Swift grammar (verified by probe_body.go).
		// "name" resolves correctly for protocol_declaration and class_declaration.
		// "body" resolves to function_body on function_declaration nodes (probe confirmed).
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "function_value_parameters",

		// ResolveKind disambiguates class_declaration into enum/class.
		// Swift structs and classes both have class_body — we return NodeKindClass
		// for both since the grammar gives us no structural way to separate them
		// without text scan. Enums have enum_class_body.
		ResolveKind: swiftResolveKind,

		// IsExported: only public/open are module-visible in Swift.
		// Default access level is internal (within-module only).
		IsExported: swiftIsExported,

		// ExtractImport strips the "import " prefix from import_declaration text.
		ExtractImport: swiftExtractImport,
	}
}

// swiftResolveKind disambiguates class_declaration into the correct semantic kind.
//
//	Has "enum_class_body" child → NodeKindEnum   (e.g. "public enum Direction")
//	Otherwise                   → NodeKindClass  (covers both struct and class)
//
// Swift's grammar uses "class_declaration" for enum, struct, and class.
// The distinguishing factor for enums is the "enum_class_body" child node.
// Structs and classes both use "class_body"; we map both to NodeKindClass
// since the resolution layer treats them equivalently for graph purposes.
func swiftResolveKind(ctx context.Context, node sitter.Node, _ string) types.NodeKind {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return types.NodeKindClass
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
		if kind == "enum_class_body" {
			return types.NodeKindEnum
		}
	}
	return types.NodeKindClass
}

// swiftIsExported reports whether a Swift node is exported (public or open).
//
// Swift's default access level is internal (module-private). Only "public" and
// "open" declarations are visible outside the module. The modifiers appear as a
// named child with kind="modifiers" whose text contains the access keyword.
//
// Note: ChildByFieldName("modifiers") returns null in this grammar (probe confirmed);
// we scan named children directly.
func swiftIsExported(ctx context.Context, node sitter.Node, source string) bool {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return false
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
			if strings.Contains(text, "public") || strings.Contains(text, "open") {
				return true
			}
		}
	}
	return false
}

// swiftExtractImport extracts the import path from an import_declaration node.
//
//	import Foundation  → name="Foundation", path="Foundation"
//	import UIKit       → name="UIKit",      path="UIKit"
func swiftExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
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
	// Strip "import " and any optional kind modifier ("import class Foo", "import typealias Bar")
	// Standard: "import ModuleName" or "import ModuleName.SubModule".
	text = strings.TrimPrefix(text, "import ")
	text = strings.TrimSpace(text)
	// Remove optional submodule qualifier from import kind prefix (e.g. "class ", "struct ")
	for _, kw := range []string{"class ", "enum ", "struct ", "protocol ", "typealias ", "func ", "var ", "let "} {
		text = strings.TrimPrefix(text, kw)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	path = text
	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}
