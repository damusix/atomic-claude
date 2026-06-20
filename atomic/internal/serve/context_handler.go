// context_handler.go — page handler with right-rail wiring (FE2).
//
// NewPageHandlerWithGraph returns an http.Handler for /page/<relpath> that
// renders the markdown body AND emits htmx OOB swaps so the right-rail
// (#rail-out-content, #rail-in-content, #rail-graph-content) and breadcrumb
// all update to the focused page in a single navigation round-trip.
//
// The right rail is populated by a single GET /rail/<relpath> request (see
// rail_handler.go). The page fragment emits ONE loader inside #rail-graph-content;
// that one request's response carries three OOB swaps for all three rail slots.
package serve

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fragmentRequest reports whether the request should receive an htmx fragment
// (just the #main-pane content) rather than the full shell document.
//
// An htmx history cache-miss restore carries BOTH HX-Request and
// HX-History-Restore-Request; htmx then replaces the whole <body> with the
// response, so it must receive the full shell — returning a bare fragment there
// destroys the nav shell on Back/Forward. Treating a restore as a document load
// (alongside htmx.config.historyRestoreAsHxRequest=false in the shell) keeps the
// shell intact on history navigation.
func fragmentRequest(r *http.Request) bool {
	if r.Header.Get("HX-History-Restore-Request") == "true" {
		return false
	}
	return r.Header.Get("HX-Request") != ""
}

// normRelPath converts a URL path segment to the forward-slash form stored in
// the graph (filepath.ToSlash + filepath.Clean). It strips a leading slash and
// cleans the path so that segments like "." and ".." are resolved before the
// graph lookup, preventing spurious 404s on requests like /context/./b.md.
func normRelPath(p string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(p, "/")))
}

// readFile reads the file at absPath. Wrapper so NewPageHandlerWithGraph does
// not import os directly.
func readFile(absPath string) ([]byte, error) {
	return os.ReadFile(absPath) //nolint:gosec // caller must validate path before calling
}

// baseName returns the base filename from a path string.
func baseName(p string) string {
	return filepath.Base(p)
}

// breadcrumbSegments builds a " › "-joined breadcrumb HTML string from a relpath.
//
// FE7: "docs/reference/serve.md" → "docs › reference › serve.md" (plain text).
// FE8: each ancestor segment is wrapped in an <a> that opens a folder in the nav.
//
//	The final segment (current page) stays plain text.
//
// Ancestor <a> elements carry data-nav-folder=<prefix> so client-side JS can
// find and open the matching <details> in the nav tree without a server round-trip.
// The home (scope) segment uses hx-get="/" so clicking it reloads the landing page.
func breadcrumbSegments(relPath string) string {
	// Normalize to forward slashes and strip leading slash.
	clean := filepath.ToSlash(strings.TrimPrefix(relPath, "/"))
	parts := strings.Split(clean, "/")

	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		// Top-level file: no ancestor links, just the escaped filename.
		return template.HTMLEscapeString(parts[0])
	}

	// Build linked ancestor segments + plain final segment.
	var sb strings.Builder
	// Ancestor segments: parts[0..len-2].
	prefix := ""
	for i, p := range parts[:len(parts)-1] {
		if i > 0 {
			sb.WriteString(" › ")
			prefix += "/"
		}
		prefix += p
		// <a> with data-nav-folder so JS can expand the matching <details> in nav.
		fmt.Fprintf(&sb, `<a href="#" class="breadcrumb-folder" data-nav-folder="%s">%s</a>`,
			template.HTMLEscapeString(prefix),
			template.HTMLEscapeString(p),
		)
	}
	// Final segment: plain text (current page).
	sb.WriteString(" › ")
	sb.WriteString(template.HTMLEscapeString(parts[len(parts)-1]))
	return sb.String()
}

