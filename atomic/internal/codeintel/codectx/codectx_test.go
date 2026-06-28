package codectx_test

// Tests for the codectx package (master CP19).
//
// Fixture structure (built via db CRUD):
//
//   Files:
//     file_a.go  (Go) — contains funcA, funcB
//     file_b.go  (Go) — contains funcC
//
//   Nodes:
//     funcA  (function, "Alpha")   in file_a.go  — calls funcB
//     funcB  (function, "Beta")    in file_a.go  — calls funcC
//     funcC  (function, "Gamma")   in file_b.go  — leaf
//     ifaceI (interface, "IAlpha") in file_a.go  — implemented by funcA's class
//     classX (class, "XImpl")      in file_b.go  — implements ifaceI
//     extra1..extra6 (functions)   in file_a.go  — diversity cap triggers
//
//   Edges:
//     funcA --calls-->      funcB
//     funcB --calls-->      funcC
//     classX --implements--> ifaceI   (for BFS via extends/implements)
//     ifaceI --calls-->      funcA    (heuristic provenance, for marker test)
//     extra1..extra6 --calls--> funcA (to inflate file_a node count for diversity cap)

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/codectx"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// insertFixture inserts a corpus with:
//   - A→B→C call chain across 2 files
//   - classX implements ifaceI (heritage for BFS type context)
//   - one heuristic edge (ifaceI→funcA)
//   - extra1..extra6 in file_a.go to trigger diversity cap on that file
func insertFixture(t *testing.T, database *db.DB) {
	t.Helper()
	ctx := context.Background()

	nodes := []types.Node{
		{ID: "funcA", Kind: types.NodeKindFunction, Name: "Alpha", QualifiedName: "pkg.Alpha", FilePath: "src/file_a.go", Language: types.LanguageGo},
		{ID: "funcB", Kind: types.NodeKindFunction, Name: "Beta", QualifiedName: "pkg.Beta", FilePath: "src/file_a.go", Language: types.LanguageGo},
		{ID: "funcC", Kind: types.NodeKindFunction, Name: "Gamma", QualifiedName: "pkg.Gamma", FilePath: "src/file_b.go", Language: types.LanguageGo},
		// ifaceI in its own file so it survives diversity capping of file_a
		{ID: "ifaceI", Kind: types.NodeKindInterface, Name: "IAlpha", QualifiedName: "pkg.IAlpha", FilePath: "src/iface.go", Language: types.LanguageGo},
		{ID: "classX", Kind: types.NodeKindClass, Name: "XImpl", QualifiedName: "pkg.XImpl", FilePath: "src/file_b.go", Language: types.LanguageGo},
		// extra nodes in file_a to trigger diversity cap
		{ID: "extra1", Kind: types.NodeKindFunction, Name: "Extra1", FilePath: "src/file_a.go", Language: types.LanguageGo},
		{ID: "extra2", Kind: types.NodeKindFunction, Name: "Extra2", FilePath: "src/file_a.go", Language: types.LanguageGo},
		{ID: "extra3", Kind: types.NodeKindFunction, Name: "Extra3", FilePath: "src/file_a.go", Language: types.LanguageGo},
		{ID: "extra4", Kind: types.NodeKindFunction, Name: "Extra4", FilePath: "src/file_a.go", Language: types.LanguageGo},
		{ID: "extra5", Kind: types.NodeKindFunction, Name: "Extra5", FilePath: "src/file_a.go", Language: types.LanguageGo},
		{ID: "extra6", Kind: types.NodeKindFunction, Name: "Extra6", FilePath: "src/file_a.go", Language: types.LanguageGo},
	}
	for _, n := range nodes {
		if err := database.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert node %s: %v", n.ID, err)
		}
	}

	edges := []types.Edge{
		{Source: "funcA", Target: "funcB", Kind: types.EdgeKindCalls},
		{Source: "funcB", Target: "funcC", Kind: types.EdgeKindCalls},
		{Source: "classX", Target: "ifaceI", Kind: types.EdgeKindImplements},
		// heuristic edge: ifaceI "calls" funcA (synthesized — low confidence)
		{Source: "ifaceI", Target: "funcA", Kind: types.EdgeKindCalls, Provenance: "heuristic"},
		// extra→funcA calls to inflate file_a count
		{Source: "extra1", Target: "funcA", Kind: types.EdgeKindCalls},
		{Source: "extra2", Target: "funcA", Kind: types.EdgeKindCalls},
		{Source: "extra3", Target: "funcA", Kind: types.EdgeKindCalls},
		{Source: "extra4", Target: "funcA", Kind: types.EdgeKindCalls},
		{Source: "extra5", Target: "funcA", Kind: types.EdgeKindCalls},
		{Source: "extra6", Target: "funcA", Kind: types.EdgeKindCalls},
	}
	for _, e := range edges {
		if _, err := database.InsertEdge(ctx, e); err != nil {
			t.Fatalf("insert edge %s→%s: %v", e.Source, e.Target, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFindRelevantContext_GatherAndBFS verifies that querying "Alpha" finds
// funcA as a seed and expands BFS to funcB (funcA calls funcB).
func TestFindRelevantContext_GatherAndBFS(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 1})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	// seed funcA should be present
	if _, ok := sg.Nodes["funcA"]; !ok {
		t.Error("expected funcA in subgraph nodes")
	}
	// BFS depth 1: funcB is a callee of funcA → should be present
	if _, ok := sg.Nodes["funcB"]; !ok {
		t.Error("expected funcB (BFS callee of funcA) in subgraph nodes")
	}

	// tier must be non-empty (fts, like, or fuzzy)
	if tier == "" {
		t.Error("expected non-empty tier")
	}
}

// TestFindRelevantContext_SourceTierPropagates verifies that the source tier
// returned by FindRelevantContext propagates correctly into Context.Source for
// the FTS, LIKE, and fuzzy tiers.
//
// FTS tier: querying "Alpha" hits the FTS5 index (normal path).
// LIKE tier: node name "LikeOnlyXq7", query "likeonlyxq" — FTS token is
//
//	"likeonlyxq7" so "likeonlyxq" misses FTS; LIKE substring match finds it.
//
// Fuzzy tier: node name "FuzzyUniq", query "fuzzyunir" — FTS token is
//
//	"fuzzyuniq" so "fuzzyunir" misses FTS; "fuzzyunir" is not a substring of
//	"fuzzyuniq" so LIKE misses; DL distance=1 hits the fuzzy tier.
func TestFindRelevantContext_SourceTierPropagates(t *testing.T) {
	t.Run("fts", func(t *testing.T) {
		database := openTestDB(t)
		insertFixture(t, database)

		builder := codectx.New(database)
		sg, tier, truncated, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{})
		if err != nil {
			t.Fatalf("FindRelevantContext: %v", err)
		}
		ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
			Format:    codectx.FormatMarkdown,
			Query:     "Alpha",
			Source:    tier,
			Truncated: truncated,
		})
		if err != nil {
			t.Fatalf("BuildContext: %v", err)
		}
		if ctx.Source != tier {
			t.Errorf("Context.Source = %q; want %q", ctx.Source, tier)
		}
		if ctx.Source != "fts" {
			t.Errorf("expected FTS tier for 'Alpha' query, got %q", ctx.Source)
		}
	})

	t.Run("like", func(t *testing.T) {
		database := openTestDB(t)
		// Insert a node with name "XqLikeOnly". Query "likeonly":
		//   - FTS5 buildFTSQuery wraps as "likeonly"* (prefix query). The FTS token
		//     for "XqLikeOnly" is "xqlikeonly" — it does NOT start with "likeonly",
		//     so the FTS prefix match misses.
		//   - LIKE %likeonly% case-insensitively matches "xqlikeonly" → LIKE hit.
		err := database.UpsertNode(context.Background(), types.Node{
			ID:       "likenode1",
			Kind:     types.NodeKindFunction,
			Name:     "XqLikeOnly",
			FilePath: "src/like_test.go",
			Language: types.LanguageGo,
		})
		if err != nil {
			t.Fatalf("upsert like node: %v", err)
		}

		builder := codectx.New(database)
		sg, tier, truncated, err := builder.FindRelevantContext(context.Background(), "likeonly", codectx.Options{})
		if err != nil {
			t.Fatalf("FindRelevantContext: %v", err)
		}
		ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
			Format:    codectx.FormatMarkdown,
			Query:     "likeonly",
			Source:    tier,
			Truncated: truncated,
		})
		if err != nil {
			t.Fatalf("BuildContext: %v", err)
		}
		if ctx.Source != "like" {
			t.Errorf("expected LIKE tier for 'likeonly' query against XqLikeOnly, got %q", ctx.Source)
		}
		if _, ok := sg.Nodes["likenode1"]; !ok {
			t.Error("expected likenode1 in subgraph for LIKE-tier query")
		}
	})

	t.Run("fuzzy", func(t *testing.T) {
		database := openTestDB(t)
		// Insert a node whose name is "FuzzyUniq". Query "fuzzyunir":
		//   - FTS token in index is "fuzzyuniq"; "fuzzyunir" is a different token → FTS miss.
		//   - "fuzzyuniq" does not contain "fuzzyunir" as a substring → LIKE miss.
		//   - DL distance("fuzzyuniq", "fuzzyunir") = 1 (q→r substitution) ≤ maxDist=2 → fuzzy hit.
		err := database.UpsertNode(context.Background(), types.Node{
			ID:       "fuzzynode1",
			Kind:     types.NodeKindFunction,
			Name:     "FuzzyUniq",
			FilePath: "src/fuzzy_test.go",
			Language: types.LanguageGo,
		})
		if err != nil {
			t.Fatalf("upsert fuzzy node: %v", err)
		}

		builder := codectx.New(database)
		sg, tier, truncated, err := builder.FindRelevantContext(context.Background(), "fuzzyunir", codectx.Options{})
		if err != nil {
			t.Fatalf("FindRelevantContext: %v", err)
		}
		ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
			Format:    codectx.FormatMarkdown,
			Query:     "fuzzyunir",
			Source:    tier,
			Truncated: truncated,
		})
		if err != nil {
			t.Fatalf("BuildContext: %v", err)
		}
		if ctx.Source != "fuzzy" {
			t.Errorf("expected fuzzy tier for 'fuzzyunir' query, got %q", ctx.Source)
		}
		if _, ok := sg.Nodes["fuzzynode1"]; !ok {
			t.Error("expected fuzzynode1 in subgraph for fuzzy-tier query")
		}
	})
}

