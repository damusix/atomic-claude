package serve_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// .claude holds servable project docs and IS walked, unlike the other hidden and
// named-skip dirs seeded here.
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

func TestBuildLinkGraph_IncludesClaudeExcludesOtherSkipDirs(t *testing.T) {
	root := buildPollutedRealm(t)
	g := serve.BuildLinkGraph(root)

	nodes := g.Nodes()
	nodeSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeSet[n] = true
	}

	// The page handler serves .claude files, so the graph has to know them too.
	for _, want := range []string{"normal.md", ".claude/project/signals.md"} {
		if !nodeSet[want] {
			t.Errorf("BuildLinkGraph: expected %q in nodes, got %v", want, nodes)
		}
	}

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

// walkMarkdownFilesRecursive is unexported, so it is driven through the nav
// handler in repo scope, whose docs group calls it on root/docs.
func TestWalkMarkdownFilesRecursive_ExcludesHiddenAndSkipDirs(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(root, "docs", ".claude", "claudedoc.md"), "# ClaudeDoc\n")
	writeFile(t, filepath.Join(root, "docs", ".git", "hidden.md"), "# Hidden\n")
	writeFile(t, filepath.Join(root, "docs", "tmp", "junk.md"), "# Junk\n")
	writeFile(t, filepath.Join(root, "docs", "node_modules", "pkg.md"), "# Pkg\n")

	opts := serve.NavOptions{
		RealmRoot:    root,
		IsRealmScope: false,
	}

	labels := navLabelsFromOpts(t, opts)

	// Both are servable, so both belong in nav.
	for _, want := range []string{"guide", "claudedoc"} {
		if !labels[want] {
			t.Errorf("buildRepoNavGroupsJSON: expected %q among nav labels, got:\n%v", want, labels)
		}
	}

	forbidden := []string{"hidden", "junk", "pkg"}
	for _, label := range forbidden {
		if labels[label] {
			t.Errorf("buildRepoNavGroupsJSON: %q must not appear in nav (hidden/skip dir), got:\n%v", label, labels)
		}
	}
}

func TestBuildExternalRegistry_ExcludesHiddenAndSkipDirs(t *testing.T) {
	root := buildPollutedRealm(t)

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

// navLabelsFromOpts flattens the nav tree to a label set, dropping tree shape.
func navLabelsFromOpts(t *testing.T, opts serve.NavOptions) map[string]bool {
	t.Helper()
	h := serve.NewAPINavHandler(opts)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/api/nav", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	h.ServeHTTP(w, req)

	var got struct {
		Groups []struct {
			Items []navLabelNode `json:"items"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal /api/nav response: %v; body=%s", err, w.Body.String())
	}

	labels := map[string]bool{}
	var walk func(nodes []navLabelNode)
	walk = func(nodes []navLabelNode) {
		for _, n := range nodes {
			labels[n.Label] = true
			walk(n.Children)
		}
	}
	for _, g := range got.Groups {
		walk(g.Items)
	}
	return labels
}

// navLabelNode is the label-bearing subset of the server's navNodeJSON.
type navLabelNode struct {
	Label    string         `json:"label"`
	Children []navLabelNode `json:"children,omitempty"`
}
