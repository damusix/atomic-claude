package db

// FTS5 execution only: the field-prefix parser, the FTS→LIKE→fuzzy fallback,
// and the scoring helpers all live a layer up.
//
// The bm25 weights follow nodes_fts column order: id(0), name(20),
// qualified_name(5), docstring(1), signature(2). Scores are negative, so ASC
// puts the best match first, and nodes.id breaks ties — without it equal scores
// fall back to rowid, which differs between indexer implementations.

import (
	"context"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// SearchNodes returns results best-first. A limit of 0 means no limit;
// applying a sensible default is the caller's job.
func (d *DB) SearchNodes(ctx context.Context, query string, limit int) ([]types.SearchResult, error) {
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	q := `
		SELECT n.id, n.kind, n.name, n.qualified_name, n.file_path, n.language,
		       n.start_line, n.end_line, n.start_column, n.end_column,
		       n.docstring, n.signature, n.visibility,
		       n.is_exported, n.is_async, n.is_static, n.is_const,
		       n.decorators, n.type_parameters, n.metadata, n.updated_at,
		       bm25(nodes_fts, 0, 20, 5, 1, 2) AS score
		FROM nodes_fts
		JOIN nodes n ON n.rowid = nodes_fts.rowid
		WHERE nodes_fts MATCH ?
		ORDER BY score, n.id`

	args := []any{ftsQuery}

	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: SearchNodes %q (fts=%q): %w", query, ftsQuery, err)
	}
	defer rows.Close()

	var results []types.SearchResult
	for rows.Next() {
		var (
			n          types.Node
			kind       string
			lang       string
			isExported int
			isAsync    int
			isStatic   int
			isConst    int
			decorators []byte
			typeParams []byte
			metadata   []byte
			updatedAt  int64
			score      float64
		)
		err := rows.Scan(
			&n.ID, &kind, &n.Name, &n.QualifiedName, &n.FilePath, &lang,
			&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn,
			&n.Docstring, &n.Signature, &n.Visibility,
			&isExported, &isAsync, &isStatic, &isConst,
			&decorators, &typeParams, &metadata,
			&updatedAt, &score,
		)
		if err != nil {
			return nil, fmt.Errorf("codeintel/db: SearchNodes scan: %w", err)
		}
		n.Kind = types.NodeKind(kind)
		n.Language = types.Language(lang)
		n.IsExported = isExported != 0
		n.IsAsync = isAsync != 0
		n.IsStatic = isStatic != 0
		n.IsConst = isConst != 0
		n.Decorators = nullBytesToRaw(decorators)
		n.TypeParameters = nullBytesToRaw(typeParams)
		n.Metadata = nullBytesToRaw(metadata)
		if updatedAt != 0 {
			n.UpdatedAt = fmt.Sprintf("%d", updatedAt)
		}
		results = append(results, types.SearchResult{Node: n, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("codeintel/db: SearchNodes rows: %w", err)
	}
	return results, nil
}

// buildFTSQuery makes a raw search string safe for MATCH. Quoting every token
// as a phrase neutralises FTS5's operators and special characters wholesale, so
// `fn AND "method"` is a search for three words rather than a syntax error.
// "::" is treated as whitespace, splitting "Parser::parse" into two terms.
//
// Returns "" when nothing is left to search for.
func buildFTSQuery(raw string) string {
	s := strings.ReplaceAll(raw, "::", " ")

	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}

	terms := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		escaped := strings.ReplaceAll(p, `"`, `""`)
		terms = append(terms, `"`+escaped+`"*`) // trailing * for prefix matching
	}
	if len(terms) == 0 {
		return ""
	}

	return strings.Join(terms, " OR ")
}
