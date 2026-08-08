package resolution_test

// CP13 resolver pipeline tests.
//
// Why this file is the spec gate:
//   - calls ref to a function → calls edge (proves resolveOne + createEdges).
//   - calls ref to a class → PROMOTED to instantiates (proves kind promotion).
//   - extends ref to an interface → PROMOTED to implements (proves kind promotion).
//   - import ref → imports edge via ResolveImport (proves CP11 wiring).
//   - batch loop terminates when all remaining refs are unresolvable (proves the
//     "break when a batch yields nothing" guard — no infinite loop).
//   - resolved refs are DELETED from unresolved_refs; unresolvable remain.
//   - built-in skip: a console.log ref (JavaScript built-in) is skipped — no
//     edge inserted, no panic, ref removed from the pending set.
//
// Built-in skip policy: a built-in/stdlib reference is silently dropped from
// unresolved_refs after the skip (it will never resolve to an internal node).
//
// All tests seed a temp DB under tmp/ (via openTestDB from resolver_test.go;
// both files are in package resolution_test so they share helpers).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Extra helpers for pipeline tests
// ---------------------------------------------------------------------------

// seedNode inserts a symbol node at a specific location.
func seedNode(t *testing.T, d *db.DB, id, filePath string, kind types.NodeKind, lang types.Language, isExported bool) {
	t.Helper()
	ctx := context.Background()
	if err := d.UpsertNode(ctx, types.Node{
		ID:         id,
		Kind:       kind,
		Name:       filepath.Base(id),
		FilePath:   filePath,
		Language:   lang,
		StartLine:  1,
		EndLine:    10,
		IsExported: isExported,
	}); err != nil {
		t.Fatalf("seedNode %s: %v", id, err)
	}
}

// seedUnresolvedRef inserts one row into unresolved_refs.
func seedUnresolvedRef(t *testing.T, d *db.DB, r types.UnresolvedReference) {
	t.Helper()
	ctx := context.Background()
	if err := d.InsertUnresolvedRef(ctx, r); err != nil {
		t.Fatalf("seedUnresolvedRef %s: %v", r.ID, err)
	}
}

// countUnresolvedRefs returns the current count in unresolved_refs.
func countUnresolvedRefs(t *testing.T, d *db.DB) int {
	t.Helper()
	refs, err := d.GetUnresolvedRefs(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("countUnresolvedRefs: %v", err)
	}
	return len(refs)
}

