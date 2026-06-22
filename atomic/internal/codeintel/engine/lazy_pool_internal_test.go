package engine

// Internal test (package engine) for the lazy extraction-pool invariant.
//
// WHY this exists: the extraction pool spins up one wazero runtime per CPU and
// compiles every tree-sitter WASM grammar. Measured cost on a real repo:
// ~4.7 s of CPU and ~1.9 GB peak RSS — paid once per pool. Read-only queries
// (search, callers, callees, impact, explore, context) are pure SQLite reads
// and need no parser at all. Before the lazy fix, open() built the pool
// unconditionally, so every single `atomic code search` invocation — and every
// serve/MCP read — paid the full grammar-compile tax to return a DB row.
//
// The pool is observable only as a private field, so this must live in
// package engine. It pins a resource-lifecycle guarantee, not an implementation
// detail: the pool may not be allocated until a method that actually parses
// source (IndexAll / IndexFiles / Sync) needs it.

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

	// Init opens (creates) the DB. It must NOT compile WASM grammars.
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if eng.pool != nil || eng.orch != nil {
		t.Fatal("Init booted the extraction pool — opening the DB must stay parser-free")
	}

	// A read query must stay DB-only.
	if _, err := eng.GetStats(ctx); err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if eng.pool != nil || eng.orch != nil {
		t.Fatal("a read query booted the extraction pool — reads must never touch tree-sitter")
	}

	// Indexing boots the pool + orchestrator on demand.
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if eng.pool == nil || eng.orch == nil {
		t.Fatal("IndexAll did not lazily boot the orchestrator/pool")
	}

	// SkippedFiles must be safe to read after an index.
	_ = eng.SkippedFiles()
}
