package languages

// Node-type strings are read from the live PHP grammar — do not guess.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// PHPExtractor returns the LanguageExtractor config for PHP source files (.php).
func PHPExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("function_definition"),

		MethodTypes: extraction.TypeSet("method_declaration"),

		ClassTypes: extraction.TypeSet("class_declaration"),

		// Traits join interfaces: they are mixin contracts, and the type graph
		// treats them the same way.
		InterfaceTypes: extraction.TypeSet("interface_declaration", "trait_declaration"),

		EnumTypes: extraction.TypeSet("enum_declaration"),

		PropertyTypes: extraction.TypeSet("property_declaration"),

		ImportTypes: extraction.TypeSet("namespace_use_declaration"),

		CallTypes: extraction.TypeSet("function_call_expression", "member_call_expression"),

		NameField: "name",

		IsExported: phpIsExported,

		ExtractImport: phpExtractImport,
	}
}

// phpIsExported reads the visibility_modifier child. An absent modifier means
// exported: unmarked class members are public, and top-level functions carry no
// modifier at all.
func phpIsExported(ctx context.Context, node sitter.Node, source string) bool {
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
		if kind == "visibility_modifier" {
			sb, _ := ch.StartByte(ctx)
			eb, _ := ch.EndByte(ctx)
			if int(eb) <= len(source) {
				text := strings.TrimSpace(source[sb:eb])
				if text == "private" || text == "protected" {
					return false
				}
			}
			// "public", or a modifier this function does not know.
			return true
		}
	}
	return true
}

// phpExtractImport returns the namespace path and its last segment as the name:
// "use App\Contracts\Drawable;" → ("Drawable", "App\Contracts\Drawable").
func phpExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])
	text = strings.TrimPrefix(text, "use ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	parts := strings.Split(text, "\\")
	return parts[len(parts)-1], text
}
