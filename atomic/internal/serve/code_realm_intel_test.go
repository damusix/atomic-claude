package serve_test

// code_realm_intel_test.go — the realm self-index path. A wiki realm with NO
// <code-index> federation, where a member was indexed the natural way
// (cd member; atomic code index → <member>/.claude/.atomic-index/atomic.db).
// serve must (1) find that member in code search and (2) open the member's own
// db for the code modal's intel pane, querying it with the MEMBER-relative path.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// recordingEngine captures the path passed to GetNodesInFile so the test can
// assert serve strips the member prefix before querying the member's index.
type recordingEngine struct {
	gotFilePath string
	nodes       []types.Node
}

func (e *recordingEngine) SearchNodes(context.Context, types.SearchOptions) ([]types.SearchResult, error) {
	return nil, nil
}
func (e *recordingEngine) GetNode(context.Context, string) (types.Node, error) {
	return types.Node{}, nil
}
func (e *recordingEngine) GetNodesByName(context.Context, string, types.NodeKind) ([]types.Node, error) {
	return nil, nil
}
func (e *recordingEngine) GetCallers(context.Context, string, int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (e *recordingEngine) GetCallees(context.Context, string, int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (e *recordingEngine) GetImpactRadius(context.Context, string, int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (e *recordingEngine) GetFiles(context.Context) ([]types.FileRecord, error) { return nil, nil }
func (e *recordingEngine) GetNodesInFile(_ context.Context, path string) ([]types.Node, error) {
	e.gotFilePath = path
	return e.nodes, nil
}
func (e *recordingEngine) GetNodesByKind(context.Context, types.NodeKind) ([]types.Node, error) {
	return nil, nil
}
func (e *recordingEngine) GetOutgoingEdges(context.Context, string) ([]types.Edge, error) {
	return nil, nil
}
func (e *recordingEngine) Close() {}

// buildSelfIndexedRealm builds a wiki realm with one self-indexed member and
// returns (realmRoot, claudeMDPath). No code.toml — federation is absent, exactly
// like a realm that was never set up for code federation.
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

func TestCodeModalIntel_RealmSelfIndex_OpensMemberDBWithRelativePath(t *testing.T) {
	realmRoot, claudeMDPath := buildSelfIndexedRealm(t, "monorepo")

	rec := &recordingEngine{
		nodes: []types.Node{{ID: "fn-x", Name: "makeHandler", Kind: types.NodeKindFunction, FilePath: "Apps/workers/src/rebuild-meili.ts", StartLine: 17}},
	}
	var openedDB string
	provider := func(_ context.Context, _ /*projectRoot*/, dbPath string) (serve.CodeEngine, error) {
		openedDB = dbPath
		return rec, nil
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      realmRoot,
		ClaudeMDPath:   claudeMDPath,
		EngineProvider: provider,
	})

	req := httptest.NewRequest(http.MethodGet,
		"/code/file?path=monorepo/Apps/workers/src/rebuild-meili.ts", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d; body: %s", rr.Code, rr.Body.String())
	}
	// Opened the member's own self-index, not the (absent) realm-root db.
	wantDB := filepath.Join(realmRoot, "monorepo", ".claude", ".atomic-index", "atomic.db")
	if openedDB != wantDB {
		t.Errorf("opened db %q, want member self-index %q", openedDB, wantDB)
	}
	// Queried with the MEMBER-relative path (prefix stripped).
	if rec.gotFilePath != "Apps/workers/src/rebuild-meili.ts" {
		t.Errorf("queried %q, want member-relative path", rec.gotFilePath)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "makeHandler") {
		t.Errorf("expected the symbol to render; body: %s", body)
	}
	// Drill-down chips must carry member= so callers/callees open the same db.
	if !strings.Contains(body, "member=monorepo") {
		t.Errorf("expected member= on drill-down links; body: %s", body)
	}
}

func TestCodeSearch_RealmSelfIndex_FindsMemberAndPrefixesLinks(t *testing.T) {
	realmRoot, claudeMDPath := buildSelfIndexedRealm(t, "monorepo")

	// The fake returns a hit only when handed the member's own db path — proving
	// discovery routed the search to the self-index, not a federation db.
	wantDB := filepath.Join(realmRoot, "monorepo", ".claude", ".atomic-index", "atomic.db")
	searchFn := func(_ context.Context, _ /*memberPath*/, dbPath, _ string) ([]types.SearchResult, error) {
		if dbPath != wantDB {
			return nil, nil
		}
		return []types.SearchResult{fakeResult("makeHandler", "function", "Apps/workers/src/rebuild-meili.ts", 17)}, nil
	}

	h := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=makeHandler", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "makeHandler") {
		t.Errorf("expected the self-indexed member's result; body: %s", body)
	}
	// Link prefixed with the member path so /file/ resolves it in the realm.
	if !strings.Contains(body, "/file/monorepo/Apps/workers/src/rebuild-meili.ts#L17") {
		t.Errorf("expected realm-prefixed file link; body: %s", body)
	}
	// Grouped under the member header.
	if !strings.Contains(body, "[monorepo]") {
		t.Errorf("expected [monorepo] group header; body: %s", body)
	}
}
