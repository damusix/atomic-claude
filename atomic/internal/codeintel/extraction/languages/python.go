package languages

// Node-type strings are read from the live Python grammar — do not guess.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// PythonExtractor returns the LanguageExtractor config for Python source files
// (.py).
func PythonExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// One node type for functions and methods alike, so methods surface as
		// NodeKindFunction — separating them would need a parent-aware hook.
		FunctionTypes: extraction.TypeSet("function_definition"),

		ClassTypes: extraction.TypeSet("class_definition"),

		ImportTypes: extraction.TypeSet("import_statement", "import_from_statement"),

		// "call", not "call_expression", in this grammar.
		CallTypes: extraction.TypeSet("call"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "return_type",

		IsExportedByName: pyIsExportedByName,

		ExtractImport: pyExtractImport,
	}
}

// pyIsExportedByName applies Python's leading-underscore privacy convention.
// __all__ overrides are invisible to a static extractor and go unchecked.
func pyIsExportedByName(name string) bool {
	return name != "" && !strings.HasPrefix(name, "_")
}

// pyExtractImport returns the module path and its last segment as the name:
// "import os.path" → ("path", "os.path"); "from typing import Protocol" →
// ("typing", "typing") — the imported symbol is not part of either result.
func pyExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return "", ""
	}

	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])

	switch kind {
	case "import_statement":
		text = strings.TrimPrefix(text, "import ")
		if idx := strings.Index(text, " as "); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
		parts := strings.Split(text, ".")
		return parts[len(parts)-1], text

	case "import_from_statement":
		text = strings.TrimPrefix(text, "from ")
		idx := strings.Index(text, " import ")
		if idx < 0 {
			return "", ""
		}
		modulePath := strings.TrimSpace(text[:idx])
		parts := strings.Split(modulePath, ".")
		return parts[len(parts)-1], modulePath
	}

	return "", ""
}
