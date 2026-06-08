package languages

// Ruby language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8c/ — Ruby grammar):
//
//	Top-level and named-iterator:
//	  call              — require 'json' / render(id) / s.draw / puts @name
//	  module            — module Drawable { ... }
//	  class             — class Shape { ... } / class Circle < Shape { ... }
//	  method            — def draw ... end / def initialize(id) ... end
//	  singleton_method  — def self.create(id, name) ... end
//
// Name field: ChildByFieldName("name") on class/module → constant child (text = name).
// ChildByFieldName("name") on method/singleton_method → identifier child (text = name).
// All verified by probe3.go.
//
// IsExported rule: Ruby has no visibility modifier keywords at the AST level
// that are structurally associated with individual method definitions (the
// `private` / `protected` keywords appear as separate sibling call nodes, not
// as modifiers on the method node). We default IsExported=true for all Ruby
// symbols. Private/protected tracking would require parent-walk context
// accumulation (out of scope for CP8 batch C).
//
// Module handling: Ruby `module` nodes are placed in ModuleTypes so they are
// extracted as NodeKindModule via the engine's ModuleTypes arm.
//
// Calls: `call` is the node type for all Ruby call expressions including
// require() / require_relative(). These go into CallTypes and emit
// EdgeKindCalls. The resolution layer is responsible for promoting
// require/require_relative callee names to import edges.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// RubyExtractor returns the LanguageExtractor config for Ruby source files (.rb).
//
// Node-type strings are verified by parsing real Ruby via the wazero binding
// (see tmp/probe-cp8c/main.go and tmp/probe-cp8c/probe3.go for exact output).
func RubyExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// method covers instance methods (def foo ... end).
		// singleton_method covers class methods (def self.foo ... end).
		// Both emit NodeKindFunction (FunctionTypes) since there is no
		// structural difference in the grammar from the framework's perspective.
		FunctionTypes: extraction.TypeSet("method", "singleton_method"),

		// class covers class declarations.
		ClassTypes: extraction.TypeSet("class"),

		// module covers module declarations; dispatched via ModuleTypes arm to
		// produce NodeKindModule (distinct from NodeKindClass).
		ModuleTypes: extraction.TypeSet("module"),

		// call is the Ruby grammar node for all call expressions.
		// require 'json'        → call with callee "require"
		// render(id)            → call with callee "render"
		// s.draw                → call with callee "s.draw"
		// All emit EdgeKindCalls; resolution layer promotes require → import.
		CallTypes: extraction.TypeSet("call"),

		// Field names in the Ruby grammar (verified by probe3).
		// ChildByFieldName("name") works on class, module, method, singleton_method.
		NameField: "name",

		// IsExportedByName: Ruby has no export keyword.
		// Default all symbols to exported (IsExported=true).
		// Private/protected tracking requires parent-walk context (not implemented).
		IsExportedByName: func(_ string) bool { return true },
	}
}
