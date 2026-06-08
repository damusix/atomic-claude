package db

// resolution.go adds the CRUD methods required by the resolution package
// (CP11–CP16). These are kept in a separate file from crud.go to avoid
// growing that file indefinitely as more engine layers come online.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// UnresolvedReference CRUD
// ---------------------------------------------------------------------------

// InsertUnresolvedRef inserts one row into unresolved_refs. The id must be
// unique; callers are responsible for deduplication (the extraction layer
// generates a UUID-style id per reference site).
func (d *DB) InsertUnresolvedRef(ctx context.Context, r types.UnresolvedReference) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO unresolved_refs
		  (id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.FromNodeID, r.ReferenceName, string(r.ReferenceKind),
		r.Line, r.Column,
		rawOrNil(r.Candidates),
		r.FilePath, string(r.Language),
		stringSliceToJSON(r.Arguments),
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: InsertUnresolvedRef %s: %w", r.ID, err)
	}
	return nil
}

// GetUnresolvedRefs returns up to limit unresolved_refs rows starting at
// offset. Passing limit=0 returns all rows.  The re-read-at-offset-0 loop
// in resolveAndPersistBatched (CP13) always passes offset=0 after deleting
// a batch — this signature supports that pattern.
func (d *DB) GetUnresolvedRefs(ctx context.Context, limit, offset int) ([]types.UnresolvedReference, error) {
	var q string
	var args []any
	if limit > 0 {
		q = `SELECT id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments
		     FROM unresolved_refs ORDER BY id LIMIT ? OFFSET ?`
		args = []any{limit, offset}
	} else {
		q = `SELECT id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments
		     FROM unresolved_refs ORDER BY id`
	}
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetUnresolvedRefs: %w", err)
	}
	return collectUnresolvedRefs(rows)
}

// DeleteUnresolvedRef deletes the unresolved_ref with the given id.
func (d *DB) DeleteUnresolvedRef(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM unresolved_refs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteUnresolvedRef %s: %w", id, err)
	}
	return nil
}

// DeleteUnresolvedRefsByIDs deletes the unresolved_refs with the given ids.
// The IN (...) clause is chunked to SQLITE_PARAM_CHUNK_SIZE (appendix O).
// This is the bulk-delete primitive used by resolveAndPersistBatched (CP13)
// after a batch resolves: delete the ids that resolved, re-read from offset 0.
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

// DeleteUnresolvedRefsByFile deletes all unresolved_refs whose file_path
// matches the given path. Called by the orchestrator on re-index.
func (d *DB) DeleteUnresolvedRefsByFile(ctx context.Context, filePath string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM unresolved_refs WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteUnresolvedRefsByFile %s: %w", filePath, err)
	}
	return nil
}

// collectUnresolvedRefs drains a *sql.Rows into []types.UnresolvedReference.
// The SELECT must include columns in the order:
//
//	id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments
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
		)
		err := rows.Scan(
			&r.ID, &r.FromNodeID, &r.ReferenceName, &refKind,
			&r.Line, &r.Column, &candidates, &r.FilePath, &lang, &arguments,
		)
		if err != nil {
			return nil, err
		}
		r.ReferenceKind = types.EdgeKind(refKind)
		r.Language = types.Language(lang)
		r.Candidates = nullBytesToRaw(candidates)
		r.Arguments = jsonToStringSlice(arguments)
		result = append(result, r)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// GetNodesByName — name-based lookup (used by CP11 re-export chain + CP12)
// ---------------------------------------------------------------------------

// GetNodesByName returns all nodes whose name (case-insensitive) matches the
// given name string.  The caller may optionally restrict to a specific kind by
// passing a non-empty kind; pass "" to match all kinds.
//
// Uses the idx_nodes_lower_name index (appendix A, v3 migration).
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

// ---------------------------------------------------------------------------
// GetFilesByPath — look up file nodes matching a path prefix (used in
// alias resolution where we expand a glob pattern to candidate paths).
// ---------------------------------------------------------------------------

// GetFileByPath returns the file record for the given exact path. Delegates to
// GetFile but returns (nil, nil) instead of ErrNotFound when absent — callers
// doing candidate probing prefer a nil-check to an errors.Is test.
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

// ---------------------------------------------------------------------------
// Tx extensions for resolution (CP13)
// ---------------------------------------------------------------------------

// InsertUnresolvedRef inserts one unresolved_ref row within a transaction.
func (t *Tx) InsertUnresolvedRef(ctx context.Context, r types.UnresolvedReference) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO unresolved_refs
		  (id, from_node_id, reference_name, reference_kind, line, col, candidates, file_path, language, arguments)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.FromNodeID, r.ReferenceName, string(r.ReferenceKind),
		r.Line, r.Column,
		rawOrNil(r.Candidates),
		r.FilePath, string(r.Language),
		stringSliceToJSON(r.Arguments),
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.InsertUnresolvedRef %s: %w", r.ID, err)
	}
	return nil
}

// DeleteUnresolvedRefsByFile deletes all unresolved_refs for a file path
// within a transaction.
func (t *Tx) DeleteUnresolvedRefsByFile(ctx context.Context, filePath string) error {
	_, err := t.tx.ExecContext(ctx, "DELETE FROM unresolved_refs WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.DeleteUnresolvedRefsByFile %s: %w", filePath, err)
	}
	return nil
}

// DeleteUnresolvedRefsByIDs deletes unresolved_refs by id within a transaction.
// Mirrors *DB.DeleteUnresolvedRefsByIDs but executes inside the caller's transaction.
// The IN (...) clause is chunked to SQLITE_PARAM_CHUNK_SIZE (appendix O).
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
