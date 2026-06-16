package serve_test

// codemodal_test.go — FE4: Code modal tests.
//
// Written before the implementation (TDD). Covers:
//  1. /file/<src> with HX-Request header returns a fragment (no <!DOCTYPE, no <html>)
//     that contains the file-view table markup.
//  2. /file/<src> without HX-Request returns a full HTML page (contains <!DOCTYPE>).
//  3. /code/file?path=<src> returns defined-symbol chips when engine has nodes in file.
//  4. /code/file?path=<src> returns the degrade note when engine returns zero nodes.
//  5. /code/file?path=<src> returns the degrade note when engine is unavailable.
//  6. graph.go: resolveMarkdownLink for an existing non-.md source file sets
//     CodeFile=true and Broken=false.
//  7. graph.go: resolveMarkdownLink for a non-existent source file keeps Broken=true.
//  8. layout.html contains the #code-modal overlay markup with required panes.
//  9. layout.html contains the delegated code-link JS handler references.
// 10. Rail outbound links for CodeFile edges render as clickable <a> (not broken span).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// ─── 1. /file/<src> HX-Request → fragment (no DOCTYPE) with file-view table ─

func TestFileHandler_HTMXFragment_NoDoctype(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc main() {}\n")

	h := serve.NewFileHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/file/main.go", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// Must NOT be a full HTML page.
	if strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("HX-Request response must not contain <!DOCTYPE; body: %s", body)
	}
	if strings.Contains(body, "<html") {
		t.Errorf("HX-Request response must not contain <html; body: %s", body)
	}

	// Must contain the file-view table (the source highlight markup).
	if !strings.Contains(body, `class="file-view"`) {
		t.Errorf("HX-Request response must contain file-view table; body: %s", body)
	}

	// Must have the wrapper div so the shell can target it.
	if !strings.Contains(body, "file-view-wrapper") {
		t.Errorf("HX-Request response must contain file-view-wrapper div; body: %s", body)
	}
}

// ─── 2. /file/<src> without HX-Request → full page (contains DOCTYPE) ────────

func TestFileHandler_FullPage_WithDoctype(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "util.go"), "package util\n")

	h := serve.NewFileHandler(root)

	req := httptest.NewRequest(http.MethodGet, "/file/util.go", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	if !strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("full response must contain <!DOCTYPE; body: %s", body)
	}
	if !strings.Contains(body, "<html") {
		t.Errorf("full response must contain <html; body: %s", body)
	}
}

// ─── 3. /code/file?path= → symbol chips when engine has nodes ────────────────

func TestCodeFileHandler_SymbolChips_WhenIndexed(t *testing.T) {
	fake := &fakeCodeEngine{
		nodesInFile: []types.Node{
			{ID: "fn-hello", Name: "HelloWorld", Kind: types.NodeKindFunction, FilePath: "main.go", StartLine: 3},
			{ID: "fn-bye", Name: "Goodbye", Kind: types.NodeKindFunction, FilePath: "main.go", StartLine: 7},
		},
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/file?path=main.go", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// Symbol names must appear.
	if !strings.Contains(body, "HelloWorld") {
		t.Errorf("missing symbol 'HelloWorld'; body: %s", body)
	}
	if !strings.Contains(body, "Goodbye") {
		t.Errorf("missing symbol 'Goodbye'; body: %s", body)
	}
	// Each chip must link to callers/callees/impact for that symbol.
	if !strings.Contains(body, "/code/callers?id=fn-hello") {
		t.Errorf("missing callers link for fn-hello; body: %s", body)
	}
	if !strings.Contains(body, "/code/callees?id=fn-hello") {
		t.Errorf("missing callees link for fn-hello; body: %s", body)
	}
	if !strings.Contains(body, "/code/impact?id=fn-hello") {
		t.Errorf("missing impact link for fn-hello; body: %s", body)
	}
	// Kind must appear.
	if !strings.Contains(body, "function") {
		t.Errorf("missing kind 'function'; body: %s", body)
	}
}

// ─── 4. /code/file?path= → degrade note when engine returns zero nodes ───────

func TestCodeFileHandler_DegradeNote_WhenNoNodes(t *testing.T) {
	fake := &fakeCodeEngine{
		nodesInFile: []types.Node{}, // zero nodes
	}

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: fakeProviderFor(fake),
	})

	req := httptest.NewRequest(http.MethodGet, "/code/file?path=main.go", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := strings.ToLower(rr.Body.String())
	// Must contain a "not indexed" or "no code intelligence" degrade note.
	if !strings.Contains(body, "not indexed") && !strings.Contains(body, "no code") && !strings.Contains(body, "no symbols") {
		t.Errorf("expected degrade note; body: %s", body)
	}
}

// ─── 5. /code/file?path= → degrade note when engine unavailable ──────────────