// directoryListingHTML renders a folder (realm-root-relative dirRel) as a
// browsable listing of its immediate markdown files and subfolders when the
// folder has no index file. Subfolders link to /page/<dir>/ (which recurses
// through this same handler); files link to /page/<dir>/<file>. Hidden files
// and skip-dirs are omitted. The result is the inner #page-content body HTML.
func directoryListingHTML(root, dirRel string) string {
	abs, ok := safeResolve(root, dirRel)
	if !ok {
		return `<h1>Folder</h1><p class="dir-empty">This folder cannot be listed.</p>`
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return `<h1>Folder</h1><p class="dir-empty">This folder cannot be listed.</p>`
	}

	var dirs, files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if shouldSkipDir(name) {
				continue
			}
			dirs = append(dirs, name)
		} else if strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".") {
			files = append(files, name)
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)

	var sb strings.Builder
	fmt.Fprintf(&sb, `<h1 class="dir-listing-title">%s/</h1>`, template.HTMLEscapeString(dirRel))

	if len(dirs) == 0 && len(files) == 0 {
		sb.WriteString(`<p class="dir-empty">No markdown files or subfolders here.</p>`)
		return sb.String()
	}

	sb.WriteString(`<ul class="dir-listing">`)
	for _, d := range dirs {
		target := filepath.ToSlash(filepath.Join(dirRel, d)) + "/"
		fmt.Fprintf(&sb,
			`<li><a class="nav-item dir-entry dir-subfolder" hx-get="/page/%s" hx-target="#main-pane" hx-push-url="true" href="/page/%s">%s/</a></li>`,
			template.HTMLEscapeString(target), template.HTMLEscapeString(target), template.HTMLEscapeString(d))
	}
	for _, f := range files {
		target := filepath.ToSlash(filepath.Join(dirRel, f))
		fmt.Fprintf(&sb,
			`<li><a class="nav-item dir-entry" hx-get="/page/%s" hx-target="#main-pane" hx-push-url="true" href="/page/%s">%s</a></li>`,
			template.HTMLEscapeString(target), template.HTMLEscapeString(target), template.HTMLEscapeString(stripMDExt(f)))
	}
	sb.WriteString(`</ul>`)
	return sb.String()
}

// ─── NewPageHandlerWithGraph ──────────────────────────────────────────────────

// pageWithGraphFragmentTmplStr is the htmx fragment variant. It includes OOB
// swaps so the right rail (out/in/graph) and breadcrumb all update when the
// main pane navigates to a new page.
//
// Strategy: emit ONE loader inside #rail-graph-content (OOB) plus a breadcrumb
// OOB swap. The single GET /rail/<relpath> request returns three OOB fragments
// that populate #rail-out-content, #rail-in-content, and #rail-graph-content in
// one shot — no redundant round-trips. hx-swap="none" on the loader prevents
// the loader element from clobbering any sibling content while the rail response
// is in flight.
const pageWithGraphFragmentTmplStr = `<div id="page-content" class="md-content" data-relpath="{{.RelPath}}">
{{.Body}}
</div>
{{if .HasMermaid -}}
<script src="/static/vendor/mermaid.min.js"></script>
<script>
(function() {
  if (window.atomicMermaidInit) { window.atomicMermaidInit(); }
  else if (window.mermaid) { mermaid.initialize({ startOnLoad: false }); mermaid.run(); }
})();
</script>
{{- end}}
<span id="breadcrumb-page" hx-swap-oob="innerHTML">{{.Breadcrumb}}</span>
<div id="rail-graph-content" hx-swap-oob="innerHTML">
  <div hx-get="/rail/{{.RelPath}}" hx-trigger="load" hx-swap="none" class="rail-loader"></div>
</div>`

var pageWithGraphFragmentTmpl = template.Must(template.New("page-with-graph-fragment").Parse(pageWithGraphFragmentTmplStr))

// notFoundFragmentTmplStr is the htmx 404 fragment. It swaps into #main-pane
// and provides a home link so the user can navigate back without losing the shell.
const notFoundFragmentTmplStr = `<div id="page-content" class="md-content not-found">
<h2>Page not found</h2>
<p>{{.Path}}</p>
<p><a href="/" hx-get="/" hx-target="#main-pane" hx-swap="innerHTML">← Home</a></p>
</div>
<span id="breadcrumb-page" hx-swap-oob="innerHTML">not found</span>`

var notFoundFragmentTmpl = template.Must(template.New("not-found-fragment").Parse(notFoundFragmentTmplStr))

// pageWithGraphData is the template data for page-with-graph templates.
type pageWithGraphData struct {
	Title      string
	Breadcrumb template.HTML // " › "-joined path segments, HTML-escaped
	Body       template.HTML
	HasMermaid bool
	RelPath    string
}

// notFoundFragmentData is the data for the 404 fragment template.
type notFoundFragmentData struct {
	Path string
}