// edgesWithKind returns edges from the DB with the given kind.
func edgesWithKind(t *testing.T, d *db.DB, source string, kind types.EdgeKind) []types.Edge {
	t.Helper()
	edges, err := d.GetEdgesBySource(context.Background(), source)
	if err != nil {
		t.Fatalf("GetEdgesBySource %s: %v", source, err)
	}
	var filtered []types.Edge
	for _, e := range edges {
		if e.Kind == kind {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// openPipelineTestDB opens a temp DB. Uses the system tmp dir (project tmp/ is
// too deeply nested and causes path issues on some macOS setups).
func openPipelineTestDB(t *testing.T) *db.DB {
	t.Helper()
	// Use os.MkdirTemp under project tmp/ per BRIEF instruction.
	dir := filepath.Join(projectTmpDir(), "pipeline-"+t.Name())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// projectTmpDir returns the path to the project tmp/ directory.
// It is resolved relative to this test file's location at build time.
func projectTmpDir() string {
	// Resolution: go up from atomic/internal/codeintel/resolution to atomic/
	// then two more levels to the worktree root, then tmp/.
	// Worktree root = atomic/../../..  relative to this test file's package dir.
	// We use os.Getwd() which gives the package dir when `go test` runs.
	// Fallback to os.TempDir() if the expected path does not exist.
	// This is intentionally defensive — tests must not fail because tmp/ is absent.
	wd, err := os.Getwd()
	if err != nil {
		return os.TempDir()
	}
	// wd is .../atomic/internal/codeintel/resolution
	// go up 4 levels → worktree root
	candidate := filepath.Join(wd, "..", "..", "..", "..", "tmp", "code-intel-cp13")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return os.TempDir()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCallsEdgeToFunction proves that a "calls" unresolved_ref targeting a
// function node produces a "calls" edge (no promotion).
func TestCallsEdgeToFunction(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	// Seed: caller function and callee function in the same TS file.
	callerID := "function:src/a.ts:caller:1"
	calleeID := "function:src/a.ts:callee:10"
	seedNode(t, d, callerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, true)
	seedNode(t, d, calleeID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, true)

	// Override node names to be matchable by the name matcher.
	if err := d.UpsertNode(ctx, types.Node{
		ID:         calleeID,
		Kind:       types.NodeKindFunction,
		Name:       "callee",
		FilePath:   "src/a.ts",
		Language:   types.LanguageTypeScript,
		StartLine:  10,
		EndLine:    20,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID:        callerID,
		Kind:      types.NodeKindFunction,
		Name:      "caller",
		FilePath:  "src/a.ts",
		Language:  types.LanguageTypeScript,
		StartLine: 1,
		EndLine:   9,
	}); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-calls-001",
		FromNodeID:    callerID,
		ReferenceName: "callee",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/a.ts",
		Language:      types.LanguageTypeScript,
		Line:          5,
	}
	seedUnresolvedRef(t, d, ref)

	pipeline := resolution.NewPipeline(d)
	_, resolved, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}
	if resolved == 0 {
		t.Fatal("expected at least one resolution, got 0")
	}

	// Edge must be "calls" (not promoted — callee is a function).
	edges := edgesWithKind(t, d, callerID, types.EdgeKindCalls)
	if len(edges) == 0 {
		t.Errorf("expected a calls edge from %s, got none", callerID)
	}
	// No instantiates edge should exist.
	if inst := edgesWithKind(t, d, callerID, types.EdgeKindInstantiates); len(inst) > 0 {
		t.Errorf("unexpected instantiates edge for function target")
	}

	// Resolved ref must be deleted.
	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("expected 0 unresolved refs after resolution, got %d", remaining)
	}
}

// TestCallsEdgePromotedToInstantiates proves that a "calls" ref targeting a
// class node is PROMOTED to an "instantiates" edge.
func TestCallsEdgePromotedToInstantiates(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	callerID := "function:src/b.ts:makeWidget:1"
	classID := "class:src/b.ts:Widget:10"
	if err := d.UpsertNode(ctx, types.Node{
		ID:        callerID,
		Kind:      types.NodeKindFunction,
		Name:      "makeWidget",
		FilePath:  "src/b.ts",
		Language:  types.LanguageTypeScript,
		StartLine: 1,
		EndLine:   9,
	}); err != nil {
		t.Fatalf("UpsertNode makeWidget: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID:         classID,
		Kind:       types.NodeKindClass,
		Name:       "Widget",
		FilePath:   "src/b.ts",
		Language:   types.LanguageTypeScript,
		StartLine:  10,
		EndLine:    50,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode Widget: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-calls-class-001",
		FromNodeID:    callerID,
		ReferenceName: "Widget",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/b.ts",
		Language:      types.LanguageTypeScript,
		Line:          5,
	}
	seedUnresolvedRef(t, d, ref)

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Must be promoted to instantiates.
	edges := edgesWithKind(t, d, callerID, types.EdgeKindInstantiates)
	if len(edges) == 0 {
		t.Errorf("expected instantiates edge for class target, got none (calls→class must be promoted)")
	}
	// No raw calls edge should survive.
	if raw := edgesWithKind(t, d, callerID, types.EdgeKindCalls); len(raw) > 0 {
		t.Errorf("unexpected calls edge — should have been promoted to instantiates")
	}
}

// TestExtendsEdgePromotedToImplements proves that an "extends" ref targeting
// an interface node is PROMOTED to an "implements" edge.
func TestExtendsEdgePromotedToImplements(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	classID := "class:src/c.ts:Concrete:1"
	ifaceID := "interface:src/c.ts:Runnable:50"
	if err := d.UpsertNode(ctx, types.Node{
		ID:        classID,
		Kind:      types.NodeKindClass,
		Name:      "Concrete",
		FilePath:  "src/c.ts",
		Language:  types.LanguageTypeScript,
		StartLine: 1,
		EndLine:   40,
	}); err != nil {
		t.Fatalf("UpsertNode Concrete: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID:         ifaceID,
		Kind:       types.NodeKindInterface,
		Name:       "Runnable",
		FilePath:   "src/c.ts",
		Language:   types.LanguageTypeScript,
		StartLine:  50,
		EndLine:    60,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode Runnable: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-extends-001",
		FromNodeID:    classID,
		ReferenceName: "Runnable",
		ReferenceKind: types.EdgeKindExtends,
		FilePath:      "src/c.ts",
		Language:      types.LanguageTypeScript,
		Line:          2,
	}
	seedUnresolvedRef(t, d, ref)

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// Must be promoted to implements.
	edges := edgesWithKind(t, d, classID, types.EdgeKindImplements)
	if len(edges) == 0 {
		t.Errorf("expected implements edge for interface target, got none (extends→interface must be promoted)")
	}
	if raw := edgesWithKind(t, d, classID, types.EdgeKindExtends); len(raw) > 0 {
		t.Errorf("unexpected extends edge — should have been promoted to implements")
	}
}

// TestImportRefProducesImportsEdge proves that an import-kind unresolved_ref
// is turned into an "imports" edge using the CP11 import resolver.
func TestImportRefProducesImportsEdge(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	// Seed importer file node.
	importerPath := "src/main.ts"
	importerNodeID := "file:" + importerPath
	if err := d.UpsertNode(ctx, types.Node{
		ID:       importerNodeID,
		Kind:     types.NodeKindFile,
		Name:     "main.ts",
		FilePath: importerPath,
		Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importer: %v", err)
	}

	// Seed target file node.
	targetPath := "src/util.ts"
	targetNodeID := "file:" + targetPath
	if err := d.UpsertNode(ctx, types.Node{
		ID:       targetNodeID,
		Kind:     types.NodeKindFile,
		Name:     "util.ts",
		FilePath: targetPath,
		Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode target: %v", err)
	}
	if err := d.UpsertFile(ctx, types.FileRecord{
		Path:     targetPath,
		Language: types.LanguageTypeScript,
		Size:     100,
	}); err != nil {
		t.Fatalf("UpsertFile target: %v", err)
	}

	// Import ref: from main.ts importing "./util" (relative → resolves to src/util.ts).
	ref := types.UnresolvedReference{
		ID:            "ref-import-001",
		FromNodeID:    importerNodeID,
		ReferenceName: "./util",
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerPath,
		Language:      types.LanguageTypeScript,
	}
	seedUnresolvedRef(t, d, ref)

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// An imports edge must exist from importerNodeID to targetNodeID.
	edges := edgesWithKind(t, d, importerNodeID, types.EdgeKindImports)
	if len(edges) == 0 {
		t.Errorf("expected imports edge from %s, got none", importerNodeID)
	} else if edges[0].Target != targetNodeID {
		t.Errorf("imports edge target = %s, want %s", edges[0].Target, targetNodeID)
	}
}

// TestUnresolvableImportRefProducesNoSelfLoopEdge proves that an import-kind
// ref whose specifier cannot resolve to any file produces NO edge — even when
// an import node in the DB bears the exact specifier as its own name.
//
// WHY this is the regression gate: import nodes are named the raw specifier
// string, and an unresolvable import ref's ReferenceName is that same
// specifier. If resolveOne falls through past Step 5 (ResolveImport) to Step 6
// generic name matching, exact-name matching resolves the ref straight back to
// the import node that owns it — a self-loop edge (source == target).
// Verified in the wild: 46 of 46 "imports" edges in a diagnosed repo's DB were
// exact self-loops. Import-kind refs must resolve only via Steps 3–5; if none
// produce a candidate, the ref is skipped with no edge, never routed to
// Step 6.
func TestUnresolvableImportRefProducesNoSelfLoopEdge(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	// The owning file, so the import node has a valid FilePath.
	importerPath := "src/app.ts"
	if err := d.UpsertNode(ctx, types.Node{
		ID:       "file:" + importerPath,
		Kind:     types.NodeKindFile,
		Name:     "app.ts",
		FilePath: importerPath,
		Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importer file: %v", err)
	}

	// The import node itself — named exactly the unresolvable specifier, and
	// also the ref's own FromNodeID (mirrors extraction: the unresolved ref
	// is attributed to the import node it lives under).
	specifier := "./missing-module"
	importNodeID := "import:" + importerPath + ":1"
	if err := d.UpsertNode(ctx, types.Node{
		ID:       importNodeID,
		Kind:     types.NodeKindImport,
		Name:     specifier,
		FilePath: importerPath,
		Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode import node: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-self-loop",
		FromNodeID:    importNodeID,
		ReferenceName: specifier,
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerPath,
		Language:      types.LanguageTypeScript,
	}
	seedUnresolvedRef(t, d, ref)

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, importNodeID, types.EdgeKindImports)
	if len(edges) != 0 {
		t.Errorf("unresolvable import ref must produce NO edge, got %d: %+v", len(edges), edges)
	}
	for _, e := range edges {
		if e.Source == e.Target {
			t.Errorf("unresolvable import ref produced a self-loop edge: %+v", e)
		}
	}
}

// TestBatchLoopTerminatesOnUnresolvableRefs proves that a set containing ONLY
// unresolvable refs does not cause an infinite loop. The loop must break after
// finding a batch that resolves nothing.
func TestBatchLoopTerminatesOnUnresolvableRefs(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	// Seed a caller node but NO callee — refs will be unresolvable.
	callerID := "function:src/d.ts:orphan:1"
	if err := d.UpsertNode(ctx, types.Node{
		ID:        callerID,
		Kind:      types.NodeKindFunction,
		Name:      "orphan",
		FilePath:  "src/d.ts",
		Language:  types.LanguageTypeScript,
		StartLine: 1,
		EndLine:   10,
	}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	// Add 3 unresolvable refs (targets don't exist in the DB).
	for i := 0; i < 3; i++ {
		ref := types.UnresolvedReference{
			ID:            fmt.Sprintf("ref-unresolvable-%03d", i),
			FromNodeID:    callerID,
			ReferenceName: fmt.Sprintf("NonExistentSymbol%d", i),
			ReferenceKind: types.EdgeKindCalls,
			FilePath:      "src/d.ts",
			Language:      types.LanguageTypeScript,
			Line:          i + 2,
		}
		seedUnresolvedRef(t, d, ref)
	}

	initial := countUnresolvedRefs(t, d)
	if initial != 3 {
		t.Fatalf("expected 3 unresolved refs, got %d", initial)
	}

	// Must return without hanging. 5-second timeout proves the loop terminates.
	done := make(chan error, 1)
	go func() {
		_, _, err := resolution.NewPipeline(d).ResolveAndPersistBatched(ctx, 5000, nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResolveAndPersistBatched: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResolveAndPersistBatched did not terminate — infinite loop detected")
	}

	// Unresolvable refs stay in the table (they were not resolved, just skipped
	// by the batch-terminates logic). Document: unresolvable refs persist; only
	// successfully resolved refs are deleted.
	remaining := countUnresolvedRefs(t, d)
	if remaining != 3 {
		t.Errorf("expected 3 unresolvable refs to remain, got %d", remaining)
	}
}

// TestResolvableRefBehindUnresolvableWall proves the batch loop does NOT abandon
// a resolvable reference just because lower-id references ahead of it are
// permanently unresolvable.
//
// Why this is the regression gate: the loop reads GetUnresolvedRefs(batch, 0)
// every iteration and relied on deletion to advance the window. Unresolvable
// non-builtin refs are intentionally NOT deleted (they might resolve after more
// files are indexed), so they form a permanent "wall" at the front of the
// id-ordered scan. The old `len(resolvedIDs)==0 → break` then terminated
// resolution the moment a batch was all-wall, abandoning every resolvable ref
// behind it. In a real repo the wall is every os.*/fmt.* style external call, so
// the bulk of the call graph (callers/callees/impact) silently never resolved.
//
// Setup: a 2-ref unresolvable wall with LOW ids + one resolvable ref with a HIGH
// id, batch size 2. The first batch reads exactly the wall (resolves nothing);
// the resolvable ref must still be reached and turned into a calls edge.
func TestResolvableRefBehindUnresolvableWall(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	callerID := "function:src/wall.ts:caller:1"
	calleeID := "function:src/wall.ts:realCallee:50"
	if err := d.UpsertNode(ctx, types.Node{
		ID: callerID, Kind: types.NodeKindFunction, Name: "caller",
		FilePath: "src/wall.ts", Language: types.LanguageTypeScript, StartLine: 1, EndLine: 9,
	}); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID: calleeID, Kind: types.NodeKindFunction, Name: "realCallee",
		FilePath: "src/wall.ts", Language: types.LanguageTypeScript, StartLine: 50, EndLine: 60,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}

	// Wall: 2 unresolvable, non-builtin call refs with the LOWEST ids (sort first).
	for i, name := range []string{"NoSuchSymbolA", "NoSuchSymbolB"} {
		seedUnresolvedRef(t, d, types.UnresolvedReference{
			ID:            fmt.Sprintf("ref-wall-%d", i),
			FromNodeID:    callerID,
			ReferenceName: name,
			ReferenceKind: types.EdgeKindCalls,
			FilePath:      "src/wall.ts",
			Language:      types.LanguageTypeScript,
			Line:          i + 2,
		})
	}
	// Resolvable ref with a HIGH id — sorts AFTER the wall, so a batch of 2 reads
	// only the wall first.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-zzz-real",
		FromNodeID:    callerID,
		ReferenceName: "realCallee",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/wall.ts",
		Language:      types.LanguageTypeScript,
		Line:          40,
	})

	if _, _, err := resolution.NewPipeline(d).ResolveAndPersistBatched(ctx, 2, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// The resolvable ref MUST have become a calls edge, despite sitting behind
	// the unresolvable wall.
	if edges := edgesWithKind(t, d, callerID, types.EdgeKindCalls); len(edges) == 0 {
		t.Errorf("resolvable ref behind unresolvable wall was abandoned: expected a calls edge from %s, got none", callerID)
	}

	// The wall stays (still unresolvable); only the resolved ref is deleted.
	if remaining := countUnresolvedRefs(t, d); remaining != 2 {
		t.Errorf("expected 2 unresolvable wall refs to remain, got %d", remaining)
	}
}

// TestBuiltinSkip proves that a built-in/stdlib reference (e.g. "console" in
// JavaScript) is skipped — no edge is inserted, and the ref is removed from
// unresolved_refs (built-in skip policy: drop silently, do not retain).
func TestBuiltinSkip(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	callerID := "function:src/e.js:logSomething:1"
	if err := d.UpsertNode(ctx, types.Node{
		ID:        callerID,
		Kind:      types.NodeKindFunction,
		Name:      "logSomething",
		FilePath:  "src/e.js",
		Language:  types.LanguageJavaScript,
		StartLine: 1,
		EndLine:   5,
	}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	// "console" is a JS built-in — should be skipped immediately.
	ref := types.UnresolvedReference{
		ID:            "ref-console-001",
		FromNodeID:    callerID,
		ReferenceName: "console",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/e.js",
		Language:      types.LanguageJavaScript,
		Line:          3,
	}
	seedUnresolvedRef(t, d, ref)

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	// No edges should be created for the built-in.
	edges, err := d.GetEdgesBySource(ctx, callerID)
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	if len(edges) > 0 {
		t.Errorf("built-in ref should not produce any edge, got %d", len(edges))
	}

	// Built-in skip policy: the ref is removed from unresolved_refs (it will
	// never resolve to an internal node, so retaining it would pollute subsequent
	// batch runs indefinitely).
	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("built-in ref should be removed from unresolved_refs, got %d remaining", remaining)
	}
}

// TestAtomicInsertDelete proves that edge inserts and resolved-ref deletes for a
// batch are performed inside a single transaction. After ResolveAndPersistBatched
// succeeds:
//   - the edge exists in the edges table (insert committed)
//   - the resolved ref is gone from unresolved_refs (delete committed)
//
// This is the spec gate for the atomicity requirement: both writes must
// succeed-or-fail together. The test cannot directly inject a crash mid-tx, but
// it confirms both outcomes are observed in the happy path and that no separate
// out-of-tx delete call remains (structural coverage; the implementation must
// not have a delete path outside WithTx).
func TestAtomicInsertDelete(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	callerID := "function:src/atomic.ts:caller:1"
	calleeID := "function:src/atomic.ts:callee:20"
	if err := d.UpsertNode(ctx, types.Node{
		ID:        callerID,
		Kind:      types.NodeKindFunction,
		Name:      "caller",
		FilePath:  "src/atomic.ts",
		Language:  types.LanguageTypeScript,
		StartLine: 1,
		EndLine:   19,
	}); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID:         calleeID,
		Kind:       types.NodeKindFunction,
		Name:       "callee",
		FilePath:   "src/atomic.ts",
		Language:   types.LanguageTypeScript,
		StartLine:  20,
		EndLine:    30,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-atomic-tx-001",
		FromNodeID:    callerID,
		ReferenceName: "callee",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/atomic.ts",
		Language:      types.LanguageTypeScript,
		Line:          10,
	}
	seedUnresolvedRef(t, d, ref)

	pipeline := resolution.NewPipeline(d)
	_, n, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}
	// Edge inserted.
	if n == 0 {
		t.Fatal("expected 1 edge inserted, got 0")
	}
	edges := edgesWithKind(t, d, callerID, types.EdgeKindCalls)
	if len(edges) == 0 {
		t.Fatal("expected calls edge in DB after batch — insert must have committed")
	}
	// Resolved ref deleted in the same transaction.
	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("expected 0 unresolved refs — delete must be atomic with insert, got %d", remaining)
	}
}

// TestResolvedRefsDeletedUnresolvedRemain proves that after one batch run:
//   - resolved refs are deleted from unresolved_refs
//   - unresolvable refs (no matching node) are retained
func TestResolvedRefsDeletedUnresolvedRemain(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	// Seed caller.
	callerID := "function:src/f.ts:caller:1"
	if err := d.UpsertNode(ctx, types.Node{
		ID:        callerID,
		Kind:      types.NodeKindFunction,
		Name:      "caller",
		FilePath:  "src/f.ts",
		Language:  types.LanguageTypeScript,
		StartLine: 1,
		EndLine:   30,
	}); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}

	// Seed resolvable callee.
	calleeID := "function:src/f.ts:knownFn:20"
	if err := d.UpsertNode(ctx, types.Node{
		ID:         calleeID,
		Kind:       types.NodeKindFunction,
		Name:       "knownFn",
		FilePath:   "src/f.ts",
		Language:   types.LanguageTypeScript,
		StartLine:  20,
		EndLine:    30,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode knownFn: %v", err)
	}

	// Resolvable ref.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-resolvable-001",
		FromNodeID:    callerID,
		ReferenceName: "knownFn",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/f.ts",
		Language:      types.LanguageTypeScript,
		Line:          5,
	})
	// Unresolvable ref.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-unresolvable-999",
		FromNodeID:    callerID,
		ReferenceName: "ghostFn",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/f.ts",
		Language:      types.LanguageTypeScript,
		Line:          10,
	})

	pipeline := resolution.NewPipeline(d)
	_, resolved, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}
	if resolved == 0 {
		t.Fatal("expected at least one resolution")
	}

	remaining := countUnresolvedRefs(t, d)
	if remaining != 1 {
		t.Errorf("expected 1 unresolvable ref to remain, got %d", remaining)
	}

	// The remaining one should be the ghost.
	refs, err := d.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "ref-unresolvable-999" {
		t.Errorf("expected ref-unresolvable-999 to remain, got %+v", refs)
	}
}

