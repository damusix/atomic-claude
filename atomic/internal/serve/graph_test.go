package serve_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildGraphRealm creates a minimal test realm for link graph tests.
//
// Layout:
//
//	a.md  → links to b.md (markdown link)
//	b.md  → links to c.md (wikilink [[c]])
//	c.md  → no outbound links
//	d.md  → no outbound links (orphan)
//	sub/e.md  → same basename as a second "e" in sub2/e.md (ambiguity test)
//	sub2/e.md → same basename as sub/e.md
//	broken.md → links to [[nonexistent]]
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

// TestLinkGraph_BacklinksOfB asserts that B has A as a backlink.
func TestLinkGraph_BacklinksOfB(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	backlinks := g.Backlinks("b.md")
	if !containsPage(backlinks, "a.md") {
		t.Errorf("expected a.md to be a backlink of b.md, got: %v", backlinks)
	}
}

// TestLinkGraph_OutboundOfA asserts that A's outbound links include b.md.
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

// TestLinkGraph_DIsOrphan asserts that d.md is an orphan (no inbound links).
func TestLinkGraph_DIsOrphan(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	if !g.IsOrphan("d.md") {
		t.Errorf("d.md should be an orphan (no inbound links), but IsOrphan returned false")
	}
	// a.md is NOT an orphan in the sense that it has outbound links, but it also
	// has no inbound links from other pages in this realm.
	// c.md has B linking to it → not orphan.
	if g.IsOrphan("c.md") {
		t.Errorf("c.md should NOT be orphan (b.md links to it via wikilink)")
	}
}

// TestLinkGraph_AmbiguousWikilink asserts that a wikilink matching two files
// is flagged as ambiguous.
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

// TestLinkGraph_BrokenWikilink asserts that a wikilink that resolves to nothing
// is flagged as broken.
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

// containsPage reports whether relPath appears in a backlinks slice.
func containsPage(pages []string, relPath string) bool {
	for _, p := range pages {
		if p == relPath {
			return true
		}
	}
	return false
}