// NewPageHandlerWithGraph returns an http.Handler for /page/* that renders
// markdown and wires the right rail. htmx fragment requests emit one OOB loader
// into #rail-graph-content; the loader fires GET /rail/<relpath> which returns
// three OOB swaps (#rail-graph-content, #rail-out-content, #rail-in-content)
// plus the breadcrumb — all in a single request.
//
// The optional shell argument, when provided and non-nil, is used for document
// (non-htmx) loads:
//   - Found page: shell renders layout.html with LandingURL = /page/<relpath>.
//   - Missing page: shell renders layout.html with status 404 and LandingURL
//     pointing at the missing path (which will produce the 404 fragment when
//     #main-pane loads it via htmx).
//
// Without shell, missing pages produce bare http.NotFound; found pages fall
// back to the fragment template (unit tests that don't need the shell use this).
//
// g may be nil; nil degrades to NewPageHandler with no rail wiring.
func NewPageHandlerWithGraph(root string, g *Graph, shell ...*ShellRenderer) http.Handler {
	var sh *ShellRenderer
	if len(shell) > 0 {
		sh = shell[0]
	}
	if g == nil {
		return NewPageHandler(root)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/page/")
		if relPath == "" || relPath == "/" {
			http.NotFound(w, r)
			return
		}

		isHX := fragmentRequest(r)

		abs, ok := safeResolve(root, relPath)
		if !ok {
			serve404(w, r, relPath, "/page/"+relPath, isHX, sh)
			return
		}

		// renderRelPath is the path whose content/breadcrumb/rail we render. It
		// equals relPath for a normal file, the resolved index file when a folder
		// has one, or the folder itself when we render a generated listing.
		renderRelPath := relPath
		var bodyHTML string
		var hasMermaid bool

		if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
			// Folder load: a directory has no readable file. Serve its index file
			// if one exists (README/index/.claude signals), else a listing of the
			// folder's markdown files and subfolders so the user is never stranded.
			dirRel := normRelPath(relPath)
			if idxRel, found := resolveDirIndex(root, dirRel); found {
				idxAbs, idxOK := safeResolve(root, idxRel)
				if !idxOK {
					serve404(w, r, relPath, "/page/"+relPath, isHX, sh)
					return
				}
				data, err := readFile(idxAbs)
				if err != nil {
					serve404(w, r, relPath, "/page/"+relPath, isHX, sh)
					return
				}
				bodyHTML, hasMermaid, err = RenderMarkdownWithGraph(data, root, idxRel, g)
				if err != nil {
					http.Error(w, "render error", http.StatusInternalServerError)
					return
				}
				renderRelPath = idxRel
			} else {
				bodyHTML = directoryListingHTML(root, dirRel)
				renderRelPath = dirRel
			}
		} else {
			data, err := readFile(abs)
			if err != nil {
				serve404(w, r, relPath, "/page/"+relPath, isHX, sh)
				return
			}
			bodyHTML, hasMermaid, err = RenderMarkdownWithGraph(data, root, normRelPath(relPath), g)
			if err != nil {
				http.Error(w, "render error", http.StatusInternalServerError)
				return
			}
		}

		title := baseName(renderRelPath)

		tplData := pageWithGraphData{
			Title:      title,
			Breadcrumb: template.HTML(breadcrumbSegments(renderRelPath)), //nolint:gosec // breadcrumbSegments HTML-escapes all segments
			Body:       template.HTML(bodyHTML),                          //nolint:gosec // goldmark output / escaped listing
			HasMermaid: hasMermaid,
			RelPath:    renderRelPath,
		}

		if isHX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = pageWithGraphFragmentTmpl.Execute(w, tplData)
			return
		}

		// Document load (no HX-Request): render the shell with LandingURL = this page.
		// FE8: the shell boots, /nav loads, and #main-pane fragment-loads this page.
		if sh != nil {
			_ = sh.Render(w, "/page/"+relPath, http.StatusOK)
			return
		}

		// Fallback: legacy shell-less full page (shell not wired, e.g. unit tests
		// that call NewPageHandlerWithGraph directly without a ShellRenderer).
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pageWithGraphFragmentTmpl.Execute(w, tplData)
	})
}

// serve404 handles a missing resource for both htmx and document loads.
//
// htmx: returns a 404 fragment containing relPath and a home link.
// document: renders the shell with status 404 and LandingURL = contentURL so
// #main-pane loads the 404 fragment via the htmx pipeline, or falls back to a
// bare 404 fragment when no shell is available.
//
// contentURL is the full /page/<relpath> or /file/<relpath> URL that the shell
// will pass to hx-get on #main-pane — callers must supply the correct prefix.
func serve404(w http.ResponseWriter, _ *http.Request, relPath, contentURL string, isHX bool, sh *ShellRenderer) {
	if isHX {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_ = notFoundFragmentTmpl.Execute(w, notFoundFragmentData{Path: relPath})
		return
	}

	if sh != nil {
		// Render the shell with status 404. LandingURL = contentURL so
		// #main-pane loads the 404 fragment via the existing htmx pipeline.
		_ = sh.Render(w, contentURL, http.StatusNotFound)
		return
	}

	// Fallback: bare 404 fragment (no shell available).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = notFoundFragmentTmpl.Execute(w, notFoundFragmentData{Path: relPath})
}