// TestFindRelevantContext_DiversityCap verifies that when a single file
// dominates the result set, diversity capping limits its contribution and
// sets Truncated=true on the resulting Context.
func TestFindRelevantContext_DiversityCap(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	// Use a big BFS depth so all extra nodes get pulled in.
	builder := codectx.New(database)
	sg, tier, truncated, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 3})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	// The raw BFS result would include funcA,funcB,ifaceI,extra1-6 from file_a.go
	// (8 nodes from the same file), plus funcC, classX from file_b.go.
	// Diversity cap should limit file_a to MaxPerFile and set Truncated on context.
	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format:    codectx.FormatMarkdown,
		Query:     "Alpha",
		Source:    tier,
		Truncated: truncated,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	// Count file_a.go nodes in the subgraph.
	fileACount := 0
	for _, n := range sg.Nodes {
		if n.FilePath == "src/file_a.go" {
			fileACount++
		}
	}
	// Should be capped at MaxPerFile (default 5)
	if fileACount > codectx.DefaultMaxPerFile {
		t.Errorf("file_a nodes = %d; want ≤ %d (diversity cap)", fileACount, codectx.DefaultMaxPerFile)
	}
	// Context must be marked Truncated because cap fired
	if !ctx.Truncated {
		t.Error("expected Truncated=true when diversity cap fired")
	}
}