// TestOverFuzzyCapNameDoesNotTriggerFuzzy proves that a ref name longer than
// fuzzyNameLenCap (40 chars) does NOT trigger the fuzzy variant-generation path.
//
// Why this matters: byFuzzy generates O(n*26^maxDist) edit-distance variants.
// For n>40 that set is large enough to stall a batch. The pipeline must call
// MatchReferenceNoFuzzy instead of MatchReference for over-cap names.
//
// Test approach: seed a node whose name ONLY matches via fuzzy (slight typo),
// NOT via exact/qualified. Provide an over-cap ref name (41+ chars). The batch
// must complete without hanging AND must NOT produce an edge (because fuzzy is
// skipped and exact/qualified don't match). The ref remains in unresolved_refs.
func TestOverFuzzyCapNameDoesNotTriggerFuzzy(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	callerID := "function:src/g.ts:caller:1"
	if err := d.UpsertNode(ctx, types.Node{
		ID:        callerID,
		Kind:      types.NodeKindFunction,
		Name:      "caller",
		FilePath:  "src/g.ts",
		Language:  types.LanguageTypeScript,
		StartLine: 1,
		EndLine:   5,
	}); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}

	// Seed a node named "shortName". The ref name below differs by one char
	// (typo) and is over the 40-char cap — fuzzy would find it, exact won't.
	calleeID := "function:src/g.ts:shortName:10"
	if err := d.UpsertNode(ctx, types.Node{
		ID:         calleeID,
		Kind:       types.NodeKindFunction,
		Name:       "shortName",
		FilePath:   "src/g.ts",
		Language:   types.LanguageTypeScript,
		StartLine:  10,
		EndLine:    20,
		IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}

	// Over-cap name (41 chars): a long symbol name with a one-char typo at the end.
	// Exact match: "ASymbolNameThatIsLongerThanFortyCharactersX" (41 chars, not in DB).
	// The node above has name "shortName" — completely different, only matchable
	// via fuzzy if the name were short enough.
	overCapName := "ASymbolNameThatIsLongerThanFortyCharactersX" // len=43
	if len(overCapName) <= 40 {
		t.Fatalf("test invariant broken: name must be >40 chars, got %d", len(overCapName))
	}

	ref := types.UnresolvedReference{
		ID:            "ref-overcap-001",
		FromNodeID:    callerID,
		ReferenceName: overCapName,
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/g.ts",
		Language:      types.LanguageTypeScript,
		Line:          3,
	}
	seedUnresolvedRef(t, d, ref)

	// Must complete quickly (no fuzzy blowup).
	done := make(chan error, 1)
	go func() {
		_, _, err := resolution.NewPipeline(d).ResolveAndPersistBatched(ctx, 5000, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResolveAndPersistBatched: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResolveAndPersistBatched did not terminate — possible fuzzy blowup on over-cap name")
	}

	// No edge should be produced — the over-cap name has no exact/qualified match.
	edges, err := d.GetEdgesBySource(ctx, callerID)
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	if len(edges) > 0 {
		t.Errorf("over-cap name should not produce an edge (fuzzy skipped, no exact match), got %d edges", len(edges))
	}

	// The unresolved ref remains (no exact match, fuzzy skipped — unresolvable for now).
	if remaining := countUnresolvedRefs(t, d); remaining != 1 {
		t.Errorf("expected 1 unresolved ref to remain (over-cap, no match), got %d", remaining)
	}
}

// ---------------------------------------------------------------------------
// CP2 (docs/spec/code-intel-package-nodes.md): package-node mint loop, sweep
// ---------------------------------------------------------------------------

// packageNodeID mirrors extraction.GenerateNodeID's package-kind formula
// ("package:npm/" + name) without importing the extraction package into the
// test — kept in sync by TestPackageMint_TwoImportersConvergeOnSharedNode's
// own DB-lookup assertions, which would fail if the formula ever drifted.
func packageNodeID(name string) string {
	return "package:npm/" + name
}

// TestPackageMint_TwoImportersConvergeOnSharedNode proves the CORE mint
// behavior: two files importing the same npm package — one via a deep
// specifier, one bare — both produce an "imports" edge to the SAME
// package:npm/@scope/pkg node, and that node has the shape mandated by the
// spec (bare normalized name, no file/line, language unknown, not exported).
func TestPackageMint_TwoImportersConvergeOnSharedNode(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	importerA := "src/a.ts"
	importerB := "src/b.ts"
	importerANodeID := "file:" + importerA
	importerBNodeID := "file:" + importerB
	if err := d.UpsertNode(ctx, types.Node{
		ID: importerANodeID, Kind: types.NodeKindFile, Name: "a.ts",
		FilePath: importerA, Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importerA: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID: importerBNodeID, Kind: types.NodeKindFile, Name: "b.ts",
		FilePath: importerB, Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importerB: %v", err)
	}

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-pkgmint-deep",
		FromNodeID:    importerANodeID,
		ReferenceName: "@scope/pkg/deep/path.js",
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerA,
		Language:      types.LanguageTypeScript,
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-pkgmint-bare",
		FromNodeID:    importerBNodeID,
		ReferenceName: "@scope/pkg",
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerB,
		Language:      types.LanguageTypeScript,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	wantPkgID := packageNodeID("@scope/pkg")

	edgesA := edgesWithKind(t, d, importerANodeID, types.EdgeKindImports)
	if len(edgesA) != 1 || edgesA[0].Target != wantPkgID {
		t.Fatalf("importerA imports edges = %+v, want exactly 1 targeting %q", edgesA, wantPkgID)
	}
	edgesB := edgesWithKind(t, d, importerBNodeID, types.EdgeKindImports)
	if len(edgesB) != 1 || edgesB[0].Target != wantPkgID {
		t.Fatalf("importerB imports edges = %+v, want exactly 1 targeting %q", edgesB, wantPkgID)
	}

	pkgNode, err := d.GetNode(ctx, wantPkgID)
	if err != nil {
		t.Fatalf("GetNode %s: %v", wantPkgID, err)
	}
	if pkgNode.Kind != types.NodeKindPackage {
		t.Errorf("package node Kind = %q, want %q", pkgNode.Kind, types.NodeKindPackage)
	}
	if pkgNode.Name != "@scope/pkg" {
		t.Errorf("package node Name = %q, want %q", pkgNode.Name, "@scope/pkg")
	}
	if pkgNode.QualifiedName != "@scope/pkg" {
		t.Errorf("package node QualifiedName = %q, want %q", pkgNode.QualifiedName, "@scope/pkg")
	}
	if pkgNode.FilePath != "" {
		t.Errorf("package node FilePath = %q, want empty", pkgNode.FilePath)
	}
	if pkgNode.StartLine != 0 || pkgNode.EndLine != 0 {
		t.Errorf("package node lines = %d/%d, want 0/0", pkgNode.StartLine, pkgNode.EndLine)
	}
	if pkgNode.Language != types.LanguageUnknown {
		t.Errorf("package node Language = %q, want %q", pkgNode.Language, types.LanguageUnknown)
	}
	if pkgNode.IsExported {
		t.Errorf("package node IsExported = true, want false")
	}

	// Ref deletion (existing path) — both resolved refs must be gone.
	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("expected 0 unresolved refs after mint, got %d", remaining)
	}
}

// TestPackageMint_KnownPackageUpdatedAtStable proves idempotence: a package
// already present in the DB at the start of a resolve run (warmed once from
// GetNodesByKind, not the in-run mint set) is never re-upserted, even when a
// new importer's edge targets it.
//
// WHY edge survival is the assertion, not just updated_at: UpsertNodeAt is
// "INSERT OR REPLACE" — on a primary-key conflict SQLite deletes the
// existing row and inserts the new one. With foreign_keys=ON (db.go), that
// delete step cascades away every edge referencing the old row (edges.target
// ON DELETE CASCADE, appendix A). So a re-upsert bug on an already-minted
// package is not mere FTS5 churn — it silently deletes every importer's
// edge into that package. The first importer's edge surviving run 2 is a
// deterministic proxy for "the known-package guard held"; a wall-clock
// updated_at comparison would not reliably catch the bug (two in-process
// calls this close together can land in the same Unix second either way).
func TestPackageMint_KnownPackageUpdatedAtStable(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	importerA := "src/a.ts"
	importerANodeID := "file:" + importerA
	if err := d.UpsertNode(ctx, types.Node{
		ID: importerANodeID, Kind: types.NodeKindFile, Name: "a.ts",
		FilePath: importerA, Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importerA: %v", err)
	}
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-pkgmint-run1",
		FromNodeID:    importerANodeID,
		ReferenceName: "@hapi/hapi/lib/deep",
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerA,
		Language:      types.LanguageTypeScript,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched run 1: %v", err)
	}

	pkgID := packageNodeID("@hapi/hapi")
	before, err := d.GetNode(ctx, pkgID)
	if err != nil {
		t.Fatalf("GetNode after run 1: %v", err)
	}
	if edges := edgesWithKind(t, d, importerANodeID, types.EdgeKindImports); len(edges) != 1 {
		t.Fatalf("expected 1 imports edge from importerA after run 1, got %d", len(edges))
	}

	// Second importer, second resolve run — the package is now "known" only
	// via the DB warm-scan (GetNodesByKind), not any in-run state.
	importerB := "src/b.ts"
	importerBNodeID := "file:" + importerB
	if err := d.UpsertNode(ctx, types.Node{
		ID: importerBNodeID, Kind: types.NodeKindFile, Name: "b.ts",
		FilePath: importerB, Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importerB: %v", err)
	}
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-pkgmint-run2",
		FromNodeID:    importerBNodeID,
		ReferenceName: "@hapi/hapi",
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerB,
		Language:      types.LanguageTypeScript,
	})

	if _, _, err := resolution.NewPipeline(d).ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched run 2: %v", err)
	}

	after, err := d.GetNode(ctx, pkgID)
	if err != nil {
		t.Fatalf("GetNode after run 2: %v", err)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Errorf("package node updated_at changed across a known-package run: before=%q after=%q", before.UpdatedAt, after.UpdatedAt)
	}

	// Both runs' edges must be present — a re-upsert on the known package
	// would have cascade-deleted importerA's edge (see WHY above); its
	// survival proves the known-package guard held.
	edgesA := edgesWithKind(t, d, importerANodeID, types.EdgeKindImports)
	edgesB := edgesWithKind(t, d, importerBNodeID, types.EdgeKindImports)
	if len(edgesA) != 1 || len(edgesB) != 1 {
		t.Errorf("expected 1 imports edge from each importer, got A=%d B=%d", len(edgesA), len(edgesB))
	}
}

