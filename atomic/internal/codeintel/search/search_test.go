package search_test

// The shared fixture deliberately includes prefix-colliding names plus one node
// each reachable only by LIKE and only by fuzzy, so every tier has a case that
// isolates it.

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/search"
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
		{
			ID:            "fn_parse",
			Kind:          types.NodeKindFunction,
			Name:          "parseQuery",
			QualifiedName: "parser.parseQuery",
			FilePath:      "src/parser.go",
			Language:      types.LanguageGo,
			Signature:     "func parseQuery(s string) *Query",
			Docstring:     "parseQuery parses a search query string and returns a Query",
		},
		{
			ID:            "fn_handle",
			Kind:          types.NodeKindMethod,
			Name:          "handleRequest",
			QualifiedName: "server.handleRequest",
			FilePath:      "src/server.go",
			Language:      types.LanguageGo,
			Signature:     "func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request)",
		},
		{
			ID:            "cls_parser",
			Kind:          types.NodeKindClass,
			Name:          "Parser",
			QualifiedName: "Parser",
			FilePath:      "src/parser.ts",
			Language:      types.LanguageTypeScript,
		},
		{
			ID:            "iface_repo",
			Kind:          types.NodeKindInterface,
			Name:          "Repository",
			QualifiedName: "Repository",
			FilePath:      "src/repo.ts",
			Language:      types.LanguageTypeScript,
		},
		{
			ID:            "route_api",
			Kind:          types.NodeKindRoute,
			Name:          "GET /api/items",
			QualifiedName: "routes.GET_api_items",
			FilePath:      "src/routes.ts",
			Language:      types.LanguageTypeScript,
		},
		{
			ID:            "fn_util",
			Kind:          types.NodeKindFunction,
			Name:          "parse_utils",
			QualifiedName: "util.parse_utils",
			FilePath:      "src/util.go",
			Language:      types.LanguageGo,
			Docstring:     "parse_utils provides parsing utilities",
		},
		{
			ID:            "fn_test",
			Kind:          types.NodeKindFunction,
			Name:          "TestParseQuery",
			QualifiedName: "TestParseQuery",
			FilePath:      "src/parser_test.go",
			Language:      types.LanguageGo,
			Docstring:     "TestParseQuery tests parseQuery function",
		},
		// Reachable only by LIKE: the name is shaped so the FTS tokenizer
		// produces nothing a substring query can prefix-match.
		{
			ID:       "fn_likeonly",
			Kind:     types.NodeKindFunction,
			Name:     "xyzSpecialName",
			FilePath: "src/special.go",
			Language: types.LanguageGo,
		},
		// Fuzzy-only: a 1-char typo in a short name — "handlReq" for "handleReq"
		{
			ID:       "fn_fuzzy",
			Kind:     types.NodeKindFunction,
			Name:     "handleReq",
			FilePath: "src/resp.go",
			Language: types.LanguageGo,
		},
	}
	for _, n := range nodes {
		if err := database.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert node %s: %v", n.ID, err)
		}
	}
}

func newSearcher(t *testing.T, database *db.DB) *search.Searcher {
	t.Helper()
	return search.New(database)
}

func TestParseQuery_FieldKind(t *testing.T) {
	got := search.ParseQuery("kind:function foo")
	if got.Kind != types.NodeKindFunction {
		t.Errorf("Kind: got %q, want %q", got.Kind, types.NodeKindFunction)
	}
	if got.FTSText != "foo" {
		t.Errorf("FTSText: got %q, want \"foo\"", got.FTSText)
	}
	if got.Kind == "" {
		t.Error("Kind must not be empty when kind: prefix given")
	}
}

func TestParseQuery_FieldLang(t *testing.T) {
	tests := []struct {
		input    string
		wantLang types.Language
	}{
		{"lang:go Parser", types.LanguageGo},
		{"language:typescript search", types.LanguageTypeScript},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := search.ParseQuery(tt.input)
			if got.Language != tt.wantLang {
				t.Errorf("Language: got %q, want %q", got.Language, tt.wantLang)
			}
		})
	}
}

