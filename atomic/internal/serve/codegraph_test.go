package serve_test

// codegraph_test.go — code-graph spec TDD: GET /code/graph/data.
//
// TDD: written before the implementation.
//
// Covers:
//  1. Response shape: fake engine's nodes/edges → flat JSON with fingerprint +
//     nodes (id/label/kind/file/line/language) + edges (source/target/kind).
//  2. ?member= resolves via the same seam as the sibling /code/* routes and
//     opens that member's own db.
//  3. Unknown member → non-200 JSON error body.
//  4. Missing/unopenable index → non-200 JSON error body.
//  5. Parallel-edge dedup: duplicate (source, target, kind) rows (one per
//     call site) collapse to a single edge; an edge differing only in kind
//     survives.
//  6. Fingerprint stability: identical index state (across two handler calls)
//     → identical fingerprint; a count-preserving rename (same node/edge
//     counts, different ids) → different fingerprint.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// codeGraphResp mirrors the /code/graph/data success JSON shape for decoding
// in tests. Named distinctly from provenance_test.go's graphDataResponse
// (same package) to avoid a symbol clash.
type codeGraphResp struct {
	Fingerprint string `json:"fingerprint"`
	Nodes       []struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Kind     string `json:"kind"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Language string `json:"language"`
	} `json:"nodes"`
	Edges []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Kind   string `json:"kind"`
	} `json:"edges"`
}

// codeGraphErr mirrors the /code/graph/data error JSON shape.
type codeGraphErr struct {
	Error string `json:"error"`
}

// ─── 1. Response shape ────────────────────────────────────────────────────────

func TestCodeGraph_ResponseShape(t *testing.T) {
	fake := &fakeCodeEngine{
		allNodes: []types.Node{
			{ID: "fn-a", Name: "makeHandler", Kind: types.NodeKindFunction, FilePath: "pkg/a.go", StartLine: 10, Language: types.LanguageGo},
			{ID: "fn-b", Name: "helper", Kind: types.NodeKindFunction, FilePath: "pkg/b.go", StartLine: 20, Language: types.LanguageGo},
		},
		allEdges: []types.Edge{
			{Source: "fn-a", Target: "fn-b", Kind: types.EdgeKindCalls},
		},
	}

	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/data", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var resp codeGraphResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body: %s", err, rr.Body.String())
	}

	if resp.Fingerprint == "" {
		t.Error("expected a non-empty fingerprint")
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(resp.Nodes))
	}
	if len(resp.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(resp.Edges))
	}

	n := resp.Nodes[0]
	if n.ID != "fn-a" || n.Label != "makeHandler" || n.Kind != "function" ||
		n.File != "pkg/a.go" || n.Line != 10 || n.Language != "go" {
		t.Errorf("unexpected node shape: %+v", n)
	}

	e := resp.Edges[0]
	if e.Source != "fn-a" || e.Target != "fn-b" || e.Kind != "calls" {
		t.Errorf("unexpected edge shape: %+v", e)
	}
}

// ─── 2. Member param resolution ────────────────────────────────────────────────

func TestCodeGraph_MemberParam_OpensMemberDB(t *testing.T) {
	realmRoot, claudeMDPath := buildSelfIndexedRealm(t, "monorepo")

	fake := &fakeCodeEngine{
		allNodes: []types.Node{{ID: "fn-x", Name: "makeHandler", Kind: types.NodeKindFunction}},
	}
	var openedDB string
	provider := func(_ context.Context, _, dbPath string) (serve.CodeEngine, error) {
		openedDB = dbPath
		return fake, nil
	}

	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot:      realmRoot,
		ClaudeMDPath:   claudeMDPath,
		EngineProvider: provider,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/data?member=monorepo", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	wantDB := filepath.Join(realmRoot, "monorepo", ".claude", ".atomic-index", "atomic.db")
	if openedDB != wantDB {
		t.Errorf("opened db %q, want member self-index %q", openedDB, wantDB)
	}

	var resp codeGraphResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].ID != "fn-x" {
		t.Errorf("expected the member's node to be returned, got %+v", resp.Nodes)
	}
}

func TestCodeGraph_NoMemberParam_SingleRepoScope(t *testing.T) {
	fake := &fakeCodeEngine{
		allNodes: []types.Node{{ID: "fn-a", Name: "a", Kind: types.NodeKindFunction}},
	}
	var openedDB string
	realmRoot := t.TempDir()
	provider := func(_ context.Context, _, dbPath string) (serve.CodeEngine, error) {
		openedDB = dbPath
		return fake, nil
	}

	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot:      realmRoot,
		EngineProvider: provider,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/data", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	wantDB := filepath.Join(realmRoot, ".claude", ".atomic-index", "atomic.db")
	if openedDB != wantDB {
		t.Errorf("opened db %q, want local index %q", openedDB, wantDB)
	}
}

// ─── 3. Unknown member ──────────────────────────────────────────────────────────

func TestCodeGraph_UnknownMember_NonOK_JSONError(t *testing.T) {
	realmRoot, claudeMDPath := buildSelfIndexedRealm(t, "monorepo")

	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		EngineProvider: func(context.Context, string, string) (serve.CodeEngine, error) {
			t.Fatal("provider should not be called for an unknown member")
			return nil, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/data?member=nonexistent", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected a non-200 status, got %d", rr.Code)
	}
	var errResp codeGraphErr
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("expected a JSON error body: %v; body: %s", err, rr.Body.String())
	}
	if errResp.Error == "" {
		t.Error("expected a non-empty error message")
	}
}

// ─── 4. Missing / unopenable index ─────────────────────────────────────────────

func TestCodeGraph_MissingIndex_NonOK_JSONError(t *testing.T) {
	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot: t.TempDir(),
		EngineProvider: func(context.Context, string, string) (serve.CodeEngine, error) {
			return nil, errors.New("index does not exist; call Init first")
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/data", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected a non-200 status, got %d", rr.Code)
	}
	var errResp codeGraphErr
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("expected a JSON error body: %v; body: %s", err, rr.Body.String())
	}
	if errResp.Error == "" {
		t.Error("expected a non-empty error message")
	}
}

// ─── 5. Parallel-edge dedup ─────────────────────────────────────────────────────

// TestCodeGraph_DedupsParallelEdges reproduces the per-callsite fan-out bug:
// the edges table stores one `calls` edge per call site (line/col
// granularity), so a helper called 178 times from the same caller yields 178
// identical (source, target, kind) rows. The client draws one stacked link
// per row. The endpoint must collapse duplicate (source, target, kind)
// triples to one edge each; an edge differing only in kind must survive.
func TestCodeGraph_DedupsParallelEdges(t *testing.T) {
	fake := &fakeCodeEngine{
		allNodes: []types.Node{
			{ID: "fn-a", Name: "caller", Kind: types.NodeKindFunction},
			{ID: "fn-b", Name: "helper", Kind: types.NodeKindFunction},
		},
		allEdges: []types.Edge{
			{Source: "fn-a", Target: "fn-b", Kind: types.EdgeKindCalls},
			{Source: "fn-a", Target: "fn-b", Kind: types.EdgeKindCalls},
			{Source: "fn-a", Target: "fn-b", Kind: types.EdgeKindCalls},
			{Source: "fn-a", Target: "fn-b", Kind: types.EdgeKindReferences},
		},
	}

	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/data", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp codeGraphResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body: %s", err, rr.Body.String())
	}

	if len(resp.Edges) != 2 {
		t.Fatalf("expected 2 deduped edges (one per distinct (source,target,kind) triple), got %d: %+v", len(resp.Edges), resp.Edges)
	}

	seen := map[string]bool{}
	for _, e := range resp.Edges {
		key := e.Source + "\x00" + e.Target + "\x00" + e.Kind
		if seen[key] {
			t.Fatalf("duplicate edge triple in response: %+v", e)
		}
		seen[key] = true
	}
	if !seen["fn-a\x00fn-b\x00calls"] {
		t.Error("expected the deduped calls edge to survive")
	}
	if !seen["fn-a\x00fn-b\x00references"] {
		t.Error("expected the differing-kind edge to survive")
	}
}

// ─── 6. Fingerprint stability ───────────────────────────────────────────────────

func TestCodeGraph_Fingerprint_StableAcrossIdenticalIndex(t *testing.T) {
	fake := &fakeCodeEngine{
		allNodes: []types.Node{
			{ID: "fn-a", Name: "helper", Kind: types.NodeKindFunction, StartLine: 10},
			{ID: "fn-b", Name: "other", Kind: types.NodeKindFunction, StartLine: 20},
		},
		allEdges: []types.Edge{
			{Source: "fn-a", Target: "fn-b", Kind: types.EdgeKindCalls},
		},
	}
	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	get := func() codeGraphResp {
		req := httptest.NewRequest(http.MethodGet, "/code/graph/data", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		var resp codeGraphResp
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	first := get()
	second := get()
	if first.Fingerprint == "" {
		t.Fatal("expected a non-empty fingerprint")
	}
	if first.Fingerprint != second.Fingerprint {
		t.Errorf("expected a stable fingerprint for an unchanged index across two handler calls: %q != %q", first.Fingerprint, second.Fingerprint)
	}
}

// TestCodeGraph_Fingerprint_ChangesOnCountPreservingRename reproduces the bug a
// counts+timestamp fingerprint recipe cannot see: a same-second re-index that
// renames a symbol deletes the old node id and inserts a new one in its place
// (edges re-resolve to the new id) — node count and edge count are UNCHANGED,
// only identity changed. The fingerprint must still change, or a client-side
// layout cache keyed by fingerprint would replay a stale layout over ids that
// no longer exist.
func TestCodeGraph_Fingerprint_ChangesOnCountPreservingRename(t *testing.T) {
	fake := &fakeCodeEngine{
		allNodes: []types.Node{
			{ID: "fn-a", Name: "helper", Kind: types.NodeKindFunction, StartLine: 10},
			{ID: "fn-b", Name: "other", Kind: types.NodeKindFunction, StartLine: 20},
		},
		allEdges: []types.Edge{
			{Source: "fn-a", Target: "fn-b", Kind: types.EdgeKindCalls},
		},
	}
	h := serve.NewCodeGraphHandler(serve.CodeGraphOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/graph/data", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var before codeGraphResp
	if err := json.Unmarshal(rr.Body.Bytes(), &before); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Rename "fn-a" → "fn-a-renamed" in place: same node count, same edge
	// count, same line, only the id (and the edge it participates in) changed.
	fake.allNodes = []types.Node{
		{ID: "fn-a-renamed", Name: "helperRenamed", Kind: types.NodeKindFunction, StartLine: 10},
		{ID: "fn-b", Name: "other", Kind: types.NodeKindFunction, StartLine: 20},
	}
	fake.allEdges = []types.Edge{
		{Source: "fn-a-renamed", Target: "fn-b", Kind: types.EdgeKindCalls},
	}

	req2 := httptest.NewRequest(http.MethodGet, "/code/graph/data", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	var after codeGraphResp
	if err := json.Unmarshal(rr2.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(before.Nodes) != len(after.Nodes) || len(before.Edges) != len(after.Edges) {
		t.Fatalf("test setup invariant violated: counts must stay equal (before nodes=%d edges=%d, after nodes=%d edges=%d)",
			len(before.Nodes), len(before.Edges), len(after.Nodes), len(after.Edges))
	}
	if before.Fingerprint == after.Fingerprint {
		t.Errorf("expected the fingerprint to change after a count-preserving rename, got the same value %q", before.Fingerprint)
	}
}
