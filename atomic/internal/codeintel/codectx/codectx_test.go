package codectx_test

// The shared fixture is an A→B→C call chain split across two files, plus an
// implements edge, one heuristic edge, and six spare nodes in file_a.go whose
// only job is to push that file past the diversity cap.

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

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

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

func TestFindRelevantContext_GatherAndBFS(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, _, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 1})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	if _, ok := sg.Nodes["funcA"]; !ok {
		t.Error("expected funcA in subgraph nodes")
	}
	if _, ok := sg.Nodes["funcB"]; !ok {
		t.Error("expected funcB (BFS callee of funcA) in subgraph nodes")
	}

	if tier == "" {
		t.Error("expected non-empty tier")
	}
}

// Each subtest picks a name/query pair engineered to miss every tier above the
// one under test, so the tier that answers is unambiguous.
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
		// FTS matches on a token prefix, and "xqlikeonly" does not start with
		// "likeonly", so only LIKE's substring match can find this.
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
		// A one-character substitution: a different FTS token, not a substring,
		// so only the fuzzy tier is left.
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

func TestFindRelevantContext_DiversityCap(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	sg, tier, truncated, err := builder.FindRelevantContext(context.Background(), "Alpha", codectx.Options{BFSDepth: 3})
	if err != nil {
		t.Fatalf("FindRelevantContext: %v", err)
	}

	// Uncapped, the BFS would pull 8 nodes from file_a.go alone.
	ctx, err := builder.BuildContext(context.Background(), sg, codectx.BuildOptions{
		Format:    codectx.FormatMarkdown,
		Query:     "Alpha",
		Source:    tier,
		Truncated: truncated,
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}

	fileACount := 0
	for _, n := range sg.Nodes {
		if n.FilePath == "src/file_a.go" {
			fileACount++
		}
	}
	if fileACount > codectx.DefaultMaxPerFile {
		t.Errorf("file_a nodes = %d; want ≤ %d (diversity cap)", fileACount, codectx.DefaultMaxPerFile)
	}
	if !ctx.Truncated {
		t.Error("expected Truncated=true when diversity cap fired")
	}
}

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
	if !strings.Contains(md, "(heuristic)") {
		t.Error("markdown: expected (heuristic) marker for low-confidence edge; not found")
	}
	// At least one unmarked edge line proves the marker is not applied blanket.
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

// Guards against edges rendering as raw node IDs, which surfaced in real
// explore output as opaque "function:<hash> → field:<hash>" lines.
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

	if !strings.Contains(rel, "Alpha → Beta (calls)") {
		t.Errorf("Relationships should resolve node IDs to names; want 'Alpha → Beta (calls)' in:\n%s", rel)
	}
	if strings.Contains(rel, "funcA → funcB") {
		t.Errorf("Relationships leaked raw node IDs instead of names:\n%s", rel)
	}
}

// The heuristic-provenance assertion is deliberately unconditional: a filtering
// regression that drops ifaceI must fail here rather than pass vacuously.
func TestJSON_StableShape(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)

	builder := codectx.New(database)
	// Depth 2 is what reaches ifaceI.
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

	if out.Query == "" {
		t.Error("JSON: query field missing or empty")
	}
	if out.Source == "" {
		t.Error("JSON: source field missing or empty")
	}
	if len(out.Nodes) == 0 {
		t.Error("JSON: nodes array empty")
	}

	for i := 1; i < len(out.Nodes); i++ {
		if out.Nodes[i].ID < out.Nodes[i-1].ID {
			t.Errorf("JSON: nodes not sorted by ID at index %d (%s < %s)", i, out.Nodes[i].ID, out.Nodes[i-1].ID)
		}
	}

	// Provenance is always present, empty string for static edges.
	for _, e := range out.Edges {
		_ = e.Provenance
	}

	// Checked first: without ifaceI the assertion below would pass vacuously.
	if _, ok := sg.Nodes["ifaceI"]; !ok {
		t.Fatal("JSON: ifaceI not in subgraph at BFSDepth=2; fixture or BFS is broken")
	}

	// The marker must survive both deduplication and serialisation.
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

// The edges table has no unique constraint on (source, target, kind), so the
// same logical edge can exist twice with different provenance. Deduplication
// must keep the heuristic one, or the low-confidence marker is silently lost.
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

// seed BFS expansion order must be deterministic

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
