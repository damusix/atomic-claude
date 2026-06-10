package indexer

// python_harvester.go — CP3: Python string-literal harvester.
//
// harvestPythonStringLiterals adapts extraction.HarvestPythonLiterals to the
// literalHarvester function signature used by embeddedSQLPostPass. It:
//   - Borrows a pool instance, sets Python language, parses src.
//   - Filters out IsDocstring spans (decision 4).
//   - Returns the remaining spans as []standalone.StringLiteralSpan with
//     post-substituted Text (f-string interpolations already replaced with "?"
//     by HarvestPythonLiterals — see extraction/python_literals.go).
//
// WHY tree-sitter instead of a flat scanner: docstring exclusion requires
// structural position (first statement in a module/class/function body).
// A byte scanner cannot determine this; tree-sitter does it via the parse tree.

import (
	"context"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

// harvestPythonStringLiterals implements literalHarvester for .py files.
// It borrows a pool instance, parses src as Python, and returns all non-docstring
// string literal spans. IsDocstring spans are excluded per decision 4.
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
			// Decision 4: exclude module/class/function docstrings entirely.
			continue
		}
		out = append(out, standalone.StringLiteralSpan{
			Text:      s.Text,
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
		})
	}
	return out, nil
}
