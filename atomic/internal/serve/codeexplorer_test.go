package serve_test

// codeexplorer_test.go — CP8: per-repo Code Explorer + SQL schema tests.
//
// TDD: these tests were written before the implementation.
//
// Covers:
//  1. Node detail (/code/node?id=): fake engine returns a Node → rendered signature/kind/file:line.
//  2. Node detail by name (/code/node?name=): resolved via GetNodesByName.
//  3. Callers/Callees/Impact: fake Subgraph with edges of kinds calls+references
//     → output has edge chips labeled with the kind, each linking to the target node.
//  4. Depth param respected (passed through to GetCallers etc.).
//  5. Files: fake []FileRecord → list links to /file/<path>.
//  6. SQL schema: fake graph with table node, two column nodes (contains edges),
//     a references edge (FK), a writes edge → schema shows table, columns, FK, writer.
//  7. SQL schema: no SQL nodes → empty/"no SQL schema" state.
//  8. Production wiring: EngineProvider opens a real index and returns real nodes.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── Fake CodeEngine ─────────────────────────────────────────────────────────

// fakeCodeEngine is a stub CodeEngine for tests.
type fakeCodeEngine struct {
	node          types.Node
	nodesByName   []types.Node
	callers       types.Subgraph
	callees       types.Subgraph
	impact        types.Subgraph
	files         []types.FileRecord
	nodesInFile   []types.Node
	nodesByKind   map[types.NodeKind][]types.Node
	nodeErr       error
	subgraphDepth int // last depth passed to callers/callees/impact
}

func (f *fakeCodeEngine) SearchNodes(_ context.Context, _ types.SearchOptions) ([]types.SearchResult, error) {
	return nil, nil
}
func (f *fakeCodeEngine) GetNode(_ context.Context, _ string) (types.Node, error) {
	return f.node, f.nodeErr
}
func (f *fakeCodeEngine) GetNodesByName(_ context.Context, _ string, _ types.NodeKind) ([]types.Node, error) {
	return f.nodesByName, f.nodeErr
}
func (f *fakeCodeEngine) GetCallers(_ context.Context, _ string, depth int) (types.Subgraph, error) {
	f.subgraphDepth = depth
	return f.callers, nil
}
func (f *fakeCodeEngine) GetCallees(_ context.Context, _ string, depth int) (types.Subgraph, error) {
	f.subgraphDepth = depth
	return f.callees, nil
}
func (f *fakeCodeEngine) GetImpactRadius(_ context.Context, _ string, depth int) (types.Subgraph, error) {
	f.subgraphDepth = depth
	return f.impact, nil
}
func (f *fakeCodeEngine) GetFiles(_ context.Context) ([]types.FileRecord, error) {
	return f.files, nil
}
func (f *fakeCodeEngine) GetNodesInFile(_ context.Context, _ string) ([]types.Node, error) {
	return f.nodesInFile, nil
}
func (f *fakeCodeEngine) GetNodesByKind(_ context.Context, kind types.NodeKind) ([]types.Node, error) {
	return f.nodesByKind[kind], nil
}
func (f *fakeCodeEngine) GetOutgoingEdges(_ context.Context, _ string) ([]types.Edge, error) {
	return nil, nil
}
func (f *fakeCodeEngine) Close() {}

// fakeProviderFor wraps a fake engine as an EngineProvider.
func fakeProviderFor(eng serve.CodeEngine) serve.EngineProvider {
	return func(_ context.Context, _, _ string) (serve.CodeEngine, error) {
		return eng, nil
	}
}

// ─── 1. Node detail by ID ─────────────────────────────────────────────────────