// TestMarkdown_StableHeadings verifies the stable section headings contract.
func TestMarkdown_StableHeadings(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 1})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format: codectx.FormatMarkdown,
		Query:  "Alpha",
		Source: tier,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	md := ctx.Content
	// Required headings in order
	headings := []string{
		"# Context:",
		"## Symbols",
		"## Call paths",
		"## Relationships",
	}
	lastPos := -1
	for _, h := range headings {
		pos := strings.Index(md, h)
		if pos < 0 {
			t.Errorf("missing heading %q in markdown output", h)
			continue
		}
		if pos <= lastPos {
			t.Errorf("heading %q appears before previous heading (want stable order)", h)
		}
		lastPos = pos
	}
}

// TestMarkdown_HeuristicEdgeMarker verifies that heuristic edges get a
// low-confidence marker and static edges do not.
func TestMarkdown_HeuristicEdgeMarker(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 2})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format: codectx.FormatMarkdown,
		Query:  "Alpha",
		Source: tier,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	md := ctx.Content
	// The heuristic edge (ifaceI→funcA) should be visible with a marker.
	// The marker is "(heuristic)" per the spec.
	if !strings.Contains(md, "(heuristic)") {
		t.Error("markdown: expected (heuristic) marker for low-confidence edge; not found")
	}
	// The static edge (funcA→funcB, no provenance) should NOT have the marker.
	// We check by counting occurrences — if both edges have the marker that's wrong.
	// A weak but sufficient check: at least one edge line without "(heuristic)".
	relSection := md
	if idx := strings.Index(md, "## Relationships"); idx >= 0 {
		relSection = md[idx:]
	}
	linesWithoutMarker := false
	for _, line := range strings.Split(relSection, "\n") {
		if strings.Contains(line, "→") && !strings.Contains(line, "(heuristic)") {
			linesWithoutMarker = true
			break
		}
	}
	if !linesWithoutMarker {
		t.Error("markdown: every edge line has (heuristic) marker; static edges should not")
	}
}

