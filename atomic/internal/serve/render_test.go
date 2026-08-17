package serve_test

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

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

// Plain CommonMark renders no table, so this is the GFM-extension wiring check.
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

func TestMarkdownGoCodeBlockHighlighted(t *testing.T) {
	md := "```go\npackage main\n\nfunc main() {}\n```\n"
	html, _, err := serve.RenderMarkdown([]byte(md))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	// Chroma's wrapper class varies with the inline-styles setting, so the stable
	// signal is the token <span>s it adds inside the block.
	if !strings.Contains(html, "main") {
		t.Errorf("expected 'main' in highlighted output, got: %s", html)
	}
	if !strings.Contains(html, "<span") {
		t.Errorf("expected chroma <span> highlight tokens in output, got: %s", html)
	}
}

func TestMarkdownMermaidBlock(t *testing.T) {
	md := "```mermaid\ngraph TD\n  A --> B\n```\n"
	html, hasMermaid, err := serve.RenderMarkdown([]byte(md))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, `<pre class="mermaid"`) {
		t.Errorf("expected <pre class=\"mermaid\"> in output, got: %s", html)
	}
	if !strings.Contains(html, "graph TD") {
		t.Errorf("expected raw mermaid content in output, got: %s", html)
	}
	// Mermaid must reach the client as raw text, so no chroma token <span> may
	// appear between the opening pre and its close.
	mermaidIdx := strings.Index(html, `<pre class="mermaid"`)
	endIdx := strings.Index(html[mermaidIdx:], "</pre>")
	if mermaidIdx >= 0 && endIdx >= 0 {
		mermaidBlock := html[mermaidIdx : mermaidIdx+endIdx+6]
		if strings.Contains(mermaidBlock, "<span") {
			t.Errorf("mermaid block must not contain chroma <span> tokens, got: %s", mermaidBlock)
		}
	}
	// The caller injects the mermaid script off this flag.
	if !hasMermaid {
		t.Error("hasMermaid should be true when a ```mermaid block is present")
	}
}

// Frontmatter must be stripped before goldmark, or the opening "---" renders as
// an <hr> and the YAML keys render as body text.
func TestRenderMarkdown_FrontmatterStripped(t *testing.T) {
	src := "---\ntitle: \"@hapi/nes\"\nrepo: nes\ngenerated: 2026-06-13\n---\n\n# Overview\n\nbody\n"
	html, _, err := serve.RenderMarkdown([]byte(src))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if strings.Contains(html, "title:") {
		t.Errorf("frontmatter key 'title:' leaked into body HTML: %s", html)
	}
	if strings.Contains(html, "repo:") {
		t.Errorf("frontmatter key 'repo:' leaked into body HTML: %s", html)
	}
	if strings.Contains(html, "generated:") {
		t.Errorf("frontmatter key 'generated:' leaked into body HTML: %s", html)
	}
	if strings.Contains(html, "<hr") {
		t.Errorf("spurious <hr> from frontmatter '---' in body HTML: %s", html)
	}
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

// A "---" that is not at byte 0 is a thematic break, and the frontmatter
// stripper must leave it for goldmark.
func TestRenderMarkdown_MidDocThematicBreak(t *testing.T) {
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

// With no closing "---" the frontmatter parse errors, and the render must fall
// back to the raw src rather than dropping the document.
func TestRenderMarkdown_UnclosedFrontmatterFallthrough(t *testing.T) {
	src := "---\ntitle: foo\n# Heading\nbody\n"
	html, _, err := serve.RenderMarkdown([]byte(src))
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "Heading") {
		t.Errorf("expected 'Heading' to survive in output after parse error; got: %s", html)
	}
	if !strings.Contains(html, "body") {
		t.Errorf("expected 'body' text to survive in output; got: %s", html)
	}
}
