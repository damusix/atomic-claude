package indexer_test

// Orchestrator and sync invariant tests. Each builds a real SQLite DB and
// tree-sitter pool over its own temp dir, exercising the full stack.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newTestPool(t *testing.T) *extraction.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 2})
	if err != nil {
		t.Skipf("tree-sitter pool unavailable (grammar WASM may not be built): %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// initGitRepo initialises a bare git repo in dir so git ls-files works.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		out, err := runCmdBytes(dir, "git", args...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
}

func TestFullIndex(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Write fixture files.
	writeFile(t, dir, "main.go", `package main

func Hello() string {
	return "hello"
}

func main() {
	Hello()
}
`)
	writeFile(t, dir, "util.py", `def add(a, b):
    return a + b
`)
	writeFile(t, dir, "app.vue", `<template><div>Hello</div></template>
<script>
export default {
  name: 'App'
}
</script>
`)
	writeFile(t, dir, "ignored.yaml", "key: value\n")
	writeFile(t, dir, ".gitignore", "*.log\n")

	// Stage and commit so git ls-files returns them.
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Verify file records exist for all files.
	for _, want := range []string{"main.go", "util.py", "app.vue", "ignored.yaml"} {
		fr, err := database.GetFile(ctx, want)
		if err != nil {
			t.Errorf("GetFile(%q): %v", want, err)
			continue
		}
		if fr.Path != want {
			t.Errorf("file path: got %q, want %q", fr.Path, want)
		}
	}

	// main.go should have nodes.
	goNodes, err := database.GetNodesInFile(ctx, "main.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(main.go): %v", err)
	}
	if len(goNodes) == 0 {
		t.Error("main.go: expected at least one node (file node), got 0")
	}
	// Should have the file node + Hello function + main function (at minimum).
	if len(goNodes) < 3 {
		t.Errorf("main.go: expected ≥3 nodes (file+Hello+main), got %d", len(goNodes))
	}

	// util.py should have at least the file node + add function.
	pyNodes, err := database.GetNodesInFile(ctx, "util.py")
	if err != nil {
		t.Fatalf("GetNodesInFile(util.py): %v", err)
	}
	if len(pyNodes) < 2 {
		t.Errorf("util.py: expected ≥2 nodes (file+add), got %d", len(pyNodes))
	}

	// app.vue should have the component node.
	vueNodes, err := database.GetNodesInFile(ctx, "app.vue")
	if err != nil {
		t.Fatalf("GetNodesInFile(app.vue): %v", err)
	}
	if len(vueNodes) == 0 {
		t.Error("app.vue: expected at least one node (component), got 0")
	}
	// Verify a component node is present.
	hasComponent := false
	for _, n := range vueNodes {
		if n.Kind == types.NodeKindComponent {
			hasComponent = true
			break
		}
	}
	if !hasComponent {
		t.Errorf("app.vue: expected a component node, got kinds: %v", nodeKinds(vueNodes))
	}

	// YAML file: no nodes (file-level only), but the file record should exist.
	yamlNodes, err := database.GetNodesInFile(ctx, "ignored.yaml")
	if err != nil {
		t.Fatalf("GetNodesInFile(ignored.yaml): %v", err)
	}
	if len(yamlNodes) != 0 {
		t.Errorf("ignored.yaml: expected 0 nodes (file-level only), got %d", len(yamlNodes))
	}
}

// TestSyncPrunesDeletedFiles: a deleted file vanishes from scanFiles, so only
// an explicit prune can reclaim its rows — otherwise the index keeps answering
// with symbols and call edges from code that no longer exists. The surviving
// file must come through untouched; pruning is scoped to vanished paths.
func TestSyncPrunesDeletedFiles(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Two files: a caller in main.go and a callee in greet.go. The call edge
	// crosses files, so deleting greet.go must also reclaim the resolved edge
	// via the node cascade.
	writeFile(t, dir, "main.go", `package main

func main() {
	Hello()
}
`)
	writeFile(t, dir, "greet.go", `package main

func Hello() string {
	return "hi"
}
`)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Sanity: greet.go is indexed before deletion.
	if _, err := database.GetFile(ctx, "greet.go"); err != nil {
		t.Fatalf("precondition: greet.go should be indexed: %v", err)
	}
	greetNodes, err := database.GetNodesInFile(ctx, "greet.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(greet.go): %v", err)
	}
	if len(greetNodes) == 0 {
		t.Fatal("precondition: greet.go should have nodes")
	}

	// Delete greet.go from disk (and stage the removal so git ls-files agrees).
	if err := os.Remove(filepath.Join(dir, "greet.go")); err != nil {
		t.Fatalf("remove greet.go: %v", err)
	}
	gitAdd(t, dir, "-A")
	gitCommit(t, dir, "rm greet.go")

	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync after delete: %v", err)
	}

	// The deleted file's record must be gone.
	if _, err := database.GetFile(ctx, "greet.go"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("greet.go file record still present after Sync (err=%v) — deleted code not pruned", err)
	}

	// Its nodes must be gone.
	afterNodes, err := database.GetNodesInFile(ctx, "greet.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(greet.go) after delete: %v", err)
	}
	if len(afterNodes) != 0 {
		t.Errorf("greet.go has %d nodes after delete+Sync; want 0 (pruning failed)", len(afterNodes))
	}

	// The surviving file must be untouched — prune is scoped to vanished paths.
	if _, err := database.GetFile(ctx, "main.go"); err != nil {
		t.Errorf("main.go file record was wrongly pruned: %v", err)
	}
	mainNodes, err := database.GetNodesInFile(ctx, "main.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(main.go) after delete: %v", err)
	}
	if len(mainNodes) == 0 {
		t.Error("main.go nodes were wrongly pruned")
	}
}

