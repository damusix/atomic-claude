package serve_test

// api_handlers_test.go — /api/page, /api/file, /api/rail, /api/nav JSON
// shape tests. TDD: written to assert the shapes pinned in the spec's
// ## API contracts table before/alongside implementation.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── /api/page ───────────────────────────────────────────────────────────────

func TestAPIPage_File(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "---\ntitle: Guide\n---\n# Guide\n\nBody text.\n")

	graph := serve.BuildLinkGraph(root)
	handler := serve.NewAPIPageHandler(root, graph, "README.md")

	req := httptest.NewRequest(http.MethodGet, "/api/page/docs/guide.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got struct {
		HTML       string `json:"html"`
		Title      string `json:"title"`
		RelPath    string `json:"relpath"`
		HasMermaid bool   `json:"hasMermaid"`
		Breadcrumb []struct {
			Label  string `json:"label"`
			Path   string `json:"path"`
			Folder bool   `json:"folder"`
		} `json:"breadcrumb"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if got.RelPath != "docs/guide.md" {
		t.Errorf("relpath: got %q, want docs/guide.md", got.RelPath)
	}
	if got.Title != "guide.md" {
		t.Errorf("title: got %q, want guide.md", got.Title)
	}
	if got.HTML == "" {
		t.Error("html: got empty, want rendered body")
	}
	if len(got.Breadcrumb) != 2 {
		t.Fatalf("breadcrumb: got %d segments, want 2 (docs, guide.md); got=%+v", len(got.Breadcrumb), got.Breadcrumb)
	}
	if got.Breadcrumb[0].Label != "docs" || !got.Breadcrumb[0].Folder {
		t.Errorf("breadcrumb[0]: got %+v, want {docs, folder:true}", got.Breadcrumb[0])
	}
	if got.Breadcrumb[1].Label != "guide.md" || got.Breadcrumb[1].Folder {
		t.Errorf("breadcrumb[1]: got %+v, want {guide.md, folder:false}", got.Breadcrumb[1])
	}
}

func TestAPIPage_Directory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "a.md"), "# A\n")
	writeFile(t, filepath.Join(root, "docs", "b.md"), "# B\n")
	// No index file (README/index) in docs/ — must produce a dir listing, not
	// a rendered page.

	graph := serve.BuildLinkGraph(root)
	handler := serve.NewAPIPageHandler(root, graph, "README.md")

	req := httptest.NewRequest(http.MethodGet, "/api/page/docs", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var got struct {
		Dir     bool   `json:"dir"`
		RelPath string `json:"relpath"`
		Entries []struct {
			Name    string `json:"name"`
			RelPath string `json:"relpath"`
			Folder  bool   `json:"folder"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if !got.Dir {
		t.Error("dir: got false, want true")
	}
	if got.RelPath != "docs" {
		t.Errorf("relpath: got %q, want docs", got.RelPath)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries: got %d, want 2; got=%+v", len(got.Entries), got.Entries)
	}
	if got.Entries[0].Name != "a" || got.Entries[0].RelPath != "docs/a.md" || got.Entries[0].Folder {
		t.Errorf("entries[0]: got %+v, want {a, docs/a.md, folder:false}", got.Entries[0])
	}
}

func TestAPIPage_NotFound(t *testing.T) {
	root := t.TempDir()
	graph := serve.BuildLinkGraph(root)
	handler := serve.NewAPIPageHandler(root, graph, "README.md")

	req := httptest.NewRequest(http.MethodGet, "/api/page/missing.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rr.Code)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.Error == "" {
		t.Error("error: got empty, want a message")
	}
}

func TestAPIPage_TraversalRejected(t *testing.T) {
	root := t.TempDir()
	graph := serve.BuildLinkGraph(root)
	handler := serve.NewAPIPageHandler(root, graph, "README.md")

	req := httptest.NewRequest(http.MethodGet, "/api/page/../../../../etc/passwd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404 (traversal rejected)", rr.Code)
	}
}

// ─── /api/file ───────────────────────────────────────────────────────────────

func TestAPIFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	handler := serve.NewAPIFileHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/api/file/main.go", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got struct {
		HTML  string `json:"html"`
		Title string `json:"title"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.Path != "main.go" || got.Title != "main.go" {
		t.Errorf("got %+v, want path/title=main.go", got)
	}
	if got.HTML == "" {
		t.Error("html: got empty, want chroma-highlighted markup")
	}
}

func TestAPIFile_NotFound(t *testing.T) {
	root := t.TempDir()
	handler := serve.NewAPIFileHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/api/file/missing.go", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rr.Code)
	}
}

// ─── /api/rail ───────────────────────────────────────────────────────────────

func TestAPIRail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\nfoo: bar\n---\n# A\n\n[link to b](b.md)\n")
	writeFile(t, filepath.Join(root, "b.md"), "# B\n\n[back to a](a.md)\n")

	graph := serve.BuildLinkGraph(root)
	handler := serve.NewAPIRailHandler(root, graph)

	req := httptest.NewRequest(http.MethodGet, "/api/rail/a.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var got struct {
		RelPath    string `json:"relpath"`
		Orphan     bool   `json:"orphan"`
		Properties []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"properties"`
		Out []struct {
			Target       string `json:"target"`
			ResolvedPath string `json:"resolvedPath"`
			Broken       bool   `json:"broken"`
		} `json:"out"`
		In []struct {
			Path string `json:"path"`
		} `json:"in"`
		GraphDataURL string `json:"graphDataURL"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if got.RelPath != "a.md" {
		t.Errorf("relpath: got %q, want a.md", got.RelPath)
	}
	if got.Orphan {
		t.Error("orphan: got true, want false (b.md links back to a.md)")
	}
	if len(got.Properties) != 1 || got.Properties[0].Key != "foo" || got.Properties[0].Value != "bar" {
		t.Errorf("properties: got %+v, want [{foo bar}]", got.Properties)
	}
	if len(got.Out) != 1 || got.Out[0].Target != "b.md" || got.Out[0].ResolvedPath != "b.md" {
		t.Errorf("out: got %+v, want one edge to b.md", got.Out)
	}
	if len(got.In) != 1 || got.In[0].Path != "b.md" {
		t.Errorf("in: got %+v, want one backlink from b.md", got.In)
	}
	if got.GraphDataURL == "" {
		t.Error("graphDataURL: got empty")
	}
}

func TestAPIRail_Orphan(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "solo.md"), "# Solo\n")

	graph := serve.BuildLinkGraph(root)
	handler := serve.NewAPIRailHandler(root, graph)

	req := httptest.NewRequest(http.MethodGet, "/api/rail/solo.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var got struct {
		Orphan     bool         `json:"orphan"`
		Properties []propKVWire `json:"properties"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if !got.Orphan {
		t.Error("orphan: got false, want true (no page links to solo.md)")
	}
	if got.Properties != nil {
		t.Errorf("properties: got %+v, want nil (no frontmatter)", got.Properties)
	}
}

// propKVWire mirrors the wire shape of a Properties entry for unmarshal targets
// that need it (kept local — the server-side propKV type is unexported).
type propKVWire struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	IsURL  bool   `json:"isURL"`
	IsJSON bool   `json:"isJSON"`
}

func TestAPIRail_NotFound(t *testing.T) {
	root := t.TempDir()
	graph := serve.BuildLinkGraph(root)
	handler := serve.NewAPIRailHandler(root, graph)

	req := httptest.NewRequest(http.MethodGet, "/api/rail/missing.md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rr.Code)
	}
}

// ─── /api/nav ────────────────────────────────────────────────────────────────

type navNodeWire struct {
	Label    string        `json:"label"`
	RelPath  string        `json:"relpath"`
	Stale    bool          `json:"stale"`
	Children []navNodeWire `json:"children"`
}

type navGroupWire struct {
	Name  string        `json:"name"`
	Items []navNodeWire `json:"items"`
}

type navResponseWire struct {
	Scope  string         `json:"scope"`
	Groups []navGroupWire `json:"groups"`
}

func TestAPINav_RepoScope_FolderChildren(t *testing.T) {
	root := buildRepoScope(t)
	writeFile(t, filepath.Join(root, "docs", "nested", "deep.md"), "# Deep\n")

	handler := serve.NewAPINavHandler(serve.NavOptions{
		RealmRoot:    root,
		IsRealmScope: false,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var got navResponseWire
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if got.Scope != "repo" {
		t.Errorf("scope: got %q, want repo", got.Scope)
	}

	var docsGroup *navGroupWire
	for i := range got.Groups {
		if got.Groups[i].Name == "Docs" {
			docsGroup = &got.Groups[i]
		}
	}
	if docsGroup == nil {
		t.Fatalf("no Docs group in %+v", got.Groups)
	}

	// README.md is a top-level leaf (RelPath set, no Children).
	var readme *navNodeWire
	var nestedFolder *navNodeWire
	for i := range docsGroup.Items {
		switch docsGroup.Items[i].Label {
		case "README":
			readme = &docsGroup.Items[i]
		case "nested":
			nestedFolder = &docsGroup.Items[i]
		}
	}
	if readme == nil || readme.RelPath != "README.md" || len(readme.Children) != 0 {
		t.Errorf("README leaf: got %+v", readme)
	}
	if nestedFolder == nil {
		t.Fatalf("nested folder node not found in %+v", docsGroup.Items)
	}
	if len(nestedFolder.Children) != 1 || nestedFolder.Children[0].RelPath != "docs/nested/deep.md" {
		t.Errorf("nested folder children: got %+v, want [{deep docs/nested/deep.md}]", nestedFolder.Children)
	}
}

func TestAPINav_RealmScope_StaleBadge(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	handler := serve.NewAPINavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
		StalenessFn: func(_, _ string) (map[string]bool, map[string]bool) {
			return map[string]bool{"alpha": true}, map[string]bool{}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var got navResponseWire
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if got.Scope != "realm" {
		t.Errorf("scope: got %q, want realm", got.Scope)
	}

	var reposGroup *navGroupWire
	for i := range got.Groups {
		if got.Groups[i].Name == "Repos" {
			reposGroup = &got.Groups[i]
		}
	}
	if reposGroup == nil {
		t.Fatalf("no Repos group in %+v", got.Groups)
	}

	var alpha *navNodeWire
	for i := range reposGroup.Items {
		if reposGroup.Items[i].Label == "alpha" {
			alpha = &reposGroup.Items[i]
		}
	}
	if alpha == nil {
		t.Fatalf("alpha member not found in %+v", reposGroup.Items)
	}
	if !alpha.Stale {
		t.Errorf("alpha.Stale: got false, want true")
	}
}
