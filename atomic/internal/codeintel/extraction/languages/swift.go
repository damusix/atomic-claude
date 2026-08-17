package languages

// Node-type strings are read from the live Swift grammar — do not guess. Notably,
// there is no struct_declaration node: enum, struct, and class all parse as
// class_declaration, which is why ResolveKind has to disambiguate them.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// SwiftExtractor returns the LanguageExtractor config for Swift source files (.swift).
func SwiftExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Protocols are Swift's interface equivalent.
		InterfaceTypes: extraction.TypeSet("protocol_declaration"),

		// Enum, struct, and class all land here; StructTypes is the arm that
		// runs ResolveKind before settling on a node kind.
		StructTypes: extraction.TypeSet("class_declaration"),

		FunctionTypes: extraction.TypeSet("function_declaration", "init_declaration"),

		PropertyTypes: extraction.TypeSet("property_declaration"),

		ImportTypes: extraction.TypeSet("import_declaration"),

		CallTypes: extraction.TypeSet("call_expression"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "function_value_parameters",

		ResolveKind: swiftResolveKind,

		IsExported: swiftIsExported,

		ExtractImport: swiftExtractImport,
	}
}

// swiftResolveKind separates enums, which carry an enum_class_body child, from
// structs and classes, which share class_body and are both reported as classes:
// the grammar offers no structural way to tell those two apart.
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

// swiftIsExported looks for public or open, the only access levels that cross a
// module boundary; Swift's default of internal does not. The modifiers child is
// found by scanning named children — ChildByFieldName("modifiers") returns null
// in this grammar.
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

// swiftExtractImport returns the module path and its last segment as the name.
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
	text = strings.TrimPrefix(text, "import ")
	text = strings.TrimSpace(text)
	// Swift allows a kind qualifier after the keyword, as in "import class Foo".
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
