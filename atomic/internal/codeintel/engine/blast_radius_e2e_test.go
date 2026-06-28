package engine_test

// Blast-radius end-to-end tests over the committed noorm LLM-memory fixtures.
//
// These tests index only the sql/ subtree of each fixture into a temporary DB
// and assert that GetImpactRadius("Memory") contains a representative
// dependent of each SQL object kind (view, procedure, function, junction table).
// They are executable proof that changing the Memory table breaks these objects.
//
// The fixtures themselves are never modified: NewWithDBPath writes the index to
// a t.TempDir() path so the fixture tree stays clean.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// repoRoot resolves the repo root from the test file location.
// The engine package is 4 directories below the repo root:
//
//	<repo>/atomic/internal/codeintel/engine/
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../../.."))
}

// runBlastRadiusTest is the shared fixture-indexing + impact-assertion body.
// fixtureName is the subdirectory name under scripts/code-eval/fixtures/.
// expectedDependents maps a human-readable description of WHY the object
// is in the blast radius to the object's name (case-insensitive).
func runBlastRadiusTest(t *testing.T, fixtureName string, expectedDependents map[string]string) {
	t.Helper()

	sqlRoot := filepath.Join(repoRoot(), "scripts", "code-eval", "fixtures", fixtureName, "sql")

	// Gracefully skip if the fixture has not been committed.
	if !pathExists(sqlRoot) {
		t.Skipf("fixture sql dir not found, skipping: %s", sqlRoot)
		return
	}

	eng, err := engine.NewWithDBPath(sqlRoot, filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatalf("NewWithDBPath: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()

	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		t.Fatalf("ResolveReferences: %v", err)
	}

	// Locate the Memory table node. The name is quoted/bracketed in SQL but the
	// extractor stores just the identifier, so match case-insensitively.
	tables, err := eng.GetNodesByKind(ctx, types.NodeKindTable)
	if err != nil {
		t.Fatalf("GetNodesByKind(table): %v", err)
	}
	var memory types.Node
	var found bool
	for _, n := range tables {
		if strings.EqualFold(n.Name, "memory") {
			memory = n
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("table 'Memory' not found in DB — extraction or indexing failed")
	}

	// GetImpactRadius(Memory, depth=3): every SQL object that breaks if you
	// change the Memory table schema.
	sg, err := eng.GetImpactRadius(ctx, memory.ID, 3)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}

	// Build a name → node map for efficient membership checks (case-insensitive).
	// Collect all names for failure output.
	nameSet := make(map[string]types.Node, len(sg.Nodes))
	for _, n := range sg.Nodes {
		nameSet[strings.ToLower(n.Name)] = n
	}

	// Fail loud if the impact graph is trivially small — that signals a
	// regression in extraction or resolution, not a real empty radius.
	if len(sg.Nodes) < 10 {
		names := make([]string, 0, len(nameSet))
		for k := range nameSet {
			names = append(names, k)
		}
		t.Errorf("impact graph too small: got %d nodes, want >= 10 (regression?)\n  nodes: %v",
			len(sg.Nodes), names)
	}

	// Assert each expected dependent is present.
	for reason, name := range expectedDependents {
		if _, ok := nameSet[strings.ToLower(name)]; !ok {
			names := make([]string, 0, len(nameSet))
			for k := range nameSet {
				names = append(names, k)
			}
			t.Errorf("missing dependent %q from blast radius\n  reason: %s\n  full set (%d): %v",
				name, reason, len(nameSet), names)
		}
	}
}

// pathExists returns true when path exists on disk.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ---------------------------------------------------------------------------
// PostgreSQL blast radius
// ---------------------------------------------------------------------------

// TestBlastRadiusPostgres indexes the noorm-llm-memory-postgres sql/ subtree
// and verifies the blast radius of the Memory table covers all expected
// dependent SQL object kinds.
func TestBlastRadiusPostgres(t *testing.T) {
	runBlastRadiusTest(t, "noorm-llm-memory-postgres", map[string]string{
		// Changing Memory's columns breaks vw_Memory (SELECT * FROM Memory in view body).
		"vw_Memory selects from Memory": "vw_Memory",
		// fn_MemoryConfidence queries Memory to count confidence booleans; any column
		// change or rename breaks the function's SELECT.
		"fn_MemoryConfidence queries Memory": "fn_MemoryConfidence",
		// sp_Memory_Update writes Memory (UPDATE Memory SET …); schema changes
		// invalidate the UPDATE column list.
		"sp_Memory_Update writes Memory": "sp_Memory_Update",
		// Memory_Tag has a FK REFERENCES Memory(memory_id); dropping or renaming
		// the PK column breaks the FK constraint.
		"Memory_Tag FK references Memory.memory_id": "Memory_Tag",
	})
}

// ---------------------------------------------------------------------------
// SQL Server blast radius
// ---------------------------------------------------------------------------

// TestBlastRadiusMSSQL indexes the noorm-llm-memory-mssql sql/ subtree and
// verifies the blast radius of the Memory table covers all expected dependent
// SQL object kinds.
func TestBlastRadiusMSSQL(t *testing.T) {
	runBlastRadiusTest(t, "noorm-llm-memory-mssql", map[string]string{
		// Changing Memory's columns breaks vw_Memory (SELECT * FROM Memory in view body).
		"vw_Memory selects from Memory": "vw_Memory",
		// fn_MemoryConfidence queries Memory to count confidence booleans; any column
		// change or rename breaks the function's SELECT.
		"fn_MemoryConfidence queries Memory": "fn_MemoryConfidence",
		// sp_Memory_Update writes Memory (UPDATE Memory SET …); schema changes
		// invalidate the UPDATE column list.
		"sp_Memory_Update writes Memory": "sp_Memory_Update",
		// Memory_Tag has a FK reference to Memory; changing the PK breaks the FK.
		"Memory_Tag FK references Memory": "Memory_Tag",
	})
}
