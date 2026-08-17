package serve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildGraphRealm covers every edge shape the graph tests need: a markdown link
// (a→b), a wikilink (b→c), an orphan (d), a wikilink whose basename matches two
// files (ambiguous→sub/e, sub2/e), and a dead wikilink (broken).
func buildGraphRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "a.md"), "# A\n\nSee [B page](b.md) for details.\n")
	writeFile(t, filepath.Join(root, "b.md"), "# B\n\nSee [[c]] for more.\n")
	writeFile(t, filepath.Join(root, "c.md"), "# C\n\nNo outbound links.\n")
	writeFile(t, filepath.Join(root, "d.md"), "# D\n\nAlso no links (orphan).\n")
	writeFile(t, filepath.Join(root, "sub", "e.md"), "# E in sub\n\nSome content.\n")
	writeFile(t, filepath.Join(root, "sub2", "e.md"), "# E in sub2\n\nSome content.\n")
	writeFile(t, filepath.Join(root, "ambiguous.md"), "# Ambiguous\n\n[[e]] resolves to two files.\n")
	writeFile(t, filepath.Join(root, "broken.md"), "# Broken\n\n[[nonexistent]] is a dead link.\n")

	return root
}

func TestLinkGraph_BacklinksOfB(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	backlinks := g.Backlinks("b.md")
	if !containsPage(backlinks, "a.md") {
		t.Errorf("expected a.md to be a backlink of b.md, got: %v", backlinks)
	}
}

func TestLinkGraph_OutboundOfA(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	outbound := g.Outbound("a.md")
	if len(outbound) == 0 {
		t.Fatalf("a.md should have outbound links")
	}
	found := false
	for _, e := range outbound {
		if e.ResolvedPath == "b.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected b.md in outbound of a.md, got: %v", outbound)
	}
}

func TestLinkGraph_DIsOrphan(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	if !g.IsOrphan("d.md") {
		t.Errorf("d.md should be an orphan (no inbound links), but IsOrphan returned false")
	}
	// Orphan means no inbound links, so a wikilink has to count as one.
	if g.IsOrphan("c.md") {
		t.Errorf("c.md should NOT be orphan (b.md links to it via wikilink)")
	}
}

func TestLinkGraph_AmbiguousWikilink(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	outbound := g.Outbound("ambiguous.md")
	if len(outbound) == 0 {
		t.Fatalf("ambiguous.md should have outbound edges")
	}
	found := false
	for _, e := range outbound {
		if e.Ambiguous {
			found = true
		}
	}
	if !found {
		t.Errorf("expected at least one ambiguous edge in outbound of ambiguous.md, got: %v", outbound)
	}
}

func TestLinkGraph_BrokenWikilink(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	outbound := g.Outbound("broken.md")
	if len(outbound) == 0 {
		t.Fatalf("broken.md should have outbound edges")
	}
	found := false
	for _, e := range outbound {
		if e.Broken {
			found = true
		}
	}
	if !found {
		t.Errorf("expected at least one broken edge in outbound of broken.md, got: %v", outbound)
	}
}

func containsPage(pages []string, relPath string) bool {
	for _, p := range pages {
		if p == relPath {
			return true
		}
	}
	return false
}

func TestLinkGraph_WikilinkToC(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	backlinks := g.Backlinks("c.md")
	if !containsPage(backlinks, "b.md") {
		t.Errorf("expected b.md to be a backlink of c.md, got: %v", backlinks)
	}
}

func TestLinkGraph_WalksMDFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.md"), "# Index\n")
	writeFile(t, filepath.Join(root, "sub", "page.md"), "# Sub page\n")
	writeFile(t, filepath.Join(root, "sub", "README.md"), "# Readme\n")

	g := serve.BuildLinkGraph(root)
	nodes := g.Nodes()
	if len(nodes) < 3 {
		t.Errorf("expected at least 3 nodes, got %d: %v", len(nodes), nodes)
	}

	wantNodes := []string{"index.md", filepath.Join("sub", "page.md"), filepath.Join("sub", "README.md")}
	for _, want := range wantNodes {
		found := false
		for _, n := range nodes {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("node %q not found in graph; nodes: %v", want, nodes)
		}
	}
}

