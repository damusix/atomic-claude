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

type graphMembersResp struct {
	Members []struct {
		Key     string `json:"key"`
		Prefix  string `json:"prefix"`
		Indexed bool   `json:"indexed"`
	} `json:"members"`
}

func TestCodeGraphMembers_RepoScope_Unindexed(t *testing.T) {
	realmRoot := t.TempDir()

	h := serve.NewCodeGraphMembersHandler(serve.CodeGraphOptions{RealmRoot: realmRoot})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/members", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	var resp graphMembersResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body: %s", err, rr.Body.String())
	}
	if len(resp.Members) != 1 {
		t.Fatalf("want 1 member, got %d: %+v", len(resp.Members), resp.Members)
	}
	if resp.Members[0].Prefix != "" {
		t.Errorf("repo scope Prefix = %q, want empty", resp.Members[0].Prefix)
	}
	if resp.Members[0].Indexed {
		t.Error("expected Indexed=false with no local db")
	}
}

func TestCodeGraphMembers_RepoScope_Indexed(t *testing.T) {
	realmRoot := t.TempDir()
	db := filepath.Join(realmRoot, ".claude", ".atomic-index", "atomic.db")
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	h := serve.NewCodeGraphMembersHandler(serve.CodeGraphOptions{RealmRoot: realmRoot})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/members", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp graphMembersResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Members) != 1 || !resp.Members[0].Indexed {
		t.Errorf("expected 1 indexed member, got %+v", resp.Members)
	}
}

// Federation lists every declared member regardless of index state, so the
// picker can reach an unbuilt one. The self-index path omits them instead.
func TestCodeGraphMembers_RealmFederation_ListsBuiltAndUnbuilt(t *testing.T) {
	realmRoot := t.TempDir()

	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n\n<wiki-scan generated=\"2026-01-01\" root=\""+realmRoot+"\">\n</wiki-scan>\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})

	buildCodeTOML(t, realmRoot, []struct{ key, path string }{
		{"alpha", "repos/alpha"},
		{"beta", "repos/beta"},
	})

	// The federation db is never built here, so memberDB falls back to this.
	alphaDB := filepath.Join(realmRoot, "repos", "alpha", ".claude", ".atomic-index", "atomic.db")
	if err := os.MkdirAll(filepath.Dir(alphaDB), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(alphaDB, []byte("x"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	// beta gets no index at all: declared but unbuilt.

	h := serve.NewCodeGraphMembersHandler(serve.CodeGraphOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/members", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var resp graphMembersResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body: %s", err, rr.Body.String())
	}
	if len(resp.Members) != 2 {
		t.Fatalf("want 2 members, got %d: %+v", len(resp.Members), resp.Members)
	}
	byPrefix := map[string]bool{}
	for _, m := range resp.Members {
		byPrefix[m.Prefix] = m.Indexed
	}
	indexedAlpha, ok := byPrefix["repos/alpha"]
	if !ok || !indexedAlpha {
		t.Errorf("expected repos/alpha indexed=true, got %+v", resp.Members)
	}
	indexedBeta, ok := byPrefix["repos/beta"]
	if !ok || indexedBeta {
		t.Errorf("expected repos/beta indexed=false (declared but unbuilt), got %+v", resp.Members)
	}
}
