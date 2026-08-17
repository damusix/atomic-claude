package indexer

// Python string-literal harvester.
//
// Tree-sitter rather than a byte scanner because excluding docstrings needs
// structural position — first statement of a module, class, or function body —
// which only the parse tree can establish.

import (
	"context"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

// harvestPythonStringLiterals returns every non-docstring literal span.
func harvestPythonStringLiterals(ctx context.Context, src string, pool *extraction.Pool) ([]standalone.StringLiteralSpan, error) {
	inst, err := pool.Borrow(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Return(inst)

	pySpans, err := extraction.HarvestPythonLiterals(ctx, inst, src)
	if err != nil {
		return nil, err
	}

	var out []standalone.StringLiteralSpan
	for _, s := range pySpans {
		if s.IsDocstring {
			continue
		}
		out = append(out, standalone.StringLiteralSpan{
			Text:       s.Text,
			StartLine:  s.StartLine,
			EndLine:    s.EndLine,
			CalleeExpr: s.CalleeExpr,
		})
	}
	return out, nil
}
