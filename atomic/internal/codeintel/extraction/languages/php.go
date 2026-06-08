package languages

// PHP language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8c/ — PHP grammar):
//
//	Top-level and named-iterator:
//	  function_definition      — function createCanvas(...): Canvas { ... }
//	  method_declaration       — public function draw(): void { ... }
//	  class_declaration        — class Canvas extends Shape { ... }
//	  interface_declaration    — interface Paintable { ... }
//	  trait_declaration        — trait HasColor { ... }
//	  enum_declaration         — enum Direction { ... }
//	  property_declaration     — private string $color = 'red';
//	  namespace_use_declaration — use App\Contracts\Drawable;
//	  function_call_expression — strlen("hello") / array_map(...)
//	  member_call_expression   — $this->renderer->render($this->id)
//
// Name field: ChildByFieldName("name") works on:
//   - function_definition      → name kind text = "createCanvas"
//   - method_declaration       → name kind text = "draw" / "helper"
//   - class_declaration        → name kind text = "Canvas" / "Shape"
//   - interface_declaration    → name kind text = "Paintable"
//   - trait_declaration        → name kind text = "HasColor"
//   - enum_declaration         → name kind text = "Direction"
//
// All verified by probe3.go.
//
// IsExported rule: PHP uses a visibility_modifier child ("public" / "protected"
// / "private") on method_declaration and property_declaration.
//   - "public" → exported
//   - "protected" / "private" → not exported
//   - No visibility_modifier present → exported (PHP class members without an
//     explicit modifier are public; top-level functions have no modifier and are
//     always public).
//
// trait_declaration → InterfaceTypes: PHP traits are compile-time mixins that
// behave like interfaces in the type graph (provide method contracts). We map
// them to NodeKindInterface for consistent cross-language treatment.
//
// Import extraction: namespace_use_declaration text has the form
// "use App\Contracts\Drawable" or "use App\Services\Renderer". We strip the
// leading "use " and interpret the path as a backslash-separated module path.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// PHPExtractor returns the LanguageExtractor config for PHP source files (.php).
//
// Node-type strings are verified by parsing real PHP via the wazero binding
// (see tmp/probe-cp8c/main.go and tmp/probe-cp8c/probe3.go for exact output).
func PHPExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_definition covers top-level (non-method) PHP functions.
		FunctionTypes: extraction.TypeSet("function_definition"),

		// method_declaration covers instance and static class methods.
		MethodTypes: extraction.TypeSet("method_declaration"),

		// class_declaration covers regular, abstract, and final PHP classes.
		ClassTypes: extraction.TypeSet("class_declaration"),

		// interface_declaration covers PHP interfaces.
		// trait_declaration is also placed here — traits are mixin contracts
		// that behave like interfaces in the type graph.
		InterfaceTypes: extraction.TypeSet("interface_declaration", "trait_declaration"),

		// enum_declaration covers PHP 8.1+ backed and unit enums.
		EnumTypes: extraction.TypeSet("enum_declaration"),

		// property_declaration covers typed and untyped class properties.
		PropertyTypes: extraction.TypeSet("property_declaration"),

		// namespace_use_declaration covers "use App\X\Y;" statements.
		ImportTypes: extraction.TypeSet("namespace_use_declaration"),

		// function_call_expression: standalone function calls (strlen, array_map).
		// member_call_expression: method calls ($obj->draw()).
		CallTypes: extraction.TypeSet("function_call_expression", "member_call_expression"),

		// Field names in the PHP grammar (verified by probe3).
		NameField: "name",

		// IsExported: PHP uses a visibility_modifier child ("public"/"private"/"protected").
		// public → exported; private/protected → not exported; absent → exported.
		IsExported: phpIsExported,

		// ExtractImport handles namespace_use_declaration nodes.
		ExtractImport: phpExtractImport,
	}
}

// phpIsExported reports whether a PHP node is publicly accessible.
// Rules:
//  1. If a named child has kind "visibility_modifier" and its text is "private"
//     or "protected" → not exported.
//  2. If a named child has kind "visibility_modifier" and its text is "public"
//     → exported.
//  3. If no visibility_modifier child is present → exported (default in PHP).
func phpIsExported(ctx context.Context, node sitter.Node, source string) bool {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return true // default exported on error
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
		if kind == "visibility_modifier" {
			sb, _ := ch.StartByte(ctx)
			eb, _ := ch.EndByte(ctx)
			if int(eb) <= len(source) {
				text := strings.TrimSpace(source[sb:eb])
				if text == "private" || text == "protected" {
					return false
				}
			}
			return true // "public" or unknown modifier → exported
		}
	}
	return true // no modifier → exported
}

// phpExtractImport extracts the import path from a namespace_use_declaration node.
//
//	use App\Contracts\Drawable;    → name="Drawable", path="App\Contracts\Drawable"
//	use App\Services\Renderer;     → name="Renderer",  path="App\Services\Renderer"
func phpExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])
	// Strip "use " prefix and trailing semicolon.
	text = strings.TrimPrefix(text, "use ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	// Split on backslash to get path segments.
	parts := strings.Split(text, "\\")
	return parts[len(parts)-1], text
}