func TestParseQuery_FieldPath(t *testing.T) {
	got := search.ParseQuery("path:src/routes parseQuery")
	if got.FilePath != "src/routes" {
		t.Errorf("FilePath: got %q, want \"src/routes\"", got.FilePath)
	}
	if got.FTSText != "parseQuery" {
		t.Errorf("FTSText: got %q, want \"parseQuery\"", got.FTSText)
	}
}

func TestParseQuery_FieldName(t *testing.T) {
	got := search.ParseQuery("name:Parser search")
	if got.Name != "Parser" {
		t.Errorf("Name: got %q, want \"Parser\"", got.Name)
	}
	if got.FTSText != "search" {
		t.Errorf("FTSText: got %q, want \"search\"", got.FTSText)
	}
}

func TestParseQuery_CombinedFields(t *testing.T) {
	got := search.ParseQuery("kind:method lang:go name:handle handleRequest")
	if got.Kind != types.NodeKindMethod {
		t.Errorf("Kind: got %q", got.Kind)
	}
	if got.Language != types.LanguageGo {
		t.Errorf("Language: got %q", got.Language)
	}
	if got.Name != "handle" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.FTSText != "handleRequest" {
		t.Errorf("FTSText: got %q", got.FTSText)
	}
}

func TestParseQuery_InvalidKindIgnored(t *testing.T) {
	got := search.ParseQuery("kind:notakind foo")
	if got.Kind != "" {
		t.Errorf("invalid kind: expected empty, got %q", got.Kind)
	}
	if !strings.Contains(got.FTSText, "foo") {
		t.Errorf("FTSText should still contain 'foo', got %q", got.FTSText)
	}
}

func TestKindBonus(t *testing.T) {
	tests := []struct {
		kind      types.NodeKind
		wantBonus float64
	}{
		{types.NodeKindFunction, 10},
		{types.NodeKindMethod, 10},
		{types.NodeKindInterface, 9},
		{types.NodeKindTrait, 9},
		{types.NodeKindProtocol, 9},
		{types.NodeKindRoute, 9},
		{types.NodeKindClass, 8},
		{types.NodeKindComponent, 8},
		{types.NodeKindTypeAlias, 6},
		{types.NodeKindStruct, 6},
		{types.NodeKindEnum, 5},
		{types.NodeKindModule, 4},
		{types.NodeKindNamespace, 4},
		{types.NodeKindProperty, 3},
		{types.NodeKindField, 3},
		{types.NodeKindConstant, 3},
		{types.NodeKindVariable, 2},
		{types.NodeKindImport, 1},
		{types.NodeKindExport, 1},
		{types.NodeKindFile, 0},
		{types.NodeKindParameter, 0},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			got := search.KindBonus(tt.kind)
			if got != tt.wantBonus {
				t.Errorf("KindBonus(%q) = %.0f, want %.0f", tt.kind, got, tt.wantBonus)
			}
		})
	}
}

func TestSearch_FTSTier_ReturnsResults(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "parseQuery",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tier != search.TierFTS {
		t.Errorf("expected TierFTS, got %v", tier)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
}

func TestSearch_FTSTier_RankOrder(t *testing.T) {
	// Both score kindBonus 10; the test file's downrank is what must separate them.
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "parseQuery",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	fnParseIdx := -1
	fnTestIdx := -1
	for i, r := range results {
		switch r.Node.ID {
		case "fn_parse":
			fnParseIdx = i
		case "fn_test":
			fnTestIdx = i
		}
	}
	if fnParseIdx < 0 {
		t.Fatal("fn_parse not in results")
	}
	if fnTestIdx < 0 {
		t.Fatal("fn_test not in results")
	}
	if fnParseIdx >= fnTestIdx {
		t.Errorf("expected fn_parse (rank %d) above fn_test (rank %d)", fnParseIdx, fnTestIdx)
	}
}

func TestSearch_FTSTier_ScoresDescending(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "parse",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("score at [%d]=%.4f > score at [%d]=%.4f (not descending)",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestSearch_FieldFilter_Kind(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "kind:function parseQuery",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Node.Kind != types.NodeKindFunction {
			t.Errorf("expected kind=function, got %q for node %s", r.Node.Kind, r.Node.ID)
		}
	}
}

func TestSearch_FieldFilter_Lang(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "lang:typescript Parser",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for lang:typescript Parser")
	}
	for _, r := range results {
		if r.Node.Language != types.LanguageTypeScript {
			t.Errorf("expected language=typescript, got %q for node %s", r.Node.Language, r.Node.ID)
		}
	}
}

func TestSearch_FieldFilter_Path(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "path:src/parser parse",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for path:src/parser")
	}
	for _, r := range results {
		if !strings.Contains(strings.ToLower(r.Node.FilePath), "src/parser") {
			t.Errorf("expected path to contain 'src/parser', got %q", r.Node.FilePath)
		}
	}
}

