package resolution_test

// sql_string_exclusion_test.go — sql-string-match (C1) precondition for C2/C3:
// the standard resolution pipeline must never feed a sql_string ref to
// resolveOne/promoteEdgeKind. As of C2, pass A (a separate batch step) runs
// after the standard loop and consumes/cleans up sql_string refs itself —
// this test uses a reference name with no SQL-object candidate so pass A
// cannot match it, isolating "never reaches promoteEdgeKind" from "pass A's
// own match/no-match behavior" (covered by sql_string_match_test.go).

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

func TestSQLStringRef_ExcludedFromStandardResolution(t *testing.T) {
	d := openPipelineTestDB(t)
	ctx := context.Background()

	// A normal, resolvable "calls" ref — proves the batch loop still does its
	// job for everything else while excluding sql_string.
	callerID := "function:src/a.ts:caller:1"
	calleeID := "function:src/a.ts:callee:10"
	seedNode(t, d, callerID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, false)
	seedNode(t, d, calleeID, "src/a.ts", types.NodeKindFunction, types.LanguageTypeScript, true)
	if err := d.UpsertNode(ctx, types.Node{
		ID: calleeID, Kind: types.NodeKindFunction, Name: "callee",
		FilePath: "src/a.ts", Language: types.LanguageTypeScript,
		StartLine: 10, EndLine: 20, IsExported: true,
	}); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}
	if err := d.UpsertNode(ctx, types.Node{
		ID: callerID, Kind: types.NodeKindFunction, Name: "caller",
		FilePath: "src/a.ts", Language: types.LanguageTypeScript,
		StartLine: 1, EndLine: 9,
	}); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}
	seedUnresolvedRef(t, d, types.UnresolvedReference{
		ID: "ref-calls-001", FromNodeID: callerID, ReferenceName: "callee",
		ReferenceKind: types.EdgeKindCalls, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 5,
	})

	// A SQL table node that would otherwise be a plausible "match" if
	// sql_string refs leaked into promoteEdgeKind — proves exclusion, not
	// just "the ref never matches anything".
	tableID := "table:orders.sql:orders:1"
	if err := d.UpsertNode(ctx, types.Node{
		ID: tableID, Kind: types.NodeKindTable, Name: "orders",
		FilePath: "orders.sql", Language: types.LanguageSQL, StartLine: 1, EndLine: 5,
	}); err != nil {
		t.Fatalf("UpsertNode orders table: %v", err)
	}

	sqlStringRef := types.UnresolvedReference{
		ID: "ref-sqlstring-001", FromNodeID: callerID, ReferenceName: "no_such_object",
		ReferenceKind: types.ReferenceKindSQLString, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 7,
	}
	seedUnresolvedRef(t, d, sqlStringRef)

	// A sql_fragment ref (C8) — excludes it from standard resolution and
	// sweeps it via C5 cleanup, same as sql_string; wires actual matching.
	sqlFragmentRef := types.UnresolvedReference{
		ID: "ref-sqlfragment-001", FromNodeID: callerID, ReferenceName: "no_such_column",
		ReferenceKind: types.ReferenceKindSQLFragment, FilePath: "src/a.ts",
		Language: types.LanguageTypeScript, Line: 8,
	}
	seedUnresolvedRef(t, d, sqlFragmentRef)

	pipeline := resolution.NewPipeline(d)
	if _, _, err := pipeline.ResolveAndPersistBatched(ctx, 5000, nil); err != nil {
		t.Fatalf("ResolveAndPersistBatched: %v", err)
	}

	t.Run("normal ref still resolves", func(t *testing.T) {
		edges := edgesWithKind(t, d, callerID, types.EdgeKindCalls)
		if len(edges) == 0 {
			t.Error("expected a calls edge from the normal ref, got none")
		}
	})

	t.Run("sql_string ref never promoted to an edge", func(t *testing.T) {
		if edges := edgesWithKind(t, d, callerID, types.EdgeKindReferences); len(edges) != 0 {
			t.Errorf("expected 0 references edges from callerID (sql_string must not reach promoteEdgeKind), got %d", len(edges))
		}
	})

	t.Run("sql_string ref consumed by C5 cleanup, not left dangling forever", func(t *testing.T) {
		refs, err := d.GetUnresolvedRefs(ctx, 1000, 0)
		if err != nil {
			t.Fatalf("GetUnresolvedRefs: %v", err)
		}
		for _, r := range refs {
			if r.ID == "ref-sqlstring-001" {
				t.Fatal("expected sql_string ref to be deleted by pass A's C5 cleanup (no matching SQL object), found it still present")
			}
		}
	})

	t.Run("sql_fragment ref never promoted to an edge", func(t *testing.T) {
		if edges := edgesWithKind(t, d, callerID, types.EdgeKindReferences); len(edges) != 0 {
			t.Errorf("expected 0 references edges from callerID after sql_fragment exclusion, got %d", len(edges))
		}
	})

	t.Run("sql_fragment ref consumed by C5 cleanup (both kinds swept), not left dangling forever", func(t *testing.T) {
		refs, err := d.GetUnresolvedRefs(ctx, 1000, 0)
		if err != nil {
			t.Fatalf("GetUnresolvedRefs: %v", err)
		}
		for _, r := range refs {
			if r.ID == "ref-sqlfragment-001" {
				t.Fatal("expected sql_fragment ref to be deleted by C5 cleanup, found it still present")
			}
		}
	})
}
