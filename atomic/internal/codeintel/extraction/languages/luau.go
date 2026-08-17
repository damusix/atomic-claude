package languages

// Node-type strings are read from the live Luau grammar — do not guess. Luau
// names several constructs differently from Lua despite being a superset of it,
// so the Lua config is deliberately not reused here.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// LuauExtractor returns the LanguageExtractor config for Luau source files (.luau).
func LuauExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Luau's function_declaration is Lua's function_statement.
		FunctionTypes: extraction.TypeSet("function_declaration"),

		// Not variable_declaration: that node carries no "name" field in Luau.
		VariableTypes: extraction.TypeSet("assignment_statement"),

		TypeAliasTypes: extraction.TypeSet("type_definition"),

		CallTypes: extraction.TypeSet("function_call"),

		NameField: "name",

		// Luau has no export keyword, so everything is exported.
		IsExportedByName: func(_ string) bool { return true },
	}
}
