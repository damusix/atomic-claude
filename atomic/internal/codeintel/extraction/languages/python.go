package languages

// Python language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-lang-details):
//
//   Top-level (direct children of module):
//     import_statement       — import os; import sys
//     import_from_statement  — from typing import Protocol
//     class_definition       — class Canvas: ...
//     function_definition    — def make_canvas(): ...
//     expression_statement   — bare assignments, lambdas
//
//   Named iterator also sees:
//     function_definition    — nested method defs inside class bodies
//     call                   — function/method call sites (NOTE: "call", not "call_expression")
//     assignment             — module-level assignments
//
// IsExported convention (Python has no export keyword):
//   A symbol is exported if its name does NOT start with an underscore ("_").
//   This matches Python's __all__ convention fallback: names starting with "_"
//   are considered private by convention. Document this choice at point-of-use.
//
// Field names:
//   - class_definition: name field = "name"
//   - function_definition: name field = "name"
//   - call: function field = "function" (the callee expression)

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// PythonExtractor returns the LanguageExtractor config for Python source files
// (.py).
//
// Node-type strings are verified by parsing real Python via the wazero binding
// (see tmp/probe-lang-details/main.go for exact output).
func PythonExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_definition covers all function and method definitions.
		// Python uses the same node for both; context (inside class_definition)
		// is not checked here — methods will appear as NodeKindFunction unless
		// a parent-aware hook is added in CP8. This is consistent with the
		// brief's scope for this checkpoint.
		FunctionTypes: extraction.TypeSet("function_definition"),

		// class_definition covers classes (including Protocol subclasses).
		ClassTypes: extraction.TypeSet("class_definition"),

		// import_statement covers "import X" and "import X as Y" forms.
		// import_from_statement covers "from X import Y" forms.
		ImportTypes: extraction.TypeSet("import_statement", "import_from_statement"),

		// "call" is the Python grammar node for function/method call expressions.
		// NOTE: Python grammar uses "call" not "call_expression" — verified by probe.
		CallTypes: extraction.TypeSet("call"),

		// Field names in the Python grammar.
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "return_type",

		// IsExportedByName: Python has no export keyword.
		// Convention: exported if the name does NOT start with "_".
		// This is the standard Python public/private convention; __all__ overrides
		// are not checked (out of scope for the static extractor).
		IsExportedByName: pyIsExportedByName,

		// ExtractImport handles both import forms.
		ExtractImport: pyExtractImport,
	}
}

// pyIsExportedByName reports whether a Python symbol is exported.
// By Python convention, any name not starting with "_" is considered public.
// Names starting with "__" (dunder) are also considered exported (they are
// part of Python's object protocol and are intentionally public).
func pyIsExportedByName(name string) bool {
	return name != "" && !strings.HasPrefix(name, "_")
}

// pyExtractImport extracts the module path from import_statement and
// import_from_statement nodes.
//
//	import os           → name="os", path="os"
//	import os.path      → name="path", path="os.path"
//	from typing import Protocol → name="typing", path="typing"
//	from pathlib import Path    → name="pathlib", path="pathlib"
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
		// "import X" or "import X as Y" or "import X.Y"
		text = strings.TrimPrefix(text, "import ")
		// Strip " as alias" suffix.
		if idx := strings.Index(text, " as "); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
		parts := strings.Split(text, ".")
		return parts[len(parts)-1], text

	case "import_from_statement":
		// "from X import Y" or "from X import Y, Z"
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
