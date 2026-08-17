package engine

// Pins a resource-lifecycle guarantee: the extraction pool may not be allocated
// until a method that actually parses source needs it. Booting it costs ~4.7 s
// CPU and ~1.9 GB peak RSS, which no read-only query should ever pay. In package
// engine because the pool is only observable as a private field.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPathDoesNotBootExtractionPool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "greeter.go"),
		[]byte("package greeter\n\nfunc Greet() string { return \"hi\" }\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	eng, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	ctx := context.Background()

	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if eng.pool != nil || eng.orch != nil {
		t.Fatal("Init booted the extraction pool — opening the DB must stay parser-free")
	}

	if _, err := eng.GetStats(ctx); err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if eng.pool != nil || eng.orch != nil {
		t.Fatal("a read query booted the extraction pool — reads must never touch tree-sitter")
	}

	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if eng.pool == nil || eng.orch == nil {
		t.Fatal("IndexAll did not lazily boot the orchestrator/pool")
	}

	_ = eng.SkippedFiles()
}