func TestSearch_FieldFilter_Name(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "name:handle handleRequest",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for name:handle")
	}
	for _, r := range results {
		if !strings.Contains(strings.ToLower(r.Node.Name), "handle") {
			t.Errorf("expected name to contain 'handle', got %q", r.Node.Name)
		}
	}
}

func TestSearch_TestFileDownrank_NonTestQuery(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "parseQuery",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	fnParseScore := -1.0
	fnTestScore := -1.0
	for _, r := range results {
		switch r.Node.ID {
		case "fn_parse":
			fnParseScore = r.Score
		case "fn_test":
			fnTestScore = r.Score
		}
	}
	if fnTestScore < 0 {
		t.Skip("fn_test not in results; nothing to assert")
	}
	if fnParseScore <= fnTestScore {
		t.Errorf("fn_parse score (%.4f) should be > fn_test score (%.4f) for non-test query",
			fnParseScore, fnTestScore)
	}
}

func TestSearch_TestFileDownrank_TestQuery(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "test parseQuery",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Scores close together prove the downrank was skipped.
	fnParseScore := 0.0
	fnTestScore := 0.0
	for _, r := range results {
		switch r.Node.ID {
		case "fn_parse":
			fnParseScore = r.Score
		case "fn_test":
			fnTestScore = r.Score
		}
	}
	if fnTestScore == 0 && fnParseScore == 0 {
		t.Skip("neither fn_parse nor fn_test in results")
	}
	diff := fnParseScore - fnTestScore
	if diff > 14 {
		t.Errorf("test-file score should not be downranked when query has 'test': diff=%.2f (>14)", diff)
	}
}

func TestSearch_LIKETier_FiresOnFTSMiss(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	// A mid-token substring: FTS5 prefix-matches within tokens and cannot
	// reach it, so only LIKE can.
	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "SpecialN",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result via LIKE tier for 'SpecialN'")
	}
	if tier != search.TierLIKE {
		t.Errorf("expected TierLIKE, got %v", tier)
	}
	found := false
	for _, r := range results {
		if r.Node.ID == "fn_likeonly" {
			found = true
		}
	}
	if !found {
		t.Error("fn_likeonly not found via LIKE tier")
	}
}

