package db

// Transaction seam for atomic store operations. The single connection db.go
// pins means SQLite serialises all writes, so BEGIN/COMMIT/ROLLBACK needs no
// contention handling here.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Tx mirrors the *DB CRUD methods storeExtractionResult needs, deliberately
// no more, executing them inside a transaction.
type Tx struct {
	tx *sql.Tx
}

// WithTx commits when fn returns nil and rolls back otherwise, including on a
// panic. The unconditional deferred rollback is safe because SQLite treats
// ROLLBACK after COMMIT as a no-op.
func (d *DB) WithTx(ctx context.Context, fn func(*Tx) error) error {
	sqlTx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("codeintel/db: begin tx: %w", err)
	}
	t := &Tx{tx: sqlTx}
	defer func() {
		_ = sqlTx.Rollback()
	}()

	if err := fn(t); err != nil {
		return err
	}

	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("codeintel/db: commit tx: %w", err)
	}
	return nil
}

// DeleteNodesByFile takes the nodes' edges with it, by FK cascade.
func (t *Tx) DeleteNodesByFile(ctx context.Context, filePath string) error {
	_, err := t.tx.ExecContext(ctx, "DELETE FROM nodes WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.DeleteNodesByFile %s: %w", filePath, err)
	}
	return nil
}

func (t *Tx) DeleteFile(ctx context.Context, path string) error {
	_, err := t.tx.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.DeleteFile %s: %w", path, err)
	}
	return nil
}

// NodeExists lets storeExtractionResult check an unresolved ref's owner before
// inserting, since from_node_id has an FK to nodes(id).
func (t *Tx) NodeExists(ctx context.Context, id string) (bool, error) {
	var exists int
	err := t.tx.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE id = ? LIMIT 1", id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("codeintel/db: Tx.NodeExists %s: %w", id, err)
	}
	return true, nil
}

func (t *Tx) UpsertNodeAt(ctx context.Context, n types.Node, updatedAt int64) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO nodes
		  (id, kind, name, qualified_name, file_path, language,
		   start_line, end_line, start_column, end_column,
		   docstring, signature, visibility,
		   is_exported, is_async, is_static, is_const,
		   decorators, type_parameters, metadata, updated_at)
		VALUES
		  (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ID, string(n.Kind), n.Name, n.QualifiedName, n.FilePath, string(n.Language),
		n.StartLine, n.EndLine, n.StartColumn, n.EndColumn,
		n.Docstring, n.Signature, n.Visibility,
		boolToInt(n.IsExported), boolToInt(n.IsAsync), boolToInt(n.IsStatic), boolToInt(n.IsConst),
		rawOrNil(n.Decorators), rawOrNil(n.TypeParameters), rawOrNil(n.Metadata),
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.UpsertNodeAt %s: %w", n.ID, err)
	}
	return nil
}

// InsertEdge returns the new ROWID.
func (t *Tx) InsertEdge(ctx context.Context, e types.Edge) (int64, error) {
	res, err := t.tx.ExecContext(ctx, `
		INSERT INTO edges (source, target, kind, metadata, line, col, provenance)
		VALUES (?,?,?,?,?,?,?)`,
		e.Source, e.Target, string(e.Kind),
		rawOrNil(e.Metadata), e.Line, e.Column, nullableString(e.Provenance),
	)
	if err != nil {
		return 0, fmt.Errorf("codeintel/db: Tx.InsertEdge %s→%s: %w", e.Source, e.Target, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("codeintel/db: Tx.InsertEdge LastInsertId: %w", err)
	}
	return id, nil
}

func (t *Tx) UpsertFile(ctx context.Context, f types.FileRecord) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO files
		  (path, content_hash, language, size, modified_at, indexed_at, node_count, errors)
		VALUES (?,?,?,?,?,?,?,?)`,
		f.Path, f.ContentHash, string(f.Language), f.Size,
		f.ModifiedAt, f.IndexedAt,
		f.NodeCount, rawOrNil(f.Errors),
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.UpsertFile %s: %w", f.Path, err)
	}
	return nil
}