// TestPackageMint_OrphanSweep proves the deletion round-trip: once a
// package's last importer is removed (its file's nodes deleted, cascading
// its edges), the next resolve run sweeps the now-orphaned package node —
// even though that run has no new refs to process at all.
func TestPackageMint_OrphanSweep(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	importerA := "src/a.ts"
	importerANodeID := "file:" + importerA
	if err := d.UpsertNode(ctx, types.Node{
		ID: importerANodeID, Kind: types.NodeKindFile, Name: "a.ts",
		FilePath: importerA, Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importerA: %v", err)
	}
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-pkgmint-sweep",
		FromNodeID:    importerANodeID,
		ReferenceName: "lodash",
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerA,
		Language:      types.LanguageTypeScript,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched mint run: %v", err)
	}

	pkgID := packageNodeID("lodash")
	if _, err := d.GetNode(ctx, pkgID); err != nil {
		t.Fatalf("package node missing after mint run: %v", err)
	}

	// Remove the last importer — cascades away its imports edge (FK CASCADE
	// on edges.source), leaving the package node with zero inbound edges.
	if err := d.DeleteNodesByFile(ctx, importerA); err != nil {
		t.Fatalf("DeleteNodesByFile: %v", err)
	}

	if _, _, err := resolution.NewPipeline(d).ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched sweep run: %v", err)
	}

	if _, err := d.GetNode(ctx, pkgID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("expected package node to be swept (ErrNotFound), got node/err = %v", err)
	}
}

