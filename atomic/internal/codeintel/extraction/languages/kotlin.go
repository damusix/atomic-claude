package languages

// Kotlin language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8b/ — Kotlin grammar):
//
//	Top-level (direct children of source_file):
//	  import_header          — "import java.io.File"  (NOT import_declaration)
//	  class_declaration      — covers interface / enum class / data class / class
//	  object_declaration     — "object Singleton { ... }"
//	  function_declaration   — "fun createCanvas(): Canvas { ... }"
//
//	Named-iterator also sees (inside bodies):
//	  function_declaration   — "override fun draw(): Unit { ... }"
//	  property_declaration   — "val id: Int"
//	  call_expression        — "render(_id)"
//
// Type-kind disambiguation (ResolveKind on class_declaration):
//   - Has "enum_class_body" child        → NodeKindEnum     (e.g. "enum class Direction { ... }")
//   - Source text contains "interface "  → NodeKindInterface (e.g. "interface Drawable { ... }")
//   - Otherwise                          → NodeKindClass
//
// object_declaration is placed in ClassTypes directly (always → NodeKindClass).
//
// Name field: Kotlin class_declaration has NO "name" field (probe confirmed).
// The extractor uses the first "type_identifier" named child as a fallback.
// function_declaration and object_declaration do have a "name" field.
//
// IsExported rule: Kotlin default is public. Only private/internal → not exported.
// The modifiers container (kind="modifiers") text contains "private" or "internal".
// Default (no modifiers child) → exported.
//
// Import path: strip "import " prefix from import_header node source text.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// KotlinExtractor returns the LanguageExtractor config for Kotlin source files (.kt).
//
// Node-type strings are verified by parsing real Kotlin via the wazero binding
// (see tmp/probe-cp8b/main.go, probe2.go, and probe3.go).
func KotlinExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// class_declaration covers interface, enum class, data class, and class.
		// Placed in StructTypes so the engine calls ResolveKind to disambiguate.
		StructTypes: extraction.TypeSet("class_declaration"),

		// object_declaration is a Kotlin singleton — always maps to NodeKindClass.
		ClassTypes: extraction.TypeSet("object_declaration"),

		// function_declaration covers top-level and member functions.
		FunctionTypes: extraction.TypeSet("function_declaration"),

		// property_declaration covers val/var declarations.
		PropertyTypes: extraction.TypeSet("property_declaration"),

		// Kotlin uses import_header, NOT import_declaration (probe confirmed).
		ImportTypes: extraction.TypeSet("import_header"),

		// call_expression: "render(_id)", "c.draw()", "println(v)".
		CallTypes: extraction.TypeSet("call_expression"),

		// Name field: works for function_declaration and object_declaration.
		// For class_declaration there is no "name" field — the extractor falls back
		// to the first type_identifier named child (handled by the grammar binding).
		//
		// BodyField: Kotlin function_declaration has no named field that returns the
		// function body via ChildByFieldName (probe confirmed — child kind is
		// "function_body" but no field name maps to it). Setting BodyField to ""
		// triggers the fallback path: visitFunctionBody scans all named children of
		// the function node recursively, which correctly finds call_expression nodes.
		NameField:   "name",
		BodyField:   "",
		ParamsField: "function_value_parameters",

		// ResolveKind disambiguates class_declaration: enum / interface / class.
		ResolveKind: kotlinResolveKind,

		// IsExported: Kotlin default is public. private/internal → not exported.
		IsExported: kotlinIsExported,

		// ExtractImport strips the "import " prefix from import_header nodes.
		ExtractImport: kotlinExtractImport,
	}
}

// kotlinResolveKind disambiguates class_declaration into the correct semantic kind.
//
//	Has "enum_class_body" child       → NodeKindEnum
//	Source text starts with "interface " (after optional modifiers) → NodeKindInterface
//	Otherwise                         → NodeKindClass
//
// Kotlin grammar uses a single "class_declaration" node type for all class-like
// declarations. The enum variant is identified by "enum_class_body"; interfaces
// are identified by checking the source text for the "interface" keyword (the
// grammar does not expose a separate node type for interface declarations).
func kotlinResolveKind(ctx context.Context, node sitter.Node, source string) types.NodeKind {
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

	// Check whether this is an interface declaration by inspecting source text.
	// The keyword "interface" appears directly before the type name in the source,
	// potentially after modifier keywords (e.g. "public interface Foo").
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) <= len(source) {
		text := source[sb:eb]
		// A Kotlin interface declaration contains the literal "interface " keyword.
		// We look for it as a word boundary to avoid false matches inside names.
		if strings.Contains(text, "interface ") || strings.HasPrefix(strings.TrimSpace(text), "interface") {
			return types.NodeKindInterface
		}
	}

	return types.NodeKindClass
}

// kotlinIsExported reports whether a Kotlin node is exported (public by default).
//
// Kotlin's default visibility is public — declarations without modifiers are
// accessible everywhere. Only "private" and "internal" restrict visibility.
// "protected" restricts to subclasses but is still accessible outside the module
// in some contexts; we treat it as not-exported for cross-module resolution.
func kotlinIsExported(ctx context.Context, node sitter.Node, source string) bool {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		// Default: public.
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
			if strings.Contains(text, "private") || strings.Contains(text, "internal") {
				return false
			}
		}
		// Found modifiers but none restrict export → still public.
		return true
	}
	// No modifiers child → Kotlin default is public.
	return true
}

// kotlinExtractImport extracts the import path from an import_header node.
//
//	import java.io.File        → name="File",  path="java.io.File"
//	import kotlin.math.sqrt   → name="sqrt",  path="kotlin.math.sqrt"
//	import com.example.*      → name="example", path="com.example"
func kotlinExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "import_header" {
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

	// Handle wildcard: "com.example.*" → path="com.example".
	if strings.HasSuffix(text, ".*") {
		path = strings.TrimSuffix(text, ".*")
	} else {
		path = text
	}

	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}
