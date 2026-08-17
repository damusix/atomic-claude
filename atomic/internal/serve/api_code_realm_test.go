package serve_test

// A realm with no <code-index> federation, where a member was indexed the natural
// way. Search must find that member, and the code modal must open the member's
// own db and query it with the member-relative path.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// realmRecordingEngine captures the path passed to GetNodesInFile so the test
// can assert serve strips the member prefix before querying the member's index.
type realmRecordingEngine struct {
	gotFilePath string
	nodes       []types.Node
}

func (e *realmRecordingEngine) SearchNodes(context.Context, types.SearchOptions) ([]types.SearchResult, error) {
	return nil, nil
}
func (e *realmRecordingEngine) GetNode(context.Context, string) (types.Node, error) {
	return types.Node{}, nil
}
func (e *realmRecordingEngine) GetNodesByName(context.Context, string, types.NodeKind) ([]types.Node, error) {
	return nil, nil
}
func (e *realmRecordingEngine) GetCallers(context.Context, string, int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (e *realmRecordingEngine) GetCallees(context.Context, string, int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (e *realmRecordingEngine) GetImpactRadius(context.Context, string, int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (e *realmRecordingEngine) GetFiles(context.Context) ([]types.FileRecord, error) {
	return nil, nil
}
func (e *realmRecordingEngine) GetNodesInFile(_ context.Context, path string) ([]types.Node, error) {
	e.gotFilePath = path
	return e.nodes, nil
}
func (e *realmRecordingEngine) GetNodesByKind(context.Context, types.NodeKind) ([]types.Node, error) {
	return nil, nil
}
func (e *realmRecordingEngine) GetOutgoingEdges(context.Context, string) ([]types.Edge, error) {
	return nil, nil
}
func (e *realmRecordingEngine) GetAllNodes(context.Context) ([]types.Node, error) { return nil, nil }
func (e *realmRecordingEngine) GetAllEdges(context.Context) ([]types.Edge, error) { return nil, nil }
func (e *realmRecordingEngine) Close()                                            {}

func TestAPICodeFile_RealmSelfIndex_OpensMemberDBWithRelativePath(t *testing.T) {
	realmRoot, claudeMDPath := buildSelfIndexedRealm(t, "monorepo")

	rec := &realmRecordingEngine{
		nodes: []types.Node{{ID: "fn-x", Name: "makeHandler", Kind: types.NodeKindFunction, FilePath: "Apps/workers/src/rebuild-meili.ts", StartLine: 17}},
	}
	var openedDB string
	provider := func(_ context.Context, _ /*projectRoot*/, dbPath string) (serve.CodeEngine, error) {
		openedDB = dbPath
		return rec, nil
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      realmRoot,
		ClaudeMDPath:   claudeMDPath,
		EngineProvider: provider,
	})

	req := httptest.NewRequest(http.MethodGet,
		"/api/code/file?path=monorepo/Apps/workers/src/rebuild-meili.ts", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d; body: %s", rr.Code, rr.Body.String())
	}
	// The realm-root db does not exist; only the member's self-index does.
	wantDB := filepath.Join(realmRoot, "monorepo", ".claude", ".atomic-index", "atomic.db")
	if openedDB != wantDB {
		t.Errorf("opened db %q, want member self-index %q", openedDB, wantDB)
	}
	// The member prefix must be stripped before the member's index sees it.
	if rec.gotFilePath != "Apps/workers/src/rebuild-meili.ts" {
		t.Errorf("queried %q, want member-relative path", rec.gotFilePath)
	}

	var got struct {
		Member string `json:"member"`
		Nodes  []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Name != "makeHandler" {
		t.Errorf("expected the symbol in nodes; got %+v", got.Nodes)
	}
	// Carried so the frontend's drill-downs open the same member db.
	if got.Member != "monorepo" {
		t.Errorf("member: got %q, want %q", got.Member, "monorepo")
	}
}

func TestAPICodeSearch_RealmSelfIndex_FindsMemberWithPrefix(t *testing.T) {
	realmRoot, claudeMDPath := buildSelfIndexedRealm(t, "monorepo")

	// Hits only on the member's own db path, so a federation db would find nothing.
	wantDB := filepath.Join(realmRoot, "monorepo", ".claude", ".atomic-index", "atomic.db")
	searchFn := func(_ context.Context, _ /*memberPath*/, dbPath, _ string) ([]types.SearchResult, error) {
		if dbPath != wantDB {
			return nil, nil
		}
		return []types.SearchResult{
			{Node: types.Node{ID: "fn-x", Name: "makeHandler", Kind: types.NodeKindFunction, FilePath: "Apps/workers/src/rebuild-meili.ts", StartLine: 17}},
		}, nil
	}

	h := serve.NewAPICodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/search?q=makeHandler", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d; body: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Members []struct {
			Prefix  string `json:"prefix"`
			Indexed bool   `json:"indexed"`
			Results []struct {
				Name      string `json:"name"`
				FilePath  string `json:"filePath"`
				StartLine int    `json:"startLine"`
			} `json:"results"`
		} `json:"members"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	var hit *struct {
		Name      string `json:"name"`
		FilePath  string `json:"filePath"`
		StartLine int    `json:"startLine"`
	}
	var hitPrefix string
	for _, m := range got.Members {
		for i := range m.Results {
			if m.Results[i].Name == "makeHandler" {
				hit = &m.Results[i]
				hitPrefix = m.Prefix
			}
		}
	}
	if hit == nil {
		t.Fatalf("expected the self-indexed member's result; body: %s", rr.Body.String())
	}
	// The frontend prefixes filePath with this to build /file/ links.
	if hitPrefix != "monorepo" {
		t.Errorf("hit grouped under %q, want %q", hitPrefix, "monorepo")
	}
	if hit.FilePath != "Apps/workers/src/rebuild-meili.ts" || hit.StartLine != 17 {
		t.Errorf("result loc: got %q:%d", hit.FilePath, hit.StartLine)
	}
}

// Not-indexed is soft state here too: 200 with a reason, not an error envelope.
func TestAPICodeSchema_NotIndexed_Degraded(t *testing.T) {
	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot: t.TempDir(), // no index anywhere
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/schema", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (soft state, not an error), got %d; body: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Tables   []any  `json:"tables"`
		Degraded string `json:"degraded"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.Degraded == "" {
		t.Error("expected non-empty degraded reason")
	}
	if len(got.Tables) != 0 {
		t.Errorf("expected no tables, got %+v", got.Tables)
	}
}

func (r *realmRecordingEngine) IndexAll(context.Context) error { return nil }
