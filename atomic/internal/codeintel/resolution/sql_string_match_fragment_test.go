package resolution_test

// sql_string_match_fragment_test.go — sql-string-match Checkpoint 6: C8
// fragment-tier resolution (one-notch demotion at every computed tier) and
// the C3 anchored-column vocabulary upgrade.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

func TestSQLStringMatch_FragmentObjectMatch_HighDemotedToMedium(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.rb:caller:1"
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedSQLNode(t, d, "table:a.sql:orders:1", "orders", "a.sql", types.NodeKindTable)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb",
		Language: types.LanguageRuby, Line: 3, CalleeExpr: "from",
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
		t.Errorf("confidence = %q, want medium (high demoted one notch)", got)
	}
}

func TestSQLStringMatch_FragmentObjectMatch_MediumDemotedToLow(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.rb:caller:1"
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedSQLNode(t, d, "table:a.sql:orders:1", "orders", "a.sql", types.NodeKindTable)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb",
		Language: types.LanguageRuby, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 references edge, got %d", len(edges))
	}
	if got := confidenceOf(t, edges[0]); got != "low" {
		t.Errorf("confidence = %q, want low (medium demoted one notch)", got)
	}
}

func TestSQLStringMatch_FragmentQualifiedColumn_MediumDemotedToLow(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.rb:caller:1"
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.total:1", "total", "orders.total", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders.total",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb",
		Language: types.LanguageRuby, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 references edge, got %d", len(edges))
	}
	if got := confidenceOf(t, edges[0]); got != "low" {
		t.Errorf("confidence = %q, want low (qualified-column medium demoted one notch)", got)
	}
}

func TestSQLStringMatch_FragmentAnchoredBareColumn_LowStaysLow(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.rb:caller:1"
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.status:1", "status", "orders.status", "a.sql")

	// The anchor comes from a fragment table match too — table anchors from
	// fragment refs must contribute to ownerAnchors just like sql_string ones.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-anchor", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb",
		Language: types.LanguageRuby, Line: 3, CalleeExpr: "from",
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-col", FromNodeID: ownerID, ReferenceName: "status",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb",
		Language: types.LanguageRuby, Line: 4,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges (table anchor + column), got %d", len(edges))
	}
	var colEdge *types.Edge
	for i := range edges {
		if edges[i].Target == "column:a.sql:orders.status:1" {
			colEdge = &edges[i]
		}
	}
	if colEdge == nil {
		t.Fatalf("no edge to the column node found among %+v", edges)
	}
	if got := confidenceOf(t, *colEdge); got != "low" {
		t.Errorf("confidence = %q, want low (already the floor tier)", got)
	}
}

func TestSQLStringMatch_FragmentAnchoredBareColumnWithVocab_MediumDemotedToLow(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.rb:caller:1"
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.status:1", "status", "orders.status", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-anchor", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb",
		Language: types.LanguageRuby, Line: 3, CalleeExpr: "from",
	})
	// Vocab callee on the column-token ref: C3 would upgrade low -> medium,
	// then C8 demotes medium -> low — net no change from the non-vocab case,
	// which is the point of "compute tier then demote" applying uniformly.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-col", FromNodeID: ownerID, ReferenceName: "status",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb",
		Language: types.LanguageRuby, Line: 4, CalleeExpr: "column",
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	var colEdge *types.Edge
	for i := range edges {
		if edges[i].Target == "column:a.sql:orders.status:1" {
			colEdge = &edges[i]
		}
	}
	if colEdge == nil {
		t.Fatalf("no edge to the column node found among %+v", edges)
	}
	if got := confidenceOf(t, *colEdge); got != "low" {
		t.Errorf("confidence = %q, want low (vocab-upgraded medium demoted one notch)", got)
	}
}

func TestSQLStringMatch_NonFragmentAnchoredColumnWithVocab_UpgradedToMedium(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.status:1", "status", "orders.status", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-anchor", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "selectFrom",
	})
	// C3: bare column with a vocab-listed callee (declaration-DSL position)
	// upgrades low -> medium; no demotion applies since this is sql_string.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-col", FromNodeID: ownerID, ReferenceName: "status",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 4, CalleeExpr: "column",
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	var colEdge *types.Edge
	for i := range edges {
		if edges[i].Target == "column:a.sql:orders.status:1" {
			colEdge = &edges[i]
		}
	}
	if colEdge == nil {
		t.Fatalf("no edge to the column node found among %+v", edges)
	}
	if got := confidenceOf(t, *colEdge); got != "medium" {
		t.Errorf("confidence = %q, want medium (C3 vocab upgrade on sql_string, no demotion)", got)
	}
}

func TestSQLStringMatch_CleanupLeavesZeroRowsOfBothKinds(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.rb:caller:1"
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedSQLNode(t, d, "table:a.sql:orders:1", "orders", "a.sql", types.NodeKindTable)

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-string-matched", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.rb", Language: types.LanguageRuby,
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-string-unmatched", FromNodeID: ownerID, ReferenceName: "gibberish_xyz",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.rb", Language: types.LanguageRuby,
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-fragment-matched", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb", Language: types.LanguageRuby,
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-fragment-unmatched", FromNodeID: ownerID, ReferenceName: "timeout",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb", Language: types.LanguageRuby,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected zero unresolved_refs of both kinds after cleanup, got %d", n)
	}
}

func TestSQLStringMatch_FragmentReindexIdempotent(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.rb:caller:1"
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedSQLNode(t, d, "table:a.sql:orders:1", "orders", "a.sql", types.NodeKindTable)
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb", Language: types.LanguageRuby,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched (1st): %v", err)
	}
	first := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(first) != 1 {
		t.Fatalf("expected 1 edge after first run, got %d", len(first))
	}

	if err := d.WithTx(ctx, func(tx *db.Tx) error {
		return tx.DeleteNodesByFile(ctx, "src/a.rb")
	}); err != nil {
		t.Fatalf("DeleteNodesByFile: %v", err)
	}
	seedNode(t, d, ownerID, "src/a.rb", types.NodeKindFunction, types.LanguageRuby, false)
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.rb", Language: types.LanguageRuby,
	})

	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched (2nd): %v", err)
	}
	second := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(second) != 1 {
		t.Fatalf("expected 1 edge after re-index (no accumulation), got %d", len(second))
	}
}
