package indexer

// embedded_literals_config.go — CP2: per-language node-kind configs for the
// generic embedded-literal harvester.
//
// embeddedLiteralConfigs encodes the 16 new host languages supported by CP2.
// Each entry carries:
//   - binding: the extraction.Lang constant for SetLanguage.
//   - cfg:     the EmbeddedLiteralConfig (StringKinds/ContentKinds/InterpKinds)
//              probed against the live wazero/tree-sitter grammars — see
//              docs/spec/embedded-sql-language-expansion.md § Grammar node-kind config.
//
// These 16 entries are deliberately separate from the 4 bespoke harvesters
// (Go/Python/TS/TSX) which keep their own per-lang logic. See spec §Non-goals.
//
// WHY map[string]bool over map[string]struct{}: EmbeddedLiteralConfig declares
// map[string]bool per project convention for O(1) membership checks. TypeSet
// returns map[string]struct{} (different type); we use map literals directly.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// embeddedLangEntry pairs a tree-sitter language binding with its node-kind
// config for the generic HarvestEmbeddedLiterals harvester.
type embeddedLangEntry struct {
	binding extraction.Lang
	cfg     extraction.EmbeddedLiteralConfig
}

// embeddedLiteralConfigs is the authoritative config table for the 16 new host
// languages. Keyed by types.Language. Node kinds are probed ground truth from
// the live grammars; do not alter without re-probing (spec §Grammar node-kind config).
var embeddedLiteralConfigs = map[types.Language]embeddedLangEntry{
	// C — content-child grammar; no interpolation.
	types.LanguageC: {
		binding: extraction.LangC,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	// C++ — content-child grammar; raw_string_literal/raw_string_content added.
	types.LanguageCpp: {
		binding: extraction.LangCpp,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true, "raw_string_literal": true},
			ContentKinds: map[string]bool{"string_content": true, "raw_string_content": true},
			InterpKinds:  nil,
		},
	},
	// C# — content-child + interpolation; verbatim strings and interpolated strings.
	types.LanguageCSharp: {
		binding: extraction.LangCSharp,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds: map[string]bool{
				"string_literal":                 true,
				"interpolated_string_expression": true,
				"verbatim_string_literal":        true,
			},
			ContentKinds: map[string]bool{
				"string_literal_content": true,
				"string_content":         true,
			},
			InterpKinds: map[string]bool{"interpolation": true},
		},
	},
	// Java — content-child grammar; string_fragment + multiline_string_fragment.
	types.LanguageJava: {
		binding: extraction.LangJava,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_fragment": true, "multiline_string_fragment": true},
			InterpKinds:  nil,
		},
	},
	// JavaScript — content-child + interpolation; template_string has substitutions.
	types.LanguageJavaScript: {
		binding: extraction.LangJavaScript,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true, "template_string": true},
			ContentKinds: map[string]bool{"string_fragment": true},
			InterpKinds:  map[string]bool{"template_substitution": true},
		},
	},
	// Kotlin — content-child + interpolation; both $ident and ${expr} forms.
	types.LanguageKotlin: {
		binding: extraction.LangKotlin,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  map[string]bool{"interpolated_identifier": true, "interpolation": true},
		},
	},
	// Lua — Shape 2 (no content children; inline content).
	types.LanguageLua: {
		binding: extraction.LangLua,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true},
			ContentKinds: nil,
			InterpKinds:  nil,
		},
	},
	// Luau — content-child grammar (Roblox Lua dialect with string_content child).
	types.LanguageLuau: {
		binding: extraction.LangLuau,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	// Objective-C — content-child grammar.
	types.LanguageObjC: {
		binding: extraction.LangObjC,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	// Pascal — Shape 2 (inline content; no content child node).
	types.LanguagePascal: {
		binding: extraction.LangPascal,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"literalString": true},
			ContentKinds: nil,
			InterpKinds:  nil,
		},
	},
	// PHP — content-child + interpolation; encapsed_string and heredoc forms.
	types.LanguagePHP: {
		binding: extraction.LangPHP,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"encapsed_string": true, "heredoc": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  map[string]bool{"variable_name": true},
		},
	},
	// Ruby — content-child + interpolation; heredoc_body also covered.
	types.LanguageRuby: {
		binding: extraction.LangRuby,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true, "heredoc_body": true},
			ContentKinds: map[string]bool{"string_content": true, "heredoc_content": true},
			InterpKinds:  map[string]bool{"interpolation": true},
		},
	},
	// Rust — content-child grammar; raw_string_literal also covered.
	types.LanguageRust: {
		binding: extraction.LangRust,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true, "raw_string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	// Scala — Shape 2 + interpolation; interpolated_string carries interpolation children.
	types.LanguageScala: {
		binding: extraction.LangScala,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true, "interpolated_string": true},
			ContentKinds: nil,
			InterpKinds:  map[string]bool{"interpolation": true},
		},
	},
	// Swift — content-child + interpolation; both single-line and multi-line forms.
	types.LanguageSwift: {
		binding: extraction.LangSwift,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"line_string_literal": true, "multi_line_string_literal": true},
			ContentKinds: map[string]bool{"line_str_text": true, "multi_line_str_text": true},
			InterpKinds:  map[string]bool{"interpolated_expression": true},
		},
	},
	// Dart — Shape 2 + interpolation; template_substitution replaces $var / ${expr}.
	types.LanguageDart: {
		binding: extraction.LangDart,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: nil,
			InterpKinds:  map[string]bool{"template_substitution": true},
		},
	},
}
