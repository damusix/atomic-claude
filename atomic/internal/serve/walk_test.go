package serve_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
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

func graphNodeSet(root string) map[string]bool {
	set := map[string]bool{}
	for _, n := range serve.BuildLinkGraph(root).Nodes() {
		set[n] = true
	}
	return set
}

func assertNodes(t *testing.T, nodes map[string]bool, want, forbidden []string) {
	t.Helper()
	for _, w := range want {
		if !nodes[w] {
			t.Errorf("expected %q in graph nodes, got %v", w, nodes)
		}
	}
	for _, f := range forbidden {
		if nodes[f] {
			t.Errorf("%q must not be in graph nodes", f)
		}
	}
}

// Pointer cards and follow-ups live in the state root; wiki links cite them.
func TestBuildLinkGraph_IncludesDefaultStateRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".atomic", "rules", "wiki", "x.md"), "# Card\n")
	writeFile(t, filepath.Join(root, ".atomic", "project", "followups", "y.md"), "# Followup\n")
	writeFile(t, filepath.Join(root, ".atomic", ".cache", "z.md"), "# Cache\n")
	writeFile(t, filepath.Join(root, ".git", "h.md"), "# Git\n")
	writeFile(t, filepath.Join(root, ".vscode", "v.md"), "# VSCode\n")
	writeFile(t, filepath.Join(root, ".foo", "f.md"), "# Foo\n")

	assertNodes(t, graphNodeSet(root),
		[]string{".atomic/rules/wiki/x.md", ".atomic/project/followups/y.md"},
		[]string{".atomic/.cache/z.md", ".git/h.md", ".vscode/v.md", ".foo/f.md"})
}

func TestBuildLinkGraph_IncludesNestedCustomStateRoot(t *testing.T) {
	t.Setenv(config.StateDirEnvVar, ".state/atomic")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".state", "atomic", "rules", "wiki", "x.md"), "# Card\n")
	writeFile(t, filepath.Join(root, ".state", "atomic", ".cache", "z.md"), "# Cache\n")
	writeFile(t, filepath.Join(root, ".state", "other", "o.md"), "# Other\n")
	writeFile(t, filepath.Join(root, ".state", "loose.md"), "# Loose\n")
	writeFile(t, filepath.Join(root, ".atomic", "rules", "wiki", "n.md"), "# Not the root\n")

	assertNodes(t, graphNodeSet(root),
		[]string{".state/atomic/rules/wiki/x.md"},
		[]string{".state/atomic/.cache/z.md", ".state/other/o.md", ".state/loose.md", ".atomic/rules/wiki/n.md"})
}

// .claude is walked on its own merit, so a state root nested in it must not
// fence off its other docs.
func TestBuildLinkGraph_StateRootUnderClaudeKeepsClaudeDocs(t *testing.T) {
	t.Setenv(config.StateDirEnvVar, ".claude/atomic")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude", "atomic", "rules", "wiki", "x.md"), "# Card\n")
	writeFile(t, filepath.Join(root, ".claude", "docs", "a.md"), "# Doc\n")

	assertNodes(t, graphNodeSet(root),
		[]string{".claude/atomic/rules/wiki/x.md", ".claude/docs/a.md"}, nil)
}

// Claude loads the cards through a .claude/rules/atomic link into the state
// root; walking .claude must not list them a second time through it.
func TestBuildLinkGraph_ClaudeCardLinkDoesNotDuplicateCards(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".atomic", "rules", "wiki", "x.md"), "# Card\n")
	writeFile(t, filepath.Join(root, ".claude", "skills", "notes-management", "SKILL.md"), "# Skill\n")
	if err := os.MkdirAll(filepath.Join(root, ".claude", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", ".atomic", "rules"), filepath.Join(root, ".claude", "rules", "atomic")); err != nil {
		t.Fatal(err)
	}

	assertNodes(t, graphNodeSet(root),
		[]string{".atomic/rules/wiki/x.md", ".claude/skills/notes-management/SKILL.md"},
		[]string{".claude/rules/atomic/wiki/x.md"})
}

func TestBuildLinkGraph_StateRootUnderBuildDump(t *testing.T) {
	t.Setenv(config.StateDirEnvVar, "tmp/atomic")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tmp", "atomic", "rules", "wiki", "x.md"), "# Card\n")
	writeFile(t, filepath.Join(root, "tmp", "other", "o.md"), "# Other\n")
	writeFile(t, filepath.Join(root, "tmp", "junk.md"), "# Junk\n")

	assertNodes(t, graphNodeSet(root),
		[]string{"tmp/atomic/rules/wiki/x.md"},
		[]string{"tmp/other/o.md", "tmp/junk.md"})
}

// A long-running serve must follow `atomic state migrate` without a restart.
func TestBuildLinkGraph_FollowsMigratedStateRoot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".atomic", "rules", "wiki", "a.md"), "# Old\n")
	writeFile(t, filepath.Join(root, ".state", "atomic", "rules", "wiki", "b.md"), "# New\n")

	assertNodes(t, graphNodeSet(root),
		[]string{".atomic/rules/wiki/a.md"}, []string{".state/atomic/rules/wiki/b.md"})

	if _, err := config.PersistStateSelection(root, ".state/atomic"); err != nil {
		t.Fatal(err)
	}
	assertNodes(t, graphNodeSet(root),
		[]string{".state/atomic/rules/wiki/b.md"}, []string{".atomic/rules/wiki/a.md"})
}

// Serve only visits members; leftover-state warnings belong to verbs run there.
func TestBuildLinkGraph_MemberLeftoverStateIsSilent(t *testing.T) {
	var warnings bytes.Buffer
	defer config.SetStateWarnOutForTest(&warnings)()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "member", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "member", ".atomic", "rules", "wiki", "c.md"), "# Card\n")
	writeFile(t, filepath.Join(root, "member", ".claude", "project", "p.md"), "# Leftover\n")

	graphNodeSet(root)
	if warnings.Len() != 0 {
		t.Errorf("walk printed state warnings: %q", warnings.String())
	}
}

// Each realm member resolves its own state root.
func TestBuildLinkGraph_IncludesMemberStateRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "member", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "member", ".atomic", "rules", "wiki", "c.md"), "# Card\n")
	writeFile(t, filepath.Join(root, "member", ".foo", "f.md"), "# Foo\n")

	assertNodes(t, graphNodeSet(root),
		[]string{"member/.atomic/rules/wiki/c.md"},
		[]string{"member/.foo/f.md"})
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