// TestOrphanInvariant moves a function between lines, which changes its
// node-id, and asserts the old node and its edges are gone. The WithoutDelete
// sub-test reproduces the orphan a naive in-place upsert would leave, proving
// delete-before-reinsert is load-bearing rather than defensive.
func TestOrphanInvariant(t *testing.T) {
	ctx := context.Background()

	const goFileV1 = `package main

func Hello() string {
	return "hi"
}
`
	// v2: Hello has moved to line 7 (a blank line + comment added before it).
	const goFileV2 = `package main

// A new comment block that pushes Hello down.
// More comments here.
// And another line.

func Hello() string {
	return "hi"
}
`

	t.Run("WithDelete_noOrphans", func(t *testing.T) {
		pool := newTestPool(t)
		database := openTestDB(t)
		dir := t.TempDir()
		initGitRepo(t, dir)

		writeFile(t, dir, "greet.go", goFileV1)
		gitAdd(t, dir, ".")
		gitCommit(t, dir, "v1")

		orch := indexer.NewOrchestrator(database, pool)

		if err := orch.IndexAll(ctx, dir); err != nil {
			t.Fatalf("IndexAll v1: %v", err)
		}

		// Capture node IDs from v1.
		v1Nodes, err := database.GetNodesInFile(ctx, "greet.go")
		if err != nil {
			t.Fatalf("GetNodesInFile v1: %v", err)
		}
		oldHelloID := findFunctionNode(t, v1Nodes, "Hello")
		if oldHelloID == "" {
			t.Skip("Hello function node not found in v1 — grammar may not extract it")
		}

		// Overwrite with v2 (Hello now at line 7 → different node-id).
		writeFile(t, dir, "greet.go", goFileV2)
		gitAdd(t, dir, ".")
		gitCommit(t, dir, "v2")

		// Re-sync.
		if err := orch.Sync(ctx, dir); err != nil {
			t.Fatalf("Sync v2: %v", err)
		}

		// Old node must be gone.
		if _, err := database.GetNode(ctx, oldHelloID); err == nil {
			t.Errorf("R-E VIOLATION: old node %s still exists after re-sync", oldHelloID)
		} else if !errors.Is(err, db.ErrNotFound) {
			t.Errorf("GetNode(old): unexpected error %v", err)
		}

		// New node (Hello at line 7) must exist.
		v2Nodes, err := database.GetNodesInFile(ctx, "greet.go")
		if err != nil {
			t.Fatalf("GetNodesInFile v2: %v", err)
		}
		newHelloID := findFunctionNode(t, v2Nodes, "Hello")
		if newHelloID == "" {
			t.Error("Hello function node not found in v2")
		}
		if newHelloID == oldHelloID {
			t.Errorf("node-id did not change after line shift: %s", oldHelloID)
		}

		// No dangling edges: old node is gone + no edge references the old id.
		assertNoDanglingEdges(t, ctx, database, v2Nodes, oldHelloID)
	})

	t.Run("WithoutDelete_proveOrphan", func(t *testing.T) {
		// Raw DB calls, standing in for what a naive REPLACE would do.
		pool := newTestPool(t)
		database := openTestDB(t)
		dir := t.TempDir()
		initGitRepo(t, dir)

		writeFile(t, dir, "greet.go", goFileV1)
		gitAdd(t, dir, ".")
		gitCommit(t, dir, "v1")

		orch := indexer.NewOrchestrator(database, pool)
		if err := orch.IndexAll(ctx, dir); err != nil {
			t.Fatalf("IndexAll v1: %v", err)
		}

		v1Nodes, err := database.GetNodesInFile(ctx, "greet.go")
		if err != nil {
			t.Fatalf("GetNodesInFile v1: %v", err)
		}
		oldHelloID := findFunctionNode(t, v1Nodes, "Hello")
		if oldHelloID == "" {
			t.Skip("Hello function node not found — grammar may not extract it")
		}

		// Upsert the moved node without deleting the old one.
		newHelloID := generateHelloNodeIDAtLine(t, "greet.go", 7)
		fakeNode := types.Node{
			ID:        newHelloID,
			Kind:      types.NodeKindFunction,
			Name:      "Hello",
			FilePath:  "greet.go",
			Language:  types.LanguageGo,
			StartLine: 7,
			EndLine:   9,
		}
		if err := database.UpsertNode(ctx, fakeNode); err != nil {
			t.Fatalf("UpsertNode fake: %v", err)
		}

		if _, err := database.GetNode(ctx, oldHelloID); err != nil {
			t.Errorf("ORPHAN PROOF: expected old node %s to still exist (no delete), got: %v", oldHelloID, err)
		}

		// The orphan persisting here is the point: it is what the invariant prevents.
		t.Log("Without delete: orphan node confirmed present — invariant is load-bearing")
	})
}

func TestContentHashDedup(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "hello.go", `package main

func Hello() {}
`)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)

	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll 1: %v", err)
	}
	nodes1, err := database.GetNodesInFile(ctx, "hello.go")
	if err != nil {
		t.Fatalf("GetNodesInFile 1: %v", err)
	}

	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync (unchanged): %v", err)
	}
	nodes2, err := database.GetNodesInFile(ctx, "hello.go")
	if err != nil {
		t.Fatalf("GetNodesInFile 2: %v", err)
	}

	if len(nodes1) != len(nodes2) {
		t.Errorf("dedup: node count changed from %d to %d after unchanged re-sync", len(nodes1), len(nodes2))
	}

	ids1 := nodeIDSet(nodes1)
	ids2 := nodeIDSet(nodes2)
	for id := range ids1 {
		if !ids2[id] {
			t.Errorf("dedup: node %s disappeared after unchanged re-sync", id)
		}
	}
	for id := range ids2 {
		if !ids1[id] {
			t.Errorf("dedup: extra node %s appeared after unchanged re-sync", id)
		}
	}
}

//
// The extractor-version tests below observe re-extraction without depending on
// clock resolution: a node deleted straight from the DB reappears only if the
// file was genuinely re-extracted, since the dedup skip returns before it
// would touch the DB at all.

