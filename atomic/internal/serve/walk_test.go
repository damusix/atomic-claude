package serve_test

// walk_test.go — tests for shouldSkipDir behaviour across all three walkers.
//
// Each test builds a temp realm that has:
//   - a "normal" page at the root level
//   - a page inside .claude/ (servable project docs — must be INCLUDED)
//   - a page inside .git/ (hidden dir — must be excluded)
//   - a page inside tmp/ / node_modules/ (named skip dirs — must be excluded)
//
// Then asserts that the respective walker sees the normal page and .claude docs
// but not the genuinely-skipped dirs.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildPollutedRealm creates a temp realm for skip-dir tests.
//
// Layout:
//
//	normal.md                    — real page, should be included
//	.claude/project/signals.md   — .claude IS walked (servable project docs),
//	                               so this should be INCLUDED
//	.git/hidden.md               — hidden dir, should be excluded
//	tmp/junk.md                  — named skip dir, should be excluded
//	node_modules/pkg.md          — named skip dir, should be excluded
func buildPollutedRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "normal.md"), "# Normal page\n")
	writeFile(t, filepath.Join(root, ".claude", "project", "signals.md"), "# Signals\n")
	writeFile(t, filepath.Join(root, ".git", "hidden.md"), "# Hidden\n")
	writeFile(t, filepath.Join(root, "tmp", "junk.md"), "# Junk\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg.md"), "# Pkg\n")

	return root
}

// TestBuildLinkGraph_IncludesClaudeExcludesOtherSkipDirs asserts that
// BuildLinkGraph walks .claude (servable project docs cited by wiki linkify)
// but still excludes .git/, tmp/, and node_modules/.
func TestBuildLinkGraph_IncludesClaudeExcludesOtherSkipDirs(t *testing.T) {
	root := buildPollutedRealm(t)
	g := serve.BuildLinkGraph(root)

	nodes := g.Nodes()
	nodeSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeSet[n] = true
	}

	// normal.md and the .claude project doc must both be present: the page
	// handler serves .claude files, so the graph must know about them too.
	for _, want := range []string{"normal.md", ".claude/project/signals.md"} {
		if !nodeSet[want] {
			t.Errorf("BuildLinkGraph: expected %q in nodes, got %v", want, nodes)
		}
	}

	// Other hidden/skip-dir pages must remain absent.
	forbidden := []string{
		".git/hidden.md",
		"tmp/junk.md",
		"node_modules/pkg.md",
	}
	for _, f := range forbidden {
		if nodeSet[f] {
			t.Errorf("BuildLinkGraph: %q must not be included in graph nodes (hidden/skip dir)", f)
		}
	}
}

// TestWalkMarkdownFilesRecursive_ExcludesHiddenAndSkipDirs asserts that the
// docs-tree walker (used by renderRepoNav) skips .* and named-skip dirs.
//
// walkMarkdownFilesRecursive is unexported so we drive it indirectly through
// the /nav handler in repo scope, whose docs group calls it on root/docs.
// We build a docs/ subtree that has the same pattern: a normal page plus
// hidden/skip-dir pages, and confirm only the normal one appears in the HTML.
func TestWalkMarkdownFilesRecursive_ExcludesHiddenAndSkipDirs(t *testing.T) {
	root := t.TempDir()

	// repo-scope nav reads docs/**/*.md via walkMarkdownFilesRecursive.
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(root, "docs", ".claude", "claudedoc.md"), "# ClaudeDoc\n")
	writeFile(t, filepath.Join(root, "docs", ".git", "hidden.md"), "# Hidden\n")
	writeFile(t, filepath.Join(root, "docs", "tmp", "junk.md"), "# Junk\n")
	writeFile(t, filepath.Join(root, "docs", "node_modules", "pkg.md"), "# Pkg\n")

	opts := serve.NavOptions{
		RealmRoot:    root,
		IsRealmScope: false,
	}

	body := navBodyFromOpts(t, opts)

	// guide.md and the .claude doc are both servable, so both appear in nav.
	for _, want := range []string{"guide", "claudedoc"} {
		if !strings.Contains(body, want) {
			t.Errorf("renderRepoNav: expected %q in nav body, got:\n%s", want, body)
		}
	}

	forbidden := []string{"hidden", "junk", "pkg"}
	for _, label := range forbidden {
		if strings.Contains(body, label) {
			t.Errorf("renderRepoNav: %q must not appear in nav (hidden/skip dir), got:\n%s", label, body)
		}
	}
}

// TestBuildExternalRegistry_ExcludesHiddenAndSkipDirs asserts that
// BuildExternalRegistry does not process files inside .claude/, tmp/, or node_modules/.
func TestBuildExternalRegistry_ExcludesHiddenAndSkipDirs(t *testing.T) {
	root := buildPollutedRealm(t)

	// Add external links to the normal page, a .claude doc (walked), and the
	// genuinely-skipped dirs.
	writeFile(t, filepath.Join(root, "normal.md"),
		"# Normal\n\n[good link](https://good.example.com/normal)\n")
	writeFile(t, filepath.Join(root, ".claude", "project", "signals.md"),
		"# Signals\n\n[claude link](https://good.example.com/claude)\n")
	writeFile(t, filepath.Join(root, ".git", "hidden.md"),
		"# Hidden\n\n[bad link](https://bad.example.com/git)\n")
	writeFile(t, filepath.Join(root, "tmp", "junk.md"),
		"# Junk\n\n[bad link](https://bad.example.com/tmp)\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg.md"),
		"# Pkg\n\n[bad link](https://bad.example.com/nodemodules)\n")

	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	urlSet := make(map[string]bool, len(reg))
	for _, e := range reg {
		urlSet[e.URL] = true
	}

	// Links from normal.md and from .claude (walked) are both registered.
	for _, u := range []string{
		"https://good.example.com/normal",
		"https://good.example.com/claude",
	} {
		if !urlSet[u] {
			t.Errorf("BuildExternalRegistry: expected %q in registry, got %v", u, urlSet)
		}
	}

	forbidden := []string{
		"https://bad.example.com/git",
		"https://bad.example.com/tmp",
		"https://bad.example.com/nodemodules",
	}
	for _, u := range forbidden {
		if urlSet[u] {
			t.Errorf("BuildExternalRegistry: %q must not be in registry (hidden/skip dir)", u)
		}
	}
}

// navBodyFromOpts fires the /nav handler and returns the response body.
func navBodyFromOpts(t *testing.T, opts serve.NavOptions) string {
	t.Helper()
	h := serve.NewNavHandler(opts)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/nav", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	h.ServeHTTP(w, req)
	return w.Body.String()
}
