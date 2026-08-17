package resolution_test

// Pass B: qualified and anchored bare-column matching.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// seedSQLObjectWithQName inserts a SQL table/view node with an explicit
// QualifiedName (pass B's qualified-form matching keys off it).
func seedSQLObjectWithQName(t *testing.T, d *db.DB, id, name, qname, filePath string, kind types.NodeKind) {
	t.Helper()
	ctx := context.Background()
	if err := d.UpsertNode(ctx, types.Node{
		ID: id, Kind: kind, Name: name, QualifiedName: qname, FilePath: filePath,
		Language: types.LanguageSQL, StartLine: 1, EndLine: 5,
	}); err != nil {
		t.Fatalf("seedSQLObjectWithQName %s: %v", id, err)
	}
}

// seedSQLColumn inserts a NodeKindColumn node with QualifiedName
// "<tableQName>.<colName>".
func seedSQLColumn(t *testing.T, d *db.DB, id, name, qname, filePath string) {
	t.Helper()
	ctx := context.Background()
	if err := d.UpsertNode(ctx, types.Node{
		ID: id, Kind: types.NodeKindColumn, Name: name, QualifiedName: qname, FilePath: filePath,
		Language: types.LanguageSQL, StartLine: 1, EndLine: 1,
	}); err != nil {
		t.Fatalf("seedSQLColumn %s: %v", id, err)
	}
}

func TestSQLStringMatch_QualifiedColumn_Medium(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.customer_id:1", "customer_id", "orders.customer_id", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders.customer_id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 references edge, got %d", len(edges))
	}
	if edges[0].Target != "column:a.sql:orders.customer_id:1" {
		t.Errorf("target = %q, want the column node", edges[0].Target)
	}
	if got := confidenceOf(t, edges[0]); got != "medium" {
		t.Errorf("confidence = %q, want medium", got)
	}
	if edges[0].Provenance != "string-match" {
		t.Errorf("provenance = %q, want string-match", edges[0].Provenance)
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected the qualified ref to be consumed, %d unresolved_refs remain", n)
	}
}

func TestSQLStringMatch_QualifiedColumn_UnknownTableNoEdge(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.customer_id:1", "customer_id", "orders.customer_id", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "nosuchtable.customer_id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences); len(edges) != 0 {
		t.Fatalf("expected 0 edges (table name doesn't resolve), got %d", len(edges))
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected ref cleaned up by C5, %d remain", n)
	}
}

func TestSQLStringMatch_QualifiedColumn_ColumnDoesNotExistNoEdge(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.customer_id:1", "customer_id", "orders.customer_id", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "orders.nosuchcolumn",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences); len(edges) != 0 {
		t.Fatalf("expected 0 edges (column doesn't exist on table), got %d", len(edges))
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected ref cleaned up by C5, %d remain", n)
	}
}

func TestSQLStringMatch_AnchoredBareColumn_Low(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.customer_id:1", "customer_id", "orders.customer_id", "a.sql")

	// Anchor ref (pass A) + bare column ref (pass B) from the same owner,
	// same call. Anchor must resolve first via pass A.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-anchor", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "selectFrom",
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-col", FromNodeID: ownerID, ReferenceName: "customer_id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 4,
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
		if edges[i].Target == "column:a.sql:orders.customer_id:1" {
			colEdge = &edges[i]
		}
	}
	if colEdge == nil {
		t.Fatalf("no edge to the column node found among %+v", edges)
	}
	if got := confidenceOf(t, *colEdge); got != "low" {
		t.Errorf("confidence = %q, want low", got)
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected all refs consumed, %d remain", n)
	}
}

func TestSQLStringMatch_BareColumn_NoAnchorNeverEmitted(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.customer_id:1", "customer_id", "orders.customer_id", "a.sql")

	// No pass-A anchor ref for this owner at all.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-col", FromNodeID: ownerID, ReferenceName: "customer_id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 4,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences); len(edges) != 0 {
		t.Fatalf("expected 0 edges (no anchor), got %d", len(edges))
	}
	if n := countUnresolvedRefs(t, d); n != 0 {
		t.Fatalf("expected ref cleaned up by C5, %d remain", n)
	}
}

