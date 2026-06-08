package db_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeinteldb "github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
)

// tempDB opens a fresh DB in the project's tmp/ dir and returns it plus a
// cleanup func. Each test gets its own file so tests don't share state.
func tempDB(t *testing.T) (*codeinteldb.DB, func()) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "tmp", "codeintel-db-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s.db", t.Name()))
	os.Remove(path) // start clean
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")

	d, err := codeinteldb.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return d, func() {
		d.Close()
		os.Remove(path)
		os.Remove(path + "-wal")
		os.Remove(path + "-shm")
	}
}

// ---------------------------------------------------------------------------
// Pragma tests — FK on, WAL mode
// ---------------------------------------------------------------------------

// TestPragmaForeignKeysOn proves that the single connection has foreign_keys=1.
// This is load-bearing: if FK is off, cascade deletes silently do nothing.
func TestPragmaForeignKeysOn(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	var fk int
	if err := d.DB().QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fk)
	}
}

// TestPragmaWAL proves WAL journal mode is active on the single connection.
func TestPragmaWAL(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	var mode string
	if err := d.DB().QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", mode)
	}
}

// ---------------------------------------------------------------------------
// Schema presence tests
// ---------------------------------------------------------------------------

// schemaObjects returns a map[name]sql of every object in sqlite_master
// (tables, virtual tables, triggers, indexes) — keyed by object name.
func schemaObjects(t *testing.T, d *codeinteldb.DB) map[string]string {
	t.Helper()
	rows, err := d.DB().QueryContext(context.Background(),
		"SELECT name, COALESCE(sql,'') FROM sqlite_master WHERE type IN ('table','trigger','index')")
	if err != nil {
		t.Fatalf("sqlite_master query: %v", err)
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var name, sql string
		if err := rows.Scan(&name, &sql); err != nil {
			t.Fatalf("scan: %v", err)
		}
		m[name] = sql
	}
	return m
}

// TestSchemaTablesPresent asserts every required table is in the schema.
func TestSchemaTablesPresent(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	objs := schemaObjects(t, d)

	required := []string{
		"nodes",
		"edges",
		"files",
		"unresolved_refs",
		"project_metadata",
		"nodes_fts", // FTS5 virtual table
	}
	for _, name := range required {
		if _, ok := objs[name]; !ok {
			t.Errorf("missing table/vtable: %q", name)
		}
	}
}

// TestSchemaTriggersPresent asserts the three FTS5 sync triggers exist.
func TestSchemaTriggersPresent(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	objs := schemaObjects(t, d)

	for _, name := range []string{"nodes_ai", "nodes_ad", "nodes_au"} {
		if _, ok := objs[name]; !ok {
			t.Errorf("missing trigger: %q", name)
		}
	}
}

// TestSchemaIndexes asserts only the intended indexes exist and the narrow
// idx_edges_source / idx_edges_target are absent (appendix A: v4 dropped them).
func TestSchemaIndexes(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	objs := schemaObjects(t, d)

	wantIndexes := []string{
		"idx_edges_kind",
		"idx_edges_source_kind",
		"idx_edges_target_kind",
		"idx_edges_provenance",
		"idx_nodes_lower_name",
	}
	for _, name := range wantIndexes {
		if _, ok := objs[name]; !ok {
			t.Errorf("missing index: %q", name)
		}
	}

	// These narrow indexes must NOT be present (appendix A: composites cover
	// source-only / target-only via left-prefix; v4 dropped the narrow ones).
	forbiddenIndexes := []string{"idx_edges_source", "idx_edges_target"}
	for _, name := range forbiddenIndexes {
		if _, ok := objs[name]; ok {
			t.Errorf("forbidden index present: %q (appendix A says v4 dropped it)", name)
		}
	}
}

