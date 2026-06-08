package languages

// Luau language extractor configuration.
//
// Luau = Lua + type annotations. It uses different grammar node names than Lua
// for some constructs.
//
// Verified node-type strings (parsed via tmp/probe-cp8c/ — Luau grammar):
//
//	Named-iterator sees:
//	  function_declaration   — function Shape.new(id: number, name: string): {} ... end
//	                         — function Shape:draw(): () ... end
//	                         — local function render(v: number): () ... end
//	  assignment_statement   — local PI: number = 3.14159
//	                         — local json = require("json")
//	                         — local Shape = {}
//	  type_definition        — type Vector2 = { x: number, y: number }
//	                         — type Drawable = { draw: (self: any) -> () }
//	  function_call          — require("json") / render(v) / print(v) / s:draw()
//
// Name field: ChildByFieldName("name") works on:
//   - function_declaration:  → identifier / dot_index_expression / method_index_expression
//     text examples: "greet", "Shape.new", "Shape:draw"
//   - assignment_statement:  → identifier child, text = "PI" / "json" / "Shape"
//   - type_definition:       → identifier child, text = "Vector2" / "Drawable"
//
// NOTE: Luau variable_declaration does NOT have a "name" field (probe4 confirmed).
// Variables are represented as assignment_statement nodes in Luau. We use
// VariableTypes: TypeSet("assignment_statement") and NameField: "name" to extract
// the identifier name.
//
// IsExported rule: same as Lua — no visibility modifiers, default all exported.
//
// TypeAlias: type_definition nodes are placed in TypeAliasTypes → NodeKindTypeAlias.
//
// Calls: same as Lua — function_call node for all call expressions.
// All emit EdgeKindCalls. The resolution layer promotes require → import.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// LuauExtractor returns the LanguageExtractor config for Luau source files (.luau).
//
// Node-type strings are verified by parsing real Luau via the wazero binding
// (see tmp/probe-cp8c/main.go, probe2.go, and probe4.go for exact output).
func LuauExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_declaration covers all Luau function definitions (global, table method,
		// local). Luau uses "function_declaration" where Lua uses "function_statement".
		FunctionTypes: extraction.TypeSet("function_declaration"),

		// assignment_statement covers typed and untyped local declarations in Luau:
		//   local PI: number = 3.14159
		//   local json = require("json")
		//   local Shape = {}
		// Luau's variable_declaration does not carry a "name" field; assignment_statement does.
		VariableTypes: extraction.TypeSet("assignment_statement"),

		// type_definition covers Luau type alias declarations:
		//   type Vector2 = { x: number, y: number }
		//   type Drawable = { draw: (self: any) -> () }
		TypeAliasTypes: extraction.TypeSet("type_definition"),

		// function_call is the Luau grammar node for all call expressions (same as Lua).
		CallTypes: extraction.TypeSet("function_call"),

		// Field names in the Luau grammar (verified by probe2 and probe4).
		// ChildByFieldName("name") works on function_declaration, assignment_statement,
		// and type_definition.
		NameField: "name",

		// IsExportedByName: Luau has no export keyword — default all to exported.
		IsExportedByName: func(_ string) bool { return true },
	}
}
