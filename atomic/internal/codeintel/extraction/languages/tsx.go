package languages

// TSX / JSX language extractor configuration.
//
// The TSX grammar is a superset of the TypeScript grammar — it adds JSX node
// types on top of the full TS type-system. TSXExtractor mirrors TypeScriptExtractor
// exactly for all declaration/call node types; it additionally populates
// JSXElementTypes so the extractor core emits "references" UnresolvedReferences
// for PascalCase JSX tags.
//
// JSXExtractor is identical to TSXExtractor but registered against
// types.LanguageJSX (.jsx files). The JS grammar does not reliably parse JSX
// (mode flags / pragma), so both .jsx and .tsx use the same tsx grammar
// (extraction.LangTSX).
//
// Verified node-type strings (parsed via tmp/probe-jsx-nodes, deleted after use):
//
//	jsx_element              — paired tag: <Panel>...</Panel>
//	jsx_self_closing_element — self-closing tag: <ChildWidget />
//	jsx_opening_element      — opening half of jsx_element; first named child
//	                           is the tag-name identifier or member_expression
//
// Tag name location (both jsx_element and jsx_self_closing_element):
//   - jsx_self_closing_element → first named child = identifier | member_expression
//   - jsx_element              → first named child = jsx_opening_element
//     → jsx_opening_element's first named child = identifier | member_expression
//
// PascalCase detection: first byte of the resolved name is ASCII A–Z.
// Lowercase names (div, span, …) are host/DOM tags — skip.
// Member tags (<Foo.Bar/>) → last segment of member_expression text.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// TSXExtractor returns the LanguageExtractor config for TSX source files (.tsx).
// It mirrors TypeScriptExtractor's declaration/call types and adds JSXElementTypes
// so JSX child usages emit "references" UnresolvedReferences.
func TSXExtractor() extraction.LanguageExtractor {
	cfg := TypeScriptExtractor()
	cfg.JSXElementTypes = extraction.TypeSet(
		"jsx_element",
		"jsx_self_closing_element",
	)
	return cfg
}

// JSXExtractor returns the LanguageExtractor config for JSX source files (.jsx).
// Uses the same tsx grammar (extraction.LangTSX) as TSXExtractor because the
// plain JS grammar requires mode flags to parse JSX reliably. Config mirrors
// TSXExtractor — identical declaration types, JSXElementTypes populated.
func JSXExtractor() extraction.LanguageExtractor {
	return TSXExtractor()
}