// TestContextHandler_PageViewTriggersRail verifies that /page/* (non-htmx) includes
// an htmx trigger to load /rail/<relpath> — the FE2 rail compositing wiring.
// The dead #context-pane is no longer referenced; the right rail slots are the
// new targets (#rail-out-content, #rail-in-content, #rail-graph-content).
func TestContextHandler_PageViewTriggersRail(t *testing.T) {
	root := buildGraphRealm(t)

	g := serve.BuildLinkGraph(root)
	pageHandler := serve.NewPageHandlerWithGraph(root, g)

	srv := httptest.NewServer(pageHandler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/page/b.md")
	if err != nil {
		t.Fatalf("GET /page/b.md: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The page view (full, non-htmx) must reference /rail/b.md to trigger the
	// right-rail load. No reference to /context/ or context-pane must remain.
	if !strings.Contains(html, "/rail/b.md") {
		t.Errorf("page view should trigger /rail/b.md for right-rail load:\n%s", html)
	}
	if strings.Contains(html, "context-pane") {
		t.Errorf("page view must NOT reference dead 'context-pane':\n%s", html)
	}
}

// TestLinkGraph_WikilinkToC verifies that b.md's wikilink [[c]] resolves to c.md.
func TestLinkGraph_WikilinkToC(t *testing.T) {
	root := buildGraphRealm(t)
	g := serve.BuildLinkGraph(root)

	// c.md should appear as a backlink recipient — b.md links to it.
	backlinks := g.Backlinks("c.md")
	if !containsPage(backlinks, "b.md") {
		t.Errorf("expected b.md to be a backlink of c.md, got: %v", backlinks)
	}
}

// TestLinkGraph_WalksMDFiles verifies that the graph builder picks up .md files
// from the realm root recursively.
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

	// Make sure all three pages are present as nodes.
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

// TestLinkGraph_ClaudeProjectLinkResolves verifies that a markdown link into a
// member's .claude/project/signals.md (the kind `atomic wiki linkify` emits)
// resolves to a real page rather than a broken link. This is the cross-cutting
// fix: .claude is walked, so the page is a graph node (the rail gates on Has)
// and the link is not falsely marked broken.
func TestLinkGraph_ClaudeProjectLinkResolves(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wiki", "index.md"),
		"# Realm\n\n[member signals](../member/.claude/project/signals.md)\n")
	writeFile(t, filepath.Join(root, "member", ".claude", "project", "signals.md"), "# Signals\n")

	g := serve.BuildLinkGraph(root)

	// The .claude doc must be a graph node so /rail/<page> (which gates on Has) serves it.
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

// TestLinkGraph_DirectoryLinkResolvesToIndex verifies that a link to a bare
// directory (e.g. a pending wiki member rendered as `../member/`) resolves to an
// index file inside it (README.md / index.md / .claude/project/signals.md)
// rather than being marked broken.
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

// TestPageHandler_FolderServesIndex verifies that loading a folder URL serves
// the folder's index file (README.md) instead of 404, and keys the rail to that
// resolved file. Fixes "there's no index when I load a folder".
func TestPageHandler_FolderServesIndex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proj", "README.md"), "# Proj Readme\n\nWelcome.\n")
	g := serve.BuildLinkGraph(root)
	srv := httptest.NewServer(serve.NewPageHandlerWithGraph(root, g))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/page/proj/", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /page/proj/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("folder load expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "Proj Readme") {
		t.Errorf("folder load should serve README.md content, got:\n%s", html)
	}
	if !strings.Contains(html, "/rail/proj/README.md") {
		t.Errorf("folder load should key the rail to the resolved index file, got:\n%s", html)
	}
}

// TestPageHandler_FolderListingNoIndex verifies that a folder with no index file
// renders a browsable listing of its markdown files instead of 404.
func TestPageHandler_FolderListingNoIndex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes", "alpha.md"), "# Alpha\n")
	writeFile(t, filepath.Join(root, "notes", "beta.md"), "# Beta\n")
	g := serve.BuildLinkGraph(root)
	srv := httptest.NewServer(serve.NewPageHandlerWithGraph(root, g))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/page/notes/", nil)
	req.Header.Set("HX-Request", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /page/notes/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("folder listing expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	for _, want := range []string{"/page/notes/alpha.md", "/page/notes/beta.md", "dir-listing"} {
		if !strings.Contains(html, want) {
			t.Errorf("folder listing should contain %q, got:\n%s", want, html)
		}
	}
}

// TestLinkGraph_LeadingSlashMarkdownLink verifies that resolveMarkdownLink treats
// a leading-slash target as bundle-root-relative (same semantics as resolvePageHref)
// rather than marking it Broken.
//
// OKF §5.1 form: `/repos/alpha.md` means the file at <root>/repos/alpha.md — it is
// NOT an OS absolute path. Both resolvePageHref (render path) and resolveMarkdownLink
// (graph-build path) must agree, otherwise a link renders as an in-shell href but the
// right-rail OUT LINKS panel shows it with a broken ❌ marker.
func TestLinkGraph_LeadingSlashMarkdownLink(t *testing.T) {
	root := t.TempDir()
	// Create a page that uses a leading-slash link, and the target page.
	writeFile(t, filepath.Join(root, "index.md"),
		"# Index\n\nSee [alpha](/repos/alpha.md) and [beta](/concerns/beta.md).\n")
	writeFile(t, filepath.Join(root, "repos", "alpha.md"), "# Alpha\n")
	writeFile(t, filepath.Join(root, "concerns", "beta.md"), "# Beta\n")

	g := serve.BuildLinkGraph(root)

	// ── case 1: leading-slash link to an existing .md page is NOT broken ─────
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

	// ── case 2: backlinks must record the edge (body and graph agree) ─────────
	backlinks := g.Backlinks("repos/alpha.md")
	if !containsPage(backlinks, "index.md") {
		t.Errorf("repos/alpha.md should have index.md as a backlink; got %v", backlinks)
	}

	// ── case 3: leading-slash traversal attempt stays Broken ──────────────────
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

	// ── case 4: leading-slash to a non-existent path stays Broken ────────────
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

// TestNodeMeta_SnippetSkipsDashLine verifies that a body whose first non-heading
// non-blank line is a setext underline or horizontal rule (---/===) is not taken
// as the snippet. The scanner must fall through to the next prose line (or return
// an empty snippet when none follows). The critical case is a bare `---` used as
// a horizontal rule — isSetextUnderline catches it (all same character, length ≥ 2)
// so it is never mistaken for prose.
func TestNodeMeta_SnippetSkipsDashLine(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantSnippet string
	}{
		{
			// Bare --- horizontal rule as the first body line after a heading —
			// must be skipped and fall through to the actual prose.
			name:        "bare horizontal rule --- followed by prose",
			content:     "# Heading\n\n---\n\nActual prose.\n",
			wantSnippet: "Actual prose.",
		},
		{
			// Long dash line (6 dashes) — isSetextUnderline covers multi-char runs.
			name:        "long dash line is also skipped",
			content:     "# Heading\n\n------\n\nProse below.\n",
			wantSnippet: "Prose below.",
		},
		{
			// === equals underline — all same character, also caught by isSetextUnderline.
			name:        "setext equals line",
			content:     "# Heading\n\n===\n\nFollowing prose.\n",
			wantSnippet: "Following prose.",
		},
		{
			// Only a horizontal rule with no prose after — snippet is empty.
			name:        "only horizontal rule no prose",
			content:     "# Heading\n\n---\n",
			wantSnippet: "",
		},
		{
			// Mixed body: heading → blank → --- → blank → prose.
			// Verifies the scanner skips the --- and finds the prose line.
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

// Stub to avoid "declared and not used" issues for the OS import.
var _ = os.DevNull
