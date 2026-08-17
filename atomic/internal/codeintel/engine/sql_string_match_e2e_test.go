package engine_test

// End-to-end tests for SQL string matching against a real SQLite backend:
// object matching, qualified and anchored column matching, confidence tiering,
// and the cleanup that must leave zero sql_string refs behind.
// See docs/spec/sql-string-match.md.

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

// Shared with the eval harness, so both stay in sync off one source.
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

	// Matches on target name, provenance, and confidence tier together.
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

	// The extractor mints column nodes for tables only, never views. Pinned
	// here because the anchoring assertions below depend on it.
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

	anchoredOwner, ok := findNode(types.NodeKindFunction, "anchoredQueries")
	if !ok {
		t.Fatal("function 'anchoredQueries' not found in DB")
	}
	unanchoredOwner, ok := findNode(types.NodeKindFunction, "unanchoredQueries")
	if !ok {
		t.Fatal("function 'unanchoredQueries' not found in DB")
	}

	assertStringMatchEdge(anchoredOwner, types.NodeKindView, "active_orders", "high")

	assertStringMatchEdge(anchoredOwner, types.NodeKindTable, "orders_tbl", "high")

	assertStringMatchEdge(anchoredOwner, types.NodeKindColumn, "status", "medium")

	// The bare "status" anchors via the table, not the view, which mints no
	// columns. Both it and the qualified edge point at a column named
	// "status", so the two are told apart only by confidence tier.
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

	// "status = ?" tokenizes to a bare column already at the low floor, so it
	// is indistinguishable from the plain bare-form edge asserted above.

	// "DESC" is stoplisted, so the qualified pair survives at medium and the
	// fragment demotion drops it to low.
	assertStringMatchEdge(anchoredOwner, types.NodeKindColumn, "total", "low")

	// Both tokens of "order_id, status" are bare columns of the anchored table.
	assertStringMatchEdge(anchoredOwner, types.NodeKindColumn, "order_id", "low")

	// "error = timeout" clears the fragment gate on its operator but names no
	// real object. Asserted on the owner's outgoing edges rather than on node
	// existence, since string matching never mints nodes anyway.
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

	// The accepted tradeoff: "retries" in "error = retries" is prose, but a
	// decoy table shares the name, so it resolves at low confidence anyway.
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

	// This owner never matched a table or view, so its bare "status" has no
	// anchor and must not resolve.
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

	// None of the negative-case strings may have minted a node.
	for _, kind := range []types.NodeKind{types.NodeKindTable, types.NodeKindView, types.NodeKindProcedure, types.NodeKindColumn, types.NodeKindFunction} {
		if _, ok := findNode(kind, "totally_unknown_object"); ok {
			t.Errorf("a node named 'totally_unknown_object' (%s) was minted — sql_string refs must never mint nodes", kind)
		}
	}
	if _, ok := findNode(types.NodeKindTable, orderTable.Name+"_ghost"); ok {
		t.Error("unexpected phantom table node")
	}

	// No sql_string ref may survive resolution. The facade exposes no
	// unresolved-ref query, so read the file directly — WAL permits it.
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