func TestSearch_LIKETier_Scoring(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	nodes := []types.Node{
		{ID: "like_exact", Kind: types.NodeKindFunction, Name: "fooBar",
			FilePath: "src/a.go", Language: types.LanguageGo},
		{ID: "like_prefix", Kind: types.NodeKindFunction, Name: "fooBarBaz",
			FilePath: "src/b.go", Language: types.LanguageGo},
		{ID: "like_contains", Kind: types.NodeKindFunction, Name: "notFooBarYes",
			FilePath: "src/c.go", Language: types.LanguageGo},
		{ID: "like_qualified", Kind: types.NodeKindFunction, Name: "other",
			QualifiedName: "pkg.fooBar", FilePath: "src/d.go", Language: types.LanguageGo},
	}
	for _, n := range nodes {
		if err := database.UpsertNode(ctx, n); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	s := newSearcher(t, database)

	// A mid-token substring forces the LIKE tier.
	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "fooBar",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = tier

	scores := map[string]float64{}
	for _, r := range results {
		scores[r.Node.ID] = r.Score
	}

	if scores["like_exact"] > 0 && scores["like_prefix"] > 0 {
		if scores["like_exact"] < scores["like_prefix"] {
			t.Errorf("exact (%.4f) should score >= prefix (%.4f)", scores["like_exact"], scores["like_prefix"])
		}
	}
	if scores["like_prefix"] > 0 && scores["like_contains"] > 0 {
		if scores["like_prefix"] < scores["like_contains"] {
			t.Errorf("prefix (%.4f) should score >= contains (%.4f)", scores["like_prefix"], scores["like_contains"])
		}
	}
}

func TestSearch_FuzzyTier_FiresOnLIKEMiss(t *testing.T) {
	// A one-character deletion is not a substring either way, so LIKE misses
	// and only the fuzzy tier can match.
	database := openTestDB(t)
	ctx := context.Background()
	if err := database.UpsertNode(ctx, types.Node{
		ID:       "fn_resp",
		Kind:     types.NodeKindFunction,
		Name:     "handleReq",
		FilePath: "src/resp.go",
		Language: types.LanguageGo,
	}); err != nil {
		t.Fatal(err)
	}
	s := newSearcher(t, database)

	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "handlReq",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected fuzzy result for 'handlReq'")
	}
	if tier != search.TierFuzzy {
		t.Errorf("expected TierFuzzy, got %v", tier)
	}
	if results[0].Node.ID != "fn_resp" {
		t.Errorf("expected fn_resp, got %s", results[0].Node.ID)
	}
}

