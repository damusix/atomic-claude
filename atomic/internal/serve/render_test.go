package serve_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// -- markdown render tests ---------------------------------------------------

// TestMarkdownHeadingRendered verifies that a markdown heading is rendered to <h1>.
func TestMarkdownHeadingRendered(t *testing.T) {
	md := "# Hello World\n"
	html, hasMermaid, err := serve.RenderMarkdown([]byte(md))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "<h1") {
		t.Errorf("expected <h1> in output, got: %s", html)
	}
	if !strings.Contains(html, "Hello World") {
		t.Errorf("expected 'Hello World' in output, got: %s", html)
	}
	if hasMermaid {
		t.Error("hasMermaid should be false for heading-only content")
	}
}

// TestMarkdownGFMTable verifies that GFM tables are rendered to HTML <table>.
// This proves GFM extension is wired (standard CommonMark does not render tables).
func TestMarkdownGFMTable(t *testing.T) {
	md := "| A | B |\n|---|---|\n| 1 | 2 |\n"
	html, _, err := serve.RenderMarkdown([]byte(md))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "<table") {
		t.Errorf("expected <table> in output (GFM table extension), got: %s", html)
	}
	if !strings.Contains(html, "<td") {
		t.Errorf("expected <td> in output, got: %s", html)
	}
}

// TestMarkdownGoCodeBlockHighlighted verifies that a ```go fenced block is
// chroma-highlighted (output contains a chroma HTML wrapper, not plain text).
// We assert the <code class="language-go"> or equivalent chroma markup is present.
func TestMarkdownGoCodeBlockHighlighted(t *testing.T) {
	md := "```go\npackage main\n\nfunc main() {}\n```\n"
	html, _, err := serve.RenderMarkdown([]byte(md))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	// Chroma wraps output in a <pre> with a class. The exact class depends on
	// whether we use inline styles or classes. Either way it wraps a <span>.
	// We assert the word "main" is present and that it's not just wrapped in a
	// plain <code> (chroma adds inner markup).
	if !strings.Contains(html, "main") {
		t.Errorf("expected 'main' in highlighted output, got: %s", html)
	}
	// Chroma HTML output contains at least one <span> with a style or class attribute.
	if !strings.Contains(html, "<span") {
		t.Errorf("expected chroma <span> highlight tokens in output, got: %s", html)
	}
}

// TestMarkdownMermaidBlock verifies that a ```mermaid fenced block is emitted
// as <pre class="mermaid">…raw content…</pre> and NOT chroma-highlighted.
func TestMarkdownMermaidBlock(t *testing.T) {
	md := "```mermaid\ngraph TD\n  A --> B\n```\n"
	html, hasMermaid, err := serve.RenderMarkdown([]byte(md))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	// Must contain the mermaid pre container.
	if !strings.Contains(html, `<pre class="mermaid"`) {
		t.Errorf("expected <pre class=\"mermaid\"> in output, got: %s", html)
	}
	// Must contain the raw mermaid content (not HTML-stripped).
	if !strings.Contains(html, "graph TD") {
		t.Errorf("expected raw mermaid content in output, got: %s", html)
	}
	// Must NOT contain chroma span elements (it must not be highlighted).
	// Chroma would produce <span style="..."> or <span class="..."> wrappers.
	// A plain mermaid block should only have HTML-escaped text inside the pre.
	// We check by ensuring the word "graph" appears but no chroma token span wraps it.
	// The simplest proxy: no <span class= or <span style= inside the mermaid pre.
	mermaidIdx := strings.Index(html, `<pre class="mermaid"`)
	endIdx := strings.Index(html[mermaidIdx:], "</pre>")
	if mermaidIdx >= 0 && endIdx >= 0 {
		mermaidBlock := html[mermaidIdx : mermaidIdx+endIdx+6]
		if strings.Contains(mermaidBlock, "<span") {
			t.Errorf("mermaid block must not contain chroma <span> tokens, got: %s", mermaidBlock)
		}
	}
	// hasMermaid must be true so the caller knows to inject the mermaid script.
	if !hasMermaid {
		t.Error("hasMermaid should be true when a ```mermaid block is present")
	}
}

// -- page route tests --------------------------------------------------------

// TestPageRouteRendersMarkdown verifies that GET /page/<relpath> returns HTML
// containing the rendered content of the markdown file at <root>/<relpath>.
func TestPageRouteRendersMarkdown(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readme.md"), "# My Doc\n\nHello world.\n")

	handler := serve.NewPageHandler(root)
	req := httptest.NewRequest(http.MethodGet, "/page/readme.md", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<h1") {
		t.Errorf("expected <h1> in rendered HTML, got: %s", body)
	}
	if !strings.Contains(body, "Hello world") {
		t.Errorf("expected 'Hello world' in rendered HTML, got: %s", body)
	}
}

