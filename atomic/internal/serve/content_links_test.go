package serve_test

// content_links_test.go — tests for server-side rewriting of in-page markdown
// links (RenderMarkdownWithLinks). Page-content links must resolve against the
// realm root and become real server routes so clicking one navigates inside the
// shell (htmx) instead of doing a full-page navigation that loses the user's
// place — the regression the user hit ("losing links like crazy").

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

func TestRenderLinks_RelativeMdBecomesHtmxPageLink(t *testing.T) {
	root := t.TempDir()
	// A page at wiki/repos/foo.md linking up-and-over to wiki/concerns/x.md.
	writeFile(t, filepath.Join(root, "wiki", "concerns", "x.md"), "# X\n")
	page := filepath.Join(root, "wiki", "repos", "foo.md")
	writeFile(t, page, "see [the concern](../concerns/x.md)\n")

	html, _, err := serve.RenderMarkdownWithLinks([]byte("see [the concern](../concerns/x.md)\n"), root, "wiki/repos/foo.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Resolved against the realm root, not the browser URL.
	if !strings.Contains(html, `href="/page/wiki/concerns/x.md"`) {
		t.Errorf("expected realm-resolved /page href; got:\n%s", html)
	}
	// htmx-navigated so the shell is preserved.
	if !strings.Contains(html, `hx-get="/page/wiki/concerns/x.md"`) ||
		!strings.Contains(html, `hx-target="#main-pane"`) {
		t.Errorf("expected htmx navigation attributes; got:\n%s", html)
	}
	// The raw "../" form must not survive — that's what the browser mis-resolves.
	if strings.Contains(html, "../concerns/x.md") {
		t.Errorf("raw relative href leaked into output:\n%s", html)
	}
}

func TestRenderLinks_SourceFileBecomesFileRoute(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "internal", "billing.go"), "package billing\n")
	html, _, err := serve.RenderMarkdownWithLinks(
		[]byte("see [billing](../internal/billing.go)\n"), root, "docs/notes.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Source files route to /file/ (the code modal handler opens them); no hx-get.
	if !strings.Contains(html, `href="/file/internal/billing.go"`) {
		t.Errorf("expected /file route for source link; got:\n%s", html)
	}
	if strings.Contains(html, "hx-get") {
		t.Errorf("source-file links must not be htmx page links; got:\n%s", html)
	}
}

func TestRenderLinks_ExternalUnchangedNewTab(t *testing.T) {
	root := t.TempDir()
	html, _, err := serve.RenderMarkdownWithLinks(
		[]byte("[site](https://example.com/x)\n"), root, "a.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `href="https://example.com/x"`) {
		t.Errorf("external href must be preserved; got:\n%s", html)
	}
	if !strings.Contains(html, `target="_blank"`) {
		t.Errorf("external link should open in a new tab; got:\n%s", html)
	}
	if strings.Contains(html, "/page/") {
		t.Errorf("external link must not be routed through /page/; got:\n%s", html)
	}
}

func TestRenderLinks_AnchorOnlyPreserved(t *testing.T) {
	root := t.TempDir()
	html, _, err := serve.RenderMarkdownWithLinks(
		[]byte("[top](#heading)\n"), root, "a.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `href="#heading"`) {
		t.Errorf("in-page anchor must be preserved verbatim; got:\n%s", html)
	}
	if strings.Contains(html, "/page/") || strings.Contains(html, "hx-get") {
		t.Errorf("anchor-only link must not be rewritten; got:\n%s", html)
	}
}

func TestRenderLinks_EscapeOutsideRealmLeftRaw(t *testing.T) {
	root := t.TempDir()
	html, _, err := serve.RenderMarkdownWithLinks(
		[]byte("[escape](../../../etc/passwd)\n"), root, "a.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Must never become a server route that could be probed.
	if strings.Contains(html, "/page/") || strings.Contains(html, "/file/") {
		t.Errorf("realm-escaping link must not be routed; got:\n%s", html)
	}
}

func TestRenderLinks_UnresolvedWithinRealmStaysInShell(t *testing.T) {
	root := t.TempDir()
	// Target does not exist, but is within the realm: route through /page/ so the
	// handler serves a graceful in-shell 404 instead of a full-page navigation.
	html, _, err := serve.RenderMarkdownWithLinks(
		[]byte("[gone](missing.md)\n"), root, "a.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `hx-get="/page/missing.md"`) {
		t.Errorf("unresolved in-realm link should still navigate in-shell; got:\n%s", html)
	}
}

// RenderMarkdown (no path) must keep raw hrefs — back-compat for callers that do
// not resolve links (and the existing test suite).
func TestRenderMarkdown_NoRewriteWithoutPath(t *testing.T) {
	html, _, err := serve.RenderMarkdown([]byte("[x](../y.md)\n"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `href="../y.md"`) {
		t.Errorf("RenderMarkdown should leave hrefs raw; got:\n%s", html)
	}
}
