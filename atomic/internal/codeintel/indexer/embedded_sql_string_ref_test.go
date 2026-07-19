package indexer_test

// embedded_sql_string_ref_test.go — sql-string-match (C1) end-to-end tests:
// speculative sql_string ref harvest via a real orchestrator index.
//
// WHY: verifies the postpass emit decision point (embedded_sql_postpass.go)
// directly against a real DB, independent of the resolution pipeline (C2-C4
// are later checkpoints — no edges are asserted here, only the shape of the
// unresolved_refs the harvest produces).

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/indexer"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

func TestEmbeddedSQLStringRef_SpeculativeHarvest(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	// worker_document is identifier-shaped and gate-fails (no SQL keyword) —
	// should become a sql_string ref with callee capture. "a.b.c" (two dots),
	// "ab" (2 chars), and prose must all be rejected by the identifier-shape
	// filter. The gate-passing SELECT literal keeps today's embedded-SQL
	// behavior untouched — no sql_string ref for it.
	const goFixture = `package x

func f() {
	db.SelectFrom("worker_document")
	q := "prose string with several words"
	two := "a.b.c"
	short := "ab"
	real := "SELECT * FROM widgets WHERE id = ?"
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

	byName := map[string]*types.UnresolvedReference{}
	for i := range refs {
		if refs[i].ReferenceKind == types.ReferenceKindSQLString {
			byName[refs[i].ReferenceName] = &refs[i]
		}
	}

	t.Run("identifier-shaped literal emitted with callee capture", func(t *testing.T) {
		ref := byName["worker_document"]
		if ref == nil {
			t.Fatal("expected sql_string ref for worker_document")
		}
		if ref.CalleeExpr != "SelectFrom" {
			t.Errorf("CalleeExpr = %q, want %q", ref.CalleeExpr, "SelectFrom")
		}
		if ref.Language != types.LanguageGo {
			t.Errorf("Language = %q, want %q", ref.Language, types.LanguageGo)
		}
		if ref.FromNodeID == "" {
			t.Error("expected non-empty owner (FromNodeID) — should attribute to func f")
		}
	})

	t.Run("prose rejected (fails identifier shape)", func(t *testing.T) {
		if _, ok := byName["prose string with several words"]; ok {
			t.Error("prose string should not produce a sql_string ref")
		}
	})

	t.Run("two-dot literal rejected", func(t *testing.T) {
		if _, ok := byName["a.b.c"]; ok {
			t.Error("a.b.c (two dots) should not produce a sql_string ref")
		}
	})

	t.Run("2-char literal rejected", func(t *testing.T) {
		if _, ok := byName["ab"]; ok {
			t.Error("ab (2 chars) should not produce a sql_string ref")
		}
	})

	t.Run("gate-passing literal keeps today's behavior — no sql_string ref", func(t *testing.T) {
		if _, ok := byName["SELECT * FROM widgets WHERE id = ?"]; ok {
			t.Error("gate-passing literal should not also produce a sql_string ref")
		}
	})
}

// TestEmbeddedSQLStringRef_OwnerAttributionAndDedupe verifies that two
// identical identifier-shaped literals in the same function produce exactly
// one sql_string ref (dedupe by owner+literal), and that a literal at file
// scope (no enclosing function) attributes to the file node.
func TestEmbeddedSQLStringRef_OwnerAttributionAndDedupe(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const goFixture = `package x

var topLevel = db.Table("orders_view")

func f() {
	db.From("worker_document")
	db.From("worker_document")
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

	var dupCount int
	var fileOwned bool
	for _, r := range refs {
		if r.ReferenceKind != types.ReferenceKindSQLString {
			continue
		}
		switch r.ReferenceName {
		case "worker_document":
			dupCount++
		case "orders_view":
			fileOwned = r.FromNodeID == "file:dup.go"
		}
	}

	if dupCount != 1 {
		t.Errorf("worker_document sql_string ref count = %d, want 1 (dedupe by owner+literal)", dupCount)
	}
	if !fileOwned {
		t.Error("expected file-scope literal orders_view to be owned by the file node")
	}
}

// TestEmbeddedSQLStringRef_GenericLanguageHarvest verifies the sql_string
// speculative-harvest path also fires for a generic-language host file (one
// routed through the generic HarvestEmbeddedLiterals config table, not one
// of the three callee-capture-aware harvesters: Go/Python/TypeScript). Ruby
// is used here since it already has a config entry (embedded_literals_config.go).
// The generic path does not track callee context, so CalleeExpr must be "".
func TestEmbeddedSQLStringRef_GenericLanguageHarvest(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	database := openTestDB(t)

	dir := t.TempDir()
	initGitRepo(t, dir)

	const rubyFixture = `def fetch
  db.select("worker_document")
end
`
	writeFile(t, dir, "app.rb", rubyFixture)
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

	var ref *types.UnresolvedReference
	for i := range refs {
		if refs[i].ReferenceKind == types.ReferenceKindSQLString && refs[i].ReferenceName == "worker_document" {
			ref = &refs[i]
			break
		}
	}
	if ref == nil {
		t.Fatal("expected sql_string ref for worker_document in generic-language (Ruby) file")
	}
	if ref.Language != types.LanguageRuby {
		t.Errorf("Language = %q, want %q", ref.Language, types.LanguageRuby)
	}
	if ref.CalleeExpr != "" {
		t.Errorf("CalleeExpr = %q, want empty (generic harvest path has no callee capture)", ref.CalleeExpr)
	}
}
