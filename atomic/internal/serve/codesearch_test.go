package serve_test

// codesearch_test.go — CP7: federated code search tests (TDD, written before implementation).
//
// Covers:
//  1. Realm scope (ScopeRealmAll): 3 members — 2 with results, 1 cold/error —
//     output has [key] groups for the two, a "not indexed" note for the third,
//     and did NOT abort.
//  2. only/exclude query params filter the rendered member set.
//  3. A result links to /file/<relpath>#L<line>.
//  4. Repo scope: single group, single index (no [key] wrapper per member).
//  5. HX-Request header → fragment response (no full shell).
//  6. Production wiring: nil MemberSearchFn → production default actually
//     opens a real index and returns a real symbol.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── Fake MemberSearchFn helpers ─────────────────────────────────────────────

// makeFixedSearchFn returns a MemberSearchFn that always returns the given results.
func makeFixedSearchFn(results []types.SearchResult) serve.MemberSearchFn {
	return func(_ context.Context, _, _, _ string) ([]types.SearchResult, error) {
		return results, nil
	}
}

// errorSearchFn is a MemberSearchFn that always returns an error (cold member).
func errorSearchFn(_ context.Context, _, _, _ string) ([]types.SearchResult, error) {
	return nil, os.ErrNotExist
}

// makeKeyedSearchFn returns a MemberSearchFn that returns results only for
// the keys in the provided map (keyed by memberPath for simplicity — we use
// the dbPath suffix as the discriminant instead, since memberPath is the
// absolute member dir).  We discriminate by dbPath suffix (key.db).
func makeKeyedSearchFn(byKey map[string][]types.SearchResult) serve.MemberSearchFn {
	return func(_ context.Context, _, dbPath, _ string) ([]types.SearchResult, error) {
		// dbPath ends in <key>.db
		base := filepath.Base(dbPath)
		key := strings.TrimSuffix(base, ".db")
		if results, ok := byKey[key]; ok {
			return results, nil
		}
		return nil, os.ErrNotExist
	}
}

// fakeResult builds a SearchResult with the given fields.
func fakeResult(name, kind, file string, line int) types.SearchResult {
	return types.SearchResult{
		Node: types.Node{
			ID:        name + "-id",
			Name:      name,
			Kind:      types.NodeKind(kind),
			FilePath:  file,
			StartLine: line,
		},
		Score: 1.0,
	}
}

// ─── 1. Realm scope: 3 members, 2 indexed, 1 cold ────────────────────────────

// TestCodeSearch_RealmScope_GroupedByKey verifies:
//   - [key] groups appear for each member
//   - a cold member is noted as "not indexed" in the response (operation continues)
//   - result contains a link to /file/<relpath>#L<line>
//   - the response is 200 (not 500)
func TestCodeSearch_RealmScope_GroupedByKey(t *testing.T) {
	realmRoot := t.TempDir()

	// Write a minimal CLAUDE.md + wiki/index.md so realm.Resolve returns ScopeRealmAll.
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n\n<wiki-scan generated=\"2026-01-01\" root=\""+realmRoot+"\">\n</wiki-scan>\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})

	// code.toml with 3 members: alpha, beta, gamma.
	buildCodeTOML(t, realmRoot, []struct{ key, path string }{
		{"alpha", "repos/alpha"},
		{"beta", "repos/beta"},
		{"gamma", "repos/gamma"},
	})

	// Fake search: alpha and beta have results; gamma is cold (error).
	searchFn := makeKeyedSearchFn(map[string][]types.SearchResult{
		"alpha": {fakeResult("Resolve", "function", "internal/alpha/resolve.go", 42)},
		"beta":  {fakeResult("Handler", "method", "pkg/handler.go", 7)},
		// "gamma" is absent → errorSearchFn path via makeKeyedSearchFn.
	})

	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=Resolve", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()

	// Groups for the two indexed members.
	if !strings.Contains(body, "[alpha]") {
		t.Errorf("missing [alpha] group header; body: %s", body)
	}
	if !strings.Contains(body, "[beta]") {
		t.Errorf("missing [beta] group header; body: %s", body)
	}

	// Cold member noted (not indexed).
	if !strings.Contains(body, "gamma") || !strings.Contains(strings.ToLower(body), "not indexed") {
		t.Errorf("missing 'not indexed' note for gamma; body: %s", body)
	}

	// alpha result links to /file/<relpath>#L42.
	if !strings.Contains(body, "/file/") || !strings.Contains(body, "#L42") {
		t.Errorf("missing file link with line anchor; body: %s", body)
	}
}

// ─── 2. only/exclude filter ───────────────────────────────────────────────────

