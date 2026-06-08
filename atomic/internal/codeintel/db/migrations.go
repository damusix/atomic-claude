package db

// Migration machinery for the code-intelligence DB.
//
// # Design
//
// schema_versions is a ledger of every applied migration: one row per version
// with the Unix timestamp at which it was applied.  It is created IF NOT EXISTS
// inside Migrate so the migration infra is self-contained.
//
// The production migrations slice holds an ordered list of migrations above the
// baseline.  Version 1 is the baseline (the CP3 schema built by runSchema); it
// is recorded as already-applied on a fresh DB — no DDL is re-run.  Future
// schema changes land here as additional entries.
//
// # Version source reconciliation
//
// schema_versions is the authoritative applied-version ledger.
// project_metadata.schema_version remains as a human-readable marker (kept in
// sync by Migrate after each successful run so callers that read the metadata
// table still see the correct value).  The two sources never contradict: the
// metadata value is derived from the ledger, not independently maintained.
//
// # Seam for testing
//
// MigrateWith(ctx, []Migration) is the low-level runner; it accepts an explicit
// migration list and is exported so tests can inject synthetic migrations without
// touching the production list.  Migrate(ctx) is the production entry point: it
// calls MigrateWith with the package-level migrations slice.

import (
	"context"
	"fmt"
)

// Migration describes a single forward-only schema change.
//
// Version must be a monotonically increasing positive integer.
// Up is a SQL statement (or semicolon-separated statements) to execute inside a
// transaction.  A non-nil error from Up causes the transaction to roll back and
// stops the runner.
type Migration struct {
	Version int
	Up      string
}

// migrations is the ordered list of schema changes above the baseline (v1).
// The baseline itself is NOT listed here — it is the schema built by runSchema
// and is recorded as already-applied on every fresh DB.
//
// Add future migrations here in ascending version order.
var migrations = []Migration{
	{
		// v2 (EE2 — call-argument capture): add arguments TEXT column to
		// unresolved_refs. NULL default means existing rows are unaffected.
		// Populated by the extractor for call_expression sites (string-literal args
		// only, in positional order) to enable event-emitter / rn-event-channel
		// synthesizers to correlate .on('event', fn) <-> .emit('event') by event name.
		Version: 2,
		Up:      `ALTER TABLE unresolved_refs ADD COLUMN arguments TEXT`,
	},
}

// createSchemaVersionsTable creates the schema_versions ledger if it does not
// already exist. Called once at the start of every Migrate / MigrateWith run.
const createSchemaVersionsSQL = `
CREATE TABLE IF NOT EXISTS schema_versions (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
)`

// Migrate applies any pending migrations from the production migrations slice
// and returns an error if any migration fails (the failing migration is rolled
// back; already-applied ones are not re-applied).
//
// Wired into Open() after runSchema so every Open() call brings the DB current.
func (d *DB) Migrate(ctx context.Context) error {
	return d.MigrateWith(ctx, migrations)
}

// MigrateWith is the low-level migration runner.  It accepts an explicit
// migration list so tests can inject synthetic migrations without modifying the
// production list.
//
// Algorithm:
//  1. Ensure schema_versions exists (idempotent CREATE IF NOT EXISTS).
//  2. Record the baseline (v1) if schema_versions is empty — on a fresh DB the
//     schema has already been applied by runSchema, so we only record the marker.
//  3. Read the current max applied version.
//  4. For each migration with version > max: apply its Up SQL inside a
//     transaction, then INSERT the schema_versions row in the same transaction.
//     Stop on first error (that migration is rolled back).
//  5. After all migrations are applied, sync project_metadata.schema_version to
//     the new max version so the human-readable marker stays current.
func (d *DB) MigrateWith(ctx context.Context, list []Migration) error {
	if _, err := d.db.ExecContext(ctx, createSchemaVersionsSQL); err != nil {
		return fmt.Errorf("codeintel/db: create schema_versions: %w", err)
	}

	// Seed the baseline row if schema_versions is empty.
	var cnt int
	if err := d.db.QueryRowContext(ctx,
		"SELECT count(*) FROM schema_versions").Scan(&cnt); err != nil {
		return fmt.Errorf("codeintel/db: count schema_versions: %w", err)
	}
	if cnt == 0 {
		if _, err := d.db.ExecContext(ctx,
			`INSERT INTO schema_versions (version, applied_at)
			 VALUES (1, strftime('%s','now'))`); err != nil {
			return fmt.Errorf("codeintel/db: seed baseline version: %w", err)
		}
	}

	// Read the current max applied version.
	var current int
	if err := d.db.QueryRowContext(ctx,
		"SELECT MAX(version) FROM schema_versions").Scan(&current); err != nil {
		return fmt.Errorf("codeintel/db: read max version: %w", err)
	}

	// Apply each pending migration in order.
	for _, m := range list {
		if m.Version <= current {
			continue // already applied
		}

		tx, err := d.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("codeintel/db: begin tx for v%d: %w", m.Version, err)
		}

		if _, err := tx.ExecContext(ctx, m.Up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("codeintel/db: apply migration v%d: %w", m.Version, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_versions (version, applied_at)
			 VALUES (?, strftime('%s','now'))`, m.Version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("codeintel/db: record migration v%d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("codeintel/db: commit migration v%d: %w", m.Version, err)
		}

		current = m.Version
	}

	// Sync the human-readable marker in project_metadata.
	if _, err := d.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO project_metadata (key, value, updated_at)
		 VALUES ('schema_version', ?, strftime('%s','now'))`,
		fmt.Sprintf("%d", current)); err != nil {
		return fmt.Errorf("codeintel/db: sync schema_version metadata: %w", err)
	}

	return nil
}
