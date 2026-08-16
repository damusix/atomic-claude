package serve_test

// api_search_test.go — /api/search/md, /api/code/search,
// /api/search/stream JSON shape tests. TDD: written to assert the shapes
// pinned in the spec's ## API contracts table before/alongside implementation.

import (
	"bufio"
	"context"
	"encoding/json"
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

// ─── /api/search/md ─────────────────────────────────────────────────────────

func TestAPISearchMD(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n\nHello world, this is a needle.\n")

	handler := serve.NewAPIMdSearchHandler(serve.MdSearchOptions{NavRoot: root})

	req := httptest.NewRequest(http.MethodGet, "/api/search/md?q=needle", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got struct {
		Query     string `json:"query"`
		Truncated bool   `json:"truncated"`
		Cap       int    `json:"cap"`
		Results   []struct {
			RelPath string `json:"relpath"`
			Line    int    `json:"line"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if got.Query != "needle" {
		t.Errorf("query: got %q, want needle", got.Query)
	}
	if got.Truncated {
		t.Error("truncated: got true, want false")
	}
	if got.Cap != 50 {
		t.Errorf("cap: got %d, want 50", got.Cap)
	}
	if len(got.Results) != 1 || got.Results[0].RelPath != "docs/guide.md" {
		t.Fatalf("results: got %+v, want one match in docs/guide.md", got.Results)
	}
	if got.Results[0].Line != 3 {
		t.Errorf("line: got %d, want 3", got.Results[0].Line)
	}
}

func TestAPISearchMD_TruncatedAtCap(t *testing.T) {
	root := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("needle line\n")
	}
	writeFile(t, filepath.Join(root, "big.md"), sb.String())

	handler := serve.NewAPIMdSearchHandler(serve.MdSearchOptions{NavRoot: root})

	req := httptest.NewRequest(http.MethodGet, "/api/search/md?q=needle", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var got struct {
		Truncated bool       `json:"truncated"`
		Cap       int        `json:"cap"`
		Results   []struct{} `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if !got.Truncated {
		t.Error("truncated: got false, want true (60 matches > cap 50)")
	}
	if len(got.Results) != 50 {
		t.Errorf("results: got %d, want 50 (capped)", len(got.Results))
	}
}

func TestAPISearchMD_MissingQuery(t *testing.T) {
	root := t.TempDir()
	handler := serve.NewAPIMdSearchHandler(serve.MdSearchOptions{NavRoot: root})

	req := httptest.NewRequest(http.MethodGet, "/api/search/md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rr.Code)
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

func TestAPISearchMD_EmptyQuery(t *testing.T) {
	root := t.TempDir()
	handler := serve.NewAPIMdSearchHandler(serve.MdSearchOptions{NavRoot: root})

	req := httptest.NewRequest(http.MethodGet, "/api/search/md?q=", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rr.Code)
	}
}

// ─── /api/code/search ────────────────────────────────────────────────────────

type apiNodeRefWire struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	FilePath  string `json:"filePath"`
	StartLine int    `json:"startLine"`
}

type apiCodeSearchMemberWire struct {
	Key     string           `json:"key"`
	Prefix  string           `json:"prefix"`
	Indexed bool             `json:"indexed"`
	Results []apiNodeRefWire `json:"results"`
}

type apiCodeSearchResponseWire struct {
	Members []apiCodeSearchMemberWire `json:"members"`
}

func TestAPICodeSearch_RepoScope(t *testing.T) {
	root := t.TempDir()

	fn := func(_ context.Context, _, _, query string) ([]types.SearchResult, error) {
		return []types.SearchResult{
			{Node: types.Node{ID: "n1", Name: "Foo", Kind: types.NodeKind("function"), FilePath: "a.go", StartLine: 10}},
		}, nil
	}

	handler := serve.NewAPICodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot: root,
		SearchFn:  fn,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/search?q=Foo", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got apiCodeSearchResponseWire
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if len(got.Members) != 1 {
		t.Fatalf("members: got %d, want 1; got=%+v", len(got.Members), got.Members)
	}
	m := got.Members[0]
	if !m.Indexed {
		t.Error("indexed: got false, want true")
	}
	if len(m.Results) != 1 || m.Results[0].Name != "Foo" || m.Results[0].FilePath != "a.go" || m.Results[0].StartLine != 10 {
		t.Errorf("results: got %+v, want one node Foo/a.go:10", m.Results)
	}
}

func TestAPICodeSearch_NotIndexedMember(t *testing.T) {
	root := t.TempDir()

	fn := func(_ context.Context, _, _, _ string) ([]types.SearchResult, error) {
		return nil, http.ErrHandlerTimeout // any error surfaces as not-indexed
	}

	handler := serve.NewAPICodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot: root,
		SearchFn:  fn,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/code/search?q=Foo", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var got apiCodeSearchResponseWire
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}

	if len(got.Members) != 1 {
		t.Fatalf("members: got %d, want 1; got=%+v", len(got.Members), got.Members)
	}
	m := got.Members[0]
	if m.Indexed {
		t.Error("indexed: got true, want false")
	}
	if len(m.Results) != 0 {
		t.Errorf("results: got %+v, want empty (not-indexed member)", m.Results)
	}
}

func TestAPICodeSearch_MissingQuery(t *testing.T) {
	root := t.TempDir()
	handler := serve.NewAPICodeSearchHandler(serve.CodeSearchOptions{RealmRoot: root})

	req := httptest.NewRequest(http.MethodGet, "/api/code/search", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rr.Code)
	}
}

// ─── /api/search/stream ──────────────────────────────────────────────────────

// sseEvent is one parsed "event: <name>\ndata: <payload>\n\n" block.
type sseEvent struct {
	Name string
	Data string
}

// parseSSE reads all events out of an SSE body (small, complete test bodies only).
func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	var cur sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if cur.Data != "" {
				cur.Data += "\n"
			}
			cur.Data += strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.Name != "" {
				events = append(events, cur)
			}
			cur = sseEvent{}
		}
	}
	if cur.Name != "" {
		events = append(events, cur)
	}
	return events
}