// TestSchemaEdgesFKCascade asserts the edges table has ON DELETE CASCADE on
// both source and target FKs by exercising the cascade (see TestCascadeDelete).
// Here we just verify the DDL contains "ON DELETE CASCADE".
func TestSchemaEdgesFKCascade(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	objs := schemaObjects(t, d)
	sql, ok := objs["edges"]
	if !ok {
		t.Fatal("edges table missing from schema")
	}

	// Both FKs (source, target) must reference ON DELETE CASCADE.
	// Count occurrences — expect at least 2 (one per FK column).
	count := 0
	needle := "ON DELETE CASCADE"
	for i := 0; i+len(needle) <= len(sql); i++ {
		if sql[i:i+len(needle)] == needle {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 ON DELETE CASCADE in edges DDL, found %d\nDDL: %s", count, sql)
	}
}

// TestSchemaVersionRow asserts that Open/Init writes the schema_version row
// in project_metadata.
func TestSchemaVersionRow(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	var val string
	err := d.DB().QueryRowContext(context.Background(),
		"SELECT value FROM project_metadata WHERE key='schema_version'").Scan(&val)
	if err != nil {
		t.Fatalf("schema_version row missing: %v", err)
	}
	if val == "" {
		t.Error("schema_version value is empty")
	}
}

// ---------------------------------------------------------------------------
// Cascade delete test
// ---------------------------------------------------------------------------

// TestCascadeDelete inserts a node and an edge referencing it, deletes the
// node, and asserts the edge is gone. Proves FK CASCADE is active on the
// single connection (if foreign_keys were off, the edge would remain).
func TestCascadeDelete(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	ctx := context.Background()
	db := d.DB()

	const insertNode = `INSERT INTO nodes
		(id, kind, name, qualified_name, file_path, language,
		 start_line, end_line, start_column, end_column,
		 is_exported, is_async, is_static, is_const, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	// Insert a parent node and a child node.
	_, err := db.ExecContext(ctx, insertNode,
		"function:parent", "function", "parent", "pkg::parent",
		"src/a.go", "go", 1, 10, 0, 0,
		1, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("insert parent node: %v", err)
	}

	_, err = db.ExecContext(ctx, insertNode,
		"function:child", "function", "child", "pkg::child",
		"src/a.go", "go", 11, 20, 0, 0,
		0, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("insert child node: %v", err)
	}

	// Insert an edge: child calls parent.
	_, err = db.ExecContext(ctx,
		`INSERT INTO edges (source, target, kind) VALUES (?, ?, ?)`,
		"function:child", "function:parent", "calls")
	if err != nil {
		t.Fatalf("insert edge: %v", err)
	}

	// Verify the edge is present.
	var cnt int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM edges WHERE source='function:child'").Scan(&cnt); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 edge before delete, got %d", cnt)
	}

	// Delete the target node (parent). The FK ON DELETE CASCADE should remove
	// the edge automatically because target references nodes(id).
	if _, err := db.ExecContext(ctx,
		"DELETE FROM nodes WHERE id='function:parent'"); err != nil {
		t.Fatalf("delete parent node: %v", err)
	}

	// Edge must be gone.
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM edges WHERE source='function:child'").Scan(&cnt); err != nil {
		t.Fatalf("count edges after delete: %v", err)
	}
	if cnt != 0 {
		t.Errorf("cascade delete failed: edge still present (count=%d); FK is likely OFF", cnt)
	}
}

// ---------------------------------------------------------------------------
// FTS sync tests
// ---------------------------------------------------------------------------

// insertTestNode is a helper for FTS tests.
func insertTestNode(t *testing.T, d *codeinteldb.DB, id, name string) {
	t.Helper()
	_, err := d.DB().ExecContext(context.Background(),
		`INSERT INTO nodes
			(id, kind, name, qualified_name, file_path, language,
			 start_line, end_line, start_column, end_column,
			 is_exported, is_async, is_static, is_const, updated_at)
			VALUES (?, 'function', ?, ?, 'src/x.go', 'go', 1, 5, 0, 0, 0, 0, 0, 0, 0)`,
		id, name, "pkg::"+name)
	if err != nil {
		t.Fatalf("insert node %s: %v", id, err)
	}
}

// TestFTSSyncInsert inserts a node and proves it is findable in nodes_fts.
// This validates the nodes_ai (after-insert) trigger.
func TestFTSSyncInsert(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	insertTestNode(t, d, "function:myFunc", "myUniqueFunc")

	var cnt int
	if err := d.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM nodes_fts WHERE nodes_fts MATCH ?",
		"myUniqueFunc").Scan(&cnt); err != nil {
		t.Fatalf("FTS query: %v", err)
	}
	if cnt == 0 {
		t.Error("FTS did not index the inserted node (nodes_ai trigger not firing)")
	}
}

// TestFTSSyncDelete inserts a node, confirms FTS has it, deletes it, and
// confirms FTS no longer returns it. Validates the nodes_ad (after-delete)
// trigger with the mandatory ('delete', …) sentinel.
func TestFTSSyncDelete(t *testing.T) {
	d, cleanup := tempDB(t)
	defer cleanup()

	insertTestNode(t, d, "function:deleteMe", "deleteMeFunc")

	// Confirm FTS finds it before delete.
	var cnt int
	if err := d.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM nodes_fts WHERE nodes_fts MATCH ?",
		"deleteMeFunc").Scan(&cnt); err != nil {
		t.Fatalf("FTS pre-delete query: %v", err)
	}
	if cnt == 0 {
		t.Fatal("FTS did not index the node before delete (pre-condition failed)")
	}

	// Delete the node.
	if _, err := d.DB().ExecContext(context.Background(),
		"DELETE FROM nodes WHERE id='function:deleteMe'"); err != nil {
		t.Fatalf("delete node: %v", err)
	}

	// FTS must not find it after delete.
	if err := d.DB().QueryRowContext(context.Background(),
		"SELECT count(*) FROM nodes_fts WHERE nodes_fts MATCH ?",
		"deleteMeFunc").Scan(&cnt); err != nil {
		t.Fatalf("FTS post-delete query: %v", err)
	}
	if cnt != 0 {
		t.Errorf("FTS still finds deleted node (nodes_ad sentinel trigger not working, count=%d)", cnt)
	}
}

// ---------------------------------------------------------------------------
// Idempotent init test
// ---------------------------------------------------------------------------

// TestIdempotentInit opens the same DB path twice and asserts no error.
// The schema uses IF NOT EXISTS throughout, so re-running init must be a no-op.
func TestIdempotentInit(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "tmp", "codeintel-db-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	path := filepath.Join(dir, "idempotent_init_test.db")
	os.Remove(path)
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")
	defer func() {
		os.Remove(path)
		os.Remove(path + "-wal")
		os.Remove(path + "-shm")
	}()

	// First open.
	d1, err := codeinteldb.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	d1.Close()

	// Second open on the same path — must succeed.
	d2, err := codeinteldb.Open(path)
	if err != nil {
		t.Fatalf("second Open (idempotent init): %v", err)
	}
	d2.Close()
}
