package languages

// Node-type strings are read from the live Lua grammar — do not guess.
//
// Lua has no class or interface construct (OO is table-based), so there are no
// ClassTypes or InterfaceTypes to configure. require() arrives here as an
// ordinary call; the resolution layer promotes it to an import.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// LuaExtractor returns the LanguageExtractor config for Lua source files (.lua).
func LuaExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// One node type covers global, table-method (dot and colon), and local forms.
		FunctionTypes: extraction.TypeSet("function_statement"),

		VariableTypes: extraction.TypeSet("variable_declaration"),

		CallTypes: extraction.TypeSet("function_call"),

		// "name" resolves on both function_statement and variable_declaration.
		NameField: "name",

		// Lua has no export keyword, so everything is exported.
		IsExportedByName: func(_ string) bool { return true },
	}
}