func TestAPISearchStream_MdAndEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "guide.md"), "a needle here\n")

	handler := serve.NewAPISearchStreamHandler(serve.SearchStreamOptions{
		NavRoot:      root,
		RealmRoot:    root,
		ClaudeMDPath: filepath.Join(root, "nonexistent-claude.md"),
		SearchFn: func(_ context.Context, _, _, _ string) ([]types.SearchResult, error) {
			return nil, http.ErrHandlerTimeout
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/search/stream?q=needle&src=md", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}

	events := parseSSE(t, rr.Body.String())
	if len(events) != 2 {
		t.Fatalf("events: got %d, want 2 (md, end); got=%+v", len(events), events)
	}
	if events[0].Name != "md" {
		t.Errorf("events[0]: got %q, want md", events[0].Name)
	}
	var mdPayload struct {
		Query   string `json:"query"`
		Results []struct {
			RelPath string `json:"relpath"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &mdPayload); err != nil {
		t.Fatalf("unmarshal md event: %v; data=%s", err, events[0].Data)
	}
	if mdPayload.Query != "needle" || len(mdPayload.Results) != 1 {
		t.Errorf("md payload: got %+v", mdPayload)
	}
	if events[1].Name != "end" {
		t.Errorf("events[1]: got %q, want end", events[1].Name)
	}
	if events[1].Data != "{}" {
		t.Errorf("end payload: got %q, want {}", events[1].Data)
	}
}

func TestAPISearchStream_CodeEvents(t *testing.T) {
	root := t.TempDir()
	// A bare repo (no realm/wikis registration) resolves to ScopeNoIndex,
	// which discoverCodeMembers still maps to a single local-index member
	// (Key: "", no [key] wrap) — the SearchFn seam makes db presence moot.

	handler := serve.NewAPISearchStreamHandler(serve.SearchStreamOptions{
		NavRoot:      root,
		RealmRoot:    root,
		ClaudeMDPath: filepath.Join(root, "nonexistent-claude.md"),
		SearchFn: func(_ context.Context, _, _, query string) ([]types.SearchResult, error) {
			return []types.SearchResult{
				{Node: types.Node{ID: "n1", Name: "Foo", Kind: types.NodeKind("function"), FilePath: "a.go", StartLine: 1}},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/search/stream?q=Foo&src=code", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	events := parseSSE(t, rr.Body.String())
	if len(events) < 1 || events[len(events)-1].Name != "end" {
		t.Fatalf("events: want terminal end event; got=%+v", events)
	}
	var sawCode bool
	for _, e := range events {
		if e.Name != "code" {
			continue
		}
		sawCode = true
		var payload struct {
			Member struct {
				Key     string `json:"key"`
				Indexed bool   `json:"indexed"`
			} `json:"member"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(e.Data), &payload); err != nil {
			t.Fatalf("unmarshal code event: %v; data=%s", err, e.Data)
		}
		if !payload.Member.Indexed {
			t.Errorf("member.indexed: got false, want true")
		}
		if len(payload.Results) != 1 || payload.Results[0].Name != "Foo" {
			t.Errorf("results: got %+v, want one node Foo", payload.Results)
		}
	}
	if !sawCode {
		t.Fatal("no code event emitted")
	}
}

func TestAPISearchStream_SrcClamping(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "guide.md"), "a needle here\n")

	handler := serve.NewAPISearchStreamHandler(serve.SearchStreamOptions{
		NavRoot:      root,
		RealmRoot:    root,
		ClaudeMDPath: filepath.Join(root, "nonexistent-claude.md"),
	})

	// An invalid src clamps to "all" (md + code + end).
	req := httptest.NewRequest(http.MethodGet, "/api/search/stream?q=needle&src=bogus", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	events := parseSSE(t, rr.Body.String())
	var names []string
	for _, e := range events {
		names = append(names, e.Name)
	}
	if len(names) < 2 || names[0] != "md" || names[len(names)-1] != "end" {
		t.Errorf("events: got %+v, want md first and end last (src=bogus clamps to all)", names)
	}
}

// TestAPICodeSearch_ProductionDefault_RealIndex proves DefaultMemberSearchFn
// (production seam) finds a symbol in a real on-disk index, and that
// NewAPICodeSearchHandler wires it when SearchFn is nil. Ported from the
// pre-cutover codesearch_test.go.
func TestAPICodeSearch_ProductionDefault_RealIndex(t *testing.T) {
	memberDir := t.TempDir()
	goSrc := "package greeter\n\n// Greet returns a greeting.\nfunc Greet(name string) string { return \"Hello, \" + name }\n"
	if err := os.WriteFile(filepath.Join(memberDir, "greeter.go"), []byte(goSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "greeter.db")
	ctx := context.Background()
	eng, err := engine.NewWithDBPath(memberDir, dbPath)
	if err != nil {
		t.Fatal("NewWithDBPath:", err)
	}
	if err := eng.Init(ctx); err != nil {
		eng.Close()
		t.Fatal("Init:", err)
	}
	if err := eng.IndexAll(ctx); err != nil {
		eng.Close()
		t.Fatal("IndexAll:", err)
	}
	eng.Close()

	prodFn := serve.DefaultMemberSearchFn()
	results, err := prodFn(ctx, memberDir, dbPath, "Greet")
	if err != nil {
		t.Fatalf("production search failed: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Node.Name, "Greet") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no result with name containing 'Greet'; results: %+v", results)
	}

	// Also verify the /api/code/search handler wires to the production seam
	// when SearchFn is nil, without panicking.
	realmRoot := t.TempDir()
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	writeFile(t, claudeMDPath, "# no wiki\n")

	handler := serve.NewAPICodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     nil, // production default
	})
	req := httptest.NewRequest(http.MethodGet, "/api/code/search?q=Greet", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req) // must not panic
	if rr.Code != http.StatusOK {
		t.Fatalf("nil SearchFn handler: expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
}

func TestAPISearchStream_EmptyQuery(t *testing.T) {
	root := t.TempDir()
	handler := serve.NewAPISearchStreamHandler(serve.SearchStreamOptions{
		NavRoot:      root,
		RealmRoot:    root,
		ClaudeMDPath: filepath.Join(root, "nonexistent-claude.md"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/search/stream?q=&src=all", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	events := parseSSE(t, rr.Body.String())
	if len(events) != 1 || events[0].Name != "end" {
		t.Fatalf("events: got %+v, want exactly one terminal end event", events)
	}
}