// `atomic wiki linkify` emits links into a member's .claude/project/, so .claude
// has to be walked — otherwise the page is not a node and the link reads broken.
func TestLinkGraph_ClaudeProjectLinkResolves(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wiki", "index.md"),
		"# Realm\n\n[member signals](../member/.claude/project/signals.md)\n")
	writeFile(t, filepath.Join(root, "member", ".claude", "project", "signals.md"), "# Signals\n")

	g := serve.BuildLinkGraph(root)

	// /rail/<page> gates on Has, so node membership is what makes the page servable.
	if !g.Has("member/.claude/project/signals.md") {
		t.Fatal("expected member/.claude/project/signals.md to be a graph node")
	}

	var found bool
	for _, e := range g.Outbound("wiki/index.md") {
		if !strings.Contains(e.Target, "signals.md") {
			continue
		}
		found = true
		if e.Broken {
			t.Errorf(".claude project link must not be broken; edge=%+v", e)
		}
		if e.ResolvedPath != "member/.claude/project/signals.md" {
			t.Errorf("expected resolved path member/.claude/project/signals.md, got %q", e.ResolvedPath)
		}
	}
	if !found {
		t.Fatalf("expected an outbound edge to signals.md from wiki/index.md")
	}
}

// A pending wiki member renders as a bare `../member/` link, which must land on
// an index file inside the directory rather than read as broken.
func TestLinkGraph_DirectoryLinkResolvesToIndex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.md"), "# Index\n\nSee [member](member/) repo.\n")
	writeFile(t, filepath.Join(root, "member", "README.md"), "# Member\n")

	g := serve.BuildLinkGraph(root)

	var found bool
	for _, e := range g.Outbound("index.md") {
		if e.Target != "member/" {
			continue
		}
		found = true
		if e.Broken {
			t.Errorf("directory link must not be broken when an index file exists; edge=%+v", e)
		}
		if e.ResolvedPath != "member/README.md" {
			t.Errorf("directory link should resolve to member/README.md, got %q", e.ResolvedPath)
		}
	}
	if !found {
		t.Fatalf("expected an outbound edge with target 'member/'")
	}
}

// A directory with no index file is still a page — /api/page serves it as a
// listing. Marking the edge Broken made the rail say "unresolved" for a link the
// reader can watch work in the body.
func TestLinkGraph_DirectoryLinkWithoutIndexIsNotBroken(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.md"), "# Index\n\nSee [pkg](src/pkg) source.\n")
	writeFile(t, filepath.Join(root, "src", "pkg", "thing.go"), "package pkg\n")

	g := serve.BuildLinkGraph(root)

	var found bool
	for _, e := range g.Outbound("index.md") {
		if e.Target != "src/pkg" {
			continue
		}
		found = true
		if e.Broken {
			t.Errorf("directory link without an index must not be broken; edge=%+v", e)
		}
		if e.ResolvedPath != "src/pkg" {
			t.Errorf("expected ResolvedPath 'src/pkg', got %q", e.ResolvedPath)
		}
		if !e.Dir {
			t.Errorf("expected Dir=true so the UI can render a folder affordance; edge=%+v", e)
		}
		if e.CodeFile {
			t.Errorf("a directory is not a code file; edge=%+v", e)
		}
	}
	if !found {
		t.Fatalf("expected an outbound edge with target 'src/pkg'")
	}
}

// Docs written for a published site are rooted at docs/, but serve is rooted at
// the repository, so a leading-slash link resolved to nothing.
func TestLinkGraph_DocsRootRelativeLinkResolves(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "wiki", "index.md"),
		"# Index\n\nSee [Concepts](/reference/concepts#wikis).\n")
	writeFile(t, filepath.Join(root, "docs", "reference", "concepts.md"), "# Concepts\n")

	g := serve.BuildLinkGraph(root)

	var found bool
	for _, e := range g.Outbound("docs/wiki/index.md") {
		if e.Target != "/reference/concepts#wikis" {
			continue
		}
		found = true
		if e.Broken {
			t.Errorf("docs-root-relative link must resolve, got broken; edge=%+v", e)
		}
		if e.ResolvedPath != "docs/reference/concepts.md" {
			t.Errorf("ResolvedPath = %q, want docs/reference/concepts.md", e.ResolvedPath)
		}
	}
	if !found {
		t.Fatalf("expected an outbound edge for the docs-root-relative link")
	}
}

