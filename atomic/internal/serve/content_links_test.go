package serve_test

// A relative href in page content resolves against the browser URL, which is not
// where the file lives. Rewriting to a real server route is what keeps a click
// inside the shell instead of throwing the reader out of it.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

func TestRenderLinks_RelativeMdBecomesHtmxPageLink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wiki", "concerns", "x.md"), "# X\n")
	page := filepath.Join(root, "wiki", "repos", "foo.md")
	writeFile(t, page, "see [the concern](../concerns/x.md)\n")

	html, _, err := serve.RenderMarkdownWithLinks([]byte("see [the concern](../concerns/x.md)\n"), root, "wiki/repos/foo.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Resolved against the realm root, not the browser URL. A plain href is enough;
	// the router intercepts in-shell navigation client-side.
	if !strings.Contains(html, `href="/page/wiki/concerns/x.md"`) {
		t.Errorf("expected realm-resolved /page href; got:\n%s", html)
	}
	if strings.Contains(html, "hx-get") {
		t.Errorf("renderer must not emit hx-get attributes; got:\n%s", html)
	}
	// The raw "../" form is exactly what the browser mis-resolves.
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
	// /file/ is where the code modal handler picks them up.
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
	// In-realm but absent: /page/ serves an in-shell 404 rather than throwing the
	// reader out to a full-page navigation.
	html, _, err := serve.RenderMarkdownWithLinks(
		[]byte("[gone](missing.md)\n"), root, "a.md")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `href="/page/missing.md"`) {
		t.Errorf("unresolved in-realm link should still route through /page/; got:\n%s", html)
	}
}

// Without a page path there is nothing to resolve against, so hrefs stay raw.
func TestRenderMarkdown_NoRewriteWithoutPath(t *testing.T) {
	html, _, err := serve.RenderMarkdown([]byte("[x](../y.md)\n"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `href="../y.md"`) {
		t.Errorf("RenderMarkdown should leave hrefs raw; got:\n%s", html)
	}
}
