package db

// Transaction seam for atomic store operations (CP10).
//
// WithTx begins a transaction, calls fn, and commits on success or rolls back
// on any error (including panics via defer). The Tx type exposes only the CRUD
// methods that storeExtractionResult needs — keeping the surface minimal.
//
// The single-connection mandate in db.go (SetMaxOpenConns(1)) means SQLite
// serialises all writes; BEGIN/COMMIT/ROLLBACK is straightforward.

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// Tx wraps a *sql.Tx and exposes the subset of CRUD operations that the
// orchestrator's storeExtractionResult needs. All methods mirror their *DB
// counterparts but execute within the transaction.
type Tx struct {
	tx *sql.Tx
}

// WithTx begins a transaction, calls fn with a *Tx handle, and commits if fn
// returns nil. If fn returns an error (or panics) the transaction is rolled
// back and the error is returned. The defer-rollback pattern is used: ROLLBACK
// after a COMMIT is a no-op in SQLite, so the defer is always safe.
func (d *DB) WithTx(ctx context.Context, fn func(*Tx) error) error {
	sqlTx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("codeintel/db: begin tx: %w", err)
	}
	t := &Tx{tx: sqlTx}
	defer func() {
		// Rollback is a no-op after a successful Commit.
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

// DeleteNodesByFile deletes all nodes with the given file_path within the
// transaction. FK CASCADE removes their edges.
func (t *Tx) DeleteNodesByFile(ctx context.Context, filePath string) error {
	_, err := t.tx.ExecContext(ctx, "DELETE FROM nodes WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.DeleteNodesByFile %s: %w", filePath, err)
	}
	return nil
}

// DeleteFile deletes the file record with the given path within the
// transaction.
func (t *Tx) DeleteFile(ctx context.Context, path string) error {
	_, err := t.tx.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("codeintel/db: Tx.DeleteFile %s: %w", path, err)
	}
	return nil
}

// UpsertNodeAt inserts or replaces a node within the transaction with an
// explicit updatedAt Unix timestamp.
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

// InsertEdge inserts a new edge within the transaction and returns the new row id.
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

// UpsertFile inserts or replaces a file record within the transaction.
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
