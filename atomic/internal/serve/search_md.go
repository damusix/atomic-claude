// search_md.go — FE5: markdown full-text search endpoint (/search/md).
//
// Route: GET /search/md?q=<query>
//
// Performs a literal, case-insensitive substring search across all *.md files
// reachable from NavRoot (using shouldSkipDir for directory filtering).
//
// For each matching line the handler emits one result item:
//
//	file path (realm-root-relative)  ·  line number  ·  trimmed snippet
//
// Each item carries a /page/<file> navigation hook so the FE5 delegated
// handler (or data-page attribute) can load the file into #main-pane.
//
// Design constraints:
//   - Empty/whitespace query → empty fragment (200).
//   - Results capped at 50; a truncation note is appended when the cap fires.
//   - Query is treated as a literal substring to grep, not a file path;
//     no filesystem access is performed on the query value itself.
//   - Snippet is trimmed to ≤120 chars to stay usable in a narrow dropdown.
//   - HX-Request: true → fragment only; otherwise a thin full-page wrapper.
package serve

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	mdSearchResultCap     = 50
	mdSearchSnippetMaxLen = 120
)

// MdSearchOptions configures NewMdSearchHandler.
type MdSearchOptions struct {
	// NavRoot is the directory to walk for .md files.
	// Subdirectories matching shouldSkipDir are excluded.
	NavRoot string
}

// mdSearchHandler implements http.Handler for /search/md.
type mdSearchHandler struct {
	navRoot string
}

// NewMdSearchHandler returns an http.Handler for GET /search/md?q=...
func NewMdSearchHandler(opts MdSearchOptions) http.Handler {
	return &mdSearchHandler{navRoot: opts.NavRoot}
}

// mdMatch is one matching line inside a .md file.
type mdMatch struct {
	// RelPath is the file path relative to NavRoot (forward slashes).
	RelPath string
	// Line is the 1-based line number of the match.
	Line int
	// Snippet is a trimmed excerpt of the matching line.
	Snippet string
}

func (h *mdSearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	isHTMX := r.Header.Get("HX-Request") == "true"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var sb strings.Builder

	if !isHTMX {
		sb.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
		sb.WriteString(`<title>MD Search</title></head><body>`)
	}

	if query == "" {
		// Empty query → empty fragment, no items.
		if !isHTMX {
			sb.WriteString(`</body></html>`)
		}
		fmt.Fprint(w, sb.String())
		return
	}

	matches, truncated := h.search(query)
	renderMdResults(&sb, query, matches, truncated)

	if !isHTMX {
		sb.WriteString(`</body></html>`)
	}
	fmt.Fprint(w, sb.String())
}

// search walks navRoot and collects up to mdSearchResultCap matching lines.
// Returns the matches and a bool indicating whether the cap was hit.
func (h *mdSearchHandler) search(query string) ([]mdMatch, bool) {
	lower := strings.ToLower(query)
	var matches []mdMatch
	truncated := false

	// A non-nil callback return signals the result cap was hit; intentionally discarded.
	_ = filepath.WalkDir(h.navRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if path == h.navRoot {
				return nil // never skip the root itself
			}
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || hiddenFile(d.Name()) {
			return nil
		}

		relPath, relErr := filepath.Rel(h.navRoot, path)
		if relErr != nil {
			return nil
		}
		// Normalize to forward slashes for URL construction.
		relPath = filepath.ToSlash(relPath)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for lineIdx, line := range lines {
			if strings.Contains(strings.ToLower(line), lower) {
				snippet := strings.TrimSpace(line)
				if len(snippet) > mdSearchSnippetMaxLen {
					snippet = snippet[:mdSearchSnippetMaxLen] + "…"
				}
				matches = append(matches, mdMatch{
					RelPath: relPath,
					Line:    lineIdx + 1,
					Snippet: snippet,
				})
				if len(matches) >= mdSearchResultCap {
					truncated = true
					// Signal early termination by returning a sentinel.
					return fmt.Errorf("cap") // WalkDir will stop; error is discarded by caller
				}
			}
		}
		return nil
	})

	return matches, truncated
}

// renderMdResults writes the result list HTML into sb.
func renderMdResults(sb *strings.Builder, query string, matches []mdMatch, truncated bool) {
	sb.WriteString(`<ul class="md-search-result-list">`)

	for _, m := range matches {
		// Each item: a link that loads /page/<relPath> into #main-pane.
		// href="/page/<relPath>" — the FE5 delegated handler intercepts these
		// (data-page attribute or href pattern) to use htmx.ajax.
		href := "/page/" + m.RelPath
		sb.WriteString(`<li class="md-search-result" data-page="`)
		sb.WriteString(template.HTMLEscapeString(href))
		sb.WriteString(`">`)
		sb.WriteString(`<a class="md-search-link" href="`)
		sb.WriteString(template.HTMLEscapeString(href))
		sb.WriteString(`">`)

		// file:line label
		sb.WriteString(`<span class="md-search-loc">`)
		sb.WriteString(template.HTMLEscapeString(m.RelPath))
		sb.WriteString(fmt.Sprintf(`:%d`, m.Line))
		sb.WriteString(`</span>`)

		// snippet
		sb.WriteString(` — <span class="md-search-snippet">`)
		sb.WriteString(template.HTMLEscapeString(m.Snippet))
		sb.WriteString(`</span>`)

		sb.WriteString(`</a>`)
		sb.WriteString(`</li>`)
	}

	sb.WriteString(`</ul>`)

	if truncated {
		sb.WriteString(`<p class="md-search-truncated">`)
		sb.WriteString(fmt.Sprintf(`Showing first %d results — refine your query to narrow down.`, mdSearchResultCap))
		sb.WriteString(`</p>`)
	}

	if len(matches) == 0 {
		sb.WriteString(`<p class="md-search-empty">No results.</p>`)
	}
}
