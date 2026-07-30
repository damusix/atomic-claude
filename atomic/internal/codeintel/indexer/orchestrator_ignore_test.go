package indexer_test

// Graphignore: discovery-time filtering tests (checkpoint 2).
//
// Filtering happens at the single discovery seam feeding indexFiles and
// pruneDeleted (IndexAll/Sync/IndexPaths input) — never as a query-time
// filter or a second prune pass. TestSync_PrunesFileMatchingNewlyAddedPattern
// is the headline test: it proves a file that becomes newly ignored is
// reclaimed by the EXISTING pruneDeleted mechanism, with no new prune code.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestIndexAll_SkipsIgnoredFile verifies that a file matched by the ignore
// matcher produces zero nodes and no file record after a full index, while a
// sibling file is indexed normally.
func TestIndexAll_SkipsIgnoredFile(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "main.go", `package main

func Main() {}
`)
	writeFile(t, dir, "vendor/lib.go", `package vendor

func Lib() {}
`)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	matcher, warns := config.NewIgnoreMatcher([]string{"vendor/**"})
	if len(warns) != 0 {
		t.Fatalf("unexpected matcher warnings: %v", warns)
	}

	orch := indexer.NewOrchestrator(database, pool)
	orch.SetIgnoreMatcher(matcher)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	if _, err := database.GetFile(ctx, "vendor/lib.go"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("vendor/lib.go: expected no file record (ignored), got err=%v", err)
	}
	vendorNodes, err := database.GetNodesInFile(ctx, "vendor/lib.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(vendor/lib.go): %v", err)
	}
	if len(vendorNodes) != 0 {
		t.Errorf("vendor/lib.go: expected 0 nodes (ignored), got %d", len(vendorNodes))
	}

	mainNodes, err := database.GetNodesInFile(ctx, "main.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(main.go): %v", err)
	}
	if len(mainNodes) == 0 {
		t.Error("main.go: expected at least one node, got 0")
	}
}

// TestSync_PrunesFileMatchingNewlyAddedPattern proves the design constraint:
// a file already indexed, that becomes ignored by a pattern added between
// runs, is removed by the EXISTING pruneDeleted mechanism on the next Sync —
// because it simply stops appearing in the filtered discovery list, exactly
// as if it had been deleted from disk. No new prune mechanism is added.
func TestSync_PrunesFileMatchingNewlyAddedPattern(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "main.go", `package main

func Main() {}
`)
	writeFile(t, dir, "vendor/lib.go", `package vendor

func Lib() {}
`)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	// Sanity: vendor/lib.go is indexed before the pattern is added.
	if _, err := database.GetFile(ctx, "vendor/lib.go"); err != nil {
		t.Fatalf("precondition: vendor/lib.go should be indexed: %v", err)
	}

	// The file still exists on disk — only the matcher changes.
	matcher, _ := config.NewIgnoreMatcher([]string{"vendor/**"})
	orch.SetIgnoreMatcher(matcher)

	if err := orch.Sync(ctx, dir); err != nil {
		t.Fatalf("Sync after ignore pattern added: %v", err)
	}

	if _, err := database.GetFile(ctx, "vendor/lib.go"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("vendor/lib.go file record still present after Sync (err=%v) — newly-ignored file not pruned", err)
	}
	vendorNodes, err := database.GetNodesInFile(ctx, "vendor/lib.go")
	if err != nil {
		t.Fatalf("GetNodesInFile(vendor/lib.go) after Sync: %v", err)
	}
	if len(vendorNodes) != 0 {
		t.Errorf("vendor/lib.go has %d nodes after ignore+Sync; want 0 (not pruned)", len(vendorNodes))
	}

	// The surviving file must be untouched — pruning is scoped to newly
	// ignored/vanished paths only, never collateral.
	if _, err := database.GetFile(ctx, "main.go"); err != nil {
		t.Errorf("main.go file record was wrongly pruned: %v", err)
	}
}

// TestIndexPaths_SkipsIgnoredPath verifies the explicit-subset selective
// indexing path also honors the matcher. IndexPaths does not prune (a
// deliberate design constraint — it is handed an explicit subset), so this
// only asserts the ignored path is skipped, not that it is reclaimed.
func TestIndexPaths_SkipsIgnoredPath(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()

	writeFile(t, dir, "main.go", `package main

func Main() {}
`)
	writeFile(t, dir, "vendor/lib.go", `package vendor

func Lib() {}
`)

	matcher, _ := config.NewIgnoreMatcher([]string{"vendor/**"})
	orch := indexer.NewOrchestrator(database, pool)
	orch.SetIgnoreMatcher(matcher)

	paths := []string{
		filepath.Join(dir, "main.go"),
		filepath.Join(dir, "vendor", "lib.go"),
	}
	if err := orch.IndexPaths(ctx, dir, paths); err != nil {
		t.Fatalf("IndexPaths: %v", err)
	}

	if _, err := database.GetFile(ctx, "vendor/lib.go"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("vendor/lib.go: expected no file record (ignored), got err=%v", err)
	}
	if _, err := database.GetFile(ctx, "main.go"); err != nil {
		t.Errorf("main.go: expected a file record, got err=%v", err)
	}
}

// TestOrchestrator_ScanFiles_FiltersIgnored covers the exported ScanFiles
// method (used by engine.ExtractFrameworkNodes) — it must return the same
// filtered set IndexAll/Sync produce, not a second unfiltered scan.
func TestOrchestrator_ScanFiles_FiltersIgnored(t *testing.T) {
	pool := newTestPool(t)
	database := openTestDB(t)
	dir := t.TempDir()
	initGitRepo(t, dir)

	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, "vendor/lib.go", "package vendor\n")
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	orch.SetIgnoreMatcher(mustMatcher(t, "vendor/**"))

	files, err := orch.ScanFiles(dir)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	for _, f := range files {
		if strings.Contains(filepath.ToSlash(f), "vendor/") {
			t.Errorf("ScanFiles returned ignored path: %s", f)
		}
	}
	found := false
	for _, f := range files {
		if strings.HasSuffix(filepath.ToSlash(f), "main.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("ScanFiles should still return main.go, got: %v", files)
	}
}

func mustMatcher(t *testing.T, patterns ...string) *config.IgnoreMatcher {
	t.Helper()
	m, warns := config.NewIgnoreMatcher(patterns)
	if len(warns) != 0 {
		t.Fatalf("unexpected matcher warnings: %v", warns)
	}
	return m
}
