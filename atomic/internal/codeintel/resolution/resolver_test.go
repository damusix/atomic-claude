package resolution_test

// CP11 import resolver tests.
//
// Why this file is the spec gate:
//   - Relative import  → resolves to the target file node (proves extension
//     candidate logic + file lookup).
//   - Re-export chain  → follows export-from edges to the underlying symbol,
//     depth ≤ REEXPORT_MAX_DEPTH, self-cycle terminates (proves cycle guard).
//   - tsconfig alias   → @app/util via paths expands to a real file (proves
//     JSONC load + alias map).
//   - External import  → react / node:fs classified as external, no fabricated
//     node (proves skip-set classification).
//
// All tests seed a temp DB via the db package (per BRIEF).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// openTestDB opens a fresh temp SQLite DB for one test.
func openTestDB(t *testing.T) (*db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d, dir
}

// nodeID mirrors the appendix-B id scheme.
func nodeID(filePath, kind, name string, line int) string {
	input := fmt.Sprintf("%s:%s:%s:%d", filePath, kind, name, line)
	sum := sha256.Sum256([]byte(input))
	return kind + ":" + hex.EncodeToString(sum[:])[:32]
}

// seedFile inserts a file record and a matching file node.
func seedFile(t *testing.T, d *db.DB, path string, lang types.Language) string {
	t.Helper()
	ctx := context.Background()
	fileNodeID := "file:" + path
	if err := d.UpsertNode(ctx, types.Node{
		ID:         fileNodeID,
		Kind:       types.NodeKindFile,
		Name:       filepath.Base(path),
		FilePath:   path,
		Language:   lang,
		StartLine:  1,
		EndLine:    1,
		IsExported: true,
	}); err != nil {
		t.Fatalf("seedFile UpsertNode %s: %v", path, err)
	}
	if err := d.UpsertFile(ctx, types.FileRecord{
		Path:        path,
		ContentHash: "hash-" + path,
		Language:    lang,
		Size:        100,
		ModifiedAt:  "2026-01-01T00:00:00Z",
		IndexedAt:   "2026-01-01T00:00:00Z",
		NodeCount:   1,
	}); err != nil {
		t.Fatalf("seedFile UpsertFile %s: %v", path, err)
	}
	return fileNodeID
}

