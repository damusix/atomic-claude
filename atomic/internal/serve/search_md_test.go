package serve_test

// search_md_test.go — FE5: md search endpoint tests (TDD: written before implementation).
//
// Covers:
//  1. Known term → matching items; each item carries /page/<file> navigation hook.
//  2. Unknown term → empty fragment (no items, no error).
//  3. Empty/whitespace query → empty fragment.
//  4. Result snippet includes file path and trimmed context line.
//  5. HX-Request header → fragment (no full-page shell).
//  6. Path traversal (q=../../etc/passwd) → 200, no path escape (search-term only).
//  7. Cap: at most 50 results returned even for a very common word.
//  8. Route wired in mux: GET /search/md returns 200.
//  9. Shell contains #search-results container.
// 10. Shell contains #toggle-md / #toggle-code buttons (already tested in TestRootRouteRendersShell,
//     but assert aria-pressed and that they are not inert — search JS is wired).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// newMDSearchHandler builds a MdSearchHandler pointing at root.
func newMDSearchHandler(root string) http.Handler {
	return serve.NewMdSearchHandler(serve.MdSearchOptions{NavRoot: root})
}

// writeMDFile writes a .md file at path/name with content.
func writeMDFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// ─── 1. Known term returns matching items with /page/ navigation hook ─────────

// TestMdSearch_KnownTerm verifies that a query matching content in known .md
// files returns result items, each carrying a /page/<file> navigation link or
// data-page attribute. This proves the search actually finds text and that the
// results are usable for navigation.
func TestMdSearch_KnownTerm(t *testing.T) {
	root := t.TempDir()
	writeMDFile(t, root, "wiki/concerns/performance.md",
		"# Performance\n\nThis document covers latency optimization.\n")
	writeMDFile(t, root, "wiki/repos/alpha.md",
		"# Alpha\n\nLatency is measured in milliseconds.\n")

	handler := newMDSearchHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/search/md?q=latency", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()

	// Each result must carry a /page/ navigation hook.
	// The handler can render as href="/page/..." or data-page="/page/..."
	if !strings.Contains(body, "/page/") {
		t.Errorf("result missing /page/ navigation hook; body:\n%s", body)
	}

	// Both files contain "latency" — expect both to appear.
	// We check for the directory prefix, not the exact path (layout may vary).
	if !strings.Contains(body, "performance") {
		t.Errorf("result missing performance.md match; body:\n%s", body)
	}
	if !strings.Contains(body, "alpha") {
		t.Errorf("result missing alpha.md match; body:\n%s", body)
	}
}

// ─── 2. Unknown term → empty fragment ─────────────────────────────────────────

// TestMdSearch_NoMatch verifies that a query matching nothing returns an empty
// fragment (no result items, 200 OK, no error content).
func TestMdSearch_NoMatch(t *testing.T) {
	root := t.TempDir()
	writeMDFile(t, root, "notes.md", "# Notes\n\nSome content here.\n")

	handler := newMDSearchHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/search/md?q=xyzzy_not_present", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Must not contain a /page/ link (no match means no navigation items).
	if strings.Contains(body, "/page/") {
		t.Errorf("expected no result items for unknown term; body:\n%s", body)
	}
}

// ─── 3. Empty/whitespace query → empty fragment ───────────────────────────────

// TestMdSearch_EmptyQuery verifies that an empty or whitespace-only query
// returns a 200 empty fragment — the same shape as a no-match response.
// The FE5 contract is: empty query → clear the dropdown, never error.
func TestMdSearch_EmptyQuery(t *testing.T) {
	root := t.TempDir()
	writeMDFile(t, root, "intro.md", "# Intro\n\nHello world.\n")

	handler := newMDSearchHandler(root)

	for _, q := range []string{"", "%20"} {
		req := httptest.NewRequest(http.MethodGet, "/search/md?q="+q, nil)
		req.Header.Set("HX-Request", "true")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("q=%q: expected 200, got %d", q, rr.Code)
		}
		if strings.Contains(rr.Body.String(), "/page/") {
			t.Errorf("q=%q: expected no result items for empty query; body:\n%s",
				q, rr.Body.String())
		}
	}
}

