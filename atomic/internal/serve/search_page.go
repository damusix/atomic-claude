// search_page.go — dedicated full-pane search results page (/search).
//
// Route: GET /search?q=<query>&src=<md|code|all>
//
// This is a presentation-only page: it does NOT run any search itself. It
// composes the existing fragment endpoints (/search/md and /code/search) via
// nested htmx loads, so the dialog (live quick-jump) and this page (browse all
// results) share one search backend.
//
//   - Document load (no HX-Request): render the full shell (FE8 envelope) with
//     this URL as the landing content, so refresh and deep-linking keep nav.
//   - HX request: return the page fragment for #main-pane (with a breadcrumb
//     OOB swap to "search"), whose result sections lazy-load the md/code
//     fragment endpoints.
//
// src selects which sections to show: "md", "code", or "all" (default).
package serve

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

// NewSearchPageHandler returns an http.Handler for GET /search.
// shell, when non-nil, wraps document loads in the full layout shell.
func NewSearchPageHandler(shell *ShellRenderer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		src := normalizeSearchSrc(r.URL.Query().Get("src"))
		isHX := fragmentRequest(r)

		// Document load → shell envelope with this URL as the landing content.
		if !isHX && shell != nil {
			landing := "/search?q=" + url.QueryEscape(q) + "&src=" + src
			_ = shell.Render(w, landing, http.StatusOK)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderSearchPageFragment(q, src)))
	})
}

// normalizeSearchSrc clamps the src param to a known value (default "all").
func normalizeSearchSrc(src string) string {
	switch src {
	case "md", "code", "all":
		return src
	default:
		return "all"
	}
}

// renderSearchPageFragment builds the #main-pane fragment for the search page.
// With a query, the page carries a data-search-stream attribute: the shell's
// htmx.onLoad handler opens an EventSource to it and routes md/code/end events
// into the section containers (data-section="md"|"code"), which start showing a
// loading indicator.
func renderSearchPageFragment(q, src string) string {
	var sb strings.Builder

	if q == "" {
		sb.WriteString(`<div id="page-content" class="md-content search-page" data-relpath="">`)
		sb.WriteString(`<h1 class="search-page-title">Search</h1>`)
		sb.WriteString(`<p class="search-page-hint">Open the search dialog with <kbd>⌘K</kbd> / <kbd>Ctrl K</kbd>, or search here:</p>`)
		sb.WriteString(`<form class="search-page-form" hx-get="/search" hx-target="#main-pane" hx-push-url="true">`)
		sb.WriteString(`<input class="search-page-input" name="q" type="search" placeholder="Search markdown or code…" autocomplete="off" autofocus>`)
		fmt.Fprintf(&sb, `<input type="hidden" name="src" value="%s">`, template.HTMLEscapeString(src))
		sb.WriteString(`<button type="submit">Search</button>`)
		sb.WriteString(`</form>`)
		sb.WriteString(`</div>`)
		sb.WriteString(searchBreadcrumbOOB())
		return sb.String()
	}

	qEsc := url.QueryEscape(q)
	streamURL := "/search/stream?q=" + qEsc + "&src=" + src

	fmt.Fprintf(&sb,
		`<div id="page-content" class="md-content search-page" data-relpath="" data-search-stream="%s">`,
		template.HTMLEscapeString(streamURL))
	sb.WriteString(`<h1 class="search-page-title">Search</h1>`)
	fmt.Fprintf(&sb, `<p class="search-page-query">Results for <strong>%s</strong></p>`, template.HTMLEscapeString(q))

	// Source tabs (re-query links).
	sb.WriteString(`<nav class="search-page-tabs">`)
	for _, t := range []struct{ key, label string }{{"all", "All"}, {"md", "Markdown"}, {"code", "Code"}} {
		cls := "search-tab"
		if t.key == src {
			cls += " active"
		}
		href := "/search?q=" + qEsc + "&src=" + t.key
		fmt.Fprintf(&sb,
			`<a class="%s" hx-get="%s" hx-target="#main-pane" hx-push-url="true" href="%s">%s</a>`,
			cls, href, href, t.label)
	}
	sb.WriteString(`</nav>`)

	// Result sections — filled by the SSE handler; start with a loading indicator.
	if src == "md" || src == "all" {
		sb.WriteString(`<section class="search-page-section"><h2 class="search-page-section-title">Markdown</h2>`)
		sb.WriteString(`<div class="search-page-results" data-section="md"><p class="loading search-loading">Searching markdown…</p></div>`)
		sb.WriteString(`</section>`)
	}
	if src == "code" || src == "all" {
		sb.WriteString(`<section class="search-page-section"><h2 class="search-page-section-title">Code</h2>`)
		sb.WriteString(`<div class="search-page-results" data-section="code"><p class="loading search-loading">Searching code…</p></div>`)
		sb.WriteString(`</section>`)
	}

	sb.WriteString(`</div>`)
	sb.WriteString(searchBreadcrumbOOB())
	return sb.String()
}

// searchBreadcrumbOOB is the breadcrumb OOB swap for the search page.
func searchBreadcrumbOOB() string {
	return `<span id="breadcrumb-page" hx-swap-oob="innerHTML">search</span>`
}