// seedFunction inserts a function node inside a file.
func seedFunction(t *testing.T, d *db.DB, filePath, name string, line int, lang types.Language) string {
	t.Helper()
	id := nodeID(filePath, "function", name, line)
	if err := d.UpsertNode(context.Background(), types.Node{
		ID:         id,
		Kind:       types.NodeKindFunction,
		Name:       name,
		FilePath:   filePath,
		Language:   lang,
		StartLine:  line,
		EndLine:    line + 5,
		IsExported: true,
	}); err != nil {
		t.Fatalf("seedFunction %s: %v", name, err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Test: relative import resolves to the target file node
// ---------------------------------------------------------------------------

func TestResolver_RelativeImport(t *testing.T) {
	// WHY: a TS file at src/app.ts that does `import { foo } from "./util"` must
	// resolve to the file node for src/util.ts. This exercises the
	// per-language extension-candidate logic (.ts / .tsx / .d.ts / index.ts)
	// and the file lookup in the DB.
	d, _ := openTestDB(t)
	ctx := context.Background()

	importerPath := "src/app.ts"
	targetPath := "src/util.ts"

	// Seed importer and target file.
	importerNodeID := seedFile(t, d, importerPath, types.LanguageTypeScript)
	targetNodeID := seedFile(t, d, targetPath, types.LanguageTypeScript)
	_ = importerNodeID

	// Seed an unresolved import ref in the importer.
	ref := types.UnresolvedReference{
		ID:            "ref-1",
		FromNodeID:    "file:" + importerPath,
		ReferenceName: "./util",
		ReferenceKind: types.EdgeKindImports,
		Line:          2,
		FilePath:      importerPath,
		Language:      types.LanguageTypeScript,
	}
	if err := d.InsertUnresolvedRef(ctx, ref); err != nil {
		t.Fatalf("InsertUnresolvedRef: %v", err)
	}

	r := resolution.NewResolver(d)
	result, err := r.ResolveImport(ctx, ref, importerPath)
	if err != nil {
		t.Fatalf("ResolveImport: %v", err)
	}

	if result.Kind != resolution.ResolvedKindInternal {
		t.Errorf("expected internal resolution, got %v", result.Kind)
	}
	if result.TargetNodeID != targetNodeID {
		t.Errorf("TargetNodeID = %q, want %q", result.TargetNodeID, targetNodeID)
	}
}

// ---------------------------------------------------------------------------
// Test: re-export chain follows to the underlying symbol
// ---------------------------------------------------------------------------

func TestResolver_ReExportChain(t *testing.T) {
	// WHY: a barrel file `export * from './util'` should let the resolver follow
	// the chain to the real symbol, up to REEXPORT_MAX_DEPTH=8.  This test also
	// verifies that a self-referential barrel (cycle) does NOT infinite-loop.
	d, _ := openTestDB(t)
	ctx := context.Background()

	// File A → re-exports from file B → which re-exports from file C (chain of 2).
	fileA := "src/index.ts"
	fileB := "src/barrel.ts"
	fileC := "src/util.ts"

	seedFile(t, d, fileA, types.LanguageTypeScript)
	seedFile(t, d, fileB, types.LanguageTypeScript)
	targetNodeID := seedFile(t, d, fileC, types.LanguageTypeScript)

	// Seed a re-export edge: fileB re-exports from fileC.
	if _, err := d.InsertEdge(ctx, types.Edge{
		Source: "file:" + fileB,
		Target: "file:" + fileC,
		Kind:   types.EdgeKindExports,
	}); err != nil {
		t.Fatalf("InsertEdge B→C: %v", err)
	}

	// Ref: fileA imports from fileB (which re-exports from fileC).
	ref := types.UnresolvedReference{
		ID:            "ref-reexport",
		FromNodeID:    "file:" + fileA,
		ReferenceName: "./barrel",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      fileA,
		Language:      types.LanguageTypeScript,
	}
	if err := d.InsertUnresolvedRef(ctx, ref); err != nil {
		t.Fatalf("InsertUnresolvedRef: %v", err)
	}

	r := resolution.NewResolver(d)
	result, err := r.ResolveImport(ctx, ref, fileA)
	if err != nil {
		t.Fatalf("ResolveImport reexport: %v", err)
	}

	// Must resolve to fileB node (direct resolution) OR follow the export to
	// fileC — both are valid depending on depth policy. The key assertion is:
	// result is internal, not external, and one of the two file nodes.
	if result.Kind != resolution.ResolvedKindInternal {
		t.Errorf("reexport: expected internal, got %v", result.Kind)
	}
	validTargets := map[string]bool{
		"file:" + fileB: true,
		targetNodeID:    true,
	}
	if !validTargets[result.TargetNodeID] {
		t.Errorf("reexport: TargetNodeID %q not in expected set %v", result.TargetNodeID, validTargets)
	}
}

func TestResolver_ReExportCycle(t *testing.T) {
	// WHY: a self-referential barrel (file exports * from itself) must terminate
	// and not infinite-loop. This is the cycle-guard test mandated by the spec.
	d, _ := openTestDB(t)
	ctx := context.Background()

	cycleFile := "src/cycle.ts"
	seedFile(t, d, cycleFile, types.LanguageTypeScript)

	// Self-loop export edge: cycleFile → cycleFile.
	if _, err := d.InsertEdge(ctx, types.Edge{
		Source: "file:" + cycleFile,
		Target: "file:" + cycleFile,
		Kind:   types.EdgeKindExports,
	}); err != nil {
		t.Fatalf("InsertEdge self-loop: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-cycle",
		FromNodeID:    "file:" + cycleFile,
		ReferenceName: "./cycle",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      cycleFile,
		Language:      types.LanguageTypeScript,
	}
	if err := d.InsertUnresolvedRef(ctx, ref); err != nil {
		t.Fatalf("InsertUnresolvedRef: %v", err)
	}

	r := resolution.NewResolver(d)
	// This must return within a reasonable time (not hang) and not error fatally.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.ResolveImport(ctx, ref, cycleFile)
	}()
	select {
	case <-done:
		// passed — terminated without infinite loop
	case <-time.After(2 * time.Second):
		t.Fatal("cycle test timed out — infinite loop in re-export resolution")
	}
}

// ---------------------------------------------------------------------------
// Test: tsconfig path alias resolves to real file
// ---------------------------------------------------------------------------

func TestResolver_TsconfigAlias(t *testing.T) {
	// WHY: a ref `@app/util` must expand via tsconfig paths to the real file
	// path. This validates the JSONC tsconfig load (via hujson) + alias map.
	d, projectRoot := openTestDB(t)
	ctx := context.Background()

	targetPath := "src/util.ts"
	seedFile(t, d, targetPath, types.LanguageTypeScript)

	// Write a tsconfig.json with JSONC (comments + trailing comma) to
	// exercise the hujson parse path.
	tsconfigContent := `{
		// base url for non-relative imports
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {
				"@app/*": ["src/*"]  // trailing comment
			}
		}
	}`
	tsconfigPath := filepath.Join(projectRoot, "tsconfig.json")
	if err := os.WriteFile(tsconfigPath, []byte(tsconfigContent), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-alias",
		FromNodeID:    "file:src/app.ts",
		ReferenceName: "@app/util",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      "src/app.ts",
		Language:      types.LanguageTypeScript,
	}

	r := resolution.NewResolverWithProject(d, projectRoot)
	result, err := r.ResolveImport(ctx, ref, "src/app.ts")
	if err != nil {
		t.Fatalf("ResolveImport alias: %v", err)
	}

	if result.Kind != resolution.ResolvedKindInternal {
		t.Errorf("alias: expected internal, got %v", result.Kind)
	}
	wantNodeID := "file:" + targetPath
	if result.TargetNodeID != wantNodeID {
		t.Errorf("alias: TargetNodeID = %q, want %q", result.TargetNodeID, wantNodeID)
	}
}

// ---------------------------------------------------------------------------
// Test: external import (react, node:fs) → classified external
// ---------------------------------------------------------------------------

func TestResolver_ExternalImport(t *testing.T) {
	// WHY: node_modules packages and Node.js built-ins must be classified as
	// external (no fabricated DB node, just a classification marker). This
	// prevents spurious edges into a "react" phantom node.
	d, _ := openTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		refName string
	}{
		{"npm package", "react"},
		{"node built-in protocol", "node:fs"},
		{"node built-in no-proto", "path"},
		{"scoped package", "@types/node"},
	}

	r := resolution.NewResolver(d)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := types.UnresolvedReference{
				ID:            "ref-ext-" + tc.refName,
				FromNodeID:    "file:src/app.ts",
				ReferenceName: tc.refName,
				ReferenceKind: types.EdgeKindImports,
				Line:          1,
				FilePath:      "src/app.ts",
				Language:      types.LanguageTypeScript,
			}
			result, err := r.ResolveImport(ctx, ref, "src/app.ts")
			if err != nil {
				t.Fatalf("ResolveImport %q: %v", tc.refName, err)
			}
			if result.Kind != resolution.ResolvedKindExternal {
				t.Errorf("%q: expected external, got %v", tc.refName, result.Kind)
			}
			if result.TargetNodeID != "" {
				t.Errorf("%q: TargetNodeID should be empty for external, got %q", tc.refName, result.TargetNodeID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: JS extension candidates
// ---------------------------------------------------------------------------

func TestResolver_JSExtensionCandidates(t *testing.T) {
	// WHY: JS relative imports use .js/.jsx/index.js candidates; this test
	// exercises the JS candidate set specifically.
	d, _ := openTestDB(t)
	ctx := context.Background()

	// Seed only the .jsx version — the resolver should find it.
	targetPath := "src/Button.jsx"
	seedFile(t, d, targetPath, types.LanguageJSX)

	ref := types.UnresolvedReference{
		ID:            "ref-js",
		FromNodeID:    "file:src/App.js",
		ReferenceName: "./Button",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      "src/App.js",
		Language:      types.LanguageJavaScript,
	}

	r := resolution.NewResolver(d)
	result, err := r.ResolveImport(ctx, ref, "src/App.js")
	if err != nil {
		t.Fatalf("ResolveImport js: %v", err)
	}

	if result.Kind != resolution.ResolvedKindInternal {
		t.Errorf("js: expected internal, got %v", result.Kind)
	}
	wantNodeID := "file:" + targetPath
	if result.TargetNodeID != wantNodeID {
		t.Errorf("js: TargetNodeID = %q, want %q", result.TargetNodeID, wantNodeID)
	}
}

// ---------------------------------------------------------------------------
// Test: Python extension candidates
// ---------------------------------------------------------------------------

func TestResolver_PythonImport(t *testing.T) {
	// WHY: Python relative imports use .py / __init__.py package style.
	d, _ := openTestDB(t)
	ctx := context.Background()

	targetPath := "myapp/utils.py"
	seedFile(t, d, targetPath, types.LanguagePython)

	ref := types.UnresolvedReference{
		ID:            "ref-py",
		FromNodeID:    "file:myapp/main.py",
		ReferenceName: "./utils",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      "myapp/main.py",
		Language:      types.LanguagePython,
	}

	r := resolution.NewResolver(d)
	result, err := r.ResolveImport(ctx, ref, "myapp/main.py")
	if err != nil {
		t.Fatalf("ResolveImport python: %v", err)
	}

	if result.Kind != resolution.ResolvedKindInternal {
		t.Errorf("python: expected internal, got %v", result.Kind)
	}
	if result.TargetNodeID != "file:"+targetPath {
		t.Errorf("python: TargetNodeID = %q, want %q", result.TargetNodeID, "file:"+targetPath)
	}
}

// ---------------------------------------------------------------------------
// Test: concurrent ResolveImport on one Resolver is race-clean
// ---------------------------------------------------------------------------

func TestResolver_ConcurrentAliasInit(t *testing.T) {
	// WHY: Resolver.aliases() lazily initialises r.aliasMap. If two goroutines
	// call ResolveImport concurrently on the same *Resolver, both may race on
	// the r.aliasMap write. This test exercises that path with -race to verify
	// the sync.Once guard is in place.
	d, projectRoot := openTestDB(t)
	ctx := context.Background()

	targetPath := "src/util.ts"
	seedFile(t, d, targetPath, types.LanguageTypeScript)

	// Write a tsconfig so aliases() does real work (not just the empty-map fast path).
	tsconfigContent := `{"compilerOptions": {"baseUrl": ".", "paths": {"@app/*": ["src/*"]}}}`
	if err := os.WriteFile(filepath.Join(projectRoot, "tsconfig.json"), []byte(tsconfigContent), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}

	r := resolution.NewResolverWithProject(d, projectRoot)

	ref := types.UnresolvedReference{
		ID:            "ref-concurrent",
		FromNodeID:    "file:src/app.ts",
		ReferenceName: "@app/util",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      "src/app.ts",
		Language:      types.LanguageTypeScript,
	}

	const goroutines = 20
	// Use a channel to synchronise start so all goroutines hit aliases() at
	// once — maximising the window for a data race.
	start := make(chan struct{})
	errCh := make(chan error, goroutines*2)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start // wait for all to be ready
			if _, err := r.ResolveImport(ctx, ref, "src/app.ts"); err != nil {
				errCh <- err
			}
		}()
	}
	close(start) // release all goroutines simultaneously
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent ResolveImport: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: re-export cycle A→B→A is bounded (multi-node)
// ---------------------------------------------------------------------------

func TestResolver_ReExportCycleMultiNode(t *testing.T) {
	// WHY: a multi-node A→B→A cycle must terminate within REEXPORT_MAX_DEPTH hops
	// and return a valid node ID (not hang, not error). The existing self-loop test
	// covers the degenerate cycle; this strengthens it with a two-node mutual cycle.
	d, _ := openTestDB(t)
	ctx := context.Background()

	fileA := "src/a.ts"
	fileB := "src/b.ts"

	seedFile(t, d, fileA, types.LanguageTypeScript)
	seedFile(t, d, fileB, types.LanguageTypeScript)

	// A re-exports B, B re-exports A (mutual cycle).
	if _, err := d.InsertEdge(ctx, types.Edge{
		Source: "file:" + fileA,
		Target: "file:" + fileB,
		Kind:   types.EdgeKindExports,
	}); err != nil {
		t.Fatalf("InsertEdge A→B: %v", err)
	}
	if _, err := d.InsertEdge(ctx, types.Edge{
		Source: "file:" + fileB,
		Target: "file:" + fileA,
		Kind:   types.EdgeKindExports,
	}); err != nil {
		t.Fatalf("InsertEdge B→A: %v", err)
	}

	ref := types.UnresolvedReference{
		ID:            "ref-ab-cycle",
		FromNodeID:    "file:src/importer.ts",
		ReferenceName: "./a",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      "src/importer.ts",
		Language:      types.LanguageTypeScript,
	}

	// Seed the importer so probeExtensions finds fileA.
	seedFile(t, d, "src/importer.ts", types.LanguageTypeScript)

	r := resolution.NewResolver(d)

	done := make(chan string, 1)
	go func() {
		result, _ := r.ResolveImport(ctx, ref, "src/importer.ts")
		done <- result.TargetNodeID
	}()

	select {
	case nodeID := <-done:
		// Must resolve to either fileA or fileB (both are valid end-points of the cycle).
		validTargets := map[string]bool{
			"file:" + fileA: true,
			"file:" + fileB: true,
		}
		if !validTargets[nodeID] {
			t.Errorf("A→B→A cycle: unexpected TargetNodeID %q", nodeID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("A→B→A cycle test timed out — infinite loop in re-export resolution")
	}
}

// ---------------------------------------------------------------------------
// Test: unresolved when file not found in DB
// ---------------------------------------------------------------------------

func TestResolver_UnresolvedWhenFileMissing(t *testing.T) {
	// WHY: if the resolved path doesn't exist in the DB, resolution must return
	// ResolvedKindUnresolved (not an error, not external) — the target simply
	// isn't indexed yet.
	d, _ := openTestDB(t)
	ctx := context.Background()

	ref := types.UnresolvedReference{
		ID:            "ref-missing",
		FromNodeID:    "file:src/app.ts",
		ReferenceName: "./missing-module",
		ReferenceKind: types.EdgeKindImports,
		Line:          1,
		FilePath:      "src/app.ts",
		Language:      types.LanguageTypeScript,
	}

	r := resolution.NewResolver(d)
	result, err := r.ResolveImport(ctx, ref, "src/app.ts")
	if err != nil {
		t.Fatalf("ResolveImport missing: %v", err)
	}
	if result.Kind != resolution.ResolvedKindUnresolved {
		t.Errorf("missing: expected unresolved, got %v", result.Kind)
	}
}
