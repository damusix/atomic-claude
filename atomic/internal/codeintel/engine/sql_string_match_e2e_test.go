package engine_test

// sql_string_match_e2e_test.go — C7: engine-level integration test for the
// sql-string-match feature (docs/spec/sql-string-match.md). Indexes the
// scripts/code-eval/fixtures/sql-string-match/ corpus with a real SQLite
// backend and asserts the resolved edge set end to end: pass A object
// matching, pass B qualified/anchored column matching, confidence tiering,
// and C5 cleanup (zero sql_string refs survive).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// sqlStringMatchFixtureDir is the corpus this test indexes verbatim — the
// same files the eval harness uses, kept in sync by pointing at one source.
const sqlStringMatchFixtureDir = "../../../../scripts/code-eval/fixtures/sql-string-match"

func TestSQLStringMatchEndToEnd(t *testing.T) {
	root := t.TempDir()

	entries, err := os.ReadDir(sqlStringMatchFixtureDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", sqlStringMatchFixtureDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(sqlStringMatchFixtureDir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(root, e.Name()), content, 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", e.Name(), err)
		}
	}

	idxDir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(idxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	eng, err := engine.New(root)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}
	if err := eng.ResolveReferences(ctx); err != nil {
		t.Fatalf("ResolveReferences: %v", err)
	}

	findNode := func(kind types.NodeKind, name string) (types.Node, bool) {
		t.Helper()
		nodes, err := eng.GetNodesByKind(ctx, kind)
		if err != nil {
			t.Fatalf("GetNodesByKind(%s): %v", kind, err)
		}
		for _, n := range nodes {
			if strings.EqualFold(n.Name, name) {
				return n, true
			}
		}
		return types.Node{}, false
	}

	// confidenceOf reads the "confidence" key out of an edge's JSON metadata.
	confidenceOf := func(e types.Edge) string {
		if len(e.Metadata) == 0 {
			return ""
		}
		var m map[string]string
		if err := json.Unmarshal(e.Metadata, &m); err != nil {
			return ""
		}
		return m["confidence"]
	}

	// assertStringMatchEdge asserts a references edge from fromNode to a
	// node named targetName, with provenance "string-match" and the given
	// confidence tier.
	assertStringMatchEdge := func(fromNode types.Node, targetKind types.NodeKind, targetName, wantConfidence string) {
		t.Helper()
		edges, err := eng.GetOutgoingEdges(ctx, fromNode.ID)
		if err != nil {
			t.Fatalf("GetOutgoingEdges(%s): %v", fromNode.ID, err)
		}
		for _, e := range edges {
			if e.Kind != types.EdgeKindReferences || e.Provenance != "string-match" {
				continue
			}
			tgt, err := eng.GetNode(ctx, e.Target)
			if err != nil {
				continue
			}
			if tgt.Kind == targetKind && strings.EqualFold(tgt.Name, targetName) {
				if got := confidenceOf(e); got != wantConfidence {
					t.Errorf("edge %s -[string-match]-> %s: confidence = %q, want %q",
						fromNode.Name, targetName, got, wantConfidence)
				}
				return
			}
		}
		t.Errorf("missing string-match edge %s -> %s (%s)\n  outgoing: %v",
			fromNode.Name, targetName, targetKind, summarizeEdges(edges))
	}

	// -- Fixture objects --
	orderTable, ok := findNode(types.NodeKindTable, "orders_tbl")
	if !ok {
		t.Fatal("table 'orders_tbl' not found in DB")
	}
	activeOrdersView, ok := findNode(types.NodeKindView, "active_orders")
	if !ok {
		t.Fatal("view 'active_orders' not found in DB")
	}
	if _, ok := findNode(types.NodeKindProcedure, "archive_orders"); !ok {
		t.Fatal("procedure 'archive_orders' not found in DB")
	}

	// C7 empirical finding: the SQL extractor mints column nodes for TABLES
	// only, never for VIEWS. Confirm that here so the rest of the test (and
	// any future reader) doesn't assume otherwise.
	viewCols, err := eng.GetOutgoingEdges(ctx, activeOrdersView.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges(active_orders): %v", err)
	}
	for _, e := range viewCols {
		if e.Kind == types.EdgeKindContains {
			tgt, gerr := eng.GetNode(ctx, e.Target)
			if gerr == nil && tgt.Kind == types.NodeKindColumn {
				t.Fatalf("unexpected column node contained by view 'active_orders': %s — the SQL extractor was assumed not to emit view columns", tgt.Name)
			}
		}
	}

	// -- Owner scopes (host functions in queries.ts) --
	anchoredOwner, ok := findNode(types.NodeKindFunction, "anchoredQueries")
	if !ok {
		t.Fatal("function 'anchoredQueries' not found in DB")
	}
	unanchoredOwner, ok := findNode(types.NodeKindFunction, "unanchoredQueries")
	if !ok {
		t.Fatal("function 'unanchoredQueries' not found in DB")
	}

	// High: selectFrom("active_orders") — vocabulary callee + object match (view).
	assertStringMatchEdge(anchoredOwner, types.NodeKindView, "active_orders", "high")

	// High: innerJoin("orders_tbl", ...) — vocabulary callee + object match (table).
	assertStringMatchEdge(anchoredOwner, types.NodeKindTable, "orders_tbl", "high")

	// Medium: qualified column "orders_tbl.status" from the innerJoin args.
	assertStringMatchEdge(anchoredOwner, types.NodeKindColumn, "status", "medium")

	// Low: bare column "status" from .select("status"), anchored via the
	// TABLE (orders_tbl) — the view anchor contributes no column candidates
	// since views mint no column nodes (finding above).
	//
	// Both the qualified-column edge and this bare-column edge target a
	// column literally named "status" with provenance string-match; the
	// medium-tier assertion above already consumed one such edge conceptually,
	// so distinguish by checking BOTH tiers are present among this owner's
	// outgoing string-match edges to a "status" column.
	edges, err := eng.GetOutgoingEdges(ctx, anchoredOwner.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges(anchoredQueries): %v", err)
	}
	var sawMedium, sawLow bool
	for _, e := range edges {
		if e.Kind != types.EdgeKindReferences || e.Provenance != "string-match" {
			continue
		}
		tgt, gerr := eng.GetNode(ctx, e.Target)
		if gerr != nil || tgt.Kind != types.NodeKindColumn || !strings.EqualFold(tgt.Name, "status") {
			continue
		}
		switch confidenceOf(e) {
		case "medium":
			sawMedium = true
		case "low":
			sawLow = true
		}
	}
	if !sawMedium {
		t.Error("anchoredQueries: no medium-confidence string-match edge to column 'status' (qualified form)")
	}
	if !sawLow {
		t.Error("anchoredQueries: no low-confidence string-match edge to column 'status' (anchored bare form)")
	}

	// C8 fragment tier — where-fragment "status = ?": tokenizes to bare
	// column "status", anchored-bare-column match computes low, fragment
	// demotion leaves it at the low floor. The plain bare-form "status" edge
	// (sql_string, also low) already makes sawLow true above, so this case
	// doesn't add a distinguishable assertion of its own beyond that check.

	// C8 fragment tier — order-DESC "orders_tbl.total DESC": qualified pair
	// survives tokenization ("DESC" stoplisted), qualified-column match
	// computes medium, fragment demotion drops it to low.
	assertStringMatchEdge(anchoredOwner, types.NodeKindColumn, "total", "low")

	// C8 fragment tier — comma-separated pluck "order_id, status": both
	// tokens are bare columns of the anchored table, low already at the
	// floor after fragment demotion.
	assertStringMatchEdge(anchoredOwner, types.NodeKindColumn, "order_id", "low")

	// C8 fragment negative — "error = timeout" passes the fragment gate (has
	// a comparison operator) but neither tokenized identifier names any SQL
	// object that exists in this index; must produce zero string-match edges
	// from its owner. (Node-existence would be trivially true here — string
	// matching never mints nodes — so this asserts on the owner's outgoing
	// edge set instead, which is what the fragment gate actually risks.)
	for _, e := range edges {
		if e.Kind != types.EdgeKindReferences || e.Provenance != "string-match" {
			continue
		}
		tgt, gerr := eng.GetNode(ctx, e.Target)
		if gerr != nil {
			continue
		}
		if strings.EqualFold(tgt.Name, "error") || strings.EqualFold(tgt.Name, "timeout") {
			t.Errorf("anchoredQueries: unexpected string-match edge to %q (%s) from the prose-negative literal 'error = timeout'", tgt.Name, tgt.Kind)
		}
	}

	// C8 fragment positive (prose-collision tradeoff) — "error = retries" in
	// proseDecoyQueries tokenizes to "error"/"retries"; "retries" is a real
	// decoy table (schema.sql), so pass A resolves it via a non-vocabulary
	// CalleeExpr ("where") at medium, demoted one notch to low by the
	// fragment tier. This is the accepted tradeoff C8 documents: a same-named
	// object anywhere in the index can produce a spurious low-confidence
	// edge from an otherwise-prose fragment.
	decoyOwner, ok := findNode(types.NodeKindFunction, "proseDecoyQueries")
	if !ok {
		t.Fatal("function 'proseDecoyQueries' not found in DB")
	}
	assertStringMatchEdge(decoyOwner, types.NodeKindTable, "retries", "low")

	decoyEdges, err := eng.GetOutgoingEdges(ctx, decoyOwner.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges(proseDecoyQueries): %v", err)
	}
	for _, e := range decoyEdges {
		if e.Kind != types.EdgeKindReferences || e.Provenance != "string-match" {
			continue
		}
		tgt, gerr := eng.GetNode(ctx, e.Target)
		if gerr != nil {
			continue
		}
		if strings.EqualFold(tgt.Name, "error") {
			t.Errorf("proseDecoyQueries: unexpected string-match edge to %q (%s) — 'error' names no SQL object", tgt.Name, tgt.Kind)
		}
	}

	// Negative: unanchoredQueries's bare "status" literal must NOT resolve —
	// this owner scope never matched a table/view via pass A, so no anchor
	// exists for bare-column matching.
	unanchoredEdges, err := eng.GetOutgoingEdges(ctx, unanchoredOwner.ID)
	if err != nil {
		t.Fatalf("GetOutgoingEdges(unanchoredQueries): %v", err)
	}
	for _, e := range unanchoredEdges {
		if e.Provenance == "string-match" {
			tgt, _ := eng.GetNode(ctx, e.Target)
			t.Errorf("unanchoredQueries: unexpected string-match edge to %q (%s) — owner has no anchor", tgt.Name, tgt.Kind)
		}
	}

	// Negative: no node was minted from any of the negative-case strings —
	// "totally_unknown_object", the prose sentence, or the unanchored
	// "status" bare column resolving to a phantom node.
	for _, kind := range []types.NodeKind{types.NodeKindTable, types.NodeKindView, types.NodeKindProcedure, types.NodeKindColumn, types.NodeKindFunction} {
		if _, ok := findNode(kind, "totally_unknown_object"); ok {
			t.Errorf("a node named 'totally_unknown_object' (%s) was minted — sql_string refs must never mint nodes", kind)
		}
	}
	if _, ok := findNode(types.NodeKindTable, orderTable.Name+"_ghost"); ok {
		t.Error("unexpected phantom table node")
	}

	// C5: zero sql_string rows must remain in unresolved_refs after
	// resolution. The engine facade doesn't expose unresolved-ref queries,
	// so open the same SQLite file directly (WAL allows concurrent readers).
	eng.Close()
	rawDB, err := db.Open(config.IndexDBPath(root))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer rawDB.Close()
	refs, err := rawDB.GetUnresolvedRefsByKind(ctx, types.ReferenceKindSQLString)
	if err != nil {
		t.Fatalf("GetUnresolvedRefsByKind: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected zero sql_string unresolved refs, got %d: %+v", len(refs), refs)
	}

	fragmentRefs, err := rawDB.GetUnresolvedRefsByKind(ctx, types.ReferenceKindSQLFragment)
	if err != nil {
		t.Fatalf("GetUnresolvedRefsByKind(sql_fragment): %v", err)
	}
	if len(fragmentRefs) != 0 {
		t.Errorf("expected zero sql_fragment unresolved refs, got %d: %+v", len(fragmentRefs), fragmentRefs)
	}
}