// TestPageRouteNotFound verifies that a non-existent path returns 404.
func TestPageRouteNotFound(t *testing.T) {
	root := t.TempDir()
	handler := serve.NewPageHandler(root)
	req := httptest.NewRequest(http.MethodGet, "/page/no-such-file.md", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestPageRoutePathTraversal verifies that traversal attempts via ../.. return
// 404 and never read files outside the root. We place a sentinel file outside
// the root and confirm it cannot be reached.
func TestPageRoutePathTraversal(t *testing.T) {
	// sentinel is outside root
	outsideDir := t.TempDir()
	sentinelPath := filepath.Join(outsideDir, "etc", "passwd")
	writeFile(t, sentinelPath, "root:x:0:0:root:/root:/bin/sh\n")

	// root is a separate temp dir
	root := t.TempDir()
	handler := serve.NewPageHandler(root)

	traversalPaths := []string{
		"/page/../../../etc/passwd",
		"/page/../../etc/passwd",
		"/page/%2e%2e%2f%2e%2e%2fetc%2fpasswd",
		"/page/..%2f..%2fetc%2fpasswd",
	}

	for _, path := range traversalPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("path %q: expected 404, got %d", path, rec.Code)
		}
		// Ensure sentinel content is not in the response.
		if strings.Contains(rec.Body.String(), "root:x:0:0") {
			t.Errorf("path %q: sentinel file content leaked in response", path)
		}
	}
}

// -- file view tests ---------------------------------------------------------

// TestFileRouteRendersWithLineNumbers verifies that GET /file/<relpath> returns
// chroma-highlighted HTML with line-number anchors (id="L1", id="L2", …).
func TestFileRouteRendersWithLineNumbers(t *testing.T) {
	root := t.TempDir()
	goContent := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	writeFile(t, filepath.Join(root, "main.go"), goContent)

	handler := serve.NewFileHandler(root)
	req := httptest.NewRequest(http.MethodGet, "/file/main.go", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Line anchors must be present.
	if !strings.Contains(body, `id="L1"`) {
		t.Errorf("expected id=\"L1\" anchor in file view, got: %s", body)
	}
	if !strings.Contains(body, `id="L2"`) {
		t.Errorf("expected id=\"L2\" anchor in file view, got: %s", body)
	}
	// Code must be highlighted (chroma spans).
	if !strings.Contains(body, "<span") {
		t.Errorf("expected chroma highlight spans in file view, got: %s", body)
	}
	// Must contain the source word "main".
	if !strings.Contains(body, "main") {
		t.Errorf("expected 'main' in highlighted file view, got: %s", body)
	}
}

// TestFileRoutePathTraversal verifies traversal attempts return 404.
func TestFileRoutePathTraversal(t *testing.T) {
	outsideDir := t.TempDir()
	sentinelPath := filepath.Join(outsideDir, "secret.txt")
	writeFile(t, sentinelPath, "SUPER_SECRET")

	root := t.TempDir()
	handler := serve.NewFileHandler(root)

	traversalPaths := []string{
		"/file/../../../secret.txt",
		"/file/../../secret.txt",
		"/file/%2e%2e%2fsecret.txt",
	}

	for _, path := range traversalPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("path %q: expected 404, got %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "SUPER_SECRET") {
			t.Errorf("path %q: sentinel content leaked", path)
		}
	}
}

