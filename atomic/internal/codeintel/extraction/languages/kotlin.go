package languages

// Node-type strings are read from the live Kotlin grammar — do not guess. Two
// of them trip people up: imports are import_header, not import_declaration, and
// class_declaration covers interface, enum class, data class, and class alike.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// KotlinExtractor returns the LanguageExtractor config for Kotlin source files (.kt).
func KotlinExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// StructTypes is the arm that runs ResolveKind, which this node type needs.
		StructTypes: extraction.TypeSet("class_declaration"),

		// Kotlin singletons, always a class.
		ClassTypes: extraction.TypeSet("object_declaration"),

		FunctionTypes: extraction.TypeSet("function_declaration"),

		PropertyTypes: extraction.TypeSet("property_declaration"),

		ImportTypes: extraction.TypeSet("import_header"),

		CallTypes: extraction.TypeSet("call_expression"),

		// "name" resolves on function_declaration and object_declaration but not
		// on class_declaration, where the binding falls back to the first
		// type_identifier child. No field maps to a function's body either, and
		// the empty BodyField is what selects the recursive named-child scan that
		// still finds the call sites inside it.
		NameField:   "name",
		BodyField:   "",
		ParamsField: "function_value_parameters",

		ResolveKind: kotlinResolveKind,

		IsExported: kotlinIsExported,

		ExtractImport: kotlinExtractImport,
	}
}

// kotlinResolveKind splits class_declaration into enum, interface, or class.
// Enums carry an enum_class_body child; interfaces have no node type of their
// own, so they are found by the keyword in the source text.
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

	// The keyword sits directly before the type name, possibly after modifiers
	// ("public interface Foo"). The trailing space guards against matching it
	// inside a longer identifier.
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) <= len(source) {
		text := source[sb:eb]
		if strings.Contains(text, "interface ") || strings.HasPrefix(strings.TrimSpace(text), "interface") {
			return types.NodeKindInterface
		}
	}

	return types.NodeKindClass
}

// kotlinIsExported treats private and internal as not-exported. Kotlin's
// default, and so this function's, is public.
func kotlinIsExported(ctx context.Context, node sitter.Node, source string) bool {
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
			if strings.Contains(text, "private") || strings.Contains(text, "internal") {
				return false
			}
		}
		return true
	}
	return true
}

// kotlinExtractImport returns the import path and its last segment as the name.
// A wildcard import yields the package it expands: "com.example.*" → "com.example".
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

	if strings.HasSuffix(text, ".*") {
		path = strings.TrimSuffix(text, ".*")
	} else {
		path = text
	}

	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}
