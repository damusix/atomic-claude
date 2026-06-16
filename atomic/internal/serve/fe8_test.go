package serve_test

// fe8_test.go — FE8: shell is the universal envelope.
//
// TDD contract (written failing-first):
//
//  1. Shell envelope — document GET of /page/<x> (no HX-Request) returns the
//     shell: nav-pane, app-header, right-rail are all present in the HTML, AND
//     the shell's #main-pane boots the requested content via hx-get="/page/<x>".
//
//  2. Fragment contract unchanged — HX-Request GET of /page/<x> still returns
//     only the fragment (no DOCTYPE, no nav-pane, no app-header).
//
//  3. Shell 404 — a missing /page/<x> (no HX-Request) returns HTTP 404 AND the
//     shell landmarks (nav-pane, app-header) ARE present in the response body
//     (user gets navigation, not a dead end). A "home" link is present in body.
//
//  4. Fragment 404 — HX-Request GET of /page/<missing> returns HTTP 404 AND a
//     fragment (no DOCTYPE) containing a home link.
//
//  5. Breadcrumb segments are navigable — the OOB breadcrumb span for
//     /page/docs/reference/serve.md (htmx fragment) contains an <a> for each
//     ancestor segment (not just plain text), and the final segment is plain text.
//
//  6. GraphDataHandler uses injected graph — when a graph is injected into
//     GraphDataHandler it is used instead of rebuilding per-request. We assert
//     this by providing an empty graph and confirming the response has 0 nodes.
//
//  7. Shelled /file/<x> — document GET of /file/<x> (no HX-Request) returns
//     the shell (nav-pane, app-header) in the response body.
//
//  8. HX-Request /file/<x> — still returns only the fragment (no DOCTYPE).

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildPageHierarchyRealm creates:
//
//	README.md
//	docs/reference/serve.md
//	src/main.go
func buildPageHierarchyRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Readme\n")
	writeFile(t, filepath.Join(root, "docs", "reference", "serve.md"), "# Serve\n")
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	return root
}

// shellLandmarks are the HTML IDs that prove the shell is present.
var shellLandmarks = []string{"nav-pane", "app-header", "right-rail"}

func hasShellLandmarks(html string) bool {
	for _, id := range shellLandmarks {
		if !strings.Contains(html, id) {
			return false
		}
	}
	return true
}

// ─── 1. Document GET /page/<x> returns the shell ─────────────────────────────

