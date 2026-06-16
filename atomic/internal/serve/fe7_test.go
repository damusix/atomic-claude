package serve_test

// fe7_test.go — FE7: nav folder hierarchy, breadcrumb path, link styling, anchor-link fix.
//
// TDD contract (written failing-first before implementation):
//
//  1. Nav folder hierarchy: renderRepoNav emits nested <details> per subdirectory
//     for docs/**/*.md. Files under docs/reference/ appear inside a nested
//     <details><summary>reference</summary>…</details> not as flat basenames.
//
//  2. Breadcrumb hierarchy: the #breadcrumb-page OOB span in the htmx fragment
//     for /page/docs/reference/serve.md contains "docs › reference › serve.md"
//     (path segments joined by " › "), not just the basename "serve.md".
//
//  3. Anchor-only links are NOT external: resolveMarkdownLink returns an Edge
//     with External=false for "#section" targets; External=true for http/https/mailto.
//
//  4. Rail renders anchor-only OUT link WITHOUT target="_blank": an anchor-only
//     outbound edge (#fragment) must render as a plain <a href="#fragment"> in the
//     rail OUT fragment, not as an external link with target="_blank".
//
//  5. Rail renders a real-URL OUT link WITH target="_blank": an https:// outbound
//     edge must render with target="_blank" and the ctx-external class.

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

// buildDocHierarchyRealm creates a repo-scope fixture with nested docs:
//
//	README.md
//	docs/guide.md
//	docs/reference/serve.md
//	docs/reference/api.md
//	docs/spec/thing.md
func buildDocHierarchyRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Readme\n")
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(root, "docs", "reference", "serve.md"), "# Serve\n")
	writeFile(t, filepath.Join(root, "docs", "reference", "api.md"), "# API\n")
	writeFile(t, filepath.Join(root, "docs", "spec", "thing.md"), "# Thing\n")
	return root
}

// buildAnchorRealm creates a realm where one page has an anchor-only link and
// another has a real https link. Used for issue #4 anchor-link tests.
//
//	hub.md → links to #section (anchor-only) and https://example.com (external)
func buildAnchorRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hub.md"),
		"# Hub\n\nSee [section](#section) and [web](https://example.com).\n")
	return root
}

// ─── Issue #2: nav folder hierarchy ──────────────────────────────────────────

