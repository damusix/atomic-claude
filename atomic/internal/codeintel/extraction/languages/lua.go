package languages

// Lua language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8c/ — Lua grammar):
//
//	Named-iterator sees:
//	  function_statement     — function Shape.new(...) ... end
//	                         — function Shape:draw() ... end
//	                         — local function render(v) ... end
//	  variable_declaration   — local json = require("json")
//	                         — local PI = 3.14159
//	                         — local Shape = {}
//	  function_call          — require("json") / render(v) / print(v) / s:draw()
//
// Name field: ChildByFieldName("name") works on both function_statement and
// variable_declaration (verified by probe3.go and probe4.go):
//   - function_statement:   returns function_name child, text = "Shape:draw" / "greet"
//   - variable_declaration: returns variable_declarator child, text = "PI" / "json"
//
// Lua has no class or interface constructs. It uses table-based OO (Shape = {},
// Shape.__index = Shape). We extract only what the grammar directly exposes:
// functions, variables, and call sites. No ClassTypes or InterfaceTypes.
//
// IsExported rule: Lua has no visibility modifiers. All symbols default to
// exported (IsExported=true). "local" scoping is a runtime concept not captured
// structurally by the grammar in a way the framework can use per-node.
//
// Calls: function_call is the Lua grammar node for all call expressions.
// require("json") → function_call with callee "require"
// render(v)       → function_call with callee "render"
// s:draw()        → function_call with callee "s:draw" (method_index_expression child)
// All emit EdgeKindCalls. The resolution layer promotes require → import.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// LuaExtractor returns the LanguageExtractor config for Lua source files (.lua).
//
// Node-type strings are verified by parsing real Lua via the wazero binding
// (see tmp/probe-cp8c/main.go, probe3.go, and probe4.go for exact output).
func LuaExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_statement covers all Lua function definitions:
		//   function greet(name) ... end         (global)
		//   function Shape.new(id, name) ... end  (table method via dot)
		//   function Shape:draw() ... end         (table method via colon)
		//   local function render(v) ... end      (local — same node type)
		FunctionTypes: extraction.TypeSet("function_statement"),

		// variable_declaration covers local variable declarations:
		//   local json = require("json")
		//   local PI = 3.14159
		//   local Shape = {}
		VariableTypes: extraction.TypeSet("variable_declaration"),

		// function_call is the Lua grammar node for all call expressions.
		// Includes require(), render(), print(), and method calls via colon.
		CallTypes: extraction.TypeSet("function_call"),

		// Field names in the Lua grammar (verified by probe3 and probe4).
		// ChildByFieldName("name") works on both function_statement and
		// variable_declaration.
		NameField: "name",

		// IsExportedByName: Lua has no export keyword — default all to exported.
		IsExportedByName: func(_ string) bool { return true },
	}
}