func TestSQLStringMatch_AnchorScopingDoesNotLeakAcrossOwners(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerX := "function:src/a.ts:callerX:1"
	ownerY := "function:src/b.ts:callerY:1"
	seedNode(t, d, ownerX, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedNode(t, d, ownerY, "src/b.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:orders.customer_id:1", "customer_id", "orders.customer_id", "a.sql")

	// Owner X gets the anchor. Owner Y gets a bare column ref but no
	// anchor of its own — X's anchor must not leak to Y.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-anchor", FromNodeID: ownerX, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "selectFrom",
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-col-y", FromNodeID: ownerY, ReferenceName: "customer_id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/b.ts",
		Language: types.LanguageTypeScript, Line: 4,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	if edges := edgesWithKind(t, d, ownerY, types.EdgeKindReferences); len(edges) != 0 {
		t.Fatalf("expected 0 edges for owner Y (anchor must not leak), got %d", len(edges))
	}
	if edges := edgesWithKind(t, d, ownerX, types.EdgeKindReferences); len(edges) != 1 {
		t.Fatalf("expected 1 edge for owner X (its own anchor), got %d", len(edges))
	}
}

func TestSQLStringMatch_AnchorFromView(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "view:a.sql:active_orders:1", "active_orders", "active_orders", "a.sql", types.NodeKindView)
	seedSQLColumn(t, d, "column:a.sql:active_orders.customer_id:1", "customer_id", "active_orders.customer_id", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-anchor", FromNodeID: ownerID, ReferenceName: "active_orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "selectFrom",
	})
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-col", FromNodeID: ownerID, ReferenceName: "customer_id",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 4,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	found := false
	for _, e := range edges {
		if e.Target == "column:a.sql:active_orders.customer_id:1" {
			found = true
			if got := confidenceOf(t, e); got != "low" {
				t.Errorf("confidence = %q, want low", got)
			}
		}
	}
	if !found {
		t.Fatalf("expected an edge to the view's column, got %+v", edges)
	}
}

func TestSQLStringMatch_QualifiedColumn_CaseInsensitive(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:Orders:1", "Orders", "Orders", "a.sql", types.NodeKindTable)
	seedSQLColumn(t, d, "column:a.sql:Orders.CustomerId:1", "CustomerId", "Orders.CustomerId", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-1", FromNodeID: ownerID, ReferenceName: "ORDERS.CUSTOMERID",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge (case-insensitive qualified match), got %d", len(edges))
	}
}

func TestSQLStringMatch_BareForm_ObjectMatchTakesPrecedenceOverAnchoredColumn(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	ownerID := "function:src/a.ts:caller:1"
	seedNode(t, d, ownerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedSQLObjectWithQName(t, d, "table:a.sql:orders:1", "orders", "orders", "a.sql", types.NodeKindTable)
	// A second table also named "status" — pass A object-name match target.
	seedSQLObjectWithQName(t, d, "table:a.sql:status:1", "status", "status", "a.sql", types.NodeKindTable)
	// The anchor table also happens to have a column literally named "status".
	seedSQLColumn(t, d, "column:a.sql:orders.status:1", "status", "orders.status", "a.sql")

	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-anchor", FromNodeID: ownerID, ReferenceName: "orders",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 3, CalleeExpr: "selectFrom",
	})
	// Bare "status" ref: ambiguous between pass-A object match and pass-B
	// anchored-column match. Pass A runs first and consumes it — it must
	// resolve as an object reference, not a column reference.
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-status", FromNodeID: ownerID, ReferenceName: "status",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 4,
	})

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	edges := edgesWithKind(t, d, ownerID, types.EdgeKindReferences)
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges (orders anchor + status object), got %d", len(edges))
	}
	for _, e := range edges {
		if e.Target == "column:a.sql:orders.status:1" {
			t.Fatalf("bare ref must resolve to the object (pass A), not the column: %+v", edges)
		}
	}
}
