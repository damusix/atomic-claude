// Package db opens and initialises the code-intelligence SQLite database.
//
// # Single-connection mandate
//
// Go's database/sql pools connections, which breaks two invariants:
//   - PRAGMA foreign_keys=ON is per-connection; a pooled connection that skips
//     the pragma silently drops ON DELETE CASCADE.
//   - PRAGMA busy_timeout must be the first pragma applied (appendix O).
//
// To enforce a single physical connection:
//   - SetMaxOpenConns(1) — at most one connection is ever opened.
//   - SetMaxIdleConns(1) — the one connection is kept alive (not recycled).
//
// All seven pragmas from appendix O are applied in exact order immediately
// after the connection opens, before any schema DDL.
//
// # Schema
//
// schema.sql is embedded via go:embed and executed idempotently (all
// statements use IF NOT EXISTS). The DB struct exposes the underlying
// *sql.DB for callers that need to execute queries directly.
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

// Open opens (or creates) the SQLite database at path, applies the
// appendix-O pragma sequence in exact order on the single connection, runs
// the embedded schema idempotently, and writes the schema_version row.
//
// The caller must call Close when done.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("codeintel/db: create parent dir: %w", err)
	}

	// Use the bare file: URI so we control every pragma ourselves in order.
	// Appendix O is explicit: busy_timeout FIRST, then the rest. Mixing some
	// into the DSN and some into exec would make the order ambiguous.
	sqldb, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: open: %w", err)
	}

	// Enforce one physical connection — the FK pragma is per-connection; with
	// more than one connection any extra conn silently skips it.
	sqldb.SetMaxOpenConns(1)
	sqldb.SetMaxIdleConns(1)

	d := &DB{db: sqldb}
	if err := d.init(context.Background()); err != nil {
		sqldb.Close()
		return nil, err
	}
	return d, nil
}

// DB returns the underlying *sql.DB. Callers may use it directly for queries
// but must not change the connection-pool settings.
func (d *DB) DB() *sql.DB {
	return d.db
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// Optimize runs PRAGMA optimize and PRAGMA wal_checkpoint(PASSIVE). Call this
// after bulk write operations (e.g. after a full index run) to flush FTS5
// internal state and reclaim WAL space.
func (d *DB) Optimize(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return fmt.Errorf("codeintel/db: PRAGMA optimize: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		return fmt.Errorf("codeintel/db: PRAGMA wal_checkpoint: %w", err)
	}
	return nil
}

// init applies the pragma sequence, runs the schema, and runs migrations.
// Called once during Open.
//
// Order is load-bearing:
//  1. applyPragmas — busy_timeout first; FK on; WAL; etc.
//  2. runSchema    — idempotent CREATE IF NOT EXISTS for all tables/indexes.
//  3. Migrate      — creates schema_versions, seeds baseline, applies any
//     pending migrations, and syncs project_metadata.schema_version.
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

// applyPragmas executes the appendix-O pragma sequence in exact order on the
// single connection. The order is load-bearing:
//  1. busy_timeout — must be first so all subsequent pragmas (and schema DDL)
//     wait on lock contention instead of failing immediately.
//  2. foreign_keys — enables ON DELETE CASCADE; per-connection, so it must be
//     applied before any DML.
//  3. journal_mode=WAL — switches to write-ahead logging.
//  4. synchronous=NORMAL — trades some durability for write throughput.
//  5. cache_size=-64000 — 64 MB page cache (negative = kibibytes).
//  6. temp_store=MEMORY — temp tables in RAM.
//  7. mmap_size=268435456 — 256 MB memory-mapped I/O.
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

// runSchema executes the embedded schema.sql. All statements use IF NOT EXISTS
// so this is idempotent — safe to call on an existing database.
func (d *DB) runSchema(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("codeintel/db: run schema: %w", err)
	}
	return nil
}