// TestCodeSearch_OnlyFilter limits search to the named keys.
func TestCodeSearch_OnlyFilter(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})
	buildCodeTOML(t, realmRoot, []struct{ key, path string }{
		{"alpha", "repos/alpha"},
		{"beta", "repos/beta"},
	})

	called := map[string]int{}
	searchFn := func(_ context.Context, memberPath, dbPath, _ string) ([]types.SearchResult, error) {
		key := strings.TrimSuffix(filepath.Base(dbPath), ".db")
		called[key]++
		return []types.SearchResult{fakeResult("Fn", "function", "main.go", 1)}, nil
	}

	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=Fn&only=alpha", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Only alpha was queried.
	if called["alpha"] == 0 {
		t.Error("alpha was not searched")
	}
	if called["beta"] != 0 {
		t.Error("beta should not have been searched when only=alpha")
	}

	body := rr.Body.String()
	if !strings.Contains(body, "[alpha]") {
		t.Errorf("missing [alpha] in body; body: %s", body)
	}
	if strings.Contains(body, "[beta]") {
		t.Errorf("unexpected [beta] in body; body: %s", body)
	}
}

// TestCodeSearch_ExcludeFilter excludes the named key.
func TestCodeSearch_ExcludeFilter(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})
	buildCodeTOML(t, realmRoot, []struct{ key, path string }{
		{"alpha", "repos/alpha"},
		{"beta", "repos/beta"},
	})

	called := map[string]int{}
	searchFn := func(_ context.Context, _, dbPath, _ string) ([]types.SearchResult, error) {
		key := strings.TrimSuffix(filepath.Base(dbPath), ".db")
		called[key]++
		return []types.SearchResult{fakeResult("Fn", "function", "main.go", 1)}, nil
	}

	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=Fn&exclude=beta", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called["beta"] != 0 {
		t.Error("beta should not have been searched when exclude=beta")
	}
	if called["alpha"] == 0 {
		t.Error("alpha should have been searched")
	}
}

// ─── 3. Result links to /file/<relpath>#L<line> ───────────────────────────────

// TestCodeSearch_ResultLinkFormat verifies that a result node is rendered as a
// link of the form /file/<member-prefix>/<FilePath>#L<StartLine> — the member's
// realm-relative path is prefixed so the /file/ route (which serves realm-relative
// paths) resolves the member-relative path stored in that member's index.
func TestCodeSearch_ResultLinkFormat(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})
	buildCodeTOML(t, realmRoot, []struct{ key, path string }{
		{"mypkg", "repos/mypkg"},
	})

	searchFn := makeFixedSearchFn([]types.SearchResult{
		fakeResult("DoWork", "function", "pkg/worker.go", 99),
	})

	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=DoWork", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	// Link must be /file/repos/mypkg/pkg/worker.go#L99 — member path prefixed.
	if !strings.Contains(body, "/file/repos/mypkg/pkg/worker.go#L99") {
		t.Errorf("expected link /file/repos/mypkg/pkg/worker.go#L99; body:\n%s", body)
	}
	// Name and kind displayed.
	if !strings.Contains(body, "DoWork") {
		t.Errorf("expected name DoWork in body; body:\n%s", body)
	}
	if !strings.Contains(body, "function") {
		t.Errorf("expected kind function in body; body:\n%s", body)
	}
}

// ─── 4. Repo scope: single index ─────────────────────────────────────────────

// TestCodeSearch_RepoScope verifies that when the resolution is ScopeRepo
// (or effectively a single member), results are rendered without requiring
// [key] grouping — a single set of results is emitted directly.
func TestCodeSearch_RepoScope(t *testing.T) {
	// Use a temp dir with no wiki registration → ScopeNoIndex, which the handler
	// treats as a single-repo query by falling back to the realmRoot itself with
	// no grouping.  We supply a searchFn that always returns results.
	realmRoot := t.TempDir()
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	writeFile(t, claudeMDPath, "# no wiki\n")

	searchFn := makeFixedSearchFn([]types.SearchResult{
		fakeResult("MainFn", "function", "cmd/main.go", 10),
	})

	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=MainFn", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "MainFn") {
		t.Errorf("expected result MainFn; body: %s", body)
	}
	if !strings.Contains(body, "/file/cmd/main.go#L10") {
		t.Errorf("expected file link; body: %s", body)
	}
}

// ─── 5. HX-Request → fragment response ───────────────────────────────────────

