package resolution_test

// Resolver pipeline tests. Helpers come from resolver_test.go, same package.

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

func seedUnresolvedRef(t *testing.T, d *db.DB, r types.UnresolvedReference) {
	t.Helper()
	ctx := context.Background()
	if err := d.InsertUnresolvedRef(ctx, r); err != nil {
		t.Fatalf("seedUnresolvedRef %s: %v", r.ID, err)
	}
}

func countUnresolvedRefs(t *testing.T, d *db.DB) int {
	t.Helper()
	refs, err := d.GetUnresolvedRefs(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("countUnresolvedRefs: %v", err)
	}
	return len(refs)
}

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

// openPipelineTestDB uses the system tmp dir; the project tmp/ is nested
// deeply enough to hit path-length problems on some macOS setups.
func openPipelineTestDB(t *testing.T) *db.DB {
	t.Helper()
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

func projectTmpDir() string {
	// Derived from the package dir, with a TempDir fallback: a missing tmp/
	// must not fail the suite.
	wd, err := os.Getwd()
	if err != nil {
		return os.TempDir()
	}
	candidate := filepath.Join(wd, "..", "..", "..", "..", "tmp", "code-intel")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return os.TempDir()
}

// A call to a function stays a call — promotion applies only to types.
func TestCallsEdgeToFunction(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	callerID := "function:src/a.ts:caller:1"
	calleeID := "function:src/a.ts:callee:10"
	seedNode(t, d, callerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, true)
	seedNode(t, d, calleeID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, true)

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

	edges := edgesWithKind(t, d, callerID, types.EdgeKindCalls)
	if len(edges) == 0 {
		t.Errorf("expected a calls edge from %s, got none", callerID)
	}
	if inst := edgesWithKind(t, d, callerID, types.EdgeKindInstantiates); len(inst) > 0 {
		t.Errorf("unexpected instantiates edge for function target")
	}

	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("expected 0 unresolved refs after resolution, got %d", remaining)
	}
}

// A call to a class is really an instantiation.
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

	edges := edgesWithKind(t, d, callerID, types.EdgeKindInstantiates)
	if len(edges) == 0 {
		t.Errorf("expected instantiates edge for class target, got none (calls→class must be promoted)")
	}
	if raw := edgesWithKind(t, d, callerID, types.EdgeKindCalls); len(raw) > 0 {
		t.Errorf("unexpected calls edge — should have been promoted to instantiates")
	}
}

// An extends of an interface is really an implements.
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

	edges := edgesWithKind(t, d, classID, types.EdgeKindImplements)
	if len(edges) == 0 {
		t.Errorf("expected implements edge for interface target, got none (extends→interface must be promoted)")
	}
	if raw := edgesWithKind(t, d, classID, types.EdgeKindExtends); len(raw) > 0 {
		t.Errorf("unexpected extends edge — should have been promoted to implements")
	}
}

func TestImportRefProducesImportsEdge(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

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

	edges := edgesWithKind(t, d, importerNodeID, types.EdgeKindImports)
	if len(edges) == 0 {
		t.Errorf("expected imports edge from %s, got none", importerNodeID)
	} else if edges[0].Target != targetNodeID {
		t.Errorf("imports edge target = %s, want %s", edges[0].Target, targetNodeID)
	}
}

// An import node is named its own specifier, and so is the ref, so generic
// name matching resolves the ref straight back to the node that owns it. Left
// unguarded this produced a graph where every imports edge was a self-loop.
func TestUnresolvableImportRefProducesNoSelfLoopEdge(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

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

	// Named exactly the unresolvable specifier, and also the ref's own owner —
	// which is what makes the self-loop possible.
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

// A table of nothing but unresolvable refs must still terminate.
func TestBatchLoopTerminatesOnUnresolvableRefs(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

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

	// Unresolvable refs persist; only resolved ones are deleted.
	remaining := countUnresolvedRefs(t, d)
	if remaining != 3 {
		t.Errorf("expected 3 unresolvable refs to remain, got %d", remaining)
	}
}

// Unresolvable refs are never deleted, so they pile up as a permanent wall at
// the front of the id-ordered scan. An offset-0 re-read plus a stop-when-
// nothing-resolved guard abandoned everything behind that wall — in a real
// repo, most of the call graph. The wall here is given the low ids and the
// resolvable ref a high one, so a batch of 2 reads only the wall first.
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

	if edges := edgesWithKind(t, d, callerID, types.EdgeKindCalls); len(edges) == 0 {
		t.Errorf("resolvable ref behind unresolvable wall was abandoned: expected a calls edge from %s, got none", callerID)
	}

	if remaining := countUnresolvedRefs(t, d); remaining != 2 {
		t.Errorf("expected 2 unresolvable wall refs to remain, got %d", remaining)
	}
}