// TestNavRepoScopeNestedFolderHierarchy verifies that the repo-scope nav tree
// renders a nested <details><summary>reference</summary>…</details> block for
// docs/reference/*.md instead of flat basenames.
//
// WHY: flat basenames mean docs/reference/serve.md, docs/spec/serve.md, and
// docs/design/serve.md all appear as "serve" with no context.
func TestNavRepoScopeNestedFolderHierarchy(t *testing.T) {
	root := buildDocHierarchyRealm(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:    root,
		IsRealmScope: false,
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// The "reference" folder must appear as a <details> or <summary> node,
	// not just as a bare "serve" or "api" leaf at top level.
	if !strings.Contains(body, ">reference<") && !strings.Contains(body, ">reference</") {
		t.Errorf("nav tree must contain folder label 'reference' as a <summary>; body:\n%s", body)
	}

	// The leaf "serve.md" or "serve" must appear inside a nested block, so the
	// full relpath "docs/reference/serve.md" must be in an hx-get attr.
	if !strings.Contains(body, "docs/reference/serve.md") {
		t.Errorf("nav tree must carry hx-get path docs/reference/serve.md for nested file; body:\n%s", body)
	}

	// "spec" folder must also appear as a folder group.
	if !strings.Contains(body, ">spec<") && !strings.Contains(body, ">spec</") {
		t.Errorf("nav tree must contain folder label 'spec' as a <summary>; body:\n%s", body)
	}

	// Guide (flat in docs/) should appear with its relpath.
	if !strings.Contains(body, "docs/guide.md") {
		t.Errorf("nav tree must carry hx-get path docs/guide.md; body:\n%s", body)
	}
}

// ─── Issue #3: breadcrumb hierarchy ──────────────────────────────────────────

// TestBreadcrumbHierarchyForNestedPage verifies that the htmx fragment for
// /page/docs/reference/serve.md sets the #breadcrumb-page OOB span to
// a " › "-joined path: "docs › reference › serve.md".
//
// WHY: showing only "serve.md" gives no context about where in the tree the
// page lives; multiple pages share the same basename under different folders.
func TestBreadcrumbHierarchyForNestedPage(t *testing.T) {
	root := buildDocHierarchyRealm(t)

	g := serve.BuildLinkGraph(root)
	pageHandler := serve.NewPageHandlerWithGraph(root, g)

	srv := httptest.NewServer(pageHandler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/page/docs/reference/serve.md", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("HX-Request", "true")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /page/docs/reference/serve.md: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The breadcrumb-page OOB span must contain path segments separated by " › ".
	// Exact check: the text between the span tags must be "docs › reference › serve.md"
	// (or encoded equivalent).
	wantSegments := []string{"docs", "reference", "serve.md"}
	for _, seg := range wantSegments {
		if !strings.Contains(html, seg) {
			t.Errorf("breadcrumb OOB missing segment %q; html:\n%s", seg, html)
		}
	}

	// The separator › (or its Unicode form) must appear between segments.
	if !strings.Contains(html, "›") && !strings.Contains(html, "&#x203A;") {
		t.Errorf("breadcrumb OOB must contain › separator between segments; html:\n%s", html)
	}
}

// TestBreadcrumbForTopLevelPage verifies that a top-level page (README.md)
// shows just its filename without separators.
func TestBreadcrumbForTopLevelPage(t *testing.T) {
	root := buildDocHierarchyRealm(t)

	g := serve.BuildLinkGraph(root)
	pageHandler := serve.NewPageHandlerWithGraph(root, g)

	srv := httptest.NewServer(pageHandler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/page/README.md", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("HX-Request", "true")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /page/README.md: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// breadcrumb-page must contain "README.md".
	if !strings.Contains(html, "README.md") {
		t.Errorf("breadcrumb for top-level page must contain 'README.md'; html:\n%s", html)
	}
}

// ─── Issue #4: anchor-link External flag on Edge ─────────────────────────────

// TestResolveMarkdownLinkAnchorOnlyNotExternal verifies that an anchor-only
// link (#section) produces an Edge with External=false (not an external link).
//
// WHY: the rail's final {{else}} branch renders no-ResolvedPath edges as
// ctx-external with target="_blank". Anchor-only links (#section) have no
// ResolvedPath and are not external, so they must not be rendered as external.
func TestResolveMarkdownLinkAnchorOnlyNotExternal(t *testing.T) {
	root := buildAnchorRealm(t)
	g := serve.BuildLinkGraph(root)

	outbound := g.Outbound("hub.md")
	if len(outbound) == 0 {
		t.Fatal("hub.md should have outbound edges")
	}

	var anchorEdge *serve.Edge
	for i := range outbound {
		if outbound[i].Target == "#section" {
			anchorEdge = &outbound[i]
			break
		}
	}
	if anchorEdge == nil {
		t.Fatalf("hub.md outbound missing edge with Target==#section; got: %v", outbound)
	}

	if anchorEdge.External {
		t.Errorf("anchor-only link #section must have External=false, got External=true")
	}
	if anchorEdge.Broken {
		t.Errorf("anchor-only link #section must not be Broken")
	}
}

// TestResolveMarkdownLinkHTTPSIsExternal verifies that an https:// link
// produces an Edge with External=true.
func TestResolveMarkdownLinkHTTPSIsExternal(t *testing.T) {
	root := buildAnchorRealm(t)
	g := serve.BuildLinkGraph(root)

	outbound := g.Outbound("hub.md")

	var extEdge *serve.Edge
	for i := range outbound {
		if strings.HasPrefix(outbound[i].Target, "https://") {
			extEdge = &outbound[i]
			break
		}
	}
	if extEdge == nil {
		t.Fatalf("hub.md outbound missing edge with https:// target; got: %v", outbound)
	}

	if !extEdge.External {
		t.Errorf("https:// link must have External=true, got External=false")
	}
}

// ─── Issue #4: rail renders anchor-only OUT link without target="_blank" ─────

// TestRailAnchorOnlyLinkNotExternal verifies that an anchor-only (#section)
// outbound edge is rendered WITHOUT target="_blank" in the /rail/ response.
//
// WHY: in-page anchor links must not open a new tab.
func TestRailAnchorOnlyLinkNotExternal(t *testing.T) {
	root := buildAnchorRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /rail/hub.md: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/rail/hub.md returned %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The anchor link must render as a plain <a href="#section"> (no target="_blank").
	if !strings.Contains(html, `href="#section"`) {
		t.Errorf("anchor-only link #section must render as <a href=\"#section\">; html:\n%s", html)
	}

	// The combination href="#section" ... target="_blank" must not appear on the same element.
	// Check: no occurrence of target="_blank" within 80 chars after href="#section".
	anchorPos := strings.Index(html, `href="#section"`)
	if anchorPos >= 0 {
		window := html[anchorPos:min(len(html), anchorPos+80)]
		if strings.Contains(window, `target="_blank"`) {
			t.Errorf("anchor-only link #section must not have target=\"_blank\"; context:\n%s", window)
		}
		// Also confirm ctx-anchor class is present, not ctx-external.
		if strings.Contains(window, "ctx-external") {
			t.Errorf("anchor-only link #section must not have ctx-external class; context:\n%s", window)
		}
	}
}

// TestRailExternalLinkHasTargetBlank verifies that an https:// outbound edge
// IS rendered with target="_blank" and the ctx-external class.
func TestRailExternalLinkHasTargetBlank(t *testing.T) {
	root := buildAnchorRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /rail/hub.md: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The https:// link must render with target="_blank".
	if !strings.Contains(html, `target="_blank"`) {
		t.Errorf("https:// external link must render with target=\"_blank\"; html:\n%s", html)
	}

	// The https:// link must carry the ctx-external class.
	if !strings.Contains(html, "ctx-external") {
		t.Errorf("https:// external link must have ctx-external class; html:\n%s", html)
	}
}