func TestCodeFileHandler_DegradeNote_WhenEngineUnavailable(t *testing.T) {
	// EngineProvider that always fails.
	failProvider := serve.EngineProvider(func(_ context.Context, _, _ string) (serve.CodeEngine, error) {
		return nil, os.ErrNotExist
	})

	h := serve.NewCodeExplorerHandler(serve.CodeExplorerOptions{
		RealmRoot:      t.TempDir(),
		EngineProvider: failProvider,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/file?path=main.go", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := strings.ToLower(rr.Body.String())
	if !strings.Contains(body, "not indexed") && !strings.Contains(body, "index not available") && !strings.Contains(body, "no code") {
		t.Errorf("expected index-unavailable degrade note; body: %s", body)
	}
}

// ─── 6. graph.go: existing non-md file → CodeFile edge, not Broken ───────────

func TestGraph_CodeFileLink_ExistingSourceFile(t *testing.T) {
	root := t.TempDir()

	// Create a Go source file and a markdown page that links to it.
	writeFile(t, filepath.Join(root, "billing.go"), "package billing\n")
	writeFile(t, filepath.Join(root, "page.md"), "# Page\n\nSee [billing](billing.go) for code.\n")

	g := serve.BuildLinkGraph(root)

	outbound := g.Outbound("page.md")
	if len(outbound) == 0 {
		t.Fatal("expected outbound edge from page.md")
	}

	var found bool
	for _, e := range outbound {
		if strings.HasSuffix(e.Target, "billing.go") || strings.HasSuffix(e.ResolvedPath, "billing.go") {
			if e.Broken {
				t.Errorf("link to billing.go must not be Broken")
			}
			if !e.CodeFile {
				t.Errorf("link to billing.go must have CodeFile=true")
			}
			found = true
		}
	}
	if !found {
		t.Errorf("no edge found targeting billing.go; outbound: %+v", outbound)
	}
}

// ─── 7. graph.go: non-existent source file keeps Broken=true ─────────────────

func TestGraph_CodeFileLink_NonExistentFile_Broken(t *testing.T) {
	root := t.TempDir()

	// No billing.go exists; only the markdown linking to it.
	writeFile(t, filepath.Join(root, "page.md"), "# Page\n\nSee [billing](billing.go) for code.\n")

	g := serve.BuildLinkGraph(root)

	outbound := g.Outbound("page.md")
	if len(outbound) == 0 {
		t.Fatal("expected outbound edge from page.md")
	}

	for _, e := range outbound {
		if strings.HasSuffix(e.Target, "billing.go") {
			if !e.Broken {
				t.Errorf("link to non-existent billing.go must be Broken")
			}
			if e.CodeFile {
				t.Errorf("link to non-existent billing.go must not have CodeFile=true")
			}
		}
	}
}

// ─── 8. layout.html contains #code-modal overlay markup ──────────────────────

func TestLayout_HasCodeModal(t *testing.T) {
	baseURL, shutdown := startTestServer(t, serve.Options{
		Port:      0,
		TargetDir: t.TempDir(),
	})
	defer shutdown()

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	body := readBodyString(t, resp)

	// #code-modal overlay must exist.
	if !strings.Contains(body, `id="code-modal"`) {
		t.Errorf("layout.html must contain #code-modal element")
	}
	// Modal must have source pane.
	if !strings.Contains(body, `id="code-modal-source"`) {
		t.Errorf("layout.html must contain #code-modal-source pane")
	}
	// Modal must have intel pane.
	if !strings.Contains(body, `id="code-modal-intel"`) {
		t.Errorf("layout.html must contain #code-modal-intel pane")
	}
}

// ─── 9. layout.html contains delegated code-link JS handler ──────────────────

func TestLayout_HasCodeLinkHandler(t *testing.T) {
	baseURL, shutdown := startTestServer(t, serve.Options{
		Port:      0,
		TargetDir: t.TempDir(),
	})
	defer shutdown()

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	body := readBodyString(t, resp)

	// The delegated handler must reference the modal and source file extensions.
	if !strings.Contains(body, "code-modal") {
		t.Errorf("layout.html must contain code-modal JS references")
	}
	// The handler must reference at least one common source file extension.
	// The extension object uses bare keys: "go": true, "ts": true, etc.
	hasExt := strings.Contains(body, `"go": true`) || strings.Contains(body, `"ts": true`) ||
		strings.Contains(body, `"py": true`) || strings.Contains(body, `SOURCE_EXTS`)
	if !hasExt {
		t.Errorf("code-link handler must reference source file extensions (SOURCE_EXTS); body length=%d", len(body))
	}
}

// ─── 10. Rail: CodeFile edges render as clickable <a>, not broken span ───────

func TestRail_CodeFileEdge_RendersAsLink(t *testing.T) {
	root := t.TempDir()

	// Create a source file and a markdown page linking to it.
	writeFile(t, filepath.Join(root, "service.go"), "package service\n")
	writeFile(t, filepath.Join(root, "notes.md"), "# Notes\n\nSee [service](service.go).\n")

	g := serve.BuildLinkGraph(root)

	h := serve.NewRailHandler(root, g)

	req := httptest.NewRequest(http.MethodGet, "/rail/notes.md", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()

	// Must NOT be a broken-span for the service.go link.
	if strings.Contains(body, "ctx-broken") && strings.Contains(body, "service.go") {
		t.Errorf("service.go link must not render as ctx-broken; body: %s", body)
	}
	// Must be a clickable anchor to /file/service.go.
	if !strings.Contains(body, `href="/file/service.go"`) {
		t.Errorf("service.go must render as /file/ link in the rail; body: %s", body)
	}
}

// ─── modal test helpers ──────────────────────────────────────────────────────

// readBodyString reads all bytes from resp.Body and returns them as a string.
func readBodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
