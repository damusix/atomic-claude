package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/db"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

func TestAPICodeNode_Found(t *testing.T) {
	fake := &fakeCodeEngine{
		node: types.Node{
			ID:        "fn-abc",
			Name:      "myFunc",
			Kind:      types.NodeKindFunction,
			FilePath:  "pkg/util.go",
			StartLine: 42,
			Signature: "func myFunc(x int) error",
			Language:  types.LanguageGo,
			Docstring: "myFunc does a thing.",
		},
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/node?id=fn-abc", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got struct {
		Member string `json:"member"`
		Node   struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			FilePath  string `json:"filePath"`
			StartLine int    `json:"startLine"`
			Signature string `json:"signature"`
			Language  string `json:"language"`
			Docstring string `json:"docstring"`
		} `json:"node"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if got.Node.ID != "fn-abc" || got.Node.Name != "myFunc" || got.Node.Kind != "function" {
		t.Errorf("node identity: got %+v", got.Node)
	}
	if got.Node.FilePath != "pkg/util.go" || got.Node.StartLine != 42 {
		t.Errorf("node location: got %+v", got.Node)
	}
	if got.Node.Signature != "func myFunc(x int) error" {
		t.Errorf("signature: got %q", got.Node.Signature)
	}
	if got.Node.Language != "go" {
		t.Errorf("language: got %q", got.Node.Language)
	}
	if got.Node.Docstring != "myFunc does a thing." {
		t.Errorf("docstring: got %q", got.Node.Docstring)
	}
}

func TestAPICodeNode_MissingID_400(t *testing.T) {
	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(&fakeCodeEngine{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/node", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rr.Code, rr.Body.String())
	}
	assertErrorEnvelope(t, rr)
}

func TestAPICodeNode_Unresolvable_404(t *testing.T) {
	fake := &fakeCodeEngine{nodeErr: errNotFound}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/node?id=missing", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
	assertErrorEnvelope(t, rr)
}

func TestAPICodeCallers_Shape(t *testing.T) {
	callers := types.Subgraph{
		Nodes: map[string]types.Node{
			"fn-abc":   {ID: "fn-abc", Name: "myFunc", Kind: types.NodeKindFunction, FilePath: "pkg/util.go", StartLine: 42},
			"caller-1": {ID: "caller-1", Name: "doSomething", Kind: types.NodeKindFunction, FilePath: "cmd/main.go", StartLine: 10},
		},
		Roots: []string{"fn-abc"},
		Edges: []types.Edge{
			{Source: "caller-1", Target: "fn-abc", Kind: types.EdgeKindCalls},
		},
	}
	fake := &fakeCodeEngine{callers: callers}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/callers?id=fn-abc", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var got struct {
		Member string `json:"member"`
		Root   struct {
			ID string `json:"id"`
		} `json:"root"`
		Edges []struct {
			Kind   string `json:"kind"`
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"edges"`
		Nodes map[string]struct {
			Name string `json:"name"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if got.Root.ID != "fn-abc" {
		t.Errorf("root: got %+v, want id=fn-abc", got.Root)
	}
	if len(got.Edges) != 1 || got.Edges[0].Kind != "calls" || got.Edges[0].Source != "caller-1" || got.Edges[0].Target != "fn-abc" {
		t.Errorf("edges: got %+v", got.Edges)
	}
	if len(got.Nodes) != 2 || got.Nodes["caller-1"].Name != "doSomething" {
		t.Errorf("nodes: got %+v", got.Nodes)
	}
}

// An unresolvable id is 404, not 500, matching /api/code/node.
func TestAPICodeCallers_Unresolvable_404(t *testing.T) {
	fake := &fakeCodeEngine{callersErr: fmt.Errorf("codeintel/db: GetNode missing: %w", db.ErrNotFound)}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/callers?id=missing", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", rr.Code, rr.Body.String())
	}
	assertErrorEnvelope(t, rr)
}

func TestAPICodeCallees_MissingID_400(t *testing.T) {
	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(&fakeCodeEngine{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/callees", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rr.Code, rr.Body.String())
	}
	assertErrorEnvelope(t, rr)
}

func TestAPICodeImpact_DepthPassedThrough(t *testing.T) {
	fake := &fakeCodeEngine{
		impact: types.Subgraph{Nodes: map[string]types.Node{}, Roots: []string{"fn-abc"}},
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/impact?id=fn-abc&depth=3", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if fake.subgraphDepth != 3 {
		t.Errorf("expected depth=3 passed to GetImpactRadius, got %d", fake.subgraphDepth)
	}
}

func TestAPICodeFiles_Shape(t *testing.T) {
	fake := &fakeCodeEngine{
		files: []types.FileRecord{
			{Path: "cmd/main.go", Language: types.LanguageGo, NodeCount: 5},
			{Path: "pkg/util.go", Language: types.LanguageGo, NodeCount: 12},
		},
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/files", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var got struct {
		Files []struct {
			Path      string `json:"path"`
			Language  string `json:"language"`
			NodeCount int    `json:"nodeCount"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if len(got.Files) != 2 {
		t.Fatalf("expected 2 files, got %d: %+v", len(got.Files), got.Files)
	}
	if got.Files[0].Path != "cmd/main.go" || got.Files[0].NodeCount != 5 {
		t.Errorf("files[0]: got %+v", got.Files[0])
	}
}

func TestAPICodeSchema_TableColumnsFK(t *testing.T) {
	richFake := &richFakeCodeEngine{
		tableNodes: []types.Node{
			{ID: "tbl-users", Name: "users", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 1},
			{ID: "tbl-orders", Name: "orders", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 10},
		},
		viewNodes: []types.Node{},
		procedureNodes: []types.Node{
			{ID: "proc-insert", Name: "insert_user", Kind: types.NodeKindProcedure, FilePath: "schema.sql", StartLine: 20},
		},
		nodes: map[string]types.Node{
			"tbl-users":   {ID: "tbl-users", Name: "users", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 1},
			"col-id":      {ID: "col-id", Name: "id", Kind: types.NodeKindColumn, FilePath: "schema.sql", StartLine: 2},
			"tbl-orders":  {ID: "tbl-orders", Name: "orders", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 10},
			"proc-insert": {ID: "proc-insert", Name: "insert_user", Kind: types.NodeKindProcedure, FilePath: "schema.sql", StartLine: 20},
		},
		outgoingEdges: map[string][]types.Edge{
			"tbl-users": {
				{Source: "tbl-users", Target: "col-id", Kind: types.EdgeKindContains},
			},
			"tbl-orders": {
				{Source: "tbl-orders", Target: "tbl-users", Kind: types.EdgeKindReferences},
			},
			"proc-insert": {
				{Source: "proc-insert", Target: "tbl-users", Kind: types.EdgeKindWrites},
			},
		},
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(richFake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/schema", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var got struct {
		Tables []struct {
			Node struct {
				Name string `json:"name"`
			} `json:"node"`
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			FKSources []struct {
				Name string `json:"name"`
			} `json:"fkSources"`
			Writers []struct {
				Name string `json:"name"`
			} `json:"writers"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if len(got.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d: %+v", len(got.Tables), got.Tables)
	}
	var users *struct {
		Node struct {
			Name string `json:"name"`
		} `json:"node"`
		Columns []struct {
			Name string `json:"name"`
		} `json:"columns"`
		FKSources []struct {
			Name string `json:"name"`
		} `json:"fkSources"`
		Writers []struct {
			Name string `json:"name"`
		} `json:"writers"`
	}
	for i := range got.Tables {
		if got.Tables[i].Node.Name == "users" {
			users = &got.Tables[i]
		}
	}
	if users == nil {
		t.Fatal("users table not found in response")
	}
	if len(users.Columns) != 1 || users.Columns[0].Name != "id" {
		t.Errorf("users.columns: got %+v", users.Columns)
	}
	if len(users.FKSources) != 1 || users.FKSources[0].Name != "orders" {
		t.Errorf("users.fkSources: got %+v", users.FKSources)
	}
	if len(users.Writers) != 1 || users.Writers[0].Name != "insert_user" {
		t.Errorf("users.writers: got %+v", users.Writers)
	}
}

func TestAPICodeSchema_Empty(t *testing.T) {
	fake := &fakeCodeEngine{
		nodesByKind: map[types.NodeKind][]types.Node{
			types.NodeKindTable: {},
			types.NodeKindView:  {},
		},
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/schema", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var got struct {
		Tables []any `json:"tables"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if len(got.Tables) != 0 {
		t.Errorf("expected empty tables, got %d: %+v", len(got.Tables), got.Tables)
	}
}

func TestAPICodeFile_Shape(t *testing.T) {
	fake := &fakeCodeEngine{
		nodesInFile: []types.Node{
			{ID: "fn-x", Name: "Foo", Kind: types.NodeKindFunction, StartLine: 3},
		},
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/file?path=foo.go", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var got struct {
		Path  string `json:"path"`
		Nodes []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			StartLine int    `json:"startLine"`
		} `json:"nodes"`
		Degraded string `json:"degraded"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.Path != "foo.go" {
		t.Errorf("path: got %q", got.Path)
	}
	if got.Degraded != "" {
		t.Errorf("degraded: got %q, want empty", got.Degraded)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Name != "Foo" || got.Nodes[0].StartLine != 3 {
		t.Errorf("nodes: got %+v", got.Nodes)
	}
}

func TestAPICodeFile_MissingPath_400(t *testing.T) {
	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(&fakeCodeEngine{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/file", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rr.Code, rr.Body.String())
	}
	assertErrorEnvelope(t, rr)
}

// Not-indexed is soft state: 200 with a data-carried reason, not an error
// envelope.
func TestAPICodeFile_NotIndexed_Degraded(t *testing.T) {
	fake := &fakeCodeEngine{nodesInFile: nil}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/file?path=unindexed.go", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (soft state, not an error), got %d; body: %s", rr.Code, rr.Body.String())
	}

	var got struct {
		Path     string `json:"path"`
		Nodes    []any  `json:"nodes"`
		Degraded string `json:"degraded"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.Degraded == "" {
		t.Error("expected non-empty degraded reason")
	}
	if len(got.Nodes) != 0 {
		t.Errorf("expected no nodes, got %+v", got.Nodes)
	}
}

// Every other test here runs on the fake seam; this one drives the production
// provider against a real on-disk index.
func TestAPICodeFiles_ProductionRealIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping production-wiring test in short mode")
	}

	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "main.go"), "package main\n\nfunc HelloWorld() {}\n")
	writeFile(t, filepath.Join(repoRoot, "go.mod"), "module example.com/tiny\n\ngo 1.21\n")

	dbDir := filepath.Join(repoRoot, ".claude", ".atomic-index")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir dbDir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "atomic.db")

	eng, err := engine.NewWithDBPath(repoRoot, dbPath)
	if err != nil {
		t.Fatalf("NewWithDBPath: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		t.Fatalf("IndexAll: %v", err)
	}

	h := serve.NewCodeExplorerAPIHandler(serve.CodeExplorerOptions{
		RealmRoot: repoRoot,
		// EngineProvider nil → DefaultEngineProvider
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/files", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "main.go") {
		t.Errorf("expected 'main.go' in file list; body: %s", rr.Body.String())
	}
}

var errNotFound = errFor("node not found")

type errFor string

func (e errFor) Error() string { return string(e) }

// assertErrorEnvelope checks the {"error": "..."} shape every /api/* route uses.
func assertErrorEnvelope(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal error envelope: %v; body=%s", err, rr.Body.String())
	}
	if got.Error == "" {
		t.Errorf("expected non-empty error message; body=%s", rr.Body.String())
	}
}
