package db

// CRUD for the resolution package, split out of crud.go to keep that file from
// growing without bound as engine layers land.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// InsertUnresolvedRef needs a unique id; dedup is the extraction layer's job,
// which mints one per reference site.
func (d *DB) InsertUnresolvedRef(ctx context.Context, r types.UnresolvedReference) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO unresolved_refs
		  (id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments, callee_expr)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.FromNodeID, r.ReferenceName, string(r.ReferenceKind),
		r.Line, r.Column,
		rawOrNil(r.Candidates),
		r.FilePath, string(r.Language),
		stringSliceToJSON(r.Arguments),
		nullIfEmpty(r.CalleeExpr),
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: InsertUnresolvedRef %s: %w", r.ID, err)
	}
	return nil
}

// GetUnresolvedRefs returns all rows when limit is 0.
func (d *DB) GetUnresolvedRefs(ctx context.Context, limit, offset int) ([]types.UnresolvedReference, error) {
	var q string
	var args []any
	if limit > 0 {
		q = `SELECT id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments, callee_expr
		     FROM unresolved_refs ORDER BY id LIMIT ? OFFSET ?`
		args = []any{limit, offset}
	} else {
		q = `SELECT id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments, callee_expr
		     FROM unresolved_refs ORDER BY id`
	}
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetUnresolvedRefs: %w", err)
	}
	return collectUnresolvedRefs(rows)
}

// GetUnresolvedRefsAfter is the keyset-pagination primitive for batched
// resolution; afterID="" starts from the beginning. Advancing by last-id-seen
// rather than a numeric offset means refs deliberately left unresolved cannot
// stall the scan, and every ref is visited exactly once.
func (d *DB) GetUnresolvedRefsAfter(ctx context.Context, afterID string, limit int) ([]types.UnresolvedReference, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments, callee_expr
		FROM unresolved_refs WHERE id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetUnresolvedRefsAfter: %w", err)
	}
	return collectUnresolvedRefs(rows)
}

// GetUnresolvedRefsByKind is a single bulk fetch rather than paginated: its
// caller is the SQL string-match pass, which works over a bounded slice of the
// table instead of the full scan.
func (d *DB) GetUnresolvedRefsByKind(ctx context.Context, kind types.EdgeKind) ([]types.UnresolvedReference, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments, callee_expr
		FROM unresolved_refs WHERE reference_kind = ? ORDER BY id`, string(kind))
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetUnresolvedRefsByKind %s: %w", kind, err)
	}
	return collectUnresolvedRefs(rows)
}

func (d *DB) DeleteUnresolvedRef(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM unresolved_refs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteUnresolvedRef %s: %w", id, err)
	}
	return nil
}

// DeleteUnresolvedRefsByIDs chunks its IN (...) to SQLITE_PARAM_CHUNK_SIZE.
func (d *DB) DeleteUnresolvedRefsByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for start := 0; start < len(ids); start += SQLITE_PARAM_CHUNK_SIZE {
		end := start + SQLITE_PARAM_CHUNK_SIZE
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		if _, err := d.db.ExecContext(ctx,
			"DELETE FROM unresolved_refs WHERE id IN ("+placeholders+")",
			args...,
		); err != nil {
			return fmt.Errorf("codeintel/db: DeleteUnresolvedRefsByIDs chunk %d-%d: %w", start, end, err)
		}
	}
	return nil
}

// DeleteUnresolvedRefsByFile clears a file's refs so re-index replaces rather
// than duplicates them.
func (d *DB) DeleteUnresolvedRefsByFile(ctx context.Context, filePath string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM unresolved_refs WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteUnresolvedRefsByFile %s: %w", filePath, err)
	}
	return nil
}

// collectUnresolvedRefs requires the SELECT column order used by every query
// in this file.
func collectUnresolvedRefs(rows *sql.Rows) ([]types.UnresolvedReference, error) {
	defer rows.Close()
	var result []types.UnresolvedReference
	for rows.Next() {
		var (
			r          types.UnresolvedReference
			refKind    string
			lang       string
			candidates []byte
			arguments  []byte
			calleeExpr sql.NullString
		)
		err := rows.Scan(
			&r.ID, &r.FromNodeID, &r.ReferenceName, &refKind,
			&r.Line, &r.Column, &candidates, &r.FilePath, &lang, &arguments, &calleeExpr,
		)
		if err != nil {
			return nil, err
		}
		r.ReferenceKind = types.EdgeKind(refKind)
		r.Language = types.Language(lang)
		r.Candidates = nullBytesToRaw(candidates)
		r.Arguments = jsonToStringSlice(arguments)
		r.CalleeExpr = calleeExpr.String
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetNodesByName matches case-insensitively via idx_nodes_lower_name. An empty
// kind matches all kinds.
func (d *DB) GetNodesByName(ctx context.Context, name string, kind types.NodeKind) ([]types.Node, error) {
	var (
		rows *sql.Rows
		err  error
	)
	lowerName := strings.ToLower(name)
	if kind != "" {
		rows, err = d.db.QueryContext(ctx, `
			SELECT id, kind, name, qualified_name, file_path, language,
			       start_line, end_line, start_column, end_column,
			       docstring, signature, visibility,
			       is_exported, is_async, is_static, is_const,
			       decorators, type_parameters, metadata, updated_at
			FROM nodes
			WHERE lower(name) = ? AND kind = ?
			ORDER BY id`,
			lowerName, string(kind),
		)
	} else {
		rows, err = d.db.QueryContext(ctx, `
			SELECT id, kind, name, qualified_name, file_path, language,
			       start_line, end_line, start_column, end_column,
			       docstring, signature, visibility,
			       is_exported, is_async, is_static, is_const,
			       decorators, type_parameters, metadata, updated_at
			FROM nodes
			WHERE lower(name) = ?
			ORDER BY id`,
			lowerName,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetNodesByName %q: %w", name, err)
	}
	return collectNodes(rows)
}

// GetFileByPath is GetFile returning (nil, nil) rather than ErrNotFound, since
// its callers probe candidate paths and want a nil check, not errors.Is.
func (d *DB) GetFileByPath(ctx context.Context, path string) (*types.FileRecord, error) {
	f, err := d.GetFile(ctx, path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

func (t *Tx) InsertUnresolvedRef(ctx context.Context, r types.UnresolvedReference) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO unresolved_refs
		  (id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments, callee_expr)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.FromNodeID, r.ReferenceName, string(r.ReferenceKind),
		r.Line, r.Column,
		rawOrNil(r.Candidates),
		r.FilePath, string(r.Language),
		stringSliceToJSON(r.Arguments),
		nullIfEmpty(r.CalleeExpr),
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.InsertUnresolvedRef %s: %w", r.ID, err)
	}
	return nil
}

func (t *Tx) DeleteUnresolvedRefsByFile(ctx context.Context, filePath string) error {
	_, err := t.tx.ExecContext(ctx, "DELETE FROM unresolved_refs WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.DeleteUnresolvedRefsByFile %s: %w", filePath, err)
	}
	return nil
}

func (t *Tx) DeleteUnresolvedRefsByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for start := 0; start < len(ids); start += SQLITE_PARAM_CHUNK_SIZE {
		end := start + SQLITE_PARAM_CHUNK_SIZE
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		if _, err := t.tx.ExecContext(ctx,
			"DELETE FROM unresolved_refs WHERE id IN ("+placeholders+")",
			args...,
		); err != nil {
			return fmt.Errorf("codeintel/db: Tx.DeleteUnresolvedRefsByIDs chunk %d-%d: %w", start, end, err)
		}
	}
	return nil
}