// TestMarkdown_RelationshipsResolveNames is a regression guard: the Relationships
// section must render human-readable node names, not raw node IDs (the graph's
// foreign keys). Previously every edge printed as "funcA → funcB" (IDs) instead
// of "Alpha → Beta" (names), which surfaced as opaque "function:<hash> → field:<hash>"
// lines in real `atomic code explore` output.
func TestMarkdown_RelationshipsResolveNames(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 1})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format: codectx.FormatMarkdown,
		Query:  "Alpha",
		Source: tier,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	rel := ctx.Content
	if idx := strings.Index(rel, "## Relationships"); idx >= 0 {
		rel = rel[idx:]
	} else {
		t.Fatal("missing ## Relationships section")
	}

	// The funcA→funcB edge must render with resolved names.
	if !strings.Contains(rel, "Alpha → Beta (calls)") {
		t.Errorf("Relationships should resolve node IDs to names; want 'Alpha → Beta (calls)' in:\n%s", rel)
	}
	// The raw ID form must not leak.
	if strings.Contains(rel, "funcA → funcB") {
		t.Errorf("Relationships leaked raw node IDs instead of names:\n%s", rel)
	}
}

// TestJSON_StableShape verifies the JSON output has the required fields and
// nodes sorted by ID.
//
// Uses BFSDepth=2 so ifaceI is always reached (it is in iface.go, not file_a.go,
// so it survives any diversity capping). The heuristic-provenance assertion is
// unconditional — a filtering regression that drops ifaceI must fail this test.
func TestJSON_StableShape(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	// BFSDepth=2 ensures ifaceI (in its own file) is pulled into the subgraph
	// so the heuristic-edge assertion below is always reachable.
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 2})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format: codectx.FormatJSON,
		Query:  "Alpha",
		Source: tier,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	var out codectx.JSONOutput
	if err := json.Unmarshal([]byte(ctx.Content), &out); err != nil {
		t.Fatalf("json.Unmarshal: %v; content: %s", err, ctx.Content)
	}

	// Required top-level fields
	if out.Query == "" {
		t.Error("JSON: query field missing or empty")
	}
	if out.Source == "" {
		t.Error("JSON: source field missing or empty")
	}
	if len(out.Nodes) == 0 {
		t.Error("JSON: nodes array empty")
	}

	// Nodes must be sorted by ID
	for i := 1; i < len(out.Nodes); i++ {
		if out.Nodes[i].ID < out.Nodes[i-1].ID {
			t.Errorf("JSON: nodes not sorted by ID at index %d (%s < %s)", i, out.Nodes[i].ID, out.Nodes[i-1].ID)
		}
	}

	// Edges must carry provenance field (even if empty string for static)
	for _, e := range out.Edges {
		// Just verify the struct has the Provenance field decoded (it may be "")
		_ = e.Provenance
	}

	// ifaceI must be in the subgraph (it lives in its own file — iface.go — so
	// diversity capping on file_a cannot remove it). Failing here means the BFS
	// or filtering is broken and the heuristic assertion below would be vacuous.
	if _, ok := sg.Nodes["ifaceI"]; !ok {
		t.Fatal("JSON: ifaceI not in subgraph at BFSDepth=2; fixture or BFS is broken")
	}

	// Heuristic edge (ifaceI→funcA) must have provenance="heuristic" in the output.
	// This assertion is unconditional: if ifaceI is present (proven above) its
	// outgoing heuristic edge must survive deduplication and serialisation.
	foundHeuristic := false
	for _, e := range out.Edges {
		if e.Provenance == "heuristic" {
			foundHeuristic = true
		}
	}
	if !foundHeuristic {
		t.Error("JSON: expected at least one edge with provenance=heuristic; none found")
	}
}