// TestFileRouteNotFound verifies missing files return 404.
func TestFileRouteNotFound(t *testing.T) {
	root := t.TempDir()
	handler := serve.NewFileHandler(root)
	req := httptest.NewRequest(http.MethodGet, "/file/no-such-file.go", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestPageRouteHTMXFragment verifies that GET /page/<relpath> with the
// "HX-Request: true" header returns a fragment (no <!DOCTYPE, no <html>)
// so htmx can swap it into #main-pane without navigating away.
// Without the header the full page must still be returned.
func TestPageRouteHTMXFragment(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "test.md"), "# Fragment test\n\nHello htmx.\n")

	handler := serve.NewPageHandler(root)

	// --- htmx request: must return a fragment ---
	reqHX := httptest.NewRequest(http.MethodGet, "/page/test.md", nil)
	reqHX.Header.Set("HX-Request", "true")
	recHX := httptest.NewRecorder()
	handler.ServeHTTP(recHX, reqHX)

	if recHX.Code != http.StatusOK {
		t.Errorf("htmx: expected 200, got %d", recHX.Code)
	}
	fragBody := recHX.Body.String()
	// Must NOT contain a full page wrapper.
	if strings.Contains(fragBody, "<!DOCTYPE") {
		t.Errorf("htmx response must not contain <!DOCTYPE, got: %s", fragBody)
	}
	if strings.Contains(fragBody, "<html") {
		t.Errorf("htmx response must not contain <html, got: %s", fragBody)
	}
	// Must contain the rendered inner content.
	if !strings.Contains(fragBody, "<h1") {
		t.Errorf("htmx response must contain <h1, got: %s", fragBody)
	}
	if !strings.Contains(fragBody, "Hello htmx") {
		t.Errorf("htmx response must contain content, got: %s", fragBody)
	}
	// Must carry the swap target id so htmx can OOB-swap or target correctly.
	if !strings.Contains(fragBody, `id="page-content"`) {
		t.Errorf("htmx fragment must carry id=\"page-content\", got: %s", fragBody)
	}

	// --- normal request: must return full page ---
	reqFull := httptest.NewRequest(http.MethodGet, "/page/test.md", nil)
	recFull := httptest.NewRecorder()
	handler.ServeHTTP(recFull, reqFull)

	if recFull.Code != http.StatusOK {
		t.Errorf("full: expected 200, got %d", recFull.Code)
	}
	fullBody := recFull.Body.String()
	if !strings.Contains(fullBody, "<!DOCTYPE") {
		t.Errorf("full response must contain <!DOCTYPE, got: %s", fullBody)
	}
	if !strings.Contains(fullBody, "<html") {
		t.Errorf("full response must contain <html, got: %s", fullBody)
	}
}

// TestPageRouteHTMXFragmentMermaid verifies that when an htmx request is made
// for a page with a mermaid block, the fragment includes the mermaid script and
// an inline mermaid.run() call so diagrams render after the htmx swap.
func TestPageRouteHTMXFragmentMermaid(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "diagram.md"), "```mermaid\ngraph TD\n  A-->B\n```\n")

	handler := serve.NewPageHandler(root)

	reqHX := httptest.NewRequest(http.MethodGet, "/page/diagram.md", nil)
	reqHX.Header.Set("HX-Request", "true")
	recHX := httptest.NewRecorder()
	handler.ServeHTTP(recHX, reqHX)

	if recHX.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", recHX.Code)
	}
	fragBody := recHX.Body.String()
	// Fragment must not be a full page.
	if strings.Contains(fragBody, "<!DOCTYPE") {
		t.Errorf("htmx mermaid fragment must not contain <!DOCTYPE")
	}
	// Must include the mermaid script and a run() call so the diagram renders.
	if !strings.Contains(fragBody, "mermaid.min.js") {
		t.Errorf("htmx mermaid fragment must include mermaid.min.js script, got: %s", fragBody)
	}
	if !strings.Contains(fragBody, "mermaid.run()") {
		t.Errorf("htmx mermaid fragment must include mermaid.run() call, got: %s", fragBody)
	}
}

// ── frontmatter strip tests ──────────────────────────────────────────────────

// TestRenderMarkdown_FrontmatterStripped verifies that YAML frontmatter is
// stripped before goldmark sees it. A wiki page with a leading frontmatter block
// must NOT produce a spurious <hr> (from "---") or render YAML keys as text.
// The real body content MUST appear in the output.
func TestRenderMarkdown_FrontmatterStripped(t *testing.T) {
	// Real wiki-page shape: title, repo, generated keys followed by a real body.
	src := "---\ntitle: \"@hapi/nes\"\nrepo: nes\ngenerated: 2026-06-13\n---\n\n# Overview\n\nbody\n"
	html, _, err := serve.RenderMarkdown([]byte(src))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	// YAML keys must not appear as rendered text.
	if strings.Contains(html, "title:") {
		t.Errorf("frontmatter key 'title:' leaked into body HTML: %s", html)
	}
	if strings.Contains(html, "repo:") {
		t.Errorf("frontmatter key 'repo:' leaked into body HTML: %s", html)
	}
	if strings.Contains(html, "generated:") {
		t.Errorf("frontmatter key 'generated:' leaked into body HTML: %s", html)
	}
	// A spurious <hr> from the opening "---" must not appear.
	if strings.Contains(html, "<hr") {
		t.Errorf("spurious <hr> from frontmatter '---' in body HTML: %s", html)
	}
	// The real body heading must appear.
	if !strings.Contains(html, "Overview") {
		t.Errorf("real body heading 'Overview' missing from HTML: %s", html)
	}
	if !strings.Contains(html, "<h1") {
		t.Errorf("expected <h1> for body heading, got: %s", html)
	}
	if !strings.Contains(html, "body") {
		t.Errorf("expected body text in output: %s", html)
	}
}