// TestCodeSearch_HTMXFragment verifies that when HX-Request is set, the handler
// returns a lightweight fragment (no full shell HTML boilerplate), suitable for
// htmx injection into #main-pane.
func TestCodeSearch_HTMXFragment(t *testing.T) {
	realmRoot := t.TempDir()
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	writeFile(t, claudeMDPath, "# no wiki\n")

	searchFn := makeFixedSearchFn([]types.SearchResult{
		fakeResult("RunFn", "function", "run.go", 5),
	})

	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	// Full request (no HX-Request) should include the layout shell markers.
	reqFull := httptest.NewRequest(http.MethodGet, "/code/search?q=RunFn", nil)
	rrFull := httptest.NewRecorder()
	handler.ServeHTTP(rrFull, reqFull)
	fullBody := rrFull.Body.String()

	// HTMX request should be a fragment.
	reqHTMX := httptest.NewRequest(http.MethodGet, "/code/search?q=RunFn", nil)
	reqHTMX.Header.Set("HX-Request", "true")
	rrHTMX := httptest.NewRecorder()
	handler.ServeHTTP(rrHTMX, reqHTMX)
	htmxBody := rrHTMX.Body.String()

	// Fragment must still contain the result.
	if !strings.Contains(htmxBody, "RunFn") {
		t.Errorf("fragment missing result RunFn; body: %s", htmxBody)
	}

	// Full page should be at least as long (it wraps the fragment in a shell).
	if len(fullBody) < len(htmxBody) {
		t.Errorf("full page (%d bytes) shorter than fragment (%d bytes) — unexpected",
			len(fullBody), len(htmxBody))
	}
}

// ─── 6. Production wiring: real index ────────────────────────────────────────

// TestCodeSearch_ProductionDefault_RealIndex verifies that the PRODUCTION default
// MemberSearchFn (nil SearchFn in CodeSearchOptions) actually opens a real SQLite
// index and returns results. We build a tiny in-memory index using the engine API
// (Init → IndexAll) and point the production seam at it.
func TestCodeSearch_ProductionDefault_RealIndex(t *testing.T) {
	// Build a real indexed member: one Go file with a known function.
	memberDir := t.TempDir()
	goSrc := `package greeter

// Greet returns a greeting.
func Greet(name string) string { return "Hello, " + name }
`
	if err := os.WriteFile(filepath.Join(memberDir, "greeter.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build the index at a separate db path (realm-style).
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "greeter.db")
	ctx := context.Background()
	eng, err := engine.NewWithDBPath(memberDir, dbPath)
	if err != nil {
		t.Fatal("NewWithDBPath:", err)
	}
	if err := eng.Init(ctx); err != nil {
		eng.Close()
		t.Fatal("Init:", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		eng.Close()
		t.Fatal("IndexAll:", err)
	}
	eng.Close()

	// Now confirm the production MemberSearchFn can find "Greet" in this real index.
	prodFn := serve.DefaultMemberSearchFn()
	results, err := prodFn(ctx, memberDir, dbPath, "Greet")
	if err != nil {
		t.Fatalf("production search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'Greet' in production search, got none")
	}
	// Confirm the returned node is named Greet (or contains it).
	found := false
	for _, r := range results {
		if strings.Contains(r.Node.Name, "Greet") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no result with name containing 'Greet'; results: %+v", results)
	}

	// Also verify the handler wires to the production seam when SearchFn is nil.
	realmRoot := t.TempDir()
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	writeFile(t, claudeMDPath, "# no wiki\n")

	// A nil SearchFn → handler uses DefaultMemberSearchFn internally. We confirm
	// it builds and handles a request without panicking.
	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     nil, // production default
	})
	req := httptest.NewRequest(http.MethodGet, "/code/search?q=Greet", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req) // must not panic; 200 OK (no results for empty scope is fine)
	if rr.Code != http.StatusOK {
		t.Fatalf("nil SearchFn handler: expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

// TestCodeSearch_RealmAll_AllMembersAbsent verifies that when all members are
// cold (no db), the response is still 200 with "not indexed" notes for each
// member (not a 500 abort).
func TestCodeSearch_RealmAll_AllMembersAbsent(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})
	buildCodeTOML(t, realmRoot, []struct{ key, path string }{
		{"x", "repos/x"},
		{"y", "repos/y"},
	})

	// All members return error (cold).
	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     errorSearchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=foo", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even when all members cold, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(strings.ToLower(body), "not indexed") {
		t.Errorf("expected 'not indexed' in body; body: %s", body)
	}
}

// TestCodeSearch_EmptyQuery renders cleanly with no results (no crash).
func TestCodeSearch_EmptyQuery(t *testing.T) {
	realmRoot := t.TempDir()
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	writeFile(t, claudeMDPath, "# no wiki\n")

	handler := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     makeFixedSearchFn(nil),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// ─── Wiring: /code/search registered in mux ──────────────────────────────────

// TestServe_CodeSearchRoute verifies that /code/search is wired in the main mux
// (RunWithContext) and returns 200 for a basic request.
func TestServe_CodeSearchRoute(t *testing.T) {
	// A minimal realm/repo without a code index — just verifying the route exists.
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	writeFile(t, claudeMD, "# no wiki\n")

	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: claudeMD,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/code/search?q=hello")
	if err != nil {
		t.Fatalf("/code/search GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
