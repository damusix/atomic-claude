package db_test

// Migration machinery tests (CP4).
//
// These tests prove:
//   - Fresh DB after Open has a schema_versions row for the baseline (v1).
//   - Calling Migrate() again is a no-op (idempotent).
//   - A synthetic pending migration (v2) applies exactly once and is recorded.
//   - Re-running after v2 is applied is a no-op.
//   - A failing migration rolls back: no schema_versions row written, DDL reverted.

import (
	"context"
	"database/sql"
	"testing"

	codeinteldb "github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
)

// countSchemaVersionRows returns the number of rows in schema_versions.
func countSchemaVersionRows(t *testing.T, d *codeinteldb.DB) int {
	t.Helper()
	var n int
	err := d.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM schema_versions").Scan(&n)
	if err != nil {
		t.Fatalf("count schema_versions: %v", err)
	}
	return n
}

// maxAppliedVersion returns the MAX(version) in schema_versions, or 0 if empty.
func maxAppliedVersion(t *testing.T, d *codeinteldb.DB) int {
	t.Helper()
	var v sql.NullInt64
	err := d.DB().QueryRowContext(context.Background(),
		"SELECT MAX(version) FROM schema_versions").Scan(&v)
	if err != nil {
		t.Fatalf("max schema_versions: %v", err)
	}
	if !v.Valid {
		return 0
	}
	return int(v.Int64)
}

// hasColumn returns true if table has a column with the given name.
func hasColumn(t *testing.T, d *codeinteldb.DB, table, column string) bool {
	t.Helper()
	rows, err := d.DB().QueryContext(context.Background(),
		"SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan pragma_table_info: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

// TestMigrationBaselineRecorded proves that after Open() the schema_versions
// table exists, contains a row for version 1 (the baseline), and the max
// applied version equals the number of production migrations (baseline + all
// registered forward migrations).
// WHY: the runner must record the baseline on every fresh DB so the ledger is
// authoritative from the first Open, not only after a forward migration.
func TestMigrationBaselineRecorded(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	// The production migrations slice currently has v2 (EE2 arguments column) and
	// v3 (callee_expr column), so a fresh DB records v1 (baseline) + v2 + v3 = 3 rows.
	// Update this comment and the constants below when new migrations are added.
	const wantRows = 3   // baseline (v1) + v2 + v3
	const wantMaxVer = 3 // highest registered production migration

	n := countSchemaVersionRows(t, d)
	if n != wantRows {
		t.Errorf("expected %d schema_versions rows, got %d", wantRows, n)
	}

	v := maxAppliedVersion(t, d)
	if v != wantMaxVer {
		t.Errorf("expected max(version)=%d, got %d", wantMaxVer, v)
	}
}

// TestMigrateIdempotentNoOp proves that calling Migrate() on a fresh DB (which
// already has the baseline recorded) is a no-op: no new rows, no error.
// WHY: Open() wires Migrate() in, so a second call must not duplicate the
// baseline row or fail.
func TestMigrateIdempotentNoOp(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	// baseline is already recorded by Open().
	before := countSchemaVersionRows(t, d)

	// Calling Migrate() (production list, just baseline) again must be a no-op.
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	after := countSchemaVersionRows(t, d)
	if after != before {
		t.Errorf("idempotent Migrate added rows: before=%d after=%d", before, after)
	}
}

// TestMigrateSyntheticPendingAppliesOnce injects a synthetic v99 migration
// (adds a column to the files table) via MigrateWith, runs it, asserts:
//   - the column exists
//   - schema_versions has a v99 row
//
// Then runs the same migration list again and asserts it is a no-op.
// WHY: this is the durable CP4 contract — a pending migration applies exactly
// once and is recorded; the runner is idempotent above the recorded version.
// v99 is used instead of v2 since the production migrations slice now contains
// a real v2 (EE2 arguments column). Synthetic test migrations must use version
// numbers above the current production maximum to avoid collisions.
func TestMigrateSyntheticPendingAppliesOnce(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Use v99 — above the current production maximum (v2) to avoid collisions.
	v99 := codeinteldb.Migration{
		Version: 99,
		Up:      `ALTER TABLE files ADD COLUMN test_col TEXT`,
	}

	// Column must NOT exist before migration.
	if hasColumn(t, d, "files", "test_col") {
		t.Fatal("test_col already present before migration (pre-condition broken)")
	}

	// Snapshot row count after production migrations have run (via Open).
	nBefore := countSchemaVersionRows(t, d)

	// Apply via test seam.
	if err := d.MigrateWith(ctx, []codeinteldb.Migration{v99}); err != nil {
		t.Fatalf("MigrateWith v99: %v", err)
	}

	// Column must exist after migration.
	if !hasColumn(t, d, "files", "test_col") {
		t.Error("test_col not present after migration (DDL not applied)")
	}

	// schema_versions must have a v99 row.
	v := maxAppliedVersion(t, d)
	if v != 99 {
		t.Errorf("expected max(version)=99 after migration, got %d", v)
	}

	nAfter := countSchemaVersionRows(t, d)
	if nAfter != nBefore+1 {
		t.Errorf("expected %d schema_versions rows after v99, got %d", nBefore+1, nAfter)
	}

	// Run again — must be a no-op (idempotent above recorded version).
	if err := d.MigrateWith(ctx, []codeinteldb.Migration{v99}); err != nil {
		t.Fatalf("second MigrateWith v99 (idempotent check): %v", err)
	}

	afterIdem := countSchemaVersionRows(t, d)
	if afterIdem != nAfter {
		t.Errorf("idempotent re-run added rows: before=%d after=%d", nAfter, afterIdem)
	}
}

// TestMigrateFailingMigrationRollsBack proves that a migration whose DDL fails
// (intentionally bad SQL) leaves no schema_versions row and does not commit
// any partial DDL.
// WHY: a partial migration that records no version leaves the DB in a corrupt
// state. The runner must wrap each migration in a transaction and roll back on
// any error so the DB stays consistent.
// Uses v99 (above the current production maximum of v2) so the runner actually
// attempts to apply it — a version <= current max is skipped, not attempted.
func TestMigrateFailingMigrationRollsBack(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Snapshot current state (after production migrations ran via Open).
	before := countSchemaVersionRows(t, d)
	wantMax := maxAppliedVersion(t, d) // should be 2 after v2 production migration

	// A migration with intentionally invalid SQL, version above current max.
	bad := codeinteldb.Migration{
		Version: 99,
		Up:      `ALTER TABLE nonexistent_table_xyz ADD COLUMN foo TEXT`,
	}

	err := d.MigrateWith(ctx, []codeinteldb.Migration{bad})
	if err == nil {
		t.Fatal("expected error from failing migration, got nil")
	}

	// No new schema_versions row must have been written.
	after := countSchemaVersionRows(t, d)
	if after != before {
		t.Errorf("failing migration wrote a schema_versions row: before=%d after=%d", before, after)
	}

	// Max version must still equal the pre-attempt max (not the failed v99).
	v := maxAppliedVersion(t, d)
	if v != wantMax {
		t.Errorf("expected max(version)=%d after rollback, got %d", wantMax, v)
	}
}