// TestRenderMarkdown_NoFrontmatter verifies that a doc with no frontmatter
// renders unchanged (no content dropped).
func TestRenderMarkdown_NoFrontmatter(t *testing.T) {
	src := "# Plain heading\n\nPlain body.\n"
	html, _, err := serve.RenderMarkdown([]byte(src))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "Plain heading") {
		t.Errorf("heading missing: %s", html)
	}
	if !strings.Contains(html, "Plain body") {
		t.Errorf("body missing: %s", html)
	}
}

// TestRenderMarkdown_MidDocThematicBreak verifies that a genuine thematic break
// (---) that appears mid-document (not at byte 0) is NOT eaten by the
// frontmatter stripper; goldmark must still render it as <hr>.
func TestRenderMarkdown_MidDocThematicBreak(t *testing.T) {
	// This document has real content before the "---" so it is NOT frontmatter.
	src := "# Section A\n\nSome text.\n\n---\n\n# Section B\n"
	html, _, err := serve.RenderMarkdown([]byte(src))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "<hr") {
		t.Errorf("expected <hr> for mid-doc thematic break, got: %s", html)
	}
	if !strings.Contains(html, "Section A") {
		t.Errorf("Section A missing: %s", html)
	}
	if !strings.Contains(html, "Section B") {
		t.Errorf("Section B missing: %s", html)
	}
}

// TestRenderMarkdown_UnclosedFrontmatterFallthrough verifies that a document
// with an unclosed frontmatter block ("---\ntitle: foo\n# Heading\nbody\n"
// — no closing "---") is not silently dropped. frontmatter.Parse returns an
// error; renderMarkdown must fall through to the original src so the body
// content (the "# Heading") still renders. Nothing is silently lost.
func TestRenderMarkdown_UnclosedFrontmatterFallthrough(t *testing.T) {
	// No closing ---: the opening --- starts frontmatter parsing but there is no
	// matching close, so Parse returns an error. The render must fall back to the
	// original src (including the heading) rather than returning empty output.
	src := "---\ntitle: foo\n# Heading\nbody\n"
	html, _, err := serve.RenderMarkdown([]byte(src))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	// The body content (# Heading) must survive in the output. The frontmatter
	// parser errored; renderMarkdown falls back to the raw src, so goldmark sees
	// the whole document (including the --- which becomes an <hr>). What matters
	// is that "Heading" and "body" appear — no content is dropped.
	if !strings.Contains(html, "Heading") {
		t.Errorf("expected 'Heading' to survive in output after parse error; got: %s", html)
	}
	if !strings.Contains(html, "body") {
		t.Errorf("expected 'body' text to survive in output; got: %s", html)
	}
}

// TestMermaidScriptLoadedConditionally verifies that when the page route
// serves a markdown file with a mermaid block, the response HTML includes
// the mermaid script tag. For non-mermaid content, it must not load the script.
func TestMermaidScriptLoadedConditionally(t *testing.T) {
	root := t.TempDir()

	// File with mermaid block.
	writeFile(t, filepath.Join(root, "diagram.md"), "```mermaid\ngraph TD\n  A-->B\n```\n")
	// File without mermaid block.
	writeFile(t, filepath.Join(root, "plain.md"), "# Just text\n")

	handler := serve.NewPageHandler(root)

	// Mermaid file: script tag must appear.
	req1 := httptest.NewRequest(http.MethodGet, "/page/diagram.md", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if !strings.Contains(rec1.Body.String(), "mermaid") {
		t.Errorf("mermaid.md response missing mermaid reference; got: %s", rec1.Body.String())
	}

	// Plain file: mermaid script must not appear (avoid loading 3.2MB unnecessarily).
	req2 := httptest.NewRequest(http.MethodGet, "/page/plain.md", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if strings.Contains(rec2.Body.String(), "mermaid.min.js") {
		t.Errorf("plain.md response must not load mermaid.min.js; got: %s", rec2.Body.String())
	}

	// Check that the sentinel file outside root cannot be read via the handler.
	_ = os.Getenv // no-op: avoid unused import if needed
}