// A built-in can never resolve, so its ref is dropped rather than retained.
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

	edges, err := d.GetEdgesBySource(ctx, callerID)
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	if len(edges) > 0 {
		t.Errorf("built-in ref should not produce any edge, got %d", len(edges))
	}

	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("built-in ref should be removed from unresolved_refs, got %d remaining", remaining)
	}
}

// Edge insert and ref delete must commit together. A crash cannot be injected
// mid-transaction here, so this asserts both outcomes in the happy path — the
// proxy for there being no delete path outside WithTx.
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
	if n == 0 {
		t.Fatal("expected 1 edge inserted, got 0")
	}
	edges := edgesWithKind(t, d, callerID, types.EdgeKindCalls)
	if len(edges) == 0 {
		t.Fatal("expected calls edge in DB after batch — insert must have committed")
	}
	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("expected 0 unresolved refs — delete must be atomic with insert, got %d", remaining)
	}
}

func TestResolvedRefsDeletedUnresolvedRemain(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

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

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-resolvable-001",
		FromNodeID:    callerID,
		ReferenceName: "knownFn",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      "src/f.ts",
		Language:      types.LanguageTypeScript,
		Line:          5,
	})
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

	refs, err := d.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "ref-unresolvable-999" {
		t.Errorf("expected ref-unresolvable-999 to remain, got %+v", refs)
	}
}

// Past the length cap the pipeline must skip fuzzy entirely, since the variant
// set stalls a batch. The seeded node is reachable only by fuzzy, so no edge
// appearing is what proves fuzzy did not run.
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

	edges, err := d.GetEdgesBySource(ctx, callerID)
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	if len(edges) > 0 {
		t.Errorf("over-cap name should not produce an edge (fuzzy skipped, no exact match), got %d edges", len(edges))
	}

	if remaining := countUnresolvedRefs(t, d); remaining != 1 {
		t.Errorf("expected 1 unresolved ref to remain (over-cap, no match), got %d", remaining)
	}
}

// packageNodeID duplicates extraction.GenerateNodeID's formula rather than
// import the package. Drift would fail the DB-lookup assertions below.
func packageNodeID(name string) string {
	return "package:npm/" + name
}

// A deep specifier and a bare one name the same package, so both importers
// must converge on one node.
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

	if remaining := countUnresolvedRefs(t, d); remaining != 0 {
		t.Errorf("expected 0 unresolved refs after mint, got %d", remaining)
	}
}

// A package already in the DB must never be re-upserted. Edge survival is the
// assertion rather than updated_at: UpsertNodeAt is INSERT OR REPLACE, so a
// re-upsert deletes the row and cascades away every edge pointing at it —
// silently unlinking every importer. Two calls this close together can also
// land in the same Unix second, which makes updated_at an unreliable witness.
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

	edgesA := edgesWithKind(t, d, importerANodeID, types.EdgeKindImports)
	edgesB := edgesWithKind(t, d, importerBNodeID, types.EdgeKindImports)
	if len(edgesA) != 1 || len(edgesB) != 1 {
		t.Errorf("expected 1 imports edge from each importer, got A=%d B=%d", len(edgesA), len(edgesB))
	}
}

// The sweep must run even when the invocation resolves nothing at all.
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

// Package node ids are computed, not looked up, so nothing stops the mint loop
// from firing on a non-External verdict. An alias whose target is unindexed is
// the case that would expose it.
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

	if remaining := countUnresolvedRefs(t, d); remaining != 1 {
		t.Errorf("expected the alias-miss ref to remain unresolved, got %d remaining", remaining)
	}
}

// The pre-filter, not just the matcher: a qualified column name is not a bare
// node name, so without the SQL simple-name fall-through the ref is dropped
// before byQualifiedName ever sees it. Two tables own an "account_id" column,
// so resolving to the right one is the second half of the assertion.
func TestQualifiedColumnRefResolvesEndToEnd(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	sqlFile := "db/schema.sql"
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
	viewID := "view:db/schema.sql:AccountOwners:20"
	if err := d.UpsertNode(ctx, types.Node{
		ID: viewID, Kind: types.NodeKindView, Name: "AccountOwners",
		QualifiedName: "dbo.AccountOwners", FilePath: sqlFile,
		Language: types.LanguageSQL, StartLine: 20, EndLine: 25,
	}); err != nil {
		t.Fatalf("UpsertNode view: %v", err)
	}

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID:            "ref-col-001",
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
		t.Fatal("qualified column ref did not resolve — pre-filter dropped it before byQualifiedName")
	}

	edges := edgesWithKind(t, d, viewID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected exactly 1 references edge from the view, got %d", len(edges))
	}
	if edges[0].Target != acctColID {
		t.Errorf("column ref resolved to %q, want dbo.Account.account_id (%q) — prefer-exact must pick the right table's column, not dbo.Orders.account_id",
			edges[0].Target, acctColID)
	}
}
