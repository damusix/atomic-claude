package serve_test

// nav_test.go — nav tree fixtures + behavior tests not already covered by the
// /api/nav shape tests in api_handlers_test.go (which reuse buildMinimalWikiRealm
// / buildRepoScope below). Shape coverage (group labels, member entries, folder
// tree, stale badges) lives in api_handlers_test.go; this file keeps the two
// behaviors that are otherwise untested by a shape assertion: the production
// (non-seam-injected) computeStaleness path, and the SSE-triggered request's
// staleness skip.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildMinimalWikiRealm creates a temp realm dir with:
//   - wiki/index.md containing a <wiki-scan> block with 2 members
//   - wiki/concerns/foo.md
//   - wiki/knowledge/bar.md
//
// Returns the realm root.
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

	// wiki/index.md with a <wiki-scan> block listing 2 members.
	indexContent := `<wiki-scan root="/realm" generated="2026-01-01">
<repo path="alpha" status="summarized" summary="repos/alpha.md"/>
<repo path="beta" status="pending"/>
</wiki-scan>

## Realm overview

Some narrative.
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	// wiki/concerns/foo.md
	writeFile(t, filepath.Join(wikiDir, "concerns", "foo.md"), "# Foo concern\n")

	// wiki/knowledge/bar.md
	writeFile(t, filepath.Join(wikiDir, "knowledge", "bar.md"), "# Bar knowledge\n")

	return root
}

// buildRepoScope creates a temp dir with some docs files and no wiki/.
func buildRepoScope(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "README.md"), "# Readme\n")
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(root, "docs", "api.md"), "# API\n")

	return root
}

// buildSelfIndexedRealm builds a wiki realm with one self-indexed member and
// returns (realmRoot, claudeMDPath). No code.toml — federation is absent,
// exactly like a realm that was never set up for code federation. Shared by
// codegraph_test.go's realm self-index coverage.
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
	// The member's own index (cd member; atomic code index).
	db := filepath.Join(realmRoot, memberPath, ".claude", ".atomic-index", "atomic.db")
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	return realmRoot, claudeMDPath
}

// TestAPINav_ProductionStalenessPath proves that with no StalenessFn injected,
// NewAPINavHandler's production computeStaleness path fires: a drifted member
// (listed in <wiki-scan> but missing on disk) badges stale, and a bucket with a
// non-empty diff badges too.
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

	// Empty baseline so every file in the bucket appears as Added (diff).
	writeFile(t, filepath.Join(wikiDir, ".buckets", "research", "baseline"), "")

	// "ghost" is listed but its dir does not exist → DRIFT removed ghost.
	indexContent := `<wiki-scan root="` + root + `" generated="2026-01-01">
<repo path="ghost" status="pending"/>
</wiki-scan>

<wiki-buckets>
<bucket name="research" path="` + bucketDir + `"/>
</wiki-buckets>
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	// No StalenessFn injected → production computeStaleness fires.
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

// TestAPINav_SSETriggeredRequestSkipsStaleness proves that a live-reload-triggered
// nav refetch (?live=1) skips the (git-subprocess-backed) StalenessFn, while an
// ordinary request still calls it.
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
