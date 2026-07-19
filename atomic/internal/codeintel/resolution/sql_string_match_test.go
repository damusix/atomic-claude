package resolution_test

// sql_string_match_test.go — sql-string-match Checkpoint 2: pass A
// object-name matching (C2), the query-builder vocabulary confidence tier
// (C4), and speculative-ref cleanup (C5).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// seedSQLNode inserts a SQL-language object node (table/view/procedure/function).
func seedSQLNode(t *testing.T, d *db.DB, id, name, filePath string, kind types.NodeKind) {
	t.Helper()
	ctx := context.Background()
	if err := d.UpsertNode(ctx, types.Node{
		ID: id, Kind: kind, Name: name, FilePath: filePath,
		Language: types.LanguageSQL, StartLine: 1, EndLine: 5,
	}); err != nil {
		t.Fatalf("seedSQLNode %s: %v", id, err)
	}
}

func confidenceOf(t *testing.T, e types.Edge) string {
	t.Helper()
	if len(e.Metadata) == 0 {
		t.Fatalf("edge %v has no metadata", e)
	}
	var m map[string]string
	if err := json.Unmarshal(e.Metadata, &m); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	return m["confidence"]
}

func TestSQLStringMatch_HighConfidenceViaVocabulary(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:orders.sql:orders:1", "orders", "orders.sql", types.NodeKindTable)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "selectFrom",
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 references edge, got %d", len(edges))
	}
	if got := confidenceOf(t, edges[0]); got != "high" {
		t.Errorf("confidence = %q, want high", got)
	}
	if edges[0].Provenance != "string-match" {
		t.Errorf("provenance = %q, want string-match", edges[0].Provenance)
	}
}

func TestSQLStringMatch_MediumConfidenceWithoutVocabulary(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:orders.sql:orders:1", "orders", "orders.sql", types.NodeKindTable)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "fetchStuff",
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 references edge, got %d", len(edges))
	}
	if got := confidenceOf(t, edges[0]); got != "medium" {
		t.Errorf("confidence = %q, want medium", got)
	}
}

func TestSQLStringMatch_VocabularyCaseInsensitive(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:orders.sql:orders:1", "orders", "orders.sql", types.NodeKindTable)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "SelectFrom",
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 || confidenceOf(t, edges[0]) != "high" {
		t.Fatalf("expected 1 high-confidence edge, got %+v", edges)
	}
}

func TestSQLStringMatch_NameCaseInsensitive(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:orders.sql:Orders:1", "Orders", "orders.sql", types.NodeKindTable)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "ORDERS",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 references edge (case-insensitive name match), got %d", len(edges))
	}
}

func TestSQLStringMatch_AmbiguousFanOut2to3(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:a.sql:widgets:1", "widgets", "a.sql", types.NodeKindTable)
	seedSQLNode(t, d, "view:b.sql:widgets:1", "widgets", "b.sql", types.NodeKindView)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "widgets",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 2 {
		t.Fatalf("expected edges to both 2 candidates, got %d", len(edges))
	}
}

func TestSQLStringMatch_AmbiguityCapOver3DeletesNoEdges(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:a.sql:items:1", "items", "a.sql", types.NodeKindTable)
	seedSQLNode(t, d, "view:b.sql:items:1", "items", "b.sql", types.NodeKindView)
	seedSQLNode(t, d, "procedure:c.sql:items:1", "items", "c.sql", types.NodeKindProcedure)
	seedSQLNode(t, d, "function:d.sql:items:1", "items", "d.sql", types.NodeKindFunction)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "items",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences); len(edges) != 0 {
		t.Fatalf("expected 0 edges above ambiguity cap, got %d", len(edges))
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected the ambiguous ref to be deleted, %d unresolved_refs remain", n)
	}
}

func TestSQLStringMatch_ProcedureAndFunctionKindsMatch(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "procedure:a.sql:archive_orders:1", "archive_orders", "a.sql", types.NodeKindProcedure)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "archive_orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "call",
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 references edge to the procedure, got %d", len(edges))
	}
	if got := confidenceOf(t, edges[0]); got != "high" {
		t.Errorf("confidence = %q, want high (call is in vocabulary)", got)
	}
}

func TestSQLStringMatch_HostLanguageFunctionSameNameNotMatched(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	// A TypeScript function node with the SAME name as the ref — must NOT
	// be treated as a candidate; only Language == SQL nodes are eligible.
	seedNode(t, d, "function:src/b.ts:orders:1", "src/b.ts", types.NodeKindFunction, types.LanguageTypeScript, true)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences); len(edges) != 0 {
		t.Fatalf("expected 0 edges (host-language node must not match), got %d", len(edges))
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected unmatched ref to be cleaned up by C5, %d remain", n)
	}
}

func TestSQLStringMatch_DottedRefsUntouchedByPassA_ButCleanedUpByC5(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:a.sql:orders:1", "orders", "a.sql", types.NodeKindTable)

	// Dotted form is pass B's (C3) job — not implemented yet. Pass A must
	// leave it unmatched; C5 cleanup then deletes it since pass B doesn't
	// exist to consume it in this checkpoint.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders.customer_id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences); len(edges) != 0 {
		t.Fatalf("expected 0 edges from a dotted ref in pass A, got %d", len(edges))
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected dotted ref to be swept up by C5 cleanup, %d unresolved_refs remain", n)
	}
}

func TestSQLStringMatch_CleanupLeavesZeroSQLStringRows(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:a.sql:orders:1", "orders", "a.sql", types.NodeKindTable)

	// One matched, one unmatched, one dotted — all three must be gone after.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-matched", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts", Language: types.LanguageTypeScript,
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-unmatched", FromNodeID: ownerID, ReferenceName: "gibberish_xyz",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts", Language: types.LanguageTypeScript,
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-dotted", FromNodeID: ownerID, ReferenceName: "orders.id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts", Language: types.LanguageTypeScript,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected zero unresolved_refs after cleanup, got %d", n)
	}
}

func TestSQLStringMatch_ReindexIdempotent(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLNode(t, d, "table:a.sql:orders:1", "orders", "a.sql", types.NodeKindTable)
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts", Language: types.LanguageTypeScript,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched (1st): %v", err)
	}
	first := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(first) != 1 {
		t.Fatalf("expected 1 edge after first run, got %d", len(first))
	}

	// Simulate re-index: the owner node's edges are cleared (as
	// DeleteNodesByFile does via FK cascade on re-index) and the same
	// sql_string ref is re-emitted by extraction, then resolved again.
	if err := d.WithTx(ctx, func(tx *db.Tx) error {
		return tx.DeleteNodesByFile(ctx, "src/a.ts")
	}); err != nil {
		t.Fatalf("DeleteNodesByFile: %v", err)
	}
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts", Language: types.LanguageTypeScript,
	})

	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched (2nd): %v", err)
	}
	second := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(second) != 1 {
		t.Fatalf("expected 1 edge after re-index (no accumulation), got %d", len(second))
	}
}
