package indexer

// Internal regression test for the dangling-owner-ref guard in
// storeExtractionResult.
//
// WHY internal (not indexer_test): storeExtractionResult is unexported. A
// synthetic ExtractionResult targets the store contract directly — a file
// whose owner-node bug lands there must not abort the whole run — without
// coupling the test to which extractor produced the dangling ref.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// TestStoreExtractionResult_DanglingOwnerRefSkippedNotFatal verifies that a
// ref whose FromNodeID names a node absent from both the extraction result and
// the DB is skipped — not a fatal error — while the rest of the file's nodes
// and refs are stored normally and the skip is recorded in the file's errors
// column (fail loud, not silent).
func TestStoreExtractionResult_DanglingOwnerRefSkippedNotFatal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	const relPath = "app.vue"
	absPath := filepath.Join(dir, relPath)
	if err := os.WriteFile(absPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	stat, err := os.Stat(absPath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	ownerNode := types.Node{
		ID:       "function:owner",
		Kind:     types.NodeKindFunction,
		Name:     "mounted",
		FilePath: relPath,
		Language: types.LanguageVue,
	}

	validRef := types.UnresolvedReference{
		ID:            "ref:valid",
		FromNodeID:    ownerNode.ID,
		ReferenceName: "fetchData",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      relPath,
	}
	// The owner "file:app.vue" is never added to result.Nodes below — this is
	// the dangling-owner-ref shape produced by the pre-fix Vue extractor.
	danglingRef := types.UnresolvedReference{
		ID:            "ref:dangling",
		FromNodeID:    "file:" + relPath,
		ReferenceName: "AtomCore",
		ReferenceKind: types.EdgeKindReferences,
		FilePath:      relPath,
	}

	result := types.ExtractionResult{
		Nodes:                []types.Node{ownerNode},
		UnresolvedReferences: []types.UnresolvedReference{validRef, danglingRef},
	}

	orch := &Orchestrator{db: database}
	if err := orch.storeExtractionResult(ctx, relPath, "hash1", types.LanguageVue, stat, result); err != nil {
		t.Fatalf("storeExtractionResult must not fail on a dangling-owner ref, got: %v", err)
	}

	// The owner node still landed — the rest of the file's data is intact.
	nodes, err := database.GetNodesInFile(ctx, relPath)
	if err != nil {
		t.Fatalf("GetNodesInFile: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (owner node must still be stored)", len(nodes))
	}

	// The valid ref landed; the dangling one was skipped, not inserted.
	refs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d unresolved refs, want 1 (only the valid ref); refs: %+v", len(refs), refs)
	}
	if refs[0].ID != validRef.ID {
		t.Errorf("surviving ref ID = %q, want %q", refs[0].ID, validRef.ID)
	}

	// The skip is recorded in the file's errors column — fail loud, not silent.
	fr, err := database.GetFile(ctx, relPath)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if len(fr.Errors) == 0 {
		t.Fatal("file record Errors is empty — the skipped ref must be recorded")
	}
	var errs []string
	if err := json.Unmarshal(fr.Errors, &errs); err != nil {
		t.Fatalf("unmarshal file errors: %v", err)
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e, danglingRef.ID) && strings.Contains(e, danglingRef.FromNodeID) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("file errors %v do not mention the skipped ref %q / owner %q", errs, danglingRef.ID, danglingRef.FromNodeID)
	}
}

// TestStoreExtractionResult_OwnerExistsInDB_RefInsertedNoSkip verifies the
// guard's other branch: when a ref's owner is absent from the *current*
// file's result.Nodes but was already committed to the DB by a prior file's
// store (a cross-file / already-indexed owner), tx.NodeExists finds it and
// the ref is inserted normally — no skip, no error recorded.
func TestStoreExtractionResult_OwnerExistsInDB_RefInsertedNoSkip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	orch := &Orchestrator{db: database}

	// First store: the owner node, indexed as its own file.
	const ownerPath = "owner.go"
	ownerAbsPath := filepath.Join(dir, ownerPath)
	if err := os.WriteFile(ownerAbsPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write owner fixture: %v", err)
	}
	ownerStat, err := os.Stat(ownerAbsPath)
	if err != nil {
		t.Fatalf("stat owner fixture: %v", err)
	}
	ownerNode := types.Node{
		ID:       "function:owner",
		Kind:     types.NodeKindFunction,
		Name:     "Owner",
		FilePath: ownerPath,
		Language: types.LanguageGo,
	}
	ownerResult := types.ExtractionResult{Nodes: []types.Node{ownerNode}}
	if err := orch.storeExtractionResult(ctx, ownerPath, "hashA", types.LanguageGo, ownerStat, ownerResult); err != nil {
		t.Fatalf("storeExtractionResult (owner file): %v", err)
	}

	// Second store: a different file whose ref names the first file's node as
	// owner. That node is absent from this file's own result.Nodes, but it now
	// exists in the DB — the guard's tx.NodeExists branch.
	const consumerPath = "consumer.go"
	consumerAbsPath := filepath.Join(dir, consumerPath)
	if err := os.WriteFile(consumerAbsPath, []byte("y"), 0o644); err != nil {
		t.Fatalf("write consumer fixture: %v", err)
	}
	consumerStat, err := os.Stat(consumerAbsPath)
	if err != nil {
		t.Fatalf("stat consumer fixture: %v", err)
	}
	crossFileRef := types.UnresolvedReference{
		ID:            "ref:cross-file",
		FromNodeID:    ownerNode.ID,
		ReferenceName: "helper",
		ReferenceKind: types.EdgeKindCalls,
		FilePath:      consumerPath,
	}
	consumerResult := types.ExtractionResult{
		UnresolvedReferences: []types.UnresolvedReference{crossFileRef},
	}
	if err := orch.storeExtractionResult(ctx, consumerPath, "hashB", types.LanguageGo, consumerStat, consumerResult); err != nil {
		t.Fatalf("storeExtractionResult (consumer file): %v", err)
	}

	// The ref landed — the DB-hit branch does not skip it.
	refs, err := database.GetUnresolvedRefs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d unresolved refs, want 1 (owner exists in DB, ref must be inserted); refs: %+v", len(refs), refs)
	}
	if refs[0].ID != crossFileRef.ID {
		t.Errorf("surviving ref ID = %q, want %q", refs[0].ID, crossFileRef.ID)
	}

	// No skip recorded: the consumer file's errors column stays empty.
	fr, err := database.GetFile(ctx, consumerPath)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if len(fr.Errors) != 0 {
		t.Errorf("file record Errors = %s, want empty (owner existed in DB — no skip should be recorded)", fr.Errors)
	}
}
