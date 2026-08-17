package indexer

// TypeScript and TSX string-literal harvesters.
//
// Tree-sitter rather than a byte scanner because template-literal ${...}
// segments must be found structurally to be substituted with "?"; a scanner
// cannot tell an interpolation from literal dollar-brace text in a string.

import (
	"context"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

func harvestTypeScriptStringLiterals(ctx context.Context, src string, pool *extraction.Pool) ([]standalone.StringLiteralSpan, error) {
	inst, err := pool.Borrow(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Return(inst)

	tsSpans, err := extraction.HarvestTypeScriptLiterals(ctx, inst, src, extraction.LangTypeScript)
	if err != nil {
		return nil, err
	}

	return convertTSSpans(tsSpans), nil
}

// harvestTSXStringLiterals differs only in grammar: TSX adds JSX syntax over
// the same string node types.
func harvestTSXStringLiterals(ctx context.Context, src string, pool *extraction.Pool) ([]standalone.StringLiteralSpan, error) {
	inst, err := pool.Borrow(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Return(inst)

	tsSpans, err := extraction.HarvestTypeScriptLiterals(ctx, inst, src, extraction.LangTSX)
	if err != nil {
		return nil, err
	}

	return convertTSSpans(tsSpans), nil
}

// convertTSSpans copies field-for-field, filtering nothing: TS and TSX have no
// docstrings to exclude.
func convertTSSpans(spans []extraction.TSLiteralSpan) []standalone.StringLiteralSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]standalone.StringLiteralSpan, len(spans))
	for i, s := range spans {
		out[i] = standalone.StringLiteralSpan{
			Text:       s.Text,
			StartLine:  s.StartLine,
			EndLine:    s.EndLine,
			CalleeExpr: s.CalleeExpr,
		}
	}
	return out
}