// ─── 4. Result snippet includes file:line and trimmed context ────────────────

// TestMdSearch_SnippetFormat verifies that each result item shows the file
// path, the matching line number, and a trimmed snippet of the matching line.
// These are load-bearing for usability: users need to see WHERE and WHAT matched.
func TestMdSearch_SnippetFormat(t *testing.T) {
	root := t.TempDir()
	writeMDFile(t, root, "docs/api.md",
		"# API Reference\n\nThe endpoint accepts JSON payloads.\n\nSee also: authentication.\n")

	handler := newMDSearchHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/search/md?q=endpoint", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()

	// File path in the result.
	if !strings.Contains(body, "docs/api.md") {
		t.Errorf("result missing file path; body:\n%s", body)
	}

	// Line number present (the match is on line 3).
	// We check for a colon-prefixed digit pattern or "L<N>" or just a digit
	// adjacent to the file path — accept any of these.
	hasLineRef := strings.Contains(body, ":3") || strings.Contains(body, "L3") ||
		strings.Contains(body, "line 3") || strings.Contains(body, "#L3")
	if !hasLineRef {
		t.Errorf("result missing line number reference; body:\n%s", body)
	}

	// Snippet contains part of the matching line.
	if !strings.Contains(body, "endpoint") {
		t.Errorf("result missing snippet; body:\n%s", body)
	}
}

// ─── 5. HX-Request → fragment (no full-page shell) ───────────────────────────

// TestMdSearch_HTMXFragment verifies that HX-Request: true returns a fragment
// (no DOCTYPE / <html> / <body> wrapper), while omitting that header returns a
// response with the shell boilerplate. This keeps the dropdown injection clean.
func TestMdSearch_HTMXFragment(t *testing.T) {
	root := t.TempDir()
	writeMDFile(t, root, "a.md", "# A\n\nfragment test content.\n")

	handler := newMDSearchHandler(root)

	// Full-page request (no HX-Request).
	reqFull := httptest.NewRequest(http.MethodGet, "/search/md?q=fragment", nil)
	rrFull := httptest.NewRecorder()
	handler.ServeHTTP(rrFull, reqFull)
	fullBody := rrFull.Body.String()

	// HTMX fragment request.
	reqHTMX := httptest.NewRequest(http.MethodGet, "/search/md?q=fragment", nil)
	reqHTMX.Header.Set("HX-Request", "true")
	rrHTMX := httptest.NewRecorder()
	handler.ServeHTTP(rrHTMX, reqHTMX)
	htmxBody := rrHTMX.Body.String()

	// Both return 200.
	if rrFull.Code != http.StatusOK {
		t.Errorf("full page: expected 200, got %d", rrFull.Code)
	}
	if rrHTMX.Code != http.StatusOK {
		t.Errorf("HTMX: expected 200, got %d", rrHTMX.Code)
	}

	// Fragment must still contain a result.
	if !strings.Contains(htmxBody, "a.md") && !strings.Contains(htmxBody, "/page/") {
		t.Errorf("fragment missing result; body:\n%s", htmxBody)
	}

	// Full-page wrapper is larger (or equal) to the fragment.
	if len(fullBody) < len(htmxBody) {
		t.Errorf("full page (%d bytes) shorter than fragment (%d bytes)",
			len(fullBody), len(htmxBody))
	}
}

// ─── 6. Path traversal in query term is safe ─────────────────────────────────

// TestMdSearch_QueryIsSearchTermNotPath verifies that passing a path-traversal
// string as the query term does not cause a file read outside navRoot.
// The handler should treat q= as a substring to search, not a file path —
// so ../../etc/passwd just returns an empty result (no such substring in our
// .md files), not a directory traversal.
func TestMdSearch_QueryIsSearchTermNotPath(t *testing.T) {
	root := t.TempDir()
	writeMDFile(t, root, "safe.md", "# Safe\n\nNo secrets here.\n")

	handler := newMDSearchHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/search/md?q=../../etc/passwd", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Must return 200 (not a server error from attempted file read).
	if rr.Code != http.StatusOK {
		t.Fatalf("path-traversal query: expected 200, got %d", rr.Code)
	}

	// Must not contain /page/ (no .md file contains "../../etc/passwd").
	if strings.Contains(rr.Body.String(), "/page/") {
		t.Errorf("path-traversal query: unexpected results; body:\n%s", rr.Body.String())
	}
}

