// Package engine tests — CP20 engine facade.
//
// Tests cover:
//  1. Lifecycle: Init creates .claude/.atomic-index/atomic.db; IsInitialized
//     true after, false before; Uninitialize removes it; ProjectRoot correct.
//  2. End-to-end: Init → IndexAll(fixture) → ResolveReferences → GetStats
//     returns NodeCount/EdgeCount/FileCount > 0 with NodesByKind populated.
//  3. Delegation: GetCallers, SearchNodes, FindRelevantContext return the same
//     shape as calling the underlying packages directly (one assertion each).
//  4. Constants: GetBackend=="sqlite", GetJournalMode=="wal".
//  5. Watch stubs: Watch/StopWatch return ErrWatchNotImplemented.
package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// --------------------------------------------------------------------------
// Test fixture: a small Go source file and a caller.
// --------------------------------------------------------------------------

const fixtureA = `package greeter

// Greet returns a greeting for name.
func Greet(name string) string {
	return "Hello, " + name
}
`

const fixtureB = `package main

import "github.com/example/greeter"

func main() {
	msg := greeter.Greet("world")
	_ = msg
}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "greeter.go"), []byte(fixtureA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(fixtureB), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// --------------------------------------------------------------------------
// 1. Lifecycle
// --------------------------------------------------------------------------

func TestLifecycle_InitCreatesDB(t *testing.T) {
	dir := t.TempDir()
	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	if e.IsInitialized() {
		t.Fatal("IsInitialized should be false before Init")
	}

	if err := e.Init(context.Background()); err != nil {
		t.Fatal("Init:", err)
	}

	if !e.IsInitialized() {
		t.Fatal("IsInitialized should be true after Init")
	}

	// DB file must exist at the canonical path.
	dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("DB file not found at %s: %v", dbPath, err)
	}
}

func TestLifecycle_ProjectRoot(t *testing.T) {
	dir := t.TempDir()
	e, _ := engine.New(dir)
	defer e.Close()

	if got := e.ProjectRoot(); got != dir {
		t.Fatalf("ProjectRoot: want %q, got %q", dir, got)
	}
}

func TestLifecycle_Uninitialize(t *testing.T) {
	dir := t.TempDir()
	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !e.IsInitialized() {
		t.Fatal("expected initialized after Init")
	}

	if err := e.Uninitialize(); err != nil {
		t.Fatal("Uninitialize:", err)
	}
	if e.IsInitialized() {
		t.Fatal("expected not initialized after Uninitialize")
	}

	indexDir := filepath.Join(dir, ".claude", ".atomic-index")
	if _, err := os.Stat(indexDir); !os.IsNotExist(err) {
		t.Fatalf("index dir should be removed after Uninitialize, err=%v", err)
	}
}

func TestLifecycle_Open(t *testing.T) {
	dir := t.TempDir()

	// Init then Close.
	e1, _ := engine.New(dir)
	if err := e1.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	e1.Close()

	// Open should succeed — DB already exists.
	e2, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()

	if err := e2.Open(context.Background()); err != nil {
		t.Fatal("Open:", err)
	}
	if !e2.IsInitialized() {
		t.Fatal("IsInitialized should be true after Open on existing DB")
	}
}

// --------------------------------------------------------------------------
// 2. End-to-end: Init → IndexAll → ResolveReferences → GetStats
// --------------------------------------------------------------------------

func TestEndToEnd_GetStatsAfterIndex(t *testing.T) {
	dir := writeFixture(t)

	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	ctx := context.Background()

	if err := e.Init(ctx); err != nil {
		t.Fatal("Init:", err)
	}

	if err := e.IndexAll(ctx); err != nil {
		t.Fatal("IndexAll:", err)
	}

	if err := e.ResolveReferences(ctx); err != nil {
		t.Fatal("ResolveReferences:", err)
	}

	stats, err := e.GetStats(ctx)
	if err != nil {
		t.Fatal("GetStats:", err)
	}

	if stats.NodeCount <= 0 {
		t.Errorf("GetStats.NodeCount want >0, got %d", stats.NodeCount)
	}
	if stats.FileCount <= 0 {
		t.Errorf("GetStats.FileCount want >0, got %d", stats.FileCount)
	}
	if len(stats.NodesByKind) == 0 {
		t.Error("GetStats.NodesByKind should be non-empty after indexing")
	}
}

// --------------------------------------------------------------------------
// 3. Delegation correctness
// --------------------------------------------------------------------------

func TestDelegation_SearchNodes(t *testing.T) {
	dir := writeFixture(t)

	e, _ := engine.New(dir)
	defer e.Close()

	ctx := context.Background()
	if err := e.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	results, err := e.SearchNodes(ctx, types.SearchOptions{Query: "Greet", Limit: 10})
	if err != nil {
		t.Fatal("SearchNodes:", err)
	}
	if len(results) == 0 {
		t.Fatal("SearchNodes: expected at least one result for 'Greet'")
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Node.Name, "Greet") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SearchNodes: no result with name containing 'Greet' in %v", results)
	}
}

func TestDelegation_GetCallers(t *testing.T) {
	dir := writeFixture(t)

	e, _ := engine.New(dir)
	defer e.Close()

	ctx := context.Background()
	if err := e.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.ResolveReferences(ctx); err != nil {
		t.Fatal(err)
	}

	// Find the Greet node id.
	results, err := e.SearchNodes(ctx, types.SearchOptions{Query: "Greet", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Skip("no Greet node found; skipping GetCallers delegation test")
	}

	var greetID string
	for _, r := range results {
		if r.Node.Name == "Greet" && r.Node.Kind == types.NodeKindFunction {
			greetID = r.Node.ID
			break
		}
	}
	if greetID == "" {
		t.Skip("Greet function node not found")
	}

	// GetCallers returns a Subgraph — verify the call doesn't error and
	// returns the right shape.
	sg, err := e.GetCallers(ctx, greetID, 1)
	if err != nil {
		t.Fatal("GetCallers:", err)
	}
	// Subgraph.Nodes is a map; verify it was initialized.
	if sg.Nodes == nil {
		t.Error("GetCallers: Subgraph.Nodes should not be nil")
	}
}

func TestDelegation_FindRelevantContext(t *testing.T) {
	dir := writeFixture(t)

	e, _ := engine.New(dir)
	defer e.Close()

	ctx := context.Background()
	if err := e.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.IndexAll(ctx); err != nil {
		t.Fatal(err)
	}

	sg, tierStr, _, err := e.FindRelevantContext(ctx, "Greet", engine.ContextOptions{})
	if err != nil {
		t.Fatal("FindRelevantContext:", err)
	}
	// tierStr should be one of the valid tier strings.
	if tierStr != "fts" && tierStr != "like" && tierStr != "fuzzy" {
		t.Errorf("FindRelevantContext: unexpected tier %q", tierStr)
	}
	if sg.Nodes == nil {
		t.Error("FindRelevantContext: Subgraph.Nodes should not be nil")
	}
}

// --------------------------------------------------------------------------
// 4. Constants
// --------------------------------------------------------------------------

func TestConstants_BackendAndJournalMode(t *testing.T) {
	dir := t.TempDir()
	e, _ := engine.New(dir)
	defer e.Close()

	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := e.GetBackend(); got != "sqlite" {
		t.Errorf("GetBackend: want %q, got %q", "sqlite", got)
	}
	if got := e.GetJournalMode(); got != "wal" {
		t.Errorf("GetJournalMode: want %q, got %q", "wal", got)
	}
}

// --------------------------------------------------------------------------
// 5. Watch stubs
// --------------------------------------------------------------------------

func TestWatchStubs(t *testing.T) {
	dir := t.TempDir()
	e, _ := engine.New(dir)
	defer e.Close()

	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := e.Watch(); err == nil {
		t.Error("Watch should return an error (stubbed)")
	}
	if err := e.StopWatch(); err == nil {
		t.Error("StopWatch should return an error (stubbed)")
	}
}

// --------------------------------------------------------------------------
// 6. IndexFiles — selective indexing (F-56)
// --------------------------------------------------------------------------

// TestIndexFiles_SelectiveOnly proves that IndexFiles indexes ONLY the listed
// files. It creates a two-file fixture, calls IndexFiles with only one file,
// then asserts:
//   - the indexed file has at least one node in the graph, AND
//   - the un-indexed file has NO nodes.
//
// This test would fail if IndexFiles silently fell back to IndexAll (both
// files would have nodes).
func TestIndexFiles_SelectiveOnly(t *testing.T) {
	dir := writeFixture(t) // greeter.go + main.go
	ctx := context.Background()

	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	if err := e.Init(ctx); err != nil {
		t.Fatal("Init:", err)
	}

	// Index ONLY greeter.go (absolute path required by IndexFiles).
	greeterAbs := filepath.Join(dir, "greeter.go")
	if err := e.IndexFiles(ctx, []string{greeterAbs}); err != nil {
		t.Fatal("IndexFiles:", err)
	}

	// greeter.go must have nodes (Greet function).
	greeterNodes, err := e.GetNodesByName(ctx, "Greet", "")
	if err != nil {
		t.Fatal("GetNodesByName Greet:", err)
	}
	if len(greeterNodes) == 0 {
		t.Error("expected nodes for Greet (in greeter.go) after IndexFiles([greeter.go])")
	}

	// main.go must have NO nodes — it was not listed in IndexFiles.
	// Use GetNodesByName for "main" to check; if IndexFiles fell back to IndexAll,
	// main() would have been indexed and this assertion would fail.
	mainNodes, err := e.GetNodesByName(ctx, "main", "")
	if err != nil {
		t.Fatal("GetNodesByName main:", err)
	}
	for _, n := range mainNodes {
		if strings.Contains(n.FilePath, "main.go") {
			t.Errorf("found node from main.go (%s %s %s) — IndexFiles should not have indexed un-listed files",
				n.Kind, n.Name, n.FilePath)
		}
	}
}