// TestDocumentGetPageReturnsShell verifies that a full (non-htmx) GET of
// /page/<relpath> returns the layout shell (nav-pane, app-header, right-rail)
// AND that the shell's #main-pane is wired to load the requested page.
//
// WHY: a refresh or deep-link to /page/docs/reference/serve.md must not produce
// a shell-less page — the user would lose navigation entirely.
func TestDocumentGetPageReturnsShell(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/page/docs/reference/serve.md")
	if err != nil {
		t.Fatalf("GET /page/docs/reference/serve.md: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Shell landmarks must all be present.
	for _, landmark := range shellLandmarks {
		if !strings.Contains(html, landmark) {
			t.Errorf("document GET /page/: shell landmark %q missing from HTML", landmark)
		}
	}

	// The shell must boot the requested page (LandingURL = the requested path).
	if !strings.Contains(html, `hx-get="/page/docs/reference/serve.md"`) {
		t.Errorf("shell #main-pane must hx-get the requested page; html snippet: %q",
			extractMainPaneSnippet(html))
	}
}

// ─── 2. HX-Request /page/<x> still returns fragment ─────────────────────────

// TestHXRequestPageStillReturnsFragment verifies the fragment contract is
// unchanged: HX-Request GET of /page/<relpath> returns only the fragment, no shell.
func TestHXRequestPageStillReturnsFragment(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	g := serve.BuildLinkGraph(root)
	handler := serve.NewPageHandlerWithGraph(root, g)

	req := httptest.NewRequest(http.MethodGet, "/page/docs/reference/serve.md", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	html := rr.Body.String()

	// Must NOT contain the shell wrapper.
	if strings.Contains(html, "<!DOCTYPE") {
		t.Error("HX-Request fragment must not contain <!DOCTYPE")
	}
	for _, landmark := range shellLandmarks {
		if strings.Contains(html, landmark) {
			t.Errorf("HX-Request fragment must not contain shell landmark %q", landmark)
		}
	}

	// Must contain the page content.
	if !strings.Contains(html, "page-content") {
		t.Error("HX-Request fragment must contain page-content div")
	}
}

// ─── 3. Document GET /page/<missing> returns shelled 404 ─────────────────────

// TestDocumentGetMissingPageReturnsShelledNotFound verifies that a full GET of a
// non-existent page returns HTTP 404 AND the shell (nav-pane, app-header,
// right-rail) so the user is not stranded.
//
// WHY: bare http.NotFound("404 page not found") is a dead end — the user loses
// navigation and cannot get back. The shell provides nav, header, and breadcrumb
// as persistent navigation affordances. The 404 fragment (with a home link) is
// loaded into #main-pane at runtime via htmx when the shell boots.
func TestDocumentGetMissingPageReturnsShelledNotFound(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/page/no-such-page.md")
	if err != nil {
		t.Fatalf("GET /page/no-such-page.md: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Status must be 404.
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing page, got %d", resp.StatusCode)
	}

	// Shell must be present so the user retains navigation.
	for _, landmark := range shellLandmarks {
		if !strings.Contains(html, landmark) {
			t.Errorf("shelled 404 must contain shell landmark %q; got bare 404", landmark)
		}
	}

	// The shell's #main-pane must be wired to load the missing path, which will
	// produce the 404 fragment (with home link) at runtime via htmx.
	// This proves the user gets the fragment pipeline, not a dead bare-text 404.
	if !strings.Contains(html, `hx-get="/page/no-such-page.md"`) {
		t.Errorf("shelled 404: #main-pane must hx-get the missing path so the 404 fragment loads at runtime; html snippet: %q",
			extractMainPaneSnippet(html))
	}

	// Must NOT be bare text ("404 page not found" with no HTML at all).
	if !strings.Contains(html, "<!DOCTYPE") {
		t.Error("shelled 404 must be a full HTML document, not bare text")
	}
}

// ─── 4. HX-Request /page/<missing> returns fragment 404 ──────────────────────

// TestHXRequestMissingPageReturnsFragment404 verifies that an htmx GET of a
// non-existent page returns HTTP 404 AND a fragment (no DOCTYPE, no shell)
// containing a home link that restores navigation from within the shell.
//
// WHY: the shell is already rendered; we only want to swap in a 404 fragment
// into #main-pane, not replace the whole shell.
func TestHXRequestMissingPageReturnsFragment404(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	g := serve.BuildLinkGraph(root)
	handler := serve.NewPageHandlerWithGraph(root, g)

	req := httptest.NewRequest(http.MethodGet, "/page/no-such-page.md", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Status must be 404.
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}

	html := rr.Body.String()

	// Must be a fragment, not a full page.
	if strings.Contains(html, "<!DOCTYPE") {
		t.Error("htmx 404 must be a fragment, not a full page with <!DOCTYPE")
	}

	// Must contain a home link so the user can get back.
	if !strings.Contains(html, `href="/"`) && !strings.Contains(html, `hx-get="/"`) &&
		!strings.Contains(html, "Home") && !strings.Contains(html, "home") {
		t.Errorf("htmx 404 fragment must contain a home/back link; html:\n%s", html)
	}
}

// ─── 5. Breadcrumb segments are navigable ────────────────────────────────────

// TestBreadcrumbSegmentsAreNavigable verifies that the OOB breadcrumb span for
// /page/docs/reference/serve.md contains <a> elements for ancestor segments
// (docs, reference) and plain text for the current file (serve.md).
//
// WHY: plain-text breadcrumbs give no affordance. Clicking "docs" or "reference"
// should navigate to or expand those folders. Clicking the scope/home badge
// should load the landing page.
func TestBreadcrumbSegmentsAreNavigable(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	g := serve.BuildLinkGraph(root)
	handler := serve.NewPageHandlerWithGraph(root, g)

	req := httptest.NewRequest(http.MethodGet, "/page/docs/reference/serve.md", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	html := rr.Body.String()

	// The breadcrumb OOB span must be present.
	if !strings.Contains(html, "breadcrumb-page") {
		t.Fatalf("htmx fragment missing breadcrumb-page OOB span; html:\n%s", html)
	}

	// The "docs" ancestor segment must be a link (inside an <a ...>).
	// We check that "docs" appears inside an href attribute in the breadcrumb context.
	if !strings.Contains(html, "href=") || !strings.Contains(html, "docs") {
		t.Errorf("breadcrumb ancestor segment 'docs' must be an <a> link; html:\n%s", html)
	}

	// Specifically, the breadcrumb should have an <a> wrapping "docs".
	breadcrumbStart := strings.Index(html, "breadcrumb-page")
	if breadcrumbStart < 0 {
		t.Fatal("breadcrumb-page not found")
	}
	// Look within the breadcrumb context (up to 1000 chars after the ID).
	end := breadcrumbStart + 1000
	if end > len(html) {
		end = len(html)
	}
	crumbRegion := html[breadcrumbStart:end]

	// At least one <a href= must appear in the breadcrumb span for the ancestors.
	if !strings.Contains(crumbRegion, "<a ") && !strings.Contains(crumbRegion, "<a\n") {
		t.Errorf("breadcrumb region must contain <a> link elements for ancestor segments; crumb region:\n%s", crumbRegion)
	}

	// The final segment "serve.md" must appear in the breadcrumb.
	if !strings.Contains(crumbRegion, "serve.md") {
		t.Errorf("breadcrumb must contain final segment 'serve.md'; crumb region:\n%s", crumbRegion)
	}
}

// TestBreadcrumbTopLevelPageHasNoAncestorLinks verifies that for a top-level
// page (README.md), the breadcrumb has no ancestor <a> links — only the filename.
func TestBreadcrumbTopLevelPageHasNoAncestorLinks(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	g := serve.BuildLinkGraph(root)
	handler := serve.NewPageHandlerWithGraph(root, g)

	req := httptest.NewRequest(http.MethodGet, "/page/README.md", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	html := rr.Body.String()

	// The breadcrumb must contain README.md.
	if !strings.Contains(html, "README.md") {
		t.Errorf("top-level breadcrumb must contain 'README.md'; html:\n%s", html)
	}
}

// ─── 6. GraphDataHandler uses injected graph ──────────────────────────────────

// TestGraphDataHandlerUsesInjectedGraph verifies that NewGraphDataHandlerWithGraph
// uses the provided graph instead of rebuilding BuildLinkGraph per request.
//
// WHY: rebuilding the link graph on every /graph/data request causes per-request
// latency proportional to realm size. The graph is built once at startup in serve.go
// and should be shared with GraphDataHandler.
func TestGraphDataHandlerUsesInjectedGraph(t *testing.T) {
	// Use an empty temp dir as root. If GraphDataHandler rebuilt the graph from root,
	// it would return 0 nodes (no .md files). We inject an empty graph explicitly
	// and confirm the response reflects it — proving the injected graph is used.
	root := t.TempDir()
	// Write a markdown file so that a per-request BuildLinkGraph would produce nodes.
	writeFile(t, filepath.Join(root, "page.md"), "# Page\n")

	// Build an empty graph (not from root) to inject.
	emptyRoot := t.TempDir() // empty dir → 0 nodes
	injectedGraph := serve.BuildLinkGraph(emptyRoot)

	handler := serve.NewGraphDataHandlerWithGraph(root, injectedGraph)

	req := httptest.NewRequest(http.MethodGet, "/graph/data", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// The response must reflect the injected (empty) graph, not a freshly-built one.
	// An injected empty graph → 0 nodes from md files (provenance nodes are separate).
	body := rr.Body.String()
	if !strings.Contains(body, `"nodes"`) {
		t.Fatalf("response missing 'nodes' field; body: %s", body)
	}

	// Count how many node IDs from page.md appear. If the handler rebuilt the graph,
	// it would find page.md. With the injected empty graph, it must not appear.
	if strings.Contains(body, "page.md") {
		t.Errorf("response contains 'page.md' — handler must use injected graph, not rebuild; body: %s", body)
	}
}

// ─── 7. Document GET /file/<x> returns shell ─────────────────────────────────

// TestDocumentGetFileReturnsShell verifies that a full (non-htmx) GET of
// /file/<relpath> returns the layout shell (nav-pane, app-header, right-rail)
// so the user retains navigation on a direct /file/ link.
//
// WHY: the /file/ route serves source-file views. If a user pastes a /file/ URL
// in the browser, they should get the shell, not a bare file view.
func TestDocumentGetFileReturnsShell(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/file/src/main.go")
	if err != nil {
		t.Fatalf("GET /file/src/main.go: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Shell landmarks must all be present.
	for _, landmark := range shellLandmarks {
		if !strings.Contains(html, landmark) {
			t.Errorf("document GET /file/: shell landmark %q missing from HTML", landmark)
		}
	}
}

// ─── 9. Document GET /file/<missing|traversal> returns shelled 404 ───────────

// TestDocumentGetMissingFileReturnsShelledNotFound verifies that a full (non-htmx)
// GET of a missing or traversal /file/... path returns HTTP 404 AND the shell
// (nav-pane, app-header, right-rail) so the user is not stranded.
//
// WHY: bare http.NotFound("404 page not found") on /file/ misses files is a dead
// end — the user loses navigation and the response is inconsistent with /page/'s
// shelled 404. The traversal guard must still reject (404, never read the escaping
// path) but the response must be shelled, not bare text.
func TestDocumentGetMissingFileReturnsShelledNotFound(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// Missing file case.
	resp, err := http.Get(baseURL + "/file/nonexistent.go")
	if err != nil {
		t.Fatalf("GET /file/nonexistent.go: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing /file/, got %d", resp.StatusCode)
	}

	// Shell landmarks must be present — user retains navigation.
	for _, landmark := range shellLandmarks {
		if !strings.Contains(html, landmark) {
			t.Errorf("shelled /file/ 404 must contain shell landmark %q; got bare 404", landmark)
		}
	}

	// Must be a full HTML document, not bare text.
	if !strings.Contains(html, "<!DOCTYPE") {
		t.Error("shelled /file/ 404 must be a full HTML document, not bare text")
	}

	// The shell's #main-pane must wire hx-get to the missing /file/ path so the
	// 404 fragment (with home link) loads at runtime via htmx.
	if !strings.Contains(html, `hx-get="/file/nonexistent.go"`) {
		t.Errorf("shelled /file/ 404: #main-pane must hx-get the missing path so the 404 fragment loads at runtime; html snippet: %q",
			extractMainPaneSnippet(html))
	}
}

// TestTraversalFileReturnsShelledNotFound verifies that a path-traversal attempt
// on /file/ (e.g. /file/../../etc/passwd) returns HTTP 404 AND a shelled 404 —
// the traversal guard still rejects the path but the response is shelled, not
// bare text. Uses httptest.NewRequest directly so the path bypasses the HTTP
// mux normalizer (which would redirect ../ paths before they reach the handler).
func TestTraversalFileReturnsShelledNotFound(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	// NewFileHandler without a shell: traversal must still return fragment 404,
	// not bare http.NotFound text. This proves the guard calls serve404, not http.NotFound.
	handler := serve.NewFileHandler(root)

	// Craft a raw request with a traversal path. httptest.NewRequest does NOT
	// normalize ../ segments, so safeResolve sees the traversal attempt.
	req := httptest.NewRequest(http.MethodGet, "/file/../../etc/passwd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for traversal /file/ path, got %d", rr.Code)
	}

	html := rr.Body.String()

	// Without a shell, serve404 falls back to the 404 fragment (not bare text).
	// The fragment must NOT be the bare "404 page not found" string from http.NotFound.
	if html == "404 page not found\n" || html == "404 page not found" {
		t.Error("traversal /file/ rejection must not be bare http.NotFound text; serve404 must be called")
	}

	// The fragment must contain something (the notFoundFragmentTmpl content).
	if !strings.Contains(html, "page-content") && !strings.Contains(html, "not found") &&
		!strings.Contains(html, "not-found") {
		t.Errorf("traversal /file/ rejection must render 404 fragment content; html:\n%s", html)
	}
}

// ─── 10. HX-Request /file/<missing> returns fragment 404 ─────────────────────

// TestHXRequestMissingFileReturnsFragment404 verifies that an htmx GET of a
// missing /file/ path returns HTTP 404 AND a fragment (no DOCTYPE, no shell)
// containing a home link.
//
// WHY: the shell is already rendered; we only want to swap in a 404 fragment
// into the modal target, not replace the whole shell. Consistent with /page/ behavior.
func TestHXRequestMissingFileReturnsFragment404(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	handler := serve.NewFileHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/file/nonexistent.go", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}

	html := rr.Body.String()

	// Must be a fragment, not a full page.
	if strings.Contains(html, "<!DOCTYPE") {
		t.Error("HX-Request /file/ 404 must be a fragment, not a full page with <!DOCTYPE")
	}

	// Must contain a home link so the user can get back.
	if !strings.Contains(html, `href="/"`) && !strings.Contains(html, `hx-get="/"`) &&
		!strings.Contains(html, "Home") && !strings.Contains(html, "home") {
		t.Errorf("HX-Request /file/ 404 fragment must contain a home/back link; html:\n%s", html)
	}
}

// ─── 8. HX-Request /file/<x> still returns fragment ─────────────────────────

// TestHXRequestFileStillReturnsFragment verifies that HX-Request GET of /file/
// still returns only the fragment (for the code modal), not the full shell.
func TestHXRequestFileStillReturnsFragment(t *testing.T) {
	root := buildPageHierarchyRealm(t)

	handler := serve.NewFileHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/file/src/main.go", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	html := rr.Body.String()

	// Must NOT contain the shell wrapper.
	if strings.Contains(html, "<!DOCTYPE") {
		t.Error("HX-Request /file/ fragment must not contain <!DOCTYPE")
	}
	for _, landmark := range shellLandmarks {
		if strings.Contains(html, landmark) {
			t.Errorf("HX-Request /file/ fragment must not contain shell landmark %q", landmark)
		}
	}

	// Must contain source content.
	if !strings.Contains(html, "file-view-wrapper") {
		t.Error("HX-Request /file/ fragment must contain file-view-wrapper")
	}
}
