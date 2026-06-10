package indexer

// typescript_harvester.go — CP4: TypeScript/TSX string-literal harvester.
//
// harvestTypeScriptStringLiterals and harvestTSXStringLiterals implement
// literalHarvester for .ts and .tsx files respectively. They adapt
// extraction.HarvestTypeScriptLiterals to the literalHarvester function
// signature used by embeddedSQLPostPass. Each:
//   - Borrows a pool instance.
//   - Sets LangTypeScript or LangTSX language on the instance.
//   - Parses src and returns all literal spans (no docstring concept in TS/TSX).
//
// WHY tree-sitter instead of a flat scanner: template-literal ${...} segments
// need structural detection so they can be substituted to "?" per decision 8.
// A byte scanner cannot reliably distinguish ${expr} from literal dollar-brace
// text inside strings.

import (
	"context"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

// harvestTypeScriptStringLiterals implements literalHarvester for .ts files.
// It borrows a pool instance, parses src as TypeScript, and returns all string
// literal spans. Template-literal interpolations are already substituted to "?"
// by HarvestTypeScriptLiterals (decision 8).
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

// harvestTSXStringLiterals implements literalHarvester for .tsx files.
// Uses LangTSX grammar — same node types as TypeScript, adds JSX syntax.
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

// convertTSSpans converts []TSLiteralSpan to []standalone.StringLiteralSpan.
// This is a field-level copy — no filtering (TS/TSX has no docstring exclusion).
func convertTSSpans(spans []extraction.TSLiteralSpan) []standalone.StringLiteralSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]standalone.StringLiteralSpan, len(spans))
	for i, s := range spans {
		out[i] = standalone.StringLiteralSpan{
			Text:      s.Text,
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
		}
	}
	return out
}
