package mdlink_test

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

func TestExtractLinks_MarkdownLink(t *testing.T) {
	content := "See [overview](docs/overview.md) for details.\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d: %v", len(links), links)
	}
	l := links[0]
	if l.Kind != mdlink.MarkdownLink {
		t.Errorf("Kind: got %v, want MarkdownLink", l.Kind)
	}
	if l.Text != "overview" {
		t.Errorf("Text: got %q, want %q", l.Text, "overview")
	}
	if l.Target != "docs/overview.md" {
		t.Errorf("Target: got %q, want %q", l.Target, "docs/overview.md")
	}
	if l.Line != 1 {
		t.Errorf("Line: got %d, want 1", l.Line)
	}
}

func TestExtractLinks_Wikilink(t *testing.T) {
	content := "See [[concepts]] for more.\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d: %v", len(links), links)
	}
	l := links[0]
	if l.Kind != mdlink.Wikilink {
		t.Errorf("Kind: got %v, want Wikilink", l.Kind)
	}
	if l.Target != "concepts" {
		t.Errorf("Target: got %q, want %q", l.Target, "concepts")
	}
	// Without an alias, Text equals Target.
	if l.Text != "concepts" {
		t.Errorf("Text: got %q, want %q", l.Text, "concepts")
	}
	if l.Line != 1 {
		t.Errorf("Line: got %d, want 1", l.Line)
	}
}

func TestExtractLinks_WikilinkAlias(t *testing.T) {
	content := "See [[architecture|the architecture doc]] here.\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d: %v", len(links), links)
	}
	l := links[0]
	if l.Kind != mdlink.Wikilink {
		t.Errorf("Kind: got %v, want Wikilink", l.Kind)
	}
	if l.Target != "architecture" {
		t.Errorf("Target: got %q, want %q", l.Target, "architecture")
	}
	if l.Text != "the architecture doc" {
		t.Errorf("Text: got %q, want %q", l.Text, "the architecture doc")
	}
}

func TestExtractLinks_FenceExcluded(t *testing.T) {
	content := "Before.\n" +
		"```\n" +
		"[inside](fence.md) and [[wikilink-in-fence]]\n" +
		"```\n" +
		"After [real](real.md).\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 1 {
		t.Fatalf("want 1 link (only the outside one), got %d: %v", len(links), links)
	}
	if links[0].Target != "real.md" {
		t.Errorf("wrong link extracted: %v", links[0])
	}
	if links[0].Line != 5 {
		t.Errorf("Line: got %d, want 5", links[0].Line)
	}
}

func TestExtractLinks_InlineCodeExcluded(t *testing.T) {
	content := "Use `[text](not-a-link.md)` to illustrate.\nBut [real](real.md) works.\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 1 {
		t.Fatalf("want 1 link (only the outside one), got %d: %v", len(links), links)
	}
	if links[0].Target != "real.md" {
		t.Errorf("wrong link extracted: got %q, want real.md", links[0].Target)
	}
}

func TestExtractLinks_LineNumbers(t *testing.T) {
	content := "line one\n" +
		"line two [link-a](a.md)\n" +
		"line three\n" +
		"line four [[wiki-b]]\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 2 {
		t.Fatalf("want 2 links, got %d: %v", len(links), links)
	}
	if links[0].Target != "a.md" || links[0].Line != 2 {
		t.Errorf("first link: got target=%q line=%d, want target=a.md line=2", links[0].Target, links[0].Line)
	}
	if links[1].Target != "wiki-b" || links[1].Line != 4 {
		t.Errorf("second link: got target=%q line=%d, want target=wiki-b line=4", links[1].Target, links[1].Line)
	}
}

func TestExtractLinks_MultipleOnLine(t *testing.T) {
	content := "See [a](a.md) and [[b]] and [c](c.md).\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 3 {
		t.Fatalf("want 3 links, got %d: %v", len(links), links)
	}
	targets := []string{links[0].Target, links[1].Target, links[2].Target}
	want := []string{"a.md", "b", "c.md"}
	for i, w := range want {
		if targets[i] != w {
			t.Errorf("link[%d].Target: got %q, want %q", i, targets[i], w)
		}
	}
}

func TestExtractLinks_HTTPLinksExtracted(t *testing.T) {
	content := "Visit [site](https://example.com) today.\n"
	links := mdlink.ExtractLinks(content)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d: %v", len(links), links)
	}
	if links[0].Target != "https://example.com" {
		t.Errorf("Target: got %q, want https://example.com", links[0].Target)
	}
}
