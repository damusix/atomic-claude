package indexer

// Per-language node-kind configs for the generic embedded-literal harvester,
// deliberately separate from the bespoke Go/Python/TS/TSX harvesters that keep
// their own logic. See docs/spec/embedded-sql-language-expansion.md.

import (
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// embeddedLangEntry pairs a tree-sitter binding with its node-kind config.
type embeddedLangEntry struct {
	binding extraction.Lang
	cfg     extraction.EmbeddedLiteralConfig
}

// embeddedLiteralConfigs holds node kinds probed from the live grammars. They
// are ground truth, not guesses — do not edit an entry without re-probing.
// A nil ContentKinds means the grammar inlines string content.
var embeddedLiteralConfigs = map[types.Language]embeddedLangEntry{
	types.LanguageC: {
		binding: extraction.LangC,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	types.LanguageCpp: {
		binding: extraction.LangCpp,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true, "raw_string_literal": true},
			ContentKinds: map[string]bool{"string_content": true, "raw_string_content": true},
			InterpKinds:  nil,
		},
	},
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
	types.LanguageJava: {
		binding: extraction.LangJava,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_fragment": true, "multiline_string_fragment": true},
			InterpKinds:  nil,
		},
	},
	types.LanguageJavaScript: {
		binding: extraction.LangJavaScript,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true, "template_string": true},
			ContentKinds: map[string]bool{"string_fragment": true},
			InterpKinds:  map[string]bool{"template_substitution": true},
		},
	},
	types.LanguageKotlin: {
		binding: extraction.LangKotlin,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  map[string]bool{"interpolated_identifier": true, "interpolation": true},
		},
	},
	types.LanguageLua: {
		binding: extraction.LangLua,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true},
			ContentKinds: nil,
			InterpKinds:  nil,
		},
	},
	types.LanguageLuau: {
		binding: extraction.LangLuau,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	types.LanguageObjC: {
		binding: extraction.LangObjC,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	types.LanguagePascal: {
		binding: extraction.LangPascal,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"literalString": true},
			ContentKinds: nil,
			InterpKinds:  nil,
		},
	},
	types.LanguagePHP: {
		binding: extraction.LangPHP,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"encapsed_string": true, "heredoc": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  map[string]bool{"variable_name": true},
		},
	},
	types.LanguageRuby: {
		binding: extraction.LangRuby,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true, "heredoc_body": true},
			ContentKinds: map[string]bool{"string_content": true, "heredoc_content": true},
			InterpKinds:  map[string]bool{"interpolation": true},
		},
	},
	types.LanguageRust: {
		binding: extraction.LangRust,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true, "raw_string_literal": true},
			ContentKinds: map[string]bool{"string_content": true},
			InterpKinds:  nil,
		},
	},
	types.LanguageScala: {
		binding: extraction.LangScala,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string": true, "interpolated_string": true},
			ContentKinds: nil,
			InterpKinds:  map[string]bool{"interpolation": true},
		},
	},
	types.LanguageSwift: {
		binding: extraction.LangSwift,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"line_string_literal": true, "multi_line_string_literal": true},
			ContentKinds: map[string]bool{"line_str_text": true, "multi_line_str_text": true},
			InterpKinds:  map[string]bool{"interpolated_expression": true},
		},
	},
	types.LanguageDart: {
		binding: extraction.LangDart,
		cfg: extraction.EmbeddedLiteralConfig{
			StringKinds:  map[string]bool{"string_literal": true},
			ContentKinds: nil,
			InterpKinds:  map[string]bool{"template_substitution": true},
		},
	},
}
