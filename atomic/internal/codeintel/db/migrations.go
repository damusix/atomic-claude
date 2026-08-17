package db

// Migration machinery for the code-intelligence DB.
//
// schema_versions is the authoritative ledger of applied versions, created by
// Migrate itself so the infrastructure is self-contained. Version 1 is the
// baseline runSchema builds and is recorded as already-applied on a fresh DB.
//
// project_metadata.schema_version is only a human-readable marker, derived
// from the ledger after each run, so the two can never contradict.

import (
	"context"
	"fmt"
)

// Migration is one forward-only schema change. Version must increase
// monotonically; Up runs inside a transaction, and its failure rolls that
// migration back and stops the runner.
type Migration struct {
	Version int
	Up      string
}

// migrations holds every schema change above the baseline, in ascending order.
var migrations = []Migration{
	{
		// Positional string-literal call arguments, so the event-emitter
		// synthesizers can pair .on('event', fn) with .emit('event').
		Version: 2,
		Up:      `ALTER TABLE unresolved_refs ADD COLUMN arguments TEXT`,
	},
	{
		// ReferenceName holds only the bare invoked segment, which the name
		// matcher can resolve; callee_expr preserves the full expression
		// ("DeviceEventEmitter.addListener") the callback synthesizers match on.
		Version: 3,
		Up:      `ALTER TABLE unresolved_refs ADD COLUMN callee_expr TEXT`,
	},
}

const createSchemaVersionsSQL = `
CREATE TABLE IF NOT EXISTS schema_versions (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
)`

// Migrate brings the DB current. Open calls it after runSchema.
func (d *DB) Migrate(ctx context.Context) error {
	return d.MigrateWith(ctx, migrations)
}

// MigrateWith takes an explicit list so tests can inject synthetic migrations.
// Each migration's DDL and its ledger row commit together, so a failure leaves
// neither behind.
func (d *DB) MigrateWith(ctx context.Context, list []Migration) error {
	if _, err := d.db.ExecContext(ctx, createSchemaVersionsSQL); err != nil {
		return fmt.Errorf("codeintel/db: create schema_versions: %w", err)
	}

	// An empty ledger means a fresh DB whose baseline runSchema already built,
	// so only the marker row is needed.
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

	var current int
	if err := d.db.QueryRowContext(ctx,
		"SELECT MAX(version) FROM schema_versions").Scan(&current); err != nil {
		return fmt.Errorf("codeintel/db: read max version: %w", err)
	}

	for _, m := range list {
		if m.Version <= current {
			continue
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

	if _, err := d.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO project_metadata (key, value, updated_at)
		 VALUES ('schema_version', ?, strftime('%s','now'))`,
		fmt.Sprintf("%d", current)); err != nil {
		return fmt.Errorf("codeintel/db: sync schema_version metadata: %w", err)
	}

	return nil
}
