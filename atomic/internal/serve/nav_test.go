package serve_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildMinimalWikiRealm seeds a two-member <wiki-scan> plus one concern and one
// knowledge page, the smallest realm the nav groups can render.
func buildMinimalWikiRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(filepath.Join(wikiDir, "concerns"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "repos"), 0o755); err != nil {
		t.Fatal(err)
	}

	indexContent := `<wiki-scan root="/realm" generated="2026-01-01">
<repo path="alpha" status="summarized" summary="repos/alpha.md"/>
<repo path="beta" status="pending"/>
</wiki-scan>

## Realm overview

Some narrative.
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	writeFile(t, filepath.Join(wikiDir, "concerns", "foo.md"), "# Foo concern\n")

	writeFile(t, filepath.Join(wikiDir, "knowledge", "bar.md"), "# Bar knowledge\n")

	return root
}

// buildRepoScope has docs files and deliberately no wiki/, so scope resolves to
// repo rather than realm.
func buildRepoScope(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "README.md"), "# Readme\n")
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(root, "docs", "api.md"), "# API\n")

	return root
}

// No code.toml, so this is a realm that was never set up for code federation and
// the member's own index is the only one available.
func buildSelfIndexedRealm(t *testing.T, memberPath string) (string, string) {
	t.Helper()
	realmRoot := t.TempDir()
	wikiIndex := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndex,
		"# wiki\n\n<wiki-scan generated=\"2026-01-01\" root=\""+realmRoot+"\">\n"+
			"<repo path=\""+memberPath+"\" status=\"summarized\" summary=\"wiki/repos/x.md\">\n"+
			"</wiki-scan>\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndex})
	// What `cd member && atomic code index` would have written.
	db := filepath.Join(realmRoot, memberPath, ".claude", ".atomic-index", "atomic.db")
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	return realmRoot, claudeMDPath
}

// Covers the real computeStaleness, which every other nav test replaces with an
// injected seam.
func TestAPINav_ProductionStalenessPath(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")

	for _, sub := range []string{"concerns", "knowledge", "repos", ".buckets/research"} {
		if err := os.MkdirAll(filepath.Join(wikiDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	bucketDir := filepath.Join(root, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bucketDir, "note.md"), "# Research note\n")

	// Empty baseline, so every bucket file reads as Added.
	writeFile(t, filepath.Join(wikiDir, ".buckets", "research", "baseline"), "")

	// Listed but absent on disk, which is what drift looks like.
	indexContent := `<wiki-scan root="` + root + `" generated="2026-01-01">
<repo path="ghost" status="pending"/>
</wiki-scan>

<wiki-buckets>
<bucket name="research" path="` + bucketDir + `"/>
</wiki-buckets>
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	// No StalenessFn, so the production path runs.
	handler := serve.NewAPINavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(wikiDir, "index.md"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rr.Code, rr.Body.String())
	}

	var got struct {
		Groups []struct {
			Name  string `json:"name"`
			Items []struct {
				Label    string `json:"label"`
				Stale    bool   `json:"stale"`
				Children []struct {
					Label string `json:"label"`
					Stale bool   `json:"stale"`
				} `json:"children"`
			} `json:"items"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	var ghostStale, bucketDiff bool
	for _, g := range got.Groups {
		if g.Name == "Repos" {
			for _, it := range g.Items {
				if it.Label == "ghost" && it.Stale {
					ghostStale = true
				}
			}
		}
		if g.Name == "Buckets" {
			for _, it := range g.Items {
				if it.Label == "research" && it.Stale {
					bucketDiff = true
				}
			}
		}
	}
	if !ghostStale {
		t.Errorf("production path: expected stale=true for drifted member 'ghost'; groups=%+v", got.Groups)
	}
	if !bucketDiff {
		t.Errorf("production path: expected stale=true (diff) for bucket 'research'; groups=%+v", got.Groups)
	}
}

// StalenessFn shells out to git, too costly to run on every live-reload refetch.
func TestAPINav_SSETriggeredRequestSkipsStaleness(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	var calls int
	handler := serve.NewAPINavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
		StalenessFn: func(_, _ string) (map[string]bool, map[string]bool) {
			calls++
			return map[string]bool{}, map[string]bool{}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nav?live=1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if calls != 0 {
		t.Errorf("SSE-triggered nav request must skip StalenessFn, got %d calls", calls)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/nav", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if calls != 1 {
		t.Errorf("ordinary nav request must still call StalenessFn, got %d calls", calls)
	}
}
