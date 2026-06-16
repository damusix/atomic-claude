package serve_test

// search_stream_test.go — tests for the SSE search stream (/search/stream) and
// the code-search empty-state fix. httptest.ResponseRecorder implements
// http.Flusher and captures the full SSE payload, so assertions run on the
// recorded body after the handler returns.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

func TestSearchStream_RealmCodeStreamsPerMember(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n\n<wiki-scan generated=\"2026-01-01\" root=\""+realmRoot+"\">\n</wiki-scan>\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})
	buildCodeTOML(t, realmRoot, []struct{ key, path string }{
		{"alpha", "repos/alpha"},
		{"beta", "repos/beta"},
	})

	// alpha indexed; beta absent → cold (errorSearchFn path).
	searchFn := makeKeyedSearchFn(map[string][]types.SearchResult{
		"alpha": {fakeResult("Resolve", "function", "internal/alpha/resolve.go", 42)},
	})

	h := serve.NewSearchStreamHandler(serve.SearchStreamOptions{
		NavRoot:      realmRoot,
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     searchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/search/stream?q=Resolve&src=code", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "event: code") {
		t.Errorf("expected per-member code events; body:\n%s", body)
	}
	if !strings.Contains(body, "[alpha]") {
		t.Errorf("expected the alpha group; body:\n%s", body)
	}
	if !strings.Contains(strings.ToLower(body), "not indexed") {
		t.Errorf("expected beta's not-indexed note; body:\n%s", body)
	}
	if !strings.Contains(body, "event: end") {
		t.Errorf("expected a terminal end event; body:\n%s", body)
	}
	// src=code must not emit a markdown event.
	if strings.Contains(body, "event: md") {
		t.Errorf("src=code should not emit md events; body:\n%s", body)
	}
}

func TestSearchStream_MarkdownEvent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "doc.md"), "# Doc\n\nfind the needle here\n")

	h := serve.NewSearchStreamHandler(serve.SearchStreamOptions{
		NavRoot:      root,
		RealmRoot:    root,
		ClaudeMDPath: filepath.Join(root, "CLAUDE.md"),
		SearchFn:     errorSearchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/search/stream?q=needle&src=md", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "event: md") {
		t.Errorf("expected an md event; body:\n%s", body)
	}
	if !strings.Contains(body, "doc.md") {
		t.Errorf("expected the md match for doc.md; body:\n%s", body)
	}
	if !strings.Contains(body, "event: end") {
		t.Errorf("expected an end event; body:\n%s", body)
	}
	if strings.Contains(body, "event: code") {
		t.Errorf("src=md must not emit code events; body:\n%s", body)
	}
}

func TestSearchStream_EmptyQueryEndsImmediately(t *testing.T) {
	root := t.TempDir()
	h := serve.NewSearchStreamHandler(serve.SearchStreamOptions{
		NavRoot:      root,
		RealmRoot:    root,
		ClaudeMDPath: filepath.Join(root, "CLAUDE.md"),
		SearchFn:     errorSearchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/search/stream?q=&src=all", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "event: end") {
		t.Errorf("empty query should still emit a terminal end; body:\n%s", body)
	}
	if strings.Contains(body, "event: md") || strings.Contains(body, "event: code") {
		t.Errorf("empty query should emit no result events; body:\n%s", body)
	}
}

// TestCodeSearch_NoMembersShowsNote guards the regression the user hit: a realm
// with no code index returned an empty results div with no feedback. It must now
// render a clear not-indexed note instead.
func TestCodeSearch_NoMembersShowsNote(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := filepath.Join(realmRoot, "wiki", "index.md")
	writeFile(t, wikiIndexPath, "# wiki\n\n<wiki-scan generated=\"2026-01-01\" root=\""+realmRoot+"\">\n</wiki-scan>\n")
	claudeMDPath := filepath.Join(realmRoot, "CLAUDE.md")
	buildClaudeMD(t, claudeMDPath, []string{wikiIndexPath})
	buildCodeTOML(t, realmRoot, []struct{ key, path string }{}) // zero code members

	h := serve.NewCodeSearchHandler(serve.CodeSearchOptions{
		RealmRoot:    realmRoot,
		ClaudeMDPath: claudeMDPath,
		SearchFn:     errorSearchFn,
	})

	req := httptest.NewRequest(http.MethodGet, "/code/search?q=anything", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, `code-search-results"></div>`) {
		t.Errorf("empty results div = no feedback (the original bug); body:\n%s", body)
	}
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "no code index") && !strings.Contains(lower, "not indexed") {
		t.Errorf("expected a clear not-indexed note; body:\n%s", body)
	}
}