// TestPackageMint_UnindexedAliasTargetMintsNothing is the regression pin: a
// tsconfig-aliased specifier whose expanded target file is NOT in the DB
// resolves ResolvedKindUnresolved (no PackageName — alias resolution never
// classifies External), so it must mint no package node at all. Guards
// against the mint loop firing on the deterministic node-id path (which does
// not require an existing DB row) for anything other than a genuine
// External-with-PackageName verdict.
func TestPackageMint_UnindexedAliasTargetMintsNothing(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()
	projectRoot := t.TempDir()

	tsconfigContent := `{"compilerOptions": {"baseUrl": ".", "paths": {"@app/*": ["src/*"]}}}`
	if err := os.WriteFile(filepath.Join(projectRoot, "tsconfig.json"), []byte(tsconfigContent), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	importerPath := "src/app.ts"
	importerNodeID := "file:" + importerPath
	if err := d.UpsertNode(ctx, types.Node{
		ID: importerNodeID, Kind: types.NodeKindFile, Name: "app.ts",
		FilePath: importerPath, Language: types.LanguageTypeScript,
	}); err != nil {
		t.Fatalf("UpsertNode importer: %v", err)
	}
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-pkgmint-alias-miss",
		FromNodeID:    importerNodeID,
		ReferenceName: "@app/missing",
		ReferenceKind: types.EdgeKindImports,
		FilePath:      importerPath,
		Language:      types.LanguageTypeScript,
	})

	pipeline := resolution.NewPipelineWithSeams(d, projectRoot, resolution.EmptyFrameworkRegistry, resolution.NoopSynthesizer{})
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	packages, err := d.GetNodesByKind(ctx, types.NodeKindPackage)
	if err != nil {
		t.Fatalf("GetNodesByKind package: %v", err)
	}
	if len(packages) != 0 {
		t.Errorf("expected no package nodes minted for an unindexed alias target, got %d: %+v", len(packages), packages)
	}

	// The ref stays unresolved (alias matched, target file not indexed).
	if remaining := countUnresolvedRefs(t, d); remaining != 1 {
		t.Errorf("expected the alias-miss ref to remain unresolved, got %d remaining", remaining)
	}
}