// metadataValue mirrors how production reaches project_metadata: the raw
// handle, since the db package exposes no accessor.
func metadataValue(t *testing.T, ctx context.Context, database *db.DB, key string) (string, bool) {
	t.Helper()
	var value string
	err := database.DB().QueryRowContext(ctx,
		`SELECT value FROM project_metadata WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("query project_metadata %s: %v", key, err)
	}
	return value, true
}

// deleteNonFileNode removes a node behind extraction's back and returns its
// id, so a later run's behaviour is legible: reappearing means re-extracted,
// still gone means skipped.
func deleteNonFileNode(t *testing.T, ctx context.Context, database *db.DB, path string) string {
	t.Helper()
	nodes, err := database.GetNodesInFile(ctx, path)
	if err != nil {
		t.Fatalf("GetNodesInFile(%q): %v", path, err)
	}
	var victimID string
	for _, n := range nodes {
		if n.Kind != types.NodeKindFile {
			victimID = n.ID
			break
		}
	}
	if victimID == "" {
		t.Fatalf("GetNodesInFile(%q): no non-file node to delete (nodes=%v)", path, nodeKinds(nodes))
	}
	if _, err := database.DB().ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, victimID); err != nil {
		t.Fatalf("manual delete node %s: %v", victimID, err)
	}
	return victimID
}

// nodeIDPresent reports whether id is among path's currently stored nodes.
func nodeIDPresent(t *testing.T, ctx context.Context, database *db.DB, path, id string) bool {
	t.Helper()
	nodes, err := database.GetNodesInFile(ctx, path)
	if err != nil {
		t.Fatalf("GetNodesInFile(%q): %v", path, err)
	}
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

func indexHelloGoFixture(t *testing.T, ctx context.Context, database *db.DB, pool *extraction.Pool, dir string) *indexer.Orchestrator {
	t.Helper()
	writeFile(t, dir, "hello.go", `package main

func Hello() {}
`)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll (setup): %v", err)
	}
	return orch
}

// TestExtractorVersionMismatchForcesFullReindex: a stale stamped version must
// override the dedup skip for a file that has not changed on disk.
func TestExtractorVersionMismatchForcesFullReindex(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	orch := indexHelloGoFixture(t, ctx, database, pool, dir)

	if _, ok := metadataValue(t, ctx, database, "extractor_version"); !ok {
		t.Fatalf("extractor_version not stamped after first index")
	}

	victimID := deleteNonFileNode(t, ctx, database, "hello.go")

	// Simulate an index built under stale extraction semantics.
	if _, err := database.DB().ExecContext(ctx,
		`UPDATE project_metadata SET value = 'stale' WHERE key = 'extractor_version'`); err != nil {
		t.Fatalf("downgrade extractor_version: %v", err)
	}

	// Touch nothing on disk. The mismatch alone must force a full pass.
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll (migration run): %v", err)
	}

	if !nodeIDPresent(t, ctx, database, "hello.go", victimID) {
		t.Errorf("extractor_version mismatch did not force a full re-extraction: manually deleted node %s was not restored", victimID)
	}

	value, ok := metadataValue(t, ctx, database, "extractor_version")
	if !ok {
		t.Fatalf("extractor_version key not stamped after migration run")
	}
	if value == "stale" {
		t.Errorf("extractor_version key not rewritten after migration run")
	}
}

// TestExtractorVersionMatchKeepsIncremental pins the other side: a matching
// version must not cost every warm run a full re-extraction.
func TestExtractorVersionMatchKeepsIncremental(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	orch := indexHelloGoFixture(t, ctx, database, pool, dir)

	valueBefore, ok := metadataValue(t, ctx, database, "extractor_version")
	if !ok {
		t.Fatalf("extractor_version not stamped after first index")
	}

	victimID := deleteNonFileNode(t, ctx, database, "hello.go")

	// Touch nothing on disk, version unchanged. The dedup skip must still
	// fire: the manually deleted node stays deleted.
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll (incremental run): %v", err)
	}

	if nodeIDPresent(t, ctx, database, "hello.go", victimID) {
		t.Errorf("matching extractor_version should keep the dedup skip: manually deleted node %s was restored", victimID)
	}

	valueAfter, _ := metadataValue(t, ctx, database, "extractor_version")
	if valueAfter != valueBefore {
		t.Errorf("extractor_version rewritten on a matching run: before=%q after=%q", valueBefore, valueAfter)
	}
}

// TestExtractorVersionStampedOnFreshIndex: an empty index stamps without a
// forced pass — every file is new on a first run, so nothing was skipped.
func TestExtractorVersionStampedOnFreshIndex(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "hello.go", `package main

func Hello() {}
`)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	if _, ok := metadataValue(t, ctx, database, "extractor_version"); ok {
		t.Fatalf("brand-new DB already has an extractor_version key")
	}

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	value, ok := metadataValue(t, ctx, database, "extractor_version")
	if !ok {
		t.Fatalf("extractor_version not stamped after first-ever index")
	}
	if value == "" {
		t.Errorf("extractor_version stamped empty")
	}

	nodes, err := database.GetNodesInFile(ctx, "hello.go")
	if err != nil {
		t.Fatalf("GetNodesInFile: %v", err)
	}
	if len(nodes) < 2 {
		t.Errorf("fresh index: expected at least 2 nodes (file + Hello func), got %d", len(nodes))
	}
}

// TestSyncExtractorVersionMismatchForcesFullReindex: warm repos only ever run
// Sync, so the migration would be inert in practice if it fired for IndexAll
// alone.
func TestSyncExtractorVersionMismatchForcesFullReindex(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Warm index via IndexAll (as a real repo would be cold-started).
	orch := indexHelloGoFixture(t, ctx, database, pool, dir)

	if _, ok := metadataValue(t, ctx, database, "extractor_version"); !ok {
		t.Fatalf("extractor_version not stamped after first index")
	}

	victimID := deleteNonFileNode(t, ctx, database, "hello.go")

	// Simulate an index built under stale extraction semantics.
	if _, err := database.DB().ExecContext(ctx,
		`UPDATE project_metadata SET value = 'stale' WHERE key = 'extractor_version'`); err != nil {
		t.Fatalf("downgrade extractor_version: %v", err)
	}

	// Touch nothing on disk. Sync (not IndexAll) must still force a full pass.
	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync (migration run): %v", err)
	}

	if !nodeIDPresent(t, ctx, database, "hello.go", victimID) {
		t.Errorf("Sync: extractor_version mismatch did not force a full re-extraction: manually deleted node %s was not restored", victimID)
	}

	value, ok := metadataValue(t, ctx, database, "extractor_version")
	if !ok {
		t.Fatalf("extractor_version key not stamped after Sync migration run")
	}
	if value == "stale" {
		t.Errorf("extractor_version key not rewritten after Sync migration run")
	}
}

// TestSyncExtractorVersionMatchKeepsIncremental: the version check must not
// cost every warm Sync a full re-extraction.
func TestSyncExtractorVersionMatchKeepsIncremental(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	orch := indexHelloGoFixture(t, ctx, database, pool, dir)

	valueBefore, ok := metadataValue(t, ctx, database, "extractor_version")
	if !ok {
		t.Fatalf("extractor_version not stamped after first index")
	}

	victimID := deleteNonFileNode(t, ctx, database, "hello.go")

	// Touch nothing on disk, version unchanged. Sync's dedup skip must still
	// fire: the manually deleted node stays deleted.
	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync (incremental run): %v", err)
	}

	if nodeIDPresent(t, ctx, database, "hello.go", victimID) {
		t.Errorf("Sync: matching extractor_version should keep the dedup skip: manually deleted node %s was restored", victimID)
	}

	valueAfter, _ := metadataValue(t, ctx, database, "extractor_version")
	if valueAfter != valueBefore {
		t.Errorf("Sync: extractor_version rewritten on a matching run: before=%q after=%q", valueBefore, valueAfter)
	}
}

// TestUnresolvedRefsPersistence: the resolution pipeline can only resolve what
// unresolved_refs holds, so every distinct ref site must persist, a re-sync
// must replace the old set rather than accumulate, and each row must carry its
// file_path and language. Counting sites catches an id collision, where
// INSERT OR IGNORE would silently keep only the first.
func TestUnresolvedRefsPersistence(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// 1 import + 1 call site = 2 distinct refs.
	const wantRefsV1 = 2
	const tsContentV1 = `import { foo } from "./util";

export function bar(): void {
  foo();
}
`

	// 2 imports + 2 call sites = 4 distinct refs, replacing the earlier 2.
	const wantRefsV2 = 4
	const tsContentV2 = `import { foo } from "./util";
import { baz } from "./other";

export function bar(): void {
  foo();
  baz();
}
`

	writeFile(t, dir, "app.ts", tsContentV1)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)

	// First index — all N distinct refs must be persisted.
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	refs1, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs after index: %v", err)
	}
	if len(refs1) == 0 {
		t.Fatal("FAIL: unresolved_refs is empty after indexing app.ts — storeExtractionResult must persist result.UnresolvedReferences")
	}
	// The real count assertion: proves all distinct refs persist (not just 1).
	// This is the regression gate for the empty-id PK collision bug.
	if len(refs1) != wantRefsV1 {
		t.Errorf("after first index: got %d unresolved refs, want %d — empty-id collision drops all but the first",
			len(refs1), wantRefsV1)
	}

	// All persisted refs must carry the correct metadata.
	for _, ref := range refs1 {
		if ref.ID == "" {
			t.Errorf("ref has empty ID — GenerateRefID was not called")
		}
		if ref.FilePath == "" {
			t.Errorf("ref %s: file_path is empty", ref.ID)
		}
		if ref.Language == "" {
			t.Errorf("ref %s: language is empty", ref.ID)
		}
	}

	// Re-sync with v2 (different content → forces re-extraction).
	writeFile(t, dir, "app.ts", tsContentV2)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "v2")

	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync v2: %v", err)
	}

	refs2, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs after resync: %v", err)
	}

	// Re-sync replacement: old refs gone, new count == wantRefsV2.
	// If DeleteUnresolvedRefsByFile is not called, count would be wantRefsV1+wantRefsV2.
	if len(refs2) != wantRefsV2 {
		t.Errorf("after re-sync: got %d unresolved refs, want %d — expected old refs replaced by new set (got %d+%d=%d if duplication, %d if empty-id collapse)",
			len(refs2), wantRefsV2, wantRefsV1, wantRefsV2, wantRefsV1+wantRefsV2, 1)
	}

	// Verify replacement: the total count is exactly wantRefsV2 (not wantRefsV1 + wantRefsV2).
	// A ref ID from v1 that reappears in v2 at the same site is fine — it was deleted then
	// re-inserted. The duplication check is the count assertion above.
	// Here we verify that only refs from app.ts are present (no stale refs from other files).
	for _, r := range refs2 {
		if r.FilePath != "app.ts" {
			t.Errorf("post-resync: unexpected ref file_path %q, want app.ts", r.FilePath)
		}
	}

	// All post-resync refs carry correct metadata.
	for _, ref := range refs2 {
		if ref.FilePath == "" {
			t.Errorf("post-resync ref %s: file_path is empty", ref.ID)
		}
	}
	t.Logf("unresolved_refs: %d after first index, %d after re-sync", len(refs1), len(refs2))
}

func TestGitignoreAwareScan(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Create a normal file and a gitignored file.
	writeFile(t, dir, "main.go", `package main

func Main() {}
`)
	writeFile(t, dir, ".gitignore", "secret.go\n")
	writeFile(t, dir, "secret.go", `package main

func Secret() {}
`)

	// Stage and commit main.go + .gitignore (NOT secret.go — it's gitignored).
	gitAdd(t, dir, "main.go")
	gitAdd(t, dir, ".gitignore")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// secret.go must not appear in the DB.
	secretNodes, err := database.GetNodesInFile(ctx, "secret.go")
	if err == nil && len(secretNodes) > 0 {
		t.Errorf("gitignore: secret.go was indexed (%d nodes), expected it to be skipped", len(secretNodes))
	}

	// main.go must be indexed.
	mainNodes, err := database.GetNodesInFile(ctx, "main.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(main.go): %v", err)
	}
	if len(mainNodes) == 0 {
		t.Error("main.go: expected at least one node, got 0")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func gitAdd(t *testing.T, dir string, args ...string) {
	t.Helper()
	a := append([]string{"add"}, args...)
	out, err := runCmdBytes(dir, "git", a...)
	if err != nil {
		t.Fatalf("git add %v: %v\n%s", args, err, out)
	}
}

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	out, err := runCmdBytes(dir, "git", "commit", "-m", msg, "--allow-empty")
	if err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func runCmdBytes(dir, name string, args ...string) ([]byte, error) {
	var buf strings.Builder
	c := buildOSCmd(dir, name, args...)
	c.Stdout = &buf
	c.Stderr = &buf
	err := c.Run()
	return []byte(buf.String()), err
}

// nodeKinds returns a slice of kind strings for display in error messages.
func nodeKinds(nodes []types.Node) []string {
	kinds := make([]string, len(nodes))
	for i, n := range nodes {
		kinds[i] = string(n.Kind)
	}
	return kinds
}

// findFunctionNode returns the node ID of the first function node named name,
// or "" if not found.
func findFunctionNode(t *testing.T, nodes []types.Node, name string) string {
	t.Helper()
	for _, n := range nodes {
		if n.Kind == types.NodeKindFunction && n.Name == name {
			return n.ID
		}
	}
	return ""
}

// assertNoDanglingEdges asserts the R-E dangling-edge half of the orphan
// invariant after a re-sync that deleted oldID:
//
//  1. oldID must be gone from the nodes table (ErrNotFound).
//  2. No edge references oldID as source.
//  3. No edge references oldID as target.
//
// nodes is the post-sync node set (used for informational context only).
func assertNoDanglingEdges(t *testing.T, ctx context.Context, database *db.DB, nodes []types.Node, oldID string) {
	t.Helper()

	// 1. The old node itself must be gone.
	if _, err := database.GetNode(ctx, oldID); err == nil {
		t.Errorf("assertNoDanglingEdges: old node %s still exists after re-sync", oldID)
	} else if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("assertNoDanglingEdges: GetNode(%s): unexpected error: %v", oldID, err)
	}

	// 2. No edge with oldID as source.
	srcEdges, err := database.GetEdgesBySource(ctx, oldID)
	if err != nil {
		t.Errorf("assertNoDanglingEdges: GetEdgesBySource(%s): %v", oldID, err)
	} else if len(srcEdges) > 0 {
		t.Errorf("assertNoDanglingEdges: %d dangling edge(s) with source=%s after re-sync", len(srcEdges), oldID)
	}

	// 3. No edge with oldID as target.
	tgtEdges, err := database.GetEdgesByTarget(ctx, oldID)
	if err != nil {
		t.Errorf("assertNoDanglingEdges: GetEdgesByTarget(%s): %v", oldID, err)
	} else if len(tgtEdges) > 0 {
		t.Errorf("assertNoDanglingEdges: %d dangling edge(s) with target=%s after re-sync", len(tgtEdges), oldID)
	}
}

// TestEmbeddedSQLInGoFile verifies that embedded SQL in Go string literals
// produces the expected nodes and edges in the DB.
//
// WHY: success criteria require that:
//   - A .go file with CREATE TABLE in a raw/interpreted string literal produces
//     ≥1 table node attributed to that file with file-absolute StartLine.
//   - Embedded DML in a .go literal produces ≥1 unresolved ref owned by the
//     enclosing host function node (or file fallback).
//   - GetEdgesByProvenance("embedded") returns the DDL contains edges.
//   - Standalone .sql routing is unchanged (zero-regression).
//
// The fixture contains:
//   - line 5:  raw string literal with CREATE TABLE users(...) — DDL path
//   - line 16: interpreted string literal with SELECT ... FROM users WHERE id = $1 — DML path
//   - Both literals are inside the CreateUsersTable() function (line 3).
func TestEmbeddedSQLInGoFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// The fixture: a Go file with embedded DDL (raw string) and embedded DML
	// (interpreted string), both inside CreateUsersTable.
	// Line numbers (1-based):
	//   1: package main
	//   2: (blank)
	//   3: func CreateUsersTable(db interface{}) {
	//   4:     _ = `
	//   5:         CREATE TABLE users (
	//   6:             id SERIAL PRIMARY KEY,
	//   7:             email TEXT NOT NULL
	//   8:         )
	//   9:     `
	//  10:     _ = "SELECT id, email FROM users WHERE id = $1"
	//  11: }
	const goFixture = `package main

func CreateUsersTable(db interface{}) {
	_ = ` + "`" + `
		CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			email TEXT NOT NULL
		)
	` + "`" + `
	_ = "SELECT id, email FROM users WHERE id = $1"
}
`
	writeFile(t, dir, "migration.go", goFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	goNodes, err := database.GetNodesInFile(ctx, "migration.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(migration.go): %v", err)
	}

	var tableNodes []types.Node
	for _, n := range goNodes {
		if n.Kind == types.NodeKindTable {
			tableNodes = append(tableNodes, n)
		}
	}
	if len(tableNodes) == 0 {
		t.Fatalf("FAIL: no table nodes found in migration.go — embedded DDL extraction not wired")
	}

	// Verify the table node is named "users" and has a file-absolute StartLine ≥ 4
	// (the raw string literal starts on line 4, CREATE TABLE is on line 5).
	var usersNode *types.Node
	for i := range tableNodes {
		if tableNodes[i].Name == "users" {
			usersNode = &tableNodes[i]
			break
		}
	}
	if usersNode == nil {
		t.Fatalf("FAIL: no table node named 'users' in migration.go; got table nodes: %v", tableNodeNames(tableNodes))
	}
	// StartLine must be file-absolute (≥4 because the literal starts on line 4).
	if usersNode.StartLine < 4 {
		t.Errorf("users table StartLine=%d, want ≥4 (file-absolute; literal starts line 4)", usersNode.StartLine)
	}

	// The DML "SELECT id, email FROM users WHERE id = $1" should produce an
	// UnresolvedReference. We can't query unresolved_refs by file directly here
	// but GetUnresolvedRefs returns all rows. Check that at least one ref is from
	// migration.go and has ReferenceName == "users".
	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}
	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "migration.go" && allRefs[i].ReferenceName == "users" {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Fatalf("FAIL: no unresolved ref for 'users' from migration.go — embedded DML not wired")
	}

	// the ref must be owned by the CreateUsersTable function
	// node specifically — not just any non-empty FromNodeID, which would pass
	// on file-node fallback too.
	var createUsersTableNode *types.Node
	for i := range goNodes {
		if goNodes[i].Kind == types.NodeKindFunction && goNodes[i].Name == "CreateUsersTable" {
			createUsersTableNode = &goNodes[i]
			break
		}
	}
	if createUsersTableNode == nil {
		t.Fatal("FAIL: CreateUsersTable function node not found in migration.go — needed for ownership assertion (F-5)")
	}
	if dmlRef.FromNodeID != createUsersTableNode.ID {
		t.Errorf("DML ref FromNodeID=%q, want CreateUsersTable node id=%q — ownership not correct (F-5)",
			dmlRef.FromNodeID, createUsersTableNode.ID)
	}
	// Language must be SQL (so the provenance seam in createEdges can detect it).
	if dmlRef.Language != types.LanguageSQL {
		t.Errorf("DML unresolved ref Language=%q, want %q", dmlRef.Language, types.LanguageSQL)
	}

	// The DDL contains edges (table→column) stamped with Provenance:"embedded".
	// After indexing, GetEdgesByProvenance("embedded") must return ≥1 edge.
	embeddedEdges, err := database.GetEdgesByProvenance(ctx, "embedded")
	if err != nil {
		t.Fatalf("GetEdgesByProvenance(embedded): %v", err)
	}
	if len(embeddedEdges) == 0 {
		t.Fatalf("FAIL: GetEdgesByProvenance(embedded) returned 0 edges — DDL embedded edges not stored")
	}

	// Index a .sql file and confirm it still works (zero-regression for standaloneExts).
	writeFile(t, dir, "schema.sql", "CREATE TABLE products (id SERIAL PRIMARY KEY, name TEXT NOT NULL);")
	gitAdd(t, dir, "schema.sql")
	gitCommit(t, dir, "add-sql")

	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync after adding schema.sql: %v", err)
	}

	sqlNodes, err := database.GetNodesInFile(ctx, "schema.sql")
	if err != nil {
		t.Fatalf("GetNodesInFile(schema.sql): %v", err)
	}
	hasSQLTable := false
	for _, n := range sqlNodes {
		if n.Kind == types.NodeKindTable {
			hasSQLTable = true
			break
		}
	}
	if !hasSQLTable {
		t.Error("REGRESSION: schema.sql no longer produces a table node — standaloneExts routing broken")
	}
}

