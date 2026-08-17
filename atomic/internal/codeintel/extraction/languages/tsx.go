package languages

// The TSX grammar is a superset of the TypeScript grammar, so both extractors
// here reuse TypeScriptExtractor and only add JSX node types. Node-type strings
// are read from the live grammar — do not guess.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// TSXExtractor returns the config for .tsx files. JSXElementTypes is what makes
// the core emit "references" for PascalCase JSX tags.
func TSXExtractor() extraction.LanguageExtractor {
	cfg := TypeScriptExtractor()
	cfg.JSXElementTypes = extraction.TypeSet(
		"jsx_element",
		"jsx_self_closing_element",
	)
	return cfg
}

// JSXExtractor returns the config for .jsx files. It is registered against the
// tsx grammar, not the JS one, which needs mode flags to parse JSX reliably.
func JSXExtractor() extraction.LanguageExtractor {
	return TSXExtractor()
}