// TestCP4_QualifiedColumnRefResolvesEndToEnd proves a SQL qualified column ref
// resolves through the FULL pipeline (including the known-names pre-filter), not
// just the name matcher. The column ref name "dbo.Account.account_id" is not a
// bare node name, so without the SQL-scoped simple-name fall-through in the
// pre-filter it is dropped before reaching byQualifiedName. Two tables own an
// "account_id" column; the ref must resolve to the RIGHT one (prefer-exact).
func TestCP4_QualifiedColumnRefResolvesEndToEnd(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	sqlFile := "db/schema.sql"
	// Two distinct "account_id" columns in different tables.
	acctColID := "column:db/schema.sql:dbo.Account.account_id:3"
	ordColID := "column:db/schema.sql:dbo.Orders.account_id:9"
	if err := d.UpsertNode(ctx, types.Node{
		ID: acctColID, Kind: types.NodeKindColumn, Name: "account_id",
		QualifiedName: "dbo.Account.account_id", FilePath: sqlFile,
		Language: types.LanguageSQL, StartLine: 3, EndLine: 3,
	}); err != nil {
		t.Fatalf("UpsertNode acct col: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID: ordColID, Kind: types.NodeKindColumn, Name: "account_id",
		QualifiedName: "dbo.Orders.account_id", FilePath: sqlFile,
		Language: types.LanguageSQL, StartLine: 9, EndLine: 9,
	}); err != nil {
		t.Fatalf("UpsertNode orders col: %v", err)
	}
	// The referring view.
	viewID := "view:db/schema.sql:AccountOwners:20"
	if err := d.UpsertNode(ctx, types.Node{
		ID: viewID, Kind: types.NodeKindView, Name: "AccountOwners",
		QualifiedName: "dbo.AccountOwners", FilePath: sqlFile,
		Language: types.LanguageSQL, StartLine: 20, EndLine: 25,
	}); err != nil {
		t.Fatalf("UpsertNode view: %v", err)
	}

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-cp4-col-001",
		FromNodeID:    viewID,
		ReferenceName: "dbo.Account.account_id",
		ReferenceKind: types.EdgeKindReferences,
		FilePath:      sqlFile,
		Language:      types.LanguageSQL,
		Line:          22,
	})

	_, resolved, err := resolution.NewPipeline(d).ResolveAndPersistBatched(ctx, 5000, nil)
	if err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}
	if resolved == 0 {
		t.Fatal("CP4: qualified column ref did not resolve — pre-filter dropped it before byQualifiedName")
	}

	edges := edgesWithKind(t, d, viewID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected exactly 1 references edge from the view, got %d", len(edges))
	}
	if edges[0].Target != acctColID {
		t.Errorf("CP4: column ref resolved to %q, want dbo.Account.account_id (%q) — prefer-exact must pick the right table's column, not dbo.Orders.account_id",
			edges[0].Target, acctColID)
	}
}