// TestJSON_EdgesSortedByCompositeKey verifies edges are sorted by source+target+kind.
func TestJSON_EdgesSortedByCompositeKey(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 2})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format: codectx.FormatJSON,
		Query:  "Alpha",
		Source: tier,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	var out codectx.JSONOutput
	if err := json.Unmarshal([]byte(ctx.Content), &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	for i := 1; i < len(out.Edges); i++ {
		prev := out.Edges[i-1]
		curr := out.Edges[i]
		prevKey := prev.Source + "\x00" + prev.Target + "\x00" + prev.Kind
		currKey := curr.Source + "\x00" + curr.Target + "\x00" + curr.Kind
		if currKey < prevKey {
			t.Errorf("JSON edges not sorted at index %d: %q < %q", i, currKey, prevKey)
		}
	}
}

// TestNodeCountEdgeCount verifies NodeCount and EdgeCount in Context.
func TestNodeCountEdgeCount(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 1})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format: codectx.FormatMarkdown,
		Query:  "Alpha",
		Source: tier,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	if ctx.NodeCount != len(sg.Nodes) {
		t.Errorf("NodeCount = %d; want %d", ctx.NodeCount, len(sg.Nodes))
	}
	if ctx.EdgeCount != len(sg.Edges) {
		t.Errorf("EdgeCount = %d; want %d", ctx.EdgeCount, len(sg.Edges))
	}
}