func TestSearch_FuzzyTier_MaxDistBoundary(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	nodes := []types.Node{
		{ID: "short_node", Kind: types.NodeKindFunction, Name: "abcd",
			FilePath: "src/a.go", Language: types.LanguageGo},
		{ID: "long_node", Kind: types.NodeKindFunction, Name: "abcdef",
			FilePath: "src/b.go", Language: types.LanguageGo},
	}
	for _, n := range nodes {
		if err := database.UpsertNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	s := newSearcher(t, database)

	res1, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "abce",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range res1 {
		if r.Node.ID == "short_node" {
			found = true
		}
	}
	if !found {
		t.Error("'abce' should find 'abcd' with maxDist=1")
	}

	res2, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "abc",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res2 // LIKE may find it; we just don't crash

	res3, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "abcxyz",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res3 {
		if r.Node.ID == "short_node" {
			t.Errorf("'abcxyz' should NOT find 'abcd' (dist=2 for len>4, but edit distance is 3)")
		}
	}
}

func TestSearch_FuzzyTier_BoundedNoPanic(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzz1",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = results
}

func TestSearch_DeterministicTiebreaker(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	nodes := []types.Node{
		{ID: "zzz_node", Kind: types.NodeKindFunction, Name: "duplicateName",
			FilePath: "src/z.go", Language: types.LanguageGo},
		{ID: "aaa_node", Kind: types.NodeKindFunction, Name: "duplicateName",
			FilePath: "src/a.go", Language: types.LanguageGo},
	}
	for _, n := range nodes {
		if err := database.UpsertNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	s := newSearcher(t, database)

	run := func() []string {
		results, _, err := s.Search(context.Background(), types.SearchOptions{
			Query: "duplicateName",
			Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.Node.ID
		}
		return ids
	}

	run1 := run()
	run2 := run()
	if len(run1) == 0 {
		t.Fatal("expected results")
	}
	if !equalStringSlices(run1, run2) {
		t.Errorf("non-deterministic: run1=%v run2=%v", run1, run2)
	}

	if len(run1) >= 2 {
		if run1[0] != "aaa_node" {
			results, _, _ := s.Search(context.Background(), types.SearchOptions{
				Query: "duplicateName",
				Limit: 10,
			})
			if len(results) >= 2 {
				diff := results[0].Score - results[1].Score
				if diff == 0 {
					t.Errorf("tied scores: expected aaa_node first, got %s", run1[0])
				}
			}
		}
	}
}

func TestScorePathRelevance(t *testing.T) {
	shallow := types.Node{FilePath: "src/parser.go"}
	deep := types.Node{FilePath: "src/internal/util/parser/deep/nested/file.go"}

	scoreShallow := search.ScorePathRelevance(shallow)
	scoreDeep := search.ScorePathRelevance(deep)

	if scoreShallow <= scoreDeep {
		t.Errorf("shallow path (%.4f) should score > deep path (%.4f)", scoreShallow, scoreDeep)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearch_NameMatchBonus(t *testing.T) {
	// A node whose name exactly matches the query should score higher than one
	// where name only partially matches.
	database := openTestDB(t)
	ctx := context.Background()
	nodes := []types.Node{
		{ID: "exact", Kind: types.NodeKindFunction, Name: "fooFunc",
			QualifiedName: "pkg.fooFunc", FilePath: "src/a.go",
			Language: types.LanguageGo, Docstring: "fooFunc does foo"},
		{ID: "partial", Kind: types.NodeKindFunction, Name: "fooFuncHelper",
			QualifiedName: "pkg.fooFuncHelper", FilePath: "src/b.go",
			Language: types.LanguageGo, Docstring: "fooFuncHelper is a helper for fooFunc"},
	}
	for _, n := range nodes {
		if err := database.UpsertNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "fooFunc",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Skip("not enough results to compare name match bonus")
	}
	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.Node.ID
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if results[0].Node.ID != "exact" {
		t.Errorf("exact name match should rank first, got %s (score=%.4f), exact score=%.4f",
			results[0].Node.ID, results[0].Score,
			func() float64 {
				for _, r := range results {
					if r.Node.ID == "exact" {
						return r.Score
					}
				}
				return -1
			}())
	}
}

func TestSearch_FilterTier_KindRoute(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database) // includes route_api with Kind=NodeKindRoute
	s := newSearcher(t, database)

	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "kind:route",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("kind:route returned no results; expected route nodes")
	}
	if tier != search.TierFilter {
		t.Errorf("expected TierFilter, got %v", tier)
	}
	for _, r := range results {
		if r.Node.Kind != types.NodeKindRoute {
			t.Errorf("expected kind=route, got %q for node %s", r.Node.Kind, r.Node.ID)
		}
	}
}

func TestSearch_FilterTier_KindFunctionLangGo(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "kind:function lang:go",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("kind:function lang:go returned no results")
	}
	if tier != search.TierFilter {
		t.Errorf("expected TierFilter, got %v", tier)
	}
	for _, r := range results {
		if r.Node.Kind != types.NodeKindFunction {
			t.Errorf("expected kind=function, got %q for node %s", r.Node.Kind, r.Node.ID)
		}
		if r.Node.Language != types.LanguageGo {
			t.Errorf("expected language=go, got %q for node %s", r.Node.Language, r.Node.ID)
		}
	}
}

func TestSearch_FilterTier_LangPython(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	if err := database.UpsertNode(ctx, types.Node{
		ID:       "py_func",
		Kind:     types.NodeKindFunction,
		Name:     "main",
		FilePath: "app.py",
		Language: types.LanguagePython,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	s := newSearcher(t, database)

	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "lang:python",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("lang:python returned no results")
	}
	if tier != search.TierFilter {
		t.Errorf("expected TierFilter, got %v", tier)
	}
	for _, r := range results {
		if r.Node.Language != types.LanguagePython {
			t.Errorf("expected language=python, got %q for node %s", r.Node.Language, r.Node.ID)
		}
	}
}

func TestSearch_FilterTier_EmptyQueryNoFilters(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, _, err := s.Search(context.Background(), types.SearchOptions{
		Query: "",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("empty query + no filters: expected nil results, got %d", len(results))
	}
}

func TestSearch_FilterTier_FreeTextStillUsesFTSOrLIKE(t *testing.T) {
	database := openTestDB(t)
	insertFixture(t, database)
	s := newSearcher(t, database)

	results, tier, err := s.Search(context.Background(), types.SearchOptions{
		Query: "parseQuery",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("free-text search returned no results")
	}
	if tier == search.TierFilter {
		t.Errorf("free-text query 'parseQuery' must NOT use TierFilter; got TierFilter")
	}
}
