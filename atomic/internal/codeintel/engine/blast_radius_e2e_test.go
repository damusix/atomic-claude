package engine_test

// Blast-radius tests over the committed noorm LLM-memory fixtures: executable
// proof that changing the Memory table reaches a dependent of every SQL object
// kind. NewWithDBPath keeps the index in a TempDir so the fixtures stay clean.

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

// repoRoot climbs the four directories from <repo>/atomic/internal/codeintel/engine.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../../.."))
}

// runBlastRadiusTest indexes one fixture and asserts its impact radius.
// expectedDependents maps why an object is in the radius to its name.
func runBlastRadiusTest(t *testing.T, fixtureName string, expectedDependents map[string]string) {
	t.Helper()

	sqlRoot := filepath.Join(repoRoot(), "scripts", "code-eval", "fixtures", fixtureName, "sql")

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

	// SQL quotes and brackets the name, but the extractor stores the bare
	// identifier, so match case-insensitively.
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

	// Everything that breaks if the Memory table's schema changes.
	sg, err := eng.GetImpactRadius(ctx, memory.ID, 3)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}

	nameSet := make(map[string]types.Node, len(sg.Nodes))
	for _, n := range sg.Nodes {
		nameSet[strings.ToLower(n.Name)] = n
	}

	// A trivially small radius means extraction or resolution regressed, not
	// that the fixture genuinely has no dependents.
	if len(sg.Nodes) < 10 {
		names := make([]string, 0, len(nameSet))
		for k := range nameSet {
			names = append(names, k)
		}
		t.Errorf("impact graph too small: got %d nodes, want >= 10 (regression?)\n  nodes: %v",
			len(sg.Nodes), names)
	}

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

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestBlastRadiusPostgres(t *testing.T) {
	runBlastRadiusTest(t, "noorm-llm-memory-postgres", map[string]string{
		"vw_Memory selects from Memory":             "vw_Memory",
		"fn_MemoryConfidence queries Memory":        "fn_MemoryConfidence",
		"sp_Memory_Update writes Memory":            "sp_Memory_Update",
		"Memory_Tag FK references Memory.memory_id": "Memory_Tag",
	})
}

func TestBlastRadiusMSSQL(t *testing.T) {
	runBlastRadiusTest(t, "noorm-llm-memory-mssql", map[string]string{
		"vw_Memory selects from Memory":      "vw_Memory",
		"fn_MemoryConfidence queries Memory": "fn_MemoryConfidence",
		"sp_Memory_Update writes Memory":     "sp_Memory_Update",
		"Memory_Tag FK references Memory":    "Memory_Tag",
	})
}
