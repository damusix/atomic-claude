// Package db opens and initialises the code-intelligence SQLite database.
//
// It pins database/sql to exactly one physical connection. Pooling would break
// two invariants: PRAGMA foreign_keys is per-connection, so a pooled
// connection that skipped it would silently drop ON DELETE CASCADE, and
// busy_timeout must be the first pragma applied.
package db

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"database/sql"

	_ "modernc.org/sqlite" // registers "sqlite" driver; pure Go, CGO_ENABLED=0 safe
)

//go:embed schema.sql
var schemaSQL string

// DB wraps the single-connection database handle.
type DB struct {
	db *sql.DB
}

// Open creates or opens the database at path, applies the pragma sequence,
// runs the schema, and migrates. The caller must Close it.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("codeintel/db: create parent dir: %w", err)
	}

	// A bare file: URI, so every pragma is applied by applyPragmas in a known
	// order. Splitting them between the DSN and exec would make it ambiguous.
	sqldb, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: open: %w", err)
	}

	// One physical connection, kept alive: see the package doc.
	sqldb.SetMaxOpenConns(1)
	sqldb.SetMaxIdleConns(1)

	d := &DB{db: sqldb}
	if err := d.init(context.Background()); err != nil {
		sqldb.Close()
		return nil, err
	}
	return d, nil
}

// DB exposes the handle for direct queries. Callers must not touch the
// connection-pool settings.
func (d *DB) DB() *sql.DB {
	return d.db
}

func (d *DB) Close() error {
	return d.db.Close()
}

// Optimize flushes FTS5 internal state and reclaims WAL space. Call it after
// bulk writes, such as a full index run.
func (d *DB) Optimize(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return fmt.Errorf("codeintel/db: PRAGMA optimize: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		return fmt.Errorf("codeintel/db: PRAGMA wal_checkpoint: %w", err)
	}
	return nil
}

// init runs once during Open. The order is load-bearing: pragmas must precede
// any DDL, and the schema must exist before migrations run against it.
func (d *DB) init(ctx context.Context) error {
	if err := d.applyPragmas(ctx); err != nil {
		return err
	}
	if err := d.runSchema(ctx); err != nil {
		return err
	}
	if err := d.Migrate(ctx); err != nil {
		return err
	}
	return nil
}

// applyPragmas runs the sequence in order. busy_timeout must come first so
// everything after it waits on lock contention instead of failing outright,
// and foreign_keys must land before any DML for CASCADE to hold.
func (d *DB) applyPragmas(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA mmap_size=268435456",
	}
	for _, p := range pragmas {
		if _, err := d.db.ExecContext(ctx, p); err != nil {
			return fmt.Errorf("codeintel/db: %s: %w", p, err)
		}
	}
	return nil
}

// runSchema is idempotent: every statement in schema.sql uses IF NOT EXISTS.
func (d *DB) runSchema(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("codeintel/db: run schema: %w", err)
	}
	return nil
}
