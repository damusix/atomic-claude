package indexer

// Internal regression test for the dangling-owner-ref guard in
// storeExtractionResult (checkpoint 1, SC1).
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
