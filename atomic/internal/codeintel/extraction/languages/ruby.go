package languages

// Node-type strings are read from the live Ruby grammar — do not guess.
//
// require/require_relative arrive here as ordinary calls; the resolution layer
// promotes them to imports.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// RubyExtractor returns the LanguageExtractor config for Ruby source files (.rb).
func RubyExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Instance and class methods; the grammar draws no distinction the
		// framework can act on, so both become NodeKindFunction.
		FunctionTypes: extraction.TypeSet("method", "singleton_method"),

		ClassTypes: extraction.TypeSet("class"),

		ModuleTypes: extraction.TypeSet("module"),

		CallTypes: extraction.TypeSet("call"),

		NameField: "name",

		// `private` / `protected` parse as sibling call nodes rather than
		// modifiers on the method, so honoring them needs a parent walk the
		// framework does not offer. Everything is exported until then.
		IsExportedByName: func(_ string) bool { return true },
	}
}