func TestCodeExplorer_NodeDetail_ByID(t *testing.T) {
	fake := &fakeCodeEngine{
		node: types.Node{
			ID:        "fn-abc",
			Name:      "myFunc",
			Kind:      types.NodeKindFunction,
			FilePath:  "pkg/util.go",
			StartLine: 42,
			Signature: "func myFunc(x int) error",
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/node?id=fn-abc", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// Must render signature, kind, file:line.
	if !strings.Contains(body, "myFunc") {
		t.Errorf("missing node name; body: %s", body)
	}
	if !strings.Contains(body, "function") {
		t.Errorf("missing kind; body: %s", body)
	}
	if !strings.Contains(body, "pkg/util.go") {
		t.Errorf("missing file path; body: %s", body)
	}
	if !strings.Contains(body, "42") {
		t.Errorf("missing start line; body: %s", body)
	}
	if !strings.Contains(body, "func myFunc(x int) error") {
		t.Errorf("missing signature; body: %s", body)
	}
}

// ─── 2. Node detail by name ───────────────────────────────────────────────────

func TestCodeExplorer_NodeDetail_ByName(t *testing.T) {
	fake := &fakeCodeEngine{
		nodesByName: []types.Node{
			{
				ID:        "cls-xyz",
				Name:      "MyService",
				Kind:      types.NodeKindClass,
				FilePath:  "service/main.go",
				StartLine: 10,
			},
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/node?name=MyService", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "MyService") {
		t.Errorf("missing node name; body: %s", body)
	}
	if !strings.Contains(body, "class") {
		t.Errorf("missing kind; body: %s", body)
	}
}

// ─── 3. Callers: edge chips with edge kind shown ──────────────────────────────

func TestCodeExplorer_Callers_EdgeChips(t *testing.T) {
	// Subgraph: root=fn-abc, two callers: caller-1 (calls) and caller-2 (references).
	callers := types.Subgraph{
		Nodes: map[string]types.Node{
			"fn-abc":   {ID: "fn-abc", Name: "myFunc", Kind: types.NodeKindFunction, FilePath: "pkg/util.go", StartLine: 42},
			"caller-1": {ID: "caller-1", Name: "doSomething", Kind: types.NodeKindFunction, FilePath: "cmd/main.go", StartLine: 10},
			"caller-2": {ID: "caller-2", Name: "referer", Kind: types.NodeKindMethod, FilePath: "pkg/x.go", StartLine: 5},
		},
		Roots: []string{"fn-abc"},
		Edges: []types.Edge{
			{Source: "caller-1", Target: "fn-abc", Kind: types.EdgeKindCalls},
			{Source: "caller-2", Target: "fn-abc", Kind: types.EdgeKindReferences},
		},
	}

	fake := &fakeCodeEngine{callers: callers}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/callers?id=fn-abc", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// Edge chips: each edge kind must appear.
	if !strings.Contains(body, "calls") {
		t.Errorf("missing 'calls' edge kind chip; body: %s", body)
	}
	if !strings.Contains(body, "references") {
		t.Errorf("missing 'references' edge kind chip; body: %s", body)
	}

	// Caller nodes must be listed with links to /code/node?id=.
	if !strings.Contains(body, "doSomething") {
		t.Errorf("missing caller node 'doSomething'; body: %s", body)
	}
	if !strings.Contains(body, "referer") {
		t.Errorf("missing caller node 'referer'; body: %s", body)
	}
	// Links to node detail.
	if !strings.Contains(body, "/code/node?id=caller-1") {
		t.Errorf("missing link to caller-1 detail; body: %s", body)
	}
}

// ─── 4. Depth param is passed through ────────────────────────────────────────

func TestCodeExplorer_Depth_PassedThrough(t *testing.T) {
	fake := &fakeCodeEngine{
		callees: types.Subgraph{
			Nodes: map[string]types.Node{},
			Roots: []string{"fn-abc"},
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/callees?id=fn-abc&depth=3", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if fake.subgraphDepth != 3 {
		t.Errorf("expected depth=3 passed to GetCallees, got %d", fake.subgraphDepth)
	}
}

// ─── 5. Files list ────────────────────────────────────────────────────────────

func TestCodeExplorer_Files_List(t *testing.T) {
	fake := &fakeCodeEngine{
		files: []types.FileRecord{
			{Path: "cmd/main.go", Language: types.LanguageGo, NodeCount: 5},
			{Path: "pkg/util.go", Language: types.LanguageGo, NodeCount: 12},
			{Path: "scripts/setup.py", Language: types.LanguagePython, NodeCount: 3},
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/files", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// Each file links to /file/<path>.
	for _, path := range []string{"cmd/main.go", "pkg/util.go", "scripts/setup.py"} {
		link := "/file/" + path
		if !strings.Contains(body, link) {
			t.Errorf("missing link %q; body: %s", link, body)
		}
		if !strings.Contains(body, path) {
			t.Errorf("missing path %q; body: %s", path, body)
		}
	}
}

// ─── 6. SQL schema: table + columns + FK + writer ────────────────────────────

func TestCodeExplorer_SQLSchema_TableColumnsFK(t *testing.T) {
	// Fake graph:
	//   table: users (table node)
	//   column: id, email (column nodes)
	//   contains edges: users→id, users→email
	//   references edge: orders→users (FK)
	//   writes edge: insert_user→users (writer proc)
	// Outgoing edges from users table.
	// We need to inject edges for the schema handler to follow. The handler calls
	// GetNodesByKind("table"), then for each table calls GetOutgoingEdges to find
	// column children (contains), and scans incoming edges for writes/references.
	// We override GetOutgoingEdges and add a custom mechanism using the struct.
	// Since fakeCodeEngine.GetOutgoingEdges returns nil, we need a richer fake.
	richFake := &richFakeCodeEngine{
		// Two tables: users (the subject) and orders (FK source referencing users).
		tableNodes: []types.Node{
			{ID: "tbl-users", Name: "users", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 1},
			{ID: "tbl-orders", Name: "orders", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 10},
		},
		viewNodes: []types.Node{},
		// Procedure node returned by GetNodesByKind(procedure).
		procedureNodes: []types.Node{
			{ID: "proc-insert", Name: "insert_user", Kind: types.NodeKindProcedure, FilePath: "schema.sql", StartLine: 20},
		},
		nodes: map[string]types.Node{
			"tbl-users":   {ID: "tbl-users", Name: "users", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 1},
			"col-id":      {ID: "col-id", Name: "id", Kind: types.NodeKindColumn, FilePath: "schema.sql", StartLine: 2},
			"col-email":   {ID: "col-email", Name: "email", Kind: types.NodeKindColumn, FilePath: "schema.sql", StartLine: 3},
			"tbl-orders":  {ID: "tbl-orders", Name: "orders", Kind: types.NodeKindTable, FilePath: "schema.sql", StartLine: 10},
			"proc-insert": {ID: "proc-insert", Name: "insert_user", Kind: types.NodeKindProcedure, FilePath: "schema.sql", StartLine: 20},
		},
		outgoingEdges: map[string][]types.Edge{
			"tbl-users": {
				{Source: "tbl-users", Target: "col-id", Kind: types.EdgeKindContains},
				{Source: "tbl-users", Target: "col-email", Kind: types.EdgeKindContains},
			},
			// references from orders→users (FK).
			"tbl-orders": {
				{Source: "tbl-orders", Target: "tbl-users", Kind: types.EdgeKindReferences},
			},
			// writes from insert_user→users.
			"proc-insert": {
				{Source: "proc-insert", Target: "tbl-users", Kind: types.EdgeKindWrites},
			},
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(richFake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/schema", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// Table name rendered.
	if !strings.Contains(body, "users") {
		t.Errorf("missing table name 'users'; body: %s", body)
	}
	// Column names rendered.
	if !strings.Contains(body, "id") {
		t.Errorf("missing column 'id'; body: %s", body)
	}
	if !strings.Contains(body, "email") {
		t.Errorf("missing column 'email'; body: %s", body)
	}
	// FK reference from orders shown.
	if !strings.Contains(body, "orders") {
		t.Errorf("missing FK reference from 'orders'; body: %s", body)
	}
	// Writer shown.
	if !strings.Contains(body, "insert_user") {
		t.Errorf("missing writer 'insert_user'; body: %s", body)
	}
}

// richFakeCodeEngine supports schema testing with outgoing edges per node.
type richFakeCodeEngine struct {
	tableNodes     []types.Node
	viewNodes      []types.Node
	procedureNodes []types.Node
	nodes          map[string]types.Node
	outgoingEdges  map[string][]types.Edge
}

func (r *richFakeCodeEngine) SearchNodes(_ context.Context, _ types.SearchOptions) ([]types.SearchResult, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetNode(_ context.Context, id string) (types.Node, error) {
	if n, ok := r.nodes[id]; ok {
		return n, nil
	}
	return types.Node{}, fmt.Errorf("node not found: %s", id)
}
func (r *richFakeCodeEngine) GetNodesByName(_ context.Context, _ string, _ types.NodeKind) ([]types.Node, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetCallers(_ context.Context, _ string, _ int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (r *richFakeCodeEngine) GetCallees(_ context.Context, _ string, _ int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (r *richFakeCodeEngine) GetImpactRadius(_ context.Context, _ string, _ int) (types.Subgraph, error) {
	return types.Subgraph{}, nil
}
func (r *richFakeCodeEngine) GetFiles(_ context.Context) ([]types.FileRecord, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetNodesInFile(_ context.Context, _ string) ([]types.Node, error) {
	return nil, nil
}
func (r *richFakeCodeEngine) GetNodesByKind(_ context.Context, kind types.NodeKind) ([]types.Node, error) {
	switch kind {
	case types.NodeKindTable:
		return r.tableNodes, nil
	case types.NodeKindView:
		return r.viewNodes, nil
	case types.NodeKindProcedure:
		return r.procedureNodes, nil
	}
	return nil, nil
}
func (r *richFakeCodeEngine) GetOutgoingEdges(_ context.Context, nodeID string) ([]types.Edge, error) {
	return r.outgoingEdges[nodeID], nil
}
func (r *richFakeCodeEngine) Close() {}

// ─── 7. SQL schema: no SQL nodes → empty state ───────────────────────────────

func TestCodeExplorer_SQLSchema_Empty(t *testing.T) {
	fake := &fakeCodeEngine{
		nodesByKind: map[types.NodeKind][]types.Node{
			types.NodeKindTable: {},
			types.NodeKindView:  {},
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/schema", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	lbody := strings.ToLower(body)
	if !strings.Contains(lbody, "no sql") && !strings.Contains(lbody, "no schema") && !strings.Contains(lbody, "not indexed") && !strings.Contains(lbody, "empty") {
		t.Errorf("expected empty-state message; body: %s", body)
	}
}

// ─── 8. Production wiring: real index ────────────────────────────────────────
//
// Build a tiny Go file, index it with the real engine, then call the code
// explorer handler with the production EngineProvider (nil → default) and
// verify that /code/files returns the indexed file.
func TestCodeExplorer_Production_RealIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping production-wiring test in short mode")
	}

	// Build a tiny temp repo.
	repoRoot := t.TempDir()
	goFile := filepath.Join(repoRoot, "main.go")
	writeFile(t, goFile, "package main\n\nfunc HelloWorld() {}\n")

	// Write a go.mod so the indexer can discover the file.
	writeFile(t, filepath.Join(repoRoot, "go.mod"), "module example.com/tiny\n\ngo 1.21\n")

	// Index it.
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

	// Now use the production EngineProvider (nil → DefaultEngineProvider).
	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot: repoRoot,
		// EngineProvider nil → DefaultEngineProvider
	})

	req := httptest.NewRequest(http.MethodGet, "/code/files", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "/file/main.go") {
		t.Errorf("expected '/file/main.go' link in file list; body: %s", body)
	}
}

// ─── HTMX fragment test ───────────────────────────────────────────────────────

func TestCodeExplorer_HTMXFragment_NoShell(t *testing.T) {
	fake := &fakeCodeEngine{
		node: types.Node{
			ID:       "fn-x",
			Name:     "Foo",
			Kind:     types.NodeKindFunction,
			FilePath: "foo.go",
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/node?id=fn-x", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	// Fragment must not contain full HTML shell.
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("HTMX response should not contain DOCTYPE; body: %s", body)
	}
	// But it must still contain the node name.
	if !strings.Contains(body, "Foo") {
		t.Errorf("missing node name in HTMX fragment; body: %s", body)
	}
}

// ─── Impact endpoint ─────────────────────────────────────────────────────────

func TestCodeExplorer_Impact_EdgeChips(t *testing.T) {
	impact := types.Subgraph{
		Nodes: map[string]types.Node{
			"fn-abc":  {ID: "fn-abc", Name: "myFunc", Kind: types.NodeKindFunction, FilePath: "pkg/util.go"},
			"svc-xyz": {ID: "svc-xyz", Name: "myService", Kind: types.NodeKindClass, FilePath: "svc/main.go"},
		},
		Roots: []string{"fn-abc"},
		Edges: []types.Edge{
			{Source: "svc-xyz", Target: "fn-abc", Kind: types.EdgeKindCalls},
		},
	}

	fake := &fakeCodeEngine{impact: impact}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/impact?id=fn-abc", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "calls") {
		t.Errorf("missing 'calls' edge kind chip; body: %s", body)
	}
	if !strings.Contains(body, "myService") {
		t.Errorf("missing impacted node 'myService'; body: %s", body)
	}
}
