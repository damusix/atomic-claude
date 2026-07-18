package serve_test

import (
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
