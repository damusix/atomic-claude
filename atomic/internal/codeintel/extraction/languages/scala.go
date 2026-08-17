package languages

// Node-type strings are read from the live Scala grammar — do not guess. Every
// declaration form has its own node type, so no ResolveKind hook is needed.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// ScalaExtractor returns the LanguageExtractor config for Scala source files (.scala).
func ScalaExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Traits are Scala's interface equivalent.
		InterfaceTypes: extraction.TypeSet("trait_definition"),

		// Scala 3 enums. Not enum_specifier — that is the C/C++ node.
		EnumTypes: extraction.TypeSet("enum_definition"),

		// Singleton objects join classes: they are Scala's static members.
		ClassTypes: extraction.TypeSet("class_definition", "object_definition"),

		// function_declaration is the abstract-signature form found in traits.
		FunctionTypes: extraction.TypeSet("function_definition", "function_declaration"),

		ImportTypes: extraction.TypeSet("import_declaration"),

		CallTypes: extraction.TypeSet("call_expression"),

		InstantiationTypes: extraction.TypeSet("instance_expression"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "class_parameters",

		IsExported: scalaIsExported,

		ExtractImport: scalaExtractImport,
	}
}

// scalaIsExported treats every visibility modifier — private, private[pkg],
// protected — as not-exported, since each narrows cross-module resolution.
// Scala's default, and so this function's, is public.
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
		return true
	}
	return true
}

// scalaExtractImport returns the import path and its last segment as the name.
// Braced selectors and wildcards are truncated to the package they sit under:
// "java.io.{File, InputStream}" and "scala._" yield "java.io" and "scala".
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

	if idx := strings.Index(text, ".{"); idx >= 0 {
		text = text[:idx]
	}

	text = strings.TrimSuffix(text, "._")
	text = strings.TrimSuffix(text, ".*")

	path = text
	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}