// TestDeduplicateEdges_HeuristicWinsOverEmpty verifies that when the same
// logical edge (source+target+kind) exists in the DB with both empty provenance
// (static) and "heuristic" provenance, deduplication in FindRelevantContext
// retains the heuristic marker. This guards the "heuristic wins over empty"
// invariant from appendix G: the low-confidence marker must not be silently lost.
//
// The edges table has no unique constraint on (source,target,kind), so inserting
// the same logical edge twice (different provenance) is valid. GetCallees (and
// GetCallers) return all matching rows; deduplicateEdges collapses them to the
// heuristic-provenance winner.
func TestDeduplicateEdges_HeuristicWinsOverEmpty(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name            string
		staticProvFirst bool // whether the static (empty) edge is inserted before the heuristic one
	}{
		{name: "static_first", staticProvFirst: true},
		{name: "heuristic_first", staticProvFirst: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openTestDB(t)
			nodeA := types.Node{ID: "da", Kind: types.NodeKindFunction, Name: "DA", FilePath: "f.go", Language: types.LanguageGo}
			nodeB := types.Node{ID: "db", Kind: types.NodeKindFunction, Name: "DB", FilePath: "f.go", Language: types.LanguageGo}
			if err := database.UpsertNode(ctx, nodeA); err != nil {
				t.Fatalf("upsert nodeA: %v", err)
			}
			if err := database.UpsertNode(ctx, nodeB); err != nil {
				t.Fatalf("upsert nodeB: %v", err)
			}

			// Insert the same logical edge (da→db calls) twice with different provenance.
			// Order is determined by the test case so both static-first and heuristic-first
			// orderings are exercised.
			first := types.Edge{Source: "da", Target: "db", Kind: types.EdgeKindCalls, Provenance: ""}
			second := types.Edge{Source: "da", Target: "db", Kind: types.EdgeKindCalls, Provenance: "heuristic"}
			if !tc.staticProvFirst {
				first, second = second, first
			}
			if _, err := database.InsertEdge(ctx, first); err != nil {
				t.Fatalf("insert first edge: %v", err)
			}
			if _, err := database.InsertEdge(ctx, second); err != nil {
				t.Fatalf("insert second edge: %v", err)
			}

			builder := codectx.New(database)
			sg, tier, _, err := builder.FindRelevantContext(ctx, "DA", codectx.Options{BFSDepth: 1})
			if err != nil {
				t.Fatalf("FindRelevantContext: %v", err)
			}
			out, err := builder.BuildContext(ctx, sg, codectx.BuildOptions{
				Format: codectx.FormatJSON,
				Query:  "DA",
				Source: tier,
			})
			if err != nil {
				t.Fatalf("BuildContext: %v", err)
			}
			var j codectx.JSONOutput
			if err := json.Unmarshal([]byte(out.Content), &j); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// After dedup there must be exactly one da→db calls edge.
			var dedupEdges []codectx.JSONEdge
			for _, e := range j.Edges {
				if e.Source == "da" && e.Target == "db" && e.Kind == string(types.EdgeKindCalls) {
					dedupEdges = append(dedupEdges, e)
				}
			}
			if len(dedupEdges) != 1 {
				t.Errorf("want 1 da→db edge after dedup, got %d", len(dedupEdges))
			}
			// The surviving edge must carry "heuristic" provenance.
			if len(dedupEdges) > 0 && dedupEdges[0].Provenance != "heuristic" {
				t.Errorf("want provenance=heuristic after dedup, got %q", dedupEdges[0].Provenance)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// F-55: seed BFS expansion order must be deterministic
// ---------------------------------------------------------------------------

// TestFindRelevantContext_SeedOrderDeterministic verifies that when multiple
// seeds are gathered (≥3), the BFS expansion order is stable across repeated
// calls. Uses 4 seeds (A, B, C, D) with distinct callees so the combined
// subgraph varies if seeds are iterated in non-deterministic map order.
func TestFindRelevantContext_SeedOrderDeterministic(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	// 4 seed nodes, each with a unique callee — callee IDs are chosen so sorted
	// seed iteration produces a different BFS expansion order from random order
	// (the callee edge weights and nodeDepth assignments differ by iteration order).
	// With sorted seeds, both runs must produce identical combined subgraphs.
	seedNodes := []types.Node{
		{ID: "seedA", Kind: types.NodeKindFunction, Name: "SeedA", FilePath: "src/s.go", Language: types.LanguageGo},
		{ID: "seedB", Kind: types.NodeKindFunction, Name: "SeedB", FilePath: "src/s.go", Language: types.LanguageGo},
		{ID: "seedC", Kind: types.NodeKindFunction, Name: "SeedC", FilePath: "src/s.go", Language: types.LanguageGo},
		{ID: "seedD", Kind: types.NodeKindFunction, Name: "SeedD", FilePath: "src/s.go", Language: types.LanguageGo},
	}
	calleeNodes := []types.Node{
		{ID: "calleeA", Kind: types.NodeKindFunction, Name: "CalleeA", FilePath: "src/c.go", Language: types.LanguageGo},
		{ID: "calleeB", Kind: types.NodeKindFunction, Name: "CalleeB", FilePath: "src/c.go", Language: types.LanguageGo},
		{ID: "calleeC", Kind: types.NodeKindFunction, Name: "CalleeC", FilePath: "src/c.go", Language: types.LanguageGo},
		{ID: "calleeD", Kind: types.NodeKindFunction, Name: "CalleeD", FilePath: "src/c.go", Language: types.LanguageGo},
	}
	for _, n := range append(seedNodes, calleeNodes...) {
		if err := database.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert %s: %v", n.ID, err)
		}
	}
	for _, e := range []types.Edge{
		{Source: "seedA", Target: "calleeA", Kind: types.EdgeKindCalls},
		{Source: "seedB", Target: "calleeB", Kind: types.EdgeKindCalls},
		{Source: "seedC", Target: "calleeC", Kind: types.EdgeKindCalls},
		{Source: "seedD", Target: "calleeD", Kind: types.EdgeKindCalls},
	} {
		if _, err := database.InsertEdge(ctx, e); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}

	builder := codectx.New(database)

	// Query "Seed" to get all 4 seeds; BFSDepth=1 expands to callees.
	sg1, tier1, _, err := builder.FindRelevantContext(ctx, "Seed", codectx.Options{BFSDepth: 1})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	ctx1, err := builder.BuildContext(ctx, sg1, codectx.BuildOptions{
		Format: codectx.FormatJSON,
		Query:  "Seed",
		Source: tier1,
	})
	if err != nil {
		t.Fatalf("first BuildContext: %v", err)
	}

	sg2, tier2, _, err := builder.FindRelevantContext(ctx, "Seed", codectx.Options{BFSDepth: 1})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	ctx2, err := builder.BuildContext(ctx, sg2, codectx.BuildOptions{
		Format: codectx.FormatJSON,
		Query:  "Seed",
		Source: tier2,
	})
	if err != nil {
		t.Fatalf("second BuildContext: %v", err)
	}

	if ctx1.Content != ctx2.Content {
		t.Errorf("JSON output differs between runs:\nrun1: %s\nrun2: %s", ctx1.Content, ctx2.Content)
	}
}

// TestReproducibility is the determinism check: build the same context N times
// and assert byte-identical output for both markdown AND JSON.
func TestReproducibility(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	const rounds = 10

	var mdOutputs [rounds]string
	var jsonOutputs [rounds]string

	for i := 0; i < rounds; i++ {
		sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 2})
		if err != nil {
			t.Fatalf("round %d FindRelevantContext: %v", i, err)
		}

		mdCtx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
			Format: codectx.FormatMarkdown,
			Query:  "Alpha",
			Source: tier,
		})
		if err != nil {
			t.Fatalf("round %d BuildContext(md): %v", i, err)
		}
		mdOutputs[i] = mdCtx.Content

		jsonCtx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
			Format: codectx.FormatJSON,
			Query:  "Alpha",
			Source: tier,
		})
		if err != nil {
			t.Fatalf("round %d BuildContext(json): %v", i, err)
		}
		jsonOutputs[i] = jsonCtx.Content
	}

	for i := 1; i < rounds; i++ {
		if mdOutputs[i] != mdOutputs[0] {
			t.Errorf("markdown output differs at round %d", i)
		}
		if jsonOutputs[i] != jsonOutputs[0] {
			t.Errorf("JSON output differs at round %d", i)
		}
	}
}
