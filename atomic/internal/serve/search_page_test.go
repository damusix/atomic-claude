package serve_test

// search_page_test.go — tests for the dedicated /search page handler. The page
// is presentation-only: it carries a data-search-stream attribute that the
// client uses to open an SSE EventSource (/search/stream), and section
// containers (data-section="md"|"code") that the stream fills. These tests
// assert the right stream URL + sections are emitted, not search results.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

func TestSearchPage_AllSourcesEmitBothSections(t *testing.T) {
	h := serve.NewSearchPageHandler(nil) // nil shell → fragment for non-HX too
	req := httptest.NewRequest(http.MethodGet, "/search?q=property&src=all", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	for _, want := range []string{
		`data-search-stream="/search/stream?q=property`, // SSE seam
		`data-section="md"`,                             // markdown section
		`data-section="code"`,                           // code section
		`class="search-tab active"`,                     // the active (all) tab
		`id="breadcrumb-page"`,                          // breadcrumb OOB swap
	} {
		if !strings.Contains(body, want) {
			t.Errorf("search page (src=all) missing %q in:\n%s", want, body)
		}
	}
}

func TestSearchPage_SrcMdOnlyShowsMarkdown(t *testing.T) {
	h := serve.NewSearchPageHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/search?q=x&src=md", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `data-section="md"`) {
		t.Errorf("src=md should include the markdown section, got:\n%s", body)
	}
	if strings.Contains(body, `data-section="code"`) {
		t.Errorf("src=md must NOT include the code section, got:\n%s", body)
	}
	if !strings.Contains(body, `src=md"`) {
		t.Errorf("stream URL should carry src=md, got:\n%s", body)
	}
}

func TestSearchPage_SrcCodeOnlyShowsCode(t *testing.T) {
	h := serve.NewSearchPageHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/search?q=x&src=code", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `data-section="code"`) {
		t.Errorf("src=code should include the code section, got:\n%s", body)
	}
	if strings.Contains(body, `data-section="md"`) {
		t.Errorf("src=code must NOT include the markdown section, got:\n%s", body)
	}
}

func TestSearchPage_EmptyQueryShowsForm(t *testing.T) {
	h := serve.NewSearchPageHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `class="search-page-form"`) {
		t.Errorf("empty query should render a search form, got:\n%s", body)
	}
	if strings.Contains(body, "data-search-stream") || strings.Contains(body, "data-section=") {
		t.Errorf("empty query must not emit a stream seam or sections, got:\n%s", body)
	}
}

func TestSearchPage_UnknownSrcDefaultsToAll(t *testing.T) {
	h := serve.NewSearchPageHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/search?q=z&src=bogus", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `data-section="md"`) || !strings.Contains(body, `data-section="code"`) {
		t.Errorf("unknown src should default to all (both sections), got:\n%s", body)
	}
}