// tableNodeNames returns the Name of each table node for error messages.
func tableNodeNames(nodes []types.Node) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return names
}

// nodeIDSet returns a map of node IDs for fast lookup.
func nodeIDSet(nodes []types.Node) map[string]bool {
	m := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		m[n.ID] = true
	}
	return m
}

// generateHelloNodeIDAtLine generates the expected node ID for a function named
// "Hello" at a specific line in a file. Used in WithoutDelete_proveOrphan.
func generateHelloNodeIDAtLine(t *testing.T, filePath string, line int) string {
	t.Helper()
	// qualified name = "Hello" (no parent scope at top level)
	return extraction.GenerateNodeID(filePath, string(types.NodeKindFunction), "Hello", line)
}

// TestEmbeddedSQLInPythonFile verifies that embedded SQL in Python string literals
// is extracted correctly per the spec:
//
//   - Regular-string DDL → ≥1 table node attributed to the file.
//   - Triple-quoted DDL → ≥1 table node.
//   - DML in a function → unresolved ref owned by the enclosing function node.
//   - Module/class/function docstrings with SQL content → excluded (zero nodes).
//   - f-string with interpolated table target → zero nodes, zero refs.
//   - f-string with literal table and interpolated value → ref to "users".
func TestEmbeddedSQLInPythonFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Fixture line layout (1-based):
	//   1:  """Module docstring: SELECT * FROM module_secret"""
	//   2:  (blank)
	//   3:  CREATE_USERS = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"
	//   4:  (blank)
	//   5:  TRIPLE = """
	//   6:  CREATE TABLE orders (id SERIAL PRIMARY KEY, user_id INTEGER)
	//   7:  """
	//   8:  (blank)
	//   9:  class MyService:
	//  10:      """Class docstring: SELECT * FROM class_secret"""
	//  11:  (blank)
	//  12:  def do_query(conn):
	//  13:      """Function docstring: CREATE TABLE fn_secret (id INT)"""
	//  14:      q = "SELECT id, email FROM users WHERE active = 1"
	//  15:      fq1 = f"SELECT a FROM {table} WHERE id = %s"
	//  16:      fq2 = f"SELECT a FROM users WHERE id = {uid}"
	//
	// Expected:
	//   - "users" table node from line 3 (regular string DDL) ✓
	//   - "orders" table node from line 5-7 (triple-quoted DDL) ✓
	//   - Unresolved ref to "users" from DML on line 14, owned by do_query ✓
	//   - module_secret, class_secret, fn_secret: NOT extracted (docstrings) ✓
	//   - fq1 (interpolated table target): zero refs ✓
	//   - fq2 (interpolated value + literal table): ref to "users" ✓
	const pyFixture = `"""Module docstring: SELECT * FROM module_secret"""

CREATE_USERS = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"

TRIPLE = """
CREATE TABLE orders (id SERIAL PRIMARY KEY, user_id INTEGER)
"""

class MyService:
    """Class docstring: SELECT * FROM class_secret"""

def do_query(conn):
    """Function docstring: CREATE TABLE fn_secret (id INT)"""
    q = "SELECT id, email FROM users WHERE active = 1"
    fq1 = f"SELECT a FROM {table} WHERE id = %s"
    fq2 = f"SELECT a FROM users WHERE id = {uid}"
`
	writeFile(t, dir, "models.py", pyFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	pyNodes, err := database.GetNodesInFile(ctx, "models.py")
	if err != nil {
		t.Fatalf("GetNodesInFile(models.py): %v", err)
	}

	var usersNode *types.Node
	for i := range pyNodes {
		if pyNodes[i].Kind == types.NodeKindTable && pyNodes[i].Name == "users" {
			usersNode = &pyNodes[i]
			break
		}
	}
	if usersNode == nil {
		t.Fatalf("FAIL: no table node 'users' from regular-string DDL (line 3) — not wired")
	}
	// StartLine must be file-absolute.
	if usersNode.StartLine < 3 {
		t.Errorf("users table StartLine=%d, want ≥3 (file-absolute)", usersNode.StartLine)
	}

	var ordersNode *types.Node
	for i := range pyNodes {
		if pyNodes[i].Kind == types.NodeKindTable && pyNodes[i].Name == "orders" {
			ordersNode = &pyNodes[i]
			break
		}
	}
	if ordersNode == nil {
		t.Fatalf("FAIL: no table node 'orders' from triple-quoted DDL (lines 5-7) — triple-quote not wired")
	}

	docstringTableNames := []string{"module_secret", "class_secret", "fn_secret"}
	for _, forbidden := range docstringTableNames {
		for _, n := range pyNodes {
			if n.Kind == types.NodeKindTable && n.Name == forbidden {
				t.Errorf("FAIL: table node %q extracted from docstring — decision 4 not enforced", forbidden)
			}
		}
	}

	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	// Find the do_query function node.
	var doQueryNode *types.Node
	for i := range pyNodes {
		if pyNodes[i].Kind == types.NodeKindFunction && pyNodes[i].Name == "do_query" {
			doQueryNode = &pyNodes[i]
			break
		}
	}
	if doQueryNode == nil {
		t.Fatal("FAIL: do_query function node not found in models.py — needed for F-5 ownership assertion")
	}

	// DML "SELECT id, email FROM users WHERE active = 1" should emit a ref to "users"
	// owned by do_query.
	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "models.py" &&
			allRefs[i].ReferenceName == "users" &&
			allRefs[i].FromNodeID == doQueryNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'users' from models.py owned by do_query (F-5 ownership, )")
	}

	// fq1 = f"SELECT a FROM {table} WHERE id = %%s" — after substitution: no valid table
	for _, ref := range allRefs {
		if ref.FilePath == "models.py" && ref.ReferenceName == "table" {
			t.Errorf("FAIL: ref to 'table' (interpolation segment) leaked — decision 8a not enforced")
		}
	}

	// fq2 = f"SELECT a FROM users WHERE id = {uid}" — literal "users" survives substitution
	// ("SELECT a FROM users WHERE id = ?"), so a second distinct ref to "users" must be
	// emitted from doQueryNode.
	//
	// WHY count ≥2: criterion 4 already confirmed one "users" ref from the plain DML q
	// (line 14). If fq2 extraction is broken the count stays at 1 and this check fails —
	// which is the correct outcome. Re-using the find-any predicate from C4 would pass
	// even with fq2 silently missing.
	var usersRefsFromDoQuery int
	for i := range allRefs {
		if allRefs[i].FilePath == "models.py" &&
			allRefs[i].ReferenceName == "users" &&
			allRefs[i].FromNodeID == doQueryNode.ID {
			usersRefsFromDoQuery++
		}
	}
	if usersRefsFromDoQuery < 2 {
		t.Errorf("FAIL: want ≥2 distinct 'users' refs from doQueryNode (q DML + fq2 f-string); got %d — fq2 literal table ref not extracted (decision 8b)", usersRefsFromDoQuery)
	}
}

