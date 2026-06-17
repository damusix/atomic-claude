package serve_test

// wikilink_test.go — tests for in-body Obsidian wikilink rendering
// (RenderMarkdownWithGraph). A bare [[page]] in a markdown body must become a
// clickable in-shell link resolved the same way the right rail resolves it — the
// regression the user hit: "[[directus-cicd]], [[directus-research]] not linking
// correctly when atomic serve" (rendered as literal text in the page body while
// the OUT-links rail resolved them fine).

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildRealmGraph writes the given files (relpath -> content) under a temp realm
// root and returns the root plus a built link graph.
func buildRealmGraph(t *testing.T, files map[string]string) (string, *serve.Graph) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	return root, serve.BuildLinkGraph(root)
}

// TestWikilink_ResolvedBecomesHtmxLink reproduces the user's exact case: a
// tickets page links two knowledge pages by bare wikilink. Both must render as
// in-shell htmx links to the resolved /page/ route, not as literal [[…]] text.
func TestWikilink_ResolvedBecomesHtmxLink(t *testing.T) {
	page := "tickets/012-directus-editorial-cms.md"
	root, g := buildRealmGraph(t, map[string]string{
		"wiki/knowledge/directus-cicd.md":     "# Directus CI/CD\n",
		"wiki/knowledge/directus-research.md": "# Directus research\n",
		page:                                  "see [[directus-cicd]], [[directus-research]]\n",
	})

	html, _, err := serve.RenderMarkdownWithGraph(
		[]byte("see [[directus-cicd]], [[directus-research]]\n"), root, page, g)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		`href="/page/wiki/knowledge/directus-cicd.md"`,
		`href="/page/wiki/knowledge/directus-research.md"`,
		`hx-get="/page/wiki/knowledge/directus-cicd.md"`,
		`hx-target="#main-pane"`,
		`class="wikilink"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q in output; got:\n%s", want, html)
		}
	}
	// The display text is the page name when no alias is given.
	if !strings.Contains(html, `>directus-cicd</a>`) {
		t.Errorf("expected display text 'directus-cicd'; got:\n%s", html)
	}
	// The raw [[…]] form must not survive into the body — that's the bug.
	if strings.Contains(html, "[[directus-cicd]]") || strings.Contains(html, "[[directus-research]]") {
		t.Errorf("raw wikilink leaked into output:\n%s", html)
	}
}

// TestWikilink_AliasUsesDisplayTextResolvesPage verifies [[page|alias]] shows the
// alias but links the resolved page.
func TestWikilink_AliasUsesDisplayTextResolvesPage(t *testing.T) {
	page := "tickets/x.md"
	root, g := buildRealmGraph(t, map[string]string{
		"wiki/knowledge/directus-cicd.md": "# CI/CD\n",
		page:                              "[[directus-cicd|the CI/CD plan]]\n",
	})

	html, _, err := serve.RenderMarkdownWithGraph(
		[]byte("[[directus-cicd|the CI/CD plan]]\n"), root, page, g)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `href="/page/wiki/knowledge/directus-cicd.md"`) {
		t.Errorf("alias wikilink must resolve to the page; got:\n%s", html)
	}
	if !strings.Contains(html, ">the CI/CD plan</a>") {
		t.Errorf("alias display text must be used; got:\n%s", html)
	}
}

// TestWikilink_BrokenRendersNonNavigableSpan verifies an unresolved wikilink
// renders as a visible broken span, not a link and not literal prose.
func TestWikilink_BrokenRendersNonNavigableSpan(t *testing.T) {
	page := "tickets/x.md"
	root, g := buildRealmGraph(t, map[string]string{
		page: "[[does-not-exist]]\n",
	})

	html, _, err := serve.RenderMarkdownWithGraph(
		[]byte("[[does-not-exist]]\n"), root, page, g)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `class="wikilink-broken"`) {
		t.Errorf("broken wikilink must render as wikilink-broken span; got:\n%s", html)
	}
	if strings.Contains(html, `/page/does-not-exist`) || strings.Contains(html, "hx-get") {
		t.Errorf("broken wikilink must not be navigable; got:\n%s", html)
	}
	if strings.Contains(html, "[[does-not-exist]]") {
		t.Errorf("broken wikilink must not render as literal [[…]] prose; got:\n%s", html)
	}
}

// TestWikilink_InsideCodeSpanNotLinked verifies a wikilink inside an inline code
// span stays literal (fence-awareness, consistent with mdlink.ExtractLinks).
func TestWikilink_InsideCodeSpanNotLinked(t *testing.T) {
	page := "tickets/x.md"
	root, g := buildRealmGraph(t, map[string]string{
		"wiki/knowledge/directus-cicd.md": "# CI/CD\n",
		page:                              "use `[[directus-cicd]]` syntax\n",
	})

	html, _, err := serve.RenderMarkdownWithGraph(
		[]byte("use `[[directus-cicd]]` syntax\n"), root, page, g)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(html, `class="wikilink"`) {
		t.Errorf("wikilink inside a code span must not be linked; got:\n%s", html)
	}
	if !strings.Contains(html, "<code>") || !strings.Contains(html, "[[directus-cicd]]") {
		t.Errorf("code span content must stay literal; got:\n%s", html)
	}
}

// TestWikilink_FencedCodeBlockNotLinked verifies a wikilink inside a fenced code
// block is left literal.
func TestWikilink_FencedCodeBlockNotLinked(t *testing.T) {
	page := "tickets/x.md"
	root, g := buildRealmGraph(t, map[string]string{
		"wiki/knowledge/directus-cicd.md": "# CI/CD\n",
		page:                              "```\n[[directus-cicd]]\n```\n",
	})

	html, _, err := serve.RenderMarkdownWithGraph(
		[]byte("```\n[[directus-cicd]]\n```\n"), root, page, g)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(html, `class="wikilink"`) {
		t.Errorf("wikilink inside a fenced block must not be linked; got:\n%s", html)
	}
}

// TestWikilink_MarkdownLinkStillWorks verifies adding the wikilink parser does
// not break standard markdown links on the same page.
func TestWikilink_MarkdownLinkStillWorks(t *testing.T) {
	page := "wiki/repos/foo.md"
	root, g := buildRealmGraph(t, map[string]string{
		"wiki/concerns/x.md":              "# X\n",
		"wiki/knowledge/directus-cicd.md": "# CI/CD\n",
		page:                              "see [the concern](../concerns/x.md) and [[directus-cicd]]\n",
	})

	html, _, err := serve.RenderMarkdownWithGraph(
		[]byte("see [the concern](../concerns/x.md) and [[directus-cicd]]\n"), root, page, g)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `href="/page/wiki/concerns/x.md"`) {
		t.Errorf("markdown link must still resolve; got:\n%s", html)
	}
	if !strings.Contains(html, `href="/page/wiki/knowledge/directus-cicd.md"`) {
		t.Errorf("wikilink must resolve alongside the markdown link; got:\n%s", html)
	}
}

// TestWikilink_NilGraphLeavesLiteral documents the graphless path: without a
// graph there is no realm basename index to resolve against, so [[…]] is left
// literal rather than guessed at.
func TestWikilink_NilGraphLeavesLiteral(t *testing.T) {
	html, _, err := serve.RenderMarkdownWithGraph(
		[]byte("see [[directus-cicd]]\n"), t.TempDir(), "a.md", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "[[directus-cicd]]") {
		t.Errorf("nil-graph path should leave wikilink literal; got:\n%s", html)
	}
	if strings.Contains(html, `class="wikilink"`) {
		t.Errorf("nil-graph path must not emit wikilink anchors; got:\n%s", html)
	}
}
