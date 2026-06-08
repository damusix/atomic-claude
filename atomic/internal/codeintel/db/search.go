package db

// FTS5 search execution layer (appendix J).
//
// SearchNodes executes a BM25-ranked FTS5 query over nodes_fts joined to nodes.
// It does NOT implement the full search query parser (kind:/lang:/path:/name:
// field prefixes, 3-tier FTS→LIKE→fuzzy fallback, or scoring helpers like
// kindBonus) — those belong to CP18. This is the db-level FTS execution only.
//
// # BM25 weights (appendix J, verbatim)
//
// Column order in nodes_fts: id(0), name(20), qualified_name(5), docstring(1), signature(2).
// Weights are passed in column order: bm25(nodes_fts, 0, 20, 5, 1, 2).
// BM25 scores are negative (more negative = less relevant). ORDER BY score ASC
// returns the best (least-negative) match first.
//
// # Tiebreaker (appendix J)
//
// "Add ORDER BY score, nodes.id" — the secondary sort on nodes.id ensures that
// rows with equal BM25 scores are returned in a deterministic order regardless
// of insertion order or rowid assignment. Without this, tied rows fall back to
// rowid which may differ between Go and TypeScript indexers.
//
// # FTS escaping and :: handling
//
// SQLite FTS5 special characters (", *, ^, (, ), {, }, :, -, NOT, AND, OR)
// must be escaped before use in MATCH expressions to avoid syntax errors.
// The :: sequence is treated as whitespace (split into separate terms) per
// appendix J: "Escape FTS special chars; treat :: as whitespace."
//
// Escaping strategy: replace :: with space, then tokenize on whitespace, then
// wrap each token as a double-quoted FTS5 phrase with trailing * for prefix
// matching. A double-quote inside the token is escaped as two double-quotes.
// Multiple tokens are joined with OR.
//
// Example: "Parser::parse" → "Parser"* OR "parse"*
// Example: `fn AND "method"` → `"fn"* OR "AND"* OR "method"*` (no syntax error)

import (
	"context"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// SearchNodes executes an FTS5 BM25-ranked search over nodes_fts joined to
// nodes. The query string is escaped and normalized before being passed to
// SQLite MATCH.
//
// limit controls the maximum number of results. 0 means no limit (all matches
// are returned). The caller's layer (CP18) is responsible for applying a
// default limit via SearchOptions.
//
// Returns []types.SearchResult ordered by bm25 score ascending (best first),
// with nodes.id as a secondary tiebreaker for stable ordering of equal scores.
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

// buildFTSQuery converts a raw search string to a safe FTS5 MATCH expression.
//
// Rules (appendix J):
//  1. Replace "::" with a single space (treat as whitespace).
//  2. Split on whitespace to get tokens.
//  3. Drop empty tokens.
//  4. Wrap each token as a double-quoted FTS5 phrase with trailing "*":
//     any literal double-quote inside the token is doubled ("").
//  5. Join tokens with " OR ".
//
// Returns "" if no non-empty tokens remain (caller should skip the query).
func buildFTSQuery(raw string) string {
	// Step 1: treat :: as whitespace.
	s := strings.ReplaceAll(raw, "::", " ")

	// Step 2-3: tokenize.
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}

	// Step 4: escape and wrap each token.
	terms := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		// Escape literal double-quotes inside the token.
		escaped := strings.ReplaceAll(p, `"`, `""`)
		terms = append(terms, `"`+escaped+`"*`)
	}
	if len(terms) == 0 {
		return ""
	}

	// Step 5: join with OR.
	return strings.Join(terms, " OR ")
}