// `/repos/alpha.md` is bundle-root-relative, not an OS absolute path. The render
// path and the graph-build path must agree on that, or the body renders a working
// href while the rail marks the same link broken.
func TestLinkGraph_LeadingSlashMarkdownLink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.md"),
		"# Index\n\nSee [alpha](/repos/alpha.md) and [beta](/concerns/beta.md).\n")
	writeFile(t, filepath.Join(root, "repos", "alpha.md"), "# Alpha\n")
	writeFile(t, filepath.Join(root, "concerns", "beta.md"), "# Beta\n")

	g := serve.BuildLinkGraph(root)

	var alphaEdge, betaEdge *serve.Edge
	for i := range g.Outbound("index.md") {
		e := g.Outbound("index.md")[i]
		if strings.HasSuffix(e.Target, "alpha.md") {
			alphaEdge = &e
		}
		if strings.HasSuffix(e.Target, "beta.md") {
			betaEdge = &e
		}
	}
	if alphaEdge == nil {
		t.Fatal("expected outbound edge to /repos/alpha.md from index.md")
	}
	if alphaEdge.Broken {
		t.Errorf("leading-slash link to existing page must NOT be Broken; edge=%+v", *alphaEdge)
	}
	if alphaEdge.ResolvedPath != "repos/alpha.md" {
		t.Errorf("leading-slash link resolved path: got %q, want %q", alphaEdge.ResolvedPath, "repos/alpha.md")
	}
	if betaEdge == nil {
		t.Fatal("expected outbound edge to /concerns/beta.md from index.md")
	}
	if betaEdge.Broken {
		t.Errorf("leading-slash link to existing page must NOT be Broken; edge=%+v", *betaEdge)
	}

	backlinks := g.Backlinks("repos/alpha.md")
	if !containsPage(backlinks, "index.md") {
		t.Errorf("repos/alpha.md should have index.md as a backlink; got %v", backlinks)
	}

	writeFile(t, filepath.Join(root, "trap.md"),
		"# Trap\n\n[escape](/../../../etc/passwd)\n")
	g2 := serve.BuildLinkGraph(root)
	for _, e := range g2.Outbound("trap.md") {
		if !strings.Contains(e.Target, "passwd") {
			continue
		}
		if !e.Broken {
			t.Errorf("traversal-escaping leading-slash link must stay Broken; edge=%+v", e)
		}
	}

	writeFile(t, filepath.Join(root, "ghost.md"),
		"# Ghost\n\n[missing](/no-such-file.md)\n")
	g3 := serve.BuildLinkGraph(root)
	for _, e := range g3.Outbound("ghost.md") {
		if !strings.Contains(e.Target, "no-such-file") {
			continue
		}
		if !e.Broken {
			t.Errorf("leading-slash to non-existent path must stay Broken; edge=%+v", e)
		}
	}
}

// A setext underline or horizontal rule (---/===) reads as prose to a naive
// scanner; isSetextUnderline must catch it and fall through to the next line.
func TestNodeMeta_SnippetSkipsDashLine(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantSnippet string
	}{
		{
			name:        "bare horizontal rule --- followed by prose",
			content:     "# Heading\n\n---\n\nActual prose.\n",
			wantSnippet: "Actual prose.",
		},
		{
			name:        "long dash line is also skipped",
			content:     "# Heading\n\n------\n\nProse below.\n",
			wantSnippet: "Prose below.",
		},
		{
			name:        "setext equals line",
			content:     "# Heading\n\n===\n\nFollowing prose.\n",
			wantSnippet: "Following prose.",
		},
		{
			name:        "only horizontal rule no prose",
			content:     "# Heading\n\n---\n",
			wantSnippet: "",
		},
		{
			name:        "heading then blank then --- then prose",
			content:     "# Title\n\n---\n\nThis is the description.\n",
			wantSnippet: "This is the description.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "page.md"), tc.content)
			g := serve.BuildLinkGraph(root)
			meta := g.Meta("page.md")
			if meta.Snippet != tc.wantSnippet {
				t.Errorf("Snippet: got %q, want %q", meta.Snippet, tc.wantSnippet)
			}
		})
	}
}

func TestNodeType_IndexAndDomain(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "wiki-index.md"),
		"---\ntype: Index\ndescription: Signals index\n---\n# Index\n")
	writeFile(t, filepath.Join(root, "wiki-domain.md"),
		"---\ntype: Domain\ndescription: A domain file\n---\n# Domain\n")
	writeFile(t, filepath.Join(root, "k.md"),
		"---\ntype: Knowledge\n---\n# K\n")
	writeFile(t, filepath.Join(root, "c.md"),
		"---\ntype: Concern\n---\n# C\n")
	writeFile(t, filepath.Join(root, "r.md"),
		"---\ntype: Repo\n---\n# R\n")
	writeFile(t, filepath.Join(root, "b.md"),
		"---\ntype: Bucket\n---\n# B\n")
	writeFile(t, filepath.Join(root, "plain.md"), "# Plain\n")

	g := serve.BuildLinkGraph(root)

	cases := []struct {
		path string
		want string
	}{
		{"wiki-index.md", "index"},
		{"wiki-domain.md", "domain"},
		{"k.md", "knowledge"},
		{"c.md", "concern"},
		{"r.md", "repo"},
		{"b.md", "bucket"},
		{"plain.md", "page"},
	}
	for _, tc := range cases {
		got := g.NodeType(tc.path)
		if got != tc.want {
			t.Errorf("NodeType(%q): got %q, want %q", tc.path, got, tc.want)
		}
	}
}

// Keeps the os import referenced.
var _ = os.DevNull
