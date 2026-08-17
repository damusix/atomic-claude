// Facade tests: lifecycle, an index-resolve-stats end-to-end pass, and one
// assertion per delegating method that it returns the underlying shape.
package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

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

	dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("DB file not found at %s: %v", dbPath, err)
	}
}

// The harness dir is configurable, so under ".pi" the db must land in
// .pi/.atomic-index rather than the hardcoded .claude default.
func TestLifecycle_InitCreatesDB_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	dir := t.TempDir()
	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	if err := e.Init(context.Background()); err != nil {
		t.Fatal("Init:", err)
	}

	dbPath := filepath.Join(dir, ".pi", ".atomic-index", "atomic.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("DB file not found at %s: %v", dbPath, err)
	}
	if got := e.IndexPath(); got != dbPath {
		t.Errorf("IndexPath() = %q, want %q", got, dbPath)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Errorf(".claude should not exist under a .pi harness, stat err=%v", err)
	}
}

// The package-level IndexPath, which doctor's code-index check calls, must
// resolve through the harness dir too.
func TestIndexPath_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	got := engine.IndexPath("/repo")
	want := filepath.Join("/repo", ".pi", ".atomic-index", "atomic.db")
	if got != want {
		t.Errorf("IndexPath = %q, want %q", got, want)
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

	e1, _ := engine.New(dir)
	if err := e1.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	e1.Close()

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

	sg, err := e.GetCallers(ctx, greetID, 1)
	if err != nil {
		t.Fatal("GetCallers:", err)
	}
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
	if tierStr != "fts" && tierStr != "like" && tierStr != "fuzzy" {
		t.Errorf("FindRelevantContext: unexpected tier %q", tierStr)
	}
	if sg.Nodes == nil {
		t.Error("FindRelevantContext: Subgraph.Nodes should not be nil")
	}
}

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

// Realm federation stores the index outside the member repo, so the explicit
// path must win and the default location must stay untouched.
func TestNewWithDBPath_ExplicitPathWritesCorrectLocation(t *testing.T) {
	scanRoot := writeFixture(t) // source tree to index
	dbDir := t.TempDir()        // separate dir — simulates <realm>/.atomic/
	explicitDB := filepath.Join(dbDir, "mykey.db")

	e, err := engine.NewWithDBPath(scanRoot, explicitDB)
	if err != nil {
		t.Fatal("NewWithDBPath:", err)
	}
	defer e.Close()

	ctx := context.Background()
	if err := e.Init(ctx); err != nil {
		t.Fatal("Init:", err)
	}

	if _, err := os.Stat(explicitDB); err != nil {
		t.Fatalf("DB not found at explicit path %s: %v", explicitDB, err)
	}

	defaultDB := filepath.Join(scanRoot, ".claude", ".atomic-index", "atomic.db")
	if _, err := os.Stat(defaultDB); err == nil {
		t.Fatalf("DB should NOT exist at default scan-root path %s", defaultDB)
	}

	if err := e.IndexAll(ctx); err != nil {
		t.Fatal("IndexAll:", err)
	}
	stats, err := e.GetStats(ctx)
	if err != nil {
		t.Fatal("GetStats:", err)
	}
	if stats.FileCount == 0 {
		t.Error("expected indexed files in explicit-path DB, got 0")
	}
}

// The explicit-path seam must not have moved New's default location.
func TestNewWithDBPath_DefaultUnchanged(t *testing.T) {
	dir := t.TempDir()
	e, err := engine.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	if err := e.Init(context.Background()); err != nil {
		t.Fatal("Init:", err)
	}

	defaultDB := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")
	if _, err := os.Stat(defaultDB); err != nil {
		t.Fatalf("DB not found at default path %s: %v", defaultDB, err)
	}
}

// Two files, one indexed: the other must stay absent from the graph, which is
// what a silent fallback to IndexAll would break.
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

	greeterAbs := filepath.Join(dir, "greeter.go")
	if err := e.IndexFiles(ctx, []string{greeterAbs}); err != nil {
		t.Fatal("IndexFiles:", err)
	}

	greeterNodes, err := e.GetNodesByName(ctx, "Greet", "")
	if err != nil {
		t.Fatal("GetNodesByName Greet:", err)
	}
	if len(greeterNodes) == 0 {
		t.Error("expected nodes for Greet (in greeter.go) after IndexFiles([greeter.go])")
	}

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
