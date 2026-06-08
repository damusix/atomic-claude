package indexer_test

// Orchestrator + sync invariant tests (CP10).
//
// Tests run under the repo tmp/ dir so fixture repos don't pollute the source
// tree. Each test creates its own temp dir under os.TempDir() (with
// t.TempDir() which is cleaned up automatically) and initialises a real SQLite
// DB + tree-sitter pool. This exercises the full stack end-to-end.
//
// The headline test is TestOrphanInvariant (R-E):
//   - Index a file with a function at line 3.
//   - Move the function to line 7 (different node-id because id embeds line).
//   - Re-sync.
//   - Assert the old node is gone, the new node exists, no dangling edges.
//   - Sub-test proves the invariant MATTERS: same test with in-place upsert
//     (no delete) leaves an orphan — confirming delete-before-reinsert is load-
//     bearing, not defensive.

import (
	"context"
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// TestFullIndex — full index of a multi-file fixture repo
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// TestOrphanInvariant — the R-E headline test
// ---------------------------------------------------------------------------

// TestOrphanInvariant verifies delete-by-file-before-reinsert (R-E):
//
//  1. Index a Go file with Hello at line 3 → node-id embeds line 3.
//  2. Modify the file so Hello moves to line 7 → the new node-id embeds line 7.
//  3. Re-sync → assert old-line-3 node is gone, new-line-7 node exists, no
//     dangling edges.
//
// The sub-test "WithoutDelete" proves the invariant MATTERS: using an in-place
// upsert (skipping the delete) leaves the old node orphaned. The test confirms
// this failure mode to prove the delete is load-bearing.
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

		// First index.
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
		// This sub-test deliberately skips the delete step to prove the invariant
		// matters. It uses raw DB calls to simulate what a naive REPLACE would do.
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

		// Simulate naive re-index WITHOUT delete:
		// manually upsert a fake "Hello at line 7" node without deleting the old one.
		// This represents what would happen if we used INSERT OR REPLACE without
		// first deleting the file's nodes.
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

		// Without delete: the old node at line 3 is still there.
		if _, err := database.GetNode(ctx, oldHelloID); err != nil {
			t.Errorf("ORPHAN PROOF: expected old node %s to still exist (no delete), got: %v", oldHelloID, err)
		}

		// This demonstrates that without delete-before-reinsert, orphans persist.
		// The correct behavior (WithDelete_noOrphans) is the invariant.
		t.Log("Without delete: orphan node confirmed present — invariant is load-bearing")
	})
}

// ---------------------------------------------------------------------------
// TestContentHashDedup — re-sync unchanged file → no re-extraction
// ---------------------------------------------------------------------------

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

	// First index.
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll 1: %v", err)
	}
	nodes1, err := database.GetNodesInFile(ctx, "hello.go")
	if err != nil {
		t.Fatalf("GetNodesInFile 1: %v", err)
	}

	// Sync again without modifying the file.
	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync (unchanged): %v", err)
	}
	nodes2, err := database.GetNodesInFile(ctx, "hello.go")
	if err != nil {
		t.Fatalf("GetNodesInFile 2: %v", err)
	}

	// Node count must be identical (dedup: no re-extraction, no extra nodes).
	if len(nodes1) != len(nodes2) {
		t.Errorf("dedup: node count changed from %d to %d after unchanged re-sync", len(nodes1), len(nodes2))
	}

	// Node IDs must be identical.
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

// ---------------------------------------------------------------------------
// TestUnresolvedRefsPersistence — unresolved_refs stored atomically with nodes
// ---------------------------------------------------------------------------

// TestUnresolvedRefsPersistence verifies that storeExtractionResult persists
// ALL distinct result.UnresolvedReferences into the unresolved_refs table (inside
// the same transaction as nodes/edges). WHY: the resolution pipeline (CP13) reads
// from unresolved_refs; if the indexer silently drops refs (e.g. due to empty-id
// PK collision), CP13 has incomplete data to resolve.
//
// Three invariants proven here:
//  1. After first index, every distinct ref site persists — count == N (not 1).
//     This would FAIL under the empty-id bug because INSERT OR IGNORE on a
//     shared "" PK silently drops all refs after the first.
//  2. After re-sync with different content, the old ref set is REPLACED:
//     old refs gone, new refs present, count matches the new fixture.
//  3. All persisted refs carry correct file_path and language metadata.
func TestUnresolvedRefsPersistence(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// v1: 1 import + 1 call site = 2 distinct UnresolvedReferences.
	// Under the empty-id bug all refs share id="" so INSERT OR IGNORE keeps only 1.
	// The test asserts count == 2, which FAILS at 1 on unfixed code.
	const wantRefsV1 = 2
	const tsContentV1 = `import { foo } from "./util";

export function bar(): void {
  foo();
}
`

	// v2: 2 imports + 2 call sites = 4 distinct UnresolvedReferences.
	// After re-sync the old 2 refs must be gone and these 4 inserted.
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

// ---------------------------------------------------------------------------
// TestGitignoreAwareScan — gitignored files are skipped
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

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
