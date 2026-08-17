package languages

// Node-type strings are read from the live C# grammar — do not guess. Calls are
// invocation_expression, not call_expression, and getting that wrong yields zero
// call references rather than an error. C# also repeats a singular "modifier"
// node per keyword where Java uses one plural "modifiers" container.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// CSharpExtractor returns the LanguageExtractor config for C# source files (.cs).
func CSharpExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Constructors are constructor_declaration and go unextracted.
		MethodTypes: extraction.TypeSet("method_declaration"),

		ClassTypes:     extraction.TypeSet("class_declaration"),
		InterfaceTypes: extraction.TypeSet("interface_declaration"),
		EnumTypes:      extraction.TypeSet("enum_declaration"),

		PropertyTypes: extraction.TypeSet("property_declaration"),

		FieldTypes: extraction.TypeSet("field_declaration"),

		ImportTypes: extraction.TypeSet("using_directive"),

		CallTypes:          extraction.TypeSet("invocation_expression"),
		InstantiationTypes: extraction.TypeSet("object_creation_expression"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameter_list",

		IsExported: csharpIsExported,

		ExtractImport: csharpExtractImport,
	}
}

// csharpIsExported looks for a "public" modifier, then falls back to treating a
// bodiless, modifier-less method as an implicitly public interface method. That
// fallback is deliberately scoped to methods: an unmarked field defaults to
// private, and calling it exported would hand private symbols the exported bonus
// in resolution scoring.
func csharpIsExported(ctx context.Context, node sitter.Node, source string) bool {
	nodeKind, err := node.Kind(ctx)
	if err != nil {
		return false
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return false
	}
	hasModifier := false
	hasBlock := false
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		kind, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		switch kind {
		case "modifier":
			hasModifier = true
			sb, _ := ch.StartByte(ctx)
			eb, _ := ch.EndByte(ctx)
			if int(eb) <= len(source) {
				text := strings.TrimSpace(source[sb:eb])
				if text == "public" {
					return true
				}
			}
		case "block":
			hasBlock = true
		}
	}
	if nodeKind == "method_declaration" && !hasModifier && !hasBlock {
		return true
	}
	return false
}

// csharpExtractImport returns the namespace path and its last segment as the name.
func csharpExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "using_directive" {
		return "", ""
	}

	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])
	text = strings.TrimPrefix(text, "using ")
	text = strings.TrimPrefix(text, "static ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)

	if text == "" {
		return "", ""
	}

	path = text
	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}