// TestEmbeddedSQLInTypeScriptFile verifies that embedded SQL in TypeScript string
// literals and template literals is extracted correctly per the spec:
//
//   - Plain-string DDL → ≥1 table node attributed to the file (file-absolute lines).
//   - Template-literal DDL → ≥1 table node.
//   - DML in a function → unresolved ref owned by the enclosing function node.
//   - Template literal with interpolated table target → zero refs (decision 8a).
//   - Template literal with interpolated value + literal table → ref to "users" (decision 8b).
func TestEmbeddedSQLInTypeScriptFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Fixture line layout (1-based):
	//   1:  // db.ts
	//   2:  (blank)
	//   3:  const CREATE_USERS = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)";
	//   4:  (blank)
	//   5:  const CREATE_ORDERS = `CREATE TABLE orders (id SERIAL PRIMARY KEY, user_id INTEGER)`;
	//   6:  (blank)
	//   7:  export function queryUsers(db: any, id: number) {
	//   8:    const q = "SELECT id, email FROM users WHERE active = 1";
	//   9:    const fq1 = `SELECT a FROM ${table} WHERE id = ?`;
	//  10:    const fq2 = `SELECT a FROM users WHERE id = ${id}`;
	//  11:    return db.query(q);
	//  12:  }
	//
	// Expected:
	//   - "users" table node from line 3 (plain-string DDL) ✓
	//   - "orders" table node from line 5 (template-literal DDL) ✓
	//   - Unresolved ref to "users" from DML on line 8, owned by queryUsers ✓
	//   - fq1 (interpolated table target): zero refs for the identifier "table" ✓
	//   - fq2 (interpolated value + literal table): second "users" ref from queryUsers ✓
	const tsFixture = `// db.ts

const CREATE_USERS = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)";

const CREATE_ORDERS = ` + "`" + `CREATE TABLE orders (id SERIAL PRIMARY KEY, user_id INTEGER)` + "`" + `;

export function queryUsers(db: any, id: number) {
  const q = "SELECT id, email FROM users WHERE active = 1";
  const fq1 = ` + "`" + `SELECT a FROM ${table} WHERE id = ?` + "`" + `;
  const fq2 = ` + "`" + `SELECT a FROM users WHERE id = ${id}` + "`" + `;
  return db.query(q);
}
`
	writeFile(t, dir, "db.ts", tsFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	tsNodes, err := database.GetNodesInFile(ctx, "db.ts")
	if err != nil {
		t.Fatalf("GetNodesInFile(db.ts): %v", err)
	}

	var usersNode *types.Node
	for i := range tsNodes {
		if tsNodes[i].Kind == types.NodeKindTable && tsNodes[i].Name == "users" {
			usersNode = &tsNodes[i]
			break
		}
	}
	if usersNode == nil {
		t.Fatalf("FAIL: no table node 'users' from plain-string DDL (line 3) — not wired for .ts")
	}
	if usersNode.StartLine < 3 {
		t.Errorf("users table StartLine=%d, want ≥3 (file-absolute)", usersNode.StartLine)
	}

	var ordersNode *types.Node
	for i := range tsNodes {
		if tsNodes[i].Kind == types.NodeKindTable && tsNodes[i].Name == "orders" {
			ordersNode = &tsNodes[i]
			break
		}
	}
	if ordersNode == nil {
		t.Fatalf("FAIL: no table node 'orders' from template-literal DDL (line 5) — template literal not harvested")
	}

	allRefs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	// Find the queryUsers function node.
	var queryUsersNode *types.Node
	for i := range tsNodes {
		if tsNodes[i].Kind == types.NodeKindFunction && tsNodes[i].Name == "queryUsers" {
			queryUsersNode = &tsNodes[i]
			break
		}
	}
	if queryUsersNode == nil {
		t.Fatal("FAIL: queryUsers function node not found in db.ts — needed for ownership assertion")
	}

	// DML "SELECT id, email FROM users WHERE active = 1" should emit a ref to
	// "users" owned by queryUsers.
	var dmlRef *types.UnresolvedReference
	for i := range allRefs {
		if allRefs[i].FilePath == "db.ts" &&
			allRefs[i].ReferenceName == "users" &&
			allRefs[i].FromNodeID == queryUsersNode.ID {
			dmlRef = &allRefs[i]
			break
		}
	}
	if dmlRef == nil {
		t.Errorf("FAIL: no unresolved ref for 'users' from db.ts owned by queryUsers — DML ownership not wired")
	}

	// fq1 = `SELECT a FROM ${table} WHERE id = ?` — after substitution: no valid table
	var tableRefs []types.UnresolvedReference
	for _, ref := range allRefs {
		if ref.FilePath == "db.ts" && ref.ReferenceName == "table" {
			tableRefs = append(tableRefs, ref)
		}
	}
	if len(tableRefs) != 0 {
		t.Errorf("FAIL: interpolated table target must yield zero refs, got %d: %+v — decision 8a not enforced for TS", len(tableRefs), tableRefs)
	}

	// fq2 = `SELECT a FROM users WHERE id = ${id}` — literal "users" survives substitution,
	// so a second distinct ref to "users" must be emitted from queryUsersNode.
	//
	// WHY count ≥2: criterion 3 already confirmed one "users" ref from the plain DML q
	// (line 8). If fq2 extraction is broken the count stays at 1 and this fails —
	// which is the correct outcome. A find-any predicate would pass even with fq2 broken.
	var usersRefsFromQueryUsers int
	for i := range allRefs {
		if allRefs[i].FilePath == "db.ts" &&
			allRefs[i].ReferenceName == "users" &&
			allRefs[i].FromNodeID == queryUsersNode.ID {
			usersRefsFromQueryUsers++
		}
	}
	if usersRefsFromQueryUsers < 2 {
		t.Errorf("FAIL: want ≥2 distinct 'users' refs from queryUsersNode (q DML + fq2 template literal); got %d — fq2 literal table ref not extracted (decision 8b)", usersRefsFromQueryUsers)
	}
}

func TestEmbeddedSQLInTSXFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Minimal TSX fixture: a component that holds embedded SQL.
	const tsxFixture = `import React from "react";

const DDL = "CREATE TABLE products (id INT PRIMARY KEY, name TEXT NOT NULL)";

export function ProductList() {
  const q = ` + "`" + `SELECT id, name FROM products WHERE active = 1` + "`" + `;
  return <div>{q}</div>;
}
`
	writeFile(t, dir, "products.tsx", tsxFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	tsxNodes, err := database.GetNodesInFile(ctx, "products.tsx")
	if err != nil {
		t.Fatalf("GetNodesInFile(products.tsx): %v", err)
	}

	// Criterion: plain-string DDL in a .tsx file → table node "products".
	var productsNode *types.Node
	for i := range tsxNodes {
		if tsxNodes[i].Kind == types.NodeKindTable && tsxNodes[i].Name == "products" {
			productsNode = &tsxNodes[i]
			break
		}
	}
	if productsNode == nil {
		t.Fatalf("FAIL: no table node 'products' from plain-string DDL in .tsx file — not wired for .tsx")
	}
}

// TestIndexSkipsUnreadableFileAndContinues proves that a single unreadable file
// in the index set (e.g. a git-tracked-but-missing file: a broken symlink or a
// deleted-but-staged path) does NOT abort the whole index. The other files must
// still be indexed, and the run must report the skip count rather than failing.
//
// WHY: a real-world repo (taxgentic/server) had 188 git-tracked-but-missing
// skill-reference files. The old code returned a fatal error on the FIRST
// unreadable file, so IndexAll returned non-nil, the CLI aborted before the
// resolution phase, and the entire index was effectively empty — `callers`
// queries returned nothing. The reference engine (codegraph) isolates the bad
// file and indexes the rest; this test pins that same resilience. The
// IndexAll docstring already promises "errors from individual files ... do not
// abort the run" — this encodes that contract.
func TestIndexSkipsUnreadableFileAndContinues(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

func Hello() string {
	return "hello"
}

func main() {
	Hello()
}
`)
	real := filepath.Join(dir, "main.go")
	// A path that is in the index set but does not exist on disk — os.ReadFile
	// will fail with ENOENT, exactly like a git-tracked-but-missing file.
	missing := filepath.Join(dir, "ghost.go")

	orch := indexer.NewOrchestrator(database, pool)

	// The missing file must not abort the run. Order it first so the old
	// fail-fast behaviour would have aborted before reaching main.go.
	if err := orch.IndexPaths(ctx, dir, []string{missing, real}); err != nil {
		t.Fatalf("IndexPaths must skip an unreadable file, not abort; got: %v", err)
	}

	// The real file must still be fully indexed (file node + Hello + main).
	nodes, err := database.GetNodesInFile(ctx, "main.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(main.go): %v", err)
	}
	if len(nodes) < 3 {
		t.Errorf("main.go must be indexed despite the unreadable sibling: got %d nodes, want >=3", len(nodes))
	}

	// The skip must be reported, not silently swallowed (fail loud).
	if got := orch.SkippedFiles(); got != 1 {
		t.Errorf("SkippedFiles() = %d, want 1 (the missing file)", got)
	}
}