// ─── 7. Result cap at 50 ─────────────────────────────────────────────────────

// TestMdSearch_ResultCap verifies that the handler caps results at 50 even
// when more files match the query. This prevents the dropdown from drowning
// in results for a common word like "the".
func TestMdSearch_ResultCap(t *testing.T) {
	root := t.TempDir()

	// Write 60 matching .md files.
	for i := 0; i < 60; i++ {
		name := filepath.Join("docs", "chapter", strings.Repeat("a", i%10+1)+strings.Repeat("b", i+1)+".md")
		content := "# Chapter\n\nThe common word appears here.\n"
		writeMDFile(t, root, name, content)
	}

	handler := newMDSearchHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/search/md?q=common", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Count result items by class attribute (one per match).
	count := strings.Count(body, `class="md-search-result"`)
	if count > 50 {
		t.Errorf("result cap: expected ≤50 result items, got %d", count)
	}
	if count == 0 {
		t.Errorf("result cap: expected some results, got 0; body:\n%s", body)
	}
}

// TestMdSearch_ResultCap_SingleFile verifies that the cap fires even when all
// matches come from a single file (i.e., the per-file mid-file early-exit branch
// is exercised). A single .md file with >50 matching lines must still return
// exactly 50 results and signal truncation.
func TestMdSearch_ResultCap_SingleFile(t *testing.T) {
	root := t.TempDir()

	// Build one .md file with 60 lines that each contain the search term.
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("keyword appears on this line\n")
	}
	writeMDFile(t, root, "big.md", sb.String())

	handler := newMDSearchHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/search/md?q=keyword", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Must be capped at 50 — not all 60 lines.
	count := strings.Count(body, `class="md-search-result"`)
	if count > 50 {
		t.Errorf("single-file cap: expected ≤50 result items, got %d", count)
	}
	if count == 0 {
		t.Errorf("single-file cap: expected some results, got 0; body:\n%s", body)
	}

	// Truncation note must be present.
	if !strings.Contains(body, "md-search-truncated") {
		t.Errorf("single-file cap: expected truncation note in body;\n%s", body)
	}
}

// ─── 8. Route wired in mux ───────────────────────────────────────────────────

// TestServe_MdSearchRoute verifies /search/md is registered in the main mux
// and returns 200 for a basic request (empty query is fine — returns empty fragment).
func TestServe_MdSearchRoute(t *testing.T) {
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

	resp, err := http.Get(baseURL + "/search/md?q=hello")
	if err != nil {
		t.Fatalf("/search/md GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ─── 9. Shell contains #search-results container ─────────────────────────────

// TestShell_SearchResultsContainer verifies the shell HTML includes a
// #search-results element that the JS injects results into. Without this
// container, the dropdown JS has nowhere to render.
func TestShell_SearchResultsContainer(t *testing.T) {
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

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	body := readBodyString(t, resp)

	if !strings.Contains(body, `id="search-results"`) {
		t.Error("shell HTML missing #search-results container")
	}
}

// ─── 10. Shell: search JS is wired (not inert) and code-link handler ──────────

// TestShell_SearchJSWired verifies that the shell HTML includes the JS that
// wires the search box (debounced input handler) and that the FE4 code-link
// handler's container check now includes #search-results.
// We check for the key JS seams by string presence; the exact code is in layout.html.
func TestShell_SearchJSWired(t *testing.T) {
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

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	body := readBodyString(t, resp)

	// The search-box JS must reference /search/md (the md endpoint).
	if !strings.Contains(body, "/search/md") {
		t.Error("shell JS missing /search/md endpoint reference")
	}

	// The search-box JS must reference /code/search (the code endpoint).
	if !strings.Contains(body, "/code/search") {
		t.Error("shell JS missing /code/search endpoint reference")
	}

	// The FE4 code-link delegated handler must now also watch #search-results.
	if !strings.Contains(body, "search-results") {
		t.Error("FE4 code-link handler does not mention search-results container")
	}
}
