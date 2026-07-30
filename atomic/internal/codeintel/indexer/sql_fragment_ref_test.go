package indexer_test

// sql_fragment_ref_test.go — sql-string-match (C8) end-to-end tests:
// speculative sql_fragment ref harvest via a real orchestrator index.
//
// WHY: mirrors embedded_sql_string_ref_test.go's pattern — verifies the
// postpass emit decision point directly against a real DB, independent of
// resolution (CP6 wires sql_fragment matching itself).

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

func TestEmbeddedSQLFragmentRef_SpeculativeHarvest(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// "name = ?" fails C1 shape (has punctuation) and IsSQLLiteral, passes the
	// C8 fragment gate: tokenizes to "name". "hello world status" fails both
	// gates entirely (no discriminator) — no ref at all.
	const goFixture = `package x

func f() {
	db.Where("name = ?")
	q := "hello world status"
}
`
	writeFile(t, dir, "app.go", goFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	refs, err := database.GetUnresolvedRefs(ctx, 1000, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var fragmentRef *types.UnresolvedReference
	for i := range refs {
		if refs[i].ReferenceKind == types.ReferenceKindSQLFragment && refs[i].ReferenceName == "name" {
			fragmentRef = &refs[i]
		}
	}

	t.Run("fragment token emitted with callee capture", func(t *testing.T) {
		if fragmentRef == nil {
			t.Fatal("expected sql_fragment ref for token 'name'")
		}
		if fragmentRef.CalleeExpr != "Where" {
			t.Errorf("CalleeExpr = %q, want %q", fragmentRef.CalleeExpr, "Where")
		}
		if fragmentRef.Language != types.LanguageGo {
			t.Errorf("Language = %q, want %q", fragmentRef.Language, types.LanguageGo)
		}
		if fragmentRef.FromNodeID == "" {
			t.Error("expected non-empty owner (FromNodeID) — should attribute to func f")
		}
	})

	t.Run("no-discriminator prose produces no ref of either kind", func(t *testing.T) {
		for _, r := range refs {
			if r.ReferenceName == "hello world status" {
				t.Errorf("prose with no discriminator should not produce a ref, got kind %q", r.ReferenceKind)
			}
		}
	})
}

// TestEmbeddedSQLFragmentRef_Dedupe verifies two identical fragment literals
// in the same function produce exactly one sql_fragment ref per surviving
// token, and that sql_string and sql_fragment dedupe do not collide with
// each other (each kind keeps its own seen map).
func TestEmbeddedSQLFragmentRef_Dedupe(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const goFixture = `package x

func f() {
	db.Where("name = ?")
	db.Where("name = ?")
	db.SelectFrom("name")
}
`
	writeFile(t, dir, "dup.go", goFixture)
	gitAdd(t, dir, ".")
	gitCommit(t, dir, "init")

	orch := indexer.NewOrchestrator(database, pool)
	if err := orch.IndexAll(ctx, dir); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	refs, err := database.GetUnresolvedRefs(ctx, 1000, 0)
	if err != nil {
		t.Fatalf("GetUnresolvedRefs: %v", err)
	}

	var fragmentCount, stringCount int
	for _, r := range refs {
		if r.ReferenceName != "name" {
			continue
		}
		switch r.ReferenceKind {
		case types.ReferenceKindSQLFragment:
			fragmentCount++
		case types.ReferenceKindSQLString:
			stringCount++
		}
	}

	if fragmentCount != 1 {
		t.Errorf("sql_fragment ref count for token 'name' = %d, want 1 (dedupe by owner+token)", fragmentCount)
	}
	if stringCount != 1 {
		t.Errorf("sql_string ref count for literal 'name' = %d, want 1 (SelectFrom(\"name\") is identifier-shaped, separate kind)", stringCount)
	}
}
