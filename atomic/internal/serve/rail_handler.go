// rail_handler.go — FE2: right-rail compositing handler.
//
// GET /rail/<relpath> renders three htmx OOB fragments for the right rail:
//
//   - #rail-out-content — outbound links from the focused page (broken/ambiguous/
//     external annotations reused from context_handler.go rendering).
//   - #rail-in-content  — backlinks to the focused page; orphan note when the page
//     has no inbound links.
//   - #rail-graph-content — a compact Cytoscape mini-graph seeded by
//     /graph/data?node=<page>&depth=1. The concentric layout is used for the small
//     rail container (lighter than ELK).
//
// Path-traversal guard and graph-membership 404 are both enforced.
// Reuses the broken/ambiguous/external rendering logic from contextFragmentTmpl.
package serve

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
)

// railFragmentTmplStr renders the three OOB fragments for the right rail.
// Each <div> carries hx-swap-oob="innerHTML" so htmx swaps each slot
// independently when the fragment is received.
//
// #rail-graph-content: carries a data-rail-graph-url attribute so the shell's
// htmx.onLoad handler (registered once in layout.html) can detect the swapped
// container and mount a Cytoscape mini-graph into it. Inline <script> tags in
// OOB innerHTML swaps are not reliably executed by htmx — the delegated
// htmx.onLoad pattern is the canonical htmx 2 solution.
//
// Note: data-rail-graph-url is intentionally distinct from the #mode-system
// button's JS click handler (FE3) — the system toggle has no data-graph-url
// attribute; it is wired via a pure JS click listener. Using a separate
// attribute name ensures the shell's htmx.onLoad selector ([data-rail-graph-url])
// cannot collide with any click-handler seam on #mode-system.
const railFragmentTmplStr = `<div id="rail-out-content" hx-swap-oob="innerHTML">
  {{if .Outbound}}
  <ul class="rail-link-list">
    {{range .Outbound}}
    <li>
      {{if .Broken}}<span class="ctx-broken" title="broken link">&#x274C; {{.Target}}</span>
      {{else if .CodeFile}}<a class="ctx-link ctx-codefile" href="/file/{{.ResolvedPath}}" title="open source file">&#x1F4C4; {{.Target}}</a>
      {{else if .Ambiguous}}<a class="ctx-link ctx-ambiguous" hx-get="/page/{{.ResolvedPath}}" hx-target="#main-pane" hx-push-url="true" href="/page/{{.ResolvedPath}}" title="ambiguous: multiple files match">&#x26A0; {{.Target}}</a>
      {{else if .ResolvedPath}}<a class="ctx-link" hx-get="/page/{{.ResolvedPath}}" hx-target="#main-pane" hx-push-url="true" href="/page/{{.ResolvedPath}}">{{.Target}}</a>
      {{else if .External}}<a class="ctx-link ctx-external" href="{{.Target}}" target="_blank" rel="noopener">&#x1F517; {{.Target}}</a>
      {{else}}<a class="ctx-link ctx-anchor" href="{{.Target}}">{{.Target}}</a>
      {{end}}
    </li>
    {{end}}
  </ul>
  {{else}}<p class="rail-empty">no outbound links</p>{{end}}
</div>
<div id="rail-in-content" hx-swap-oob="innerHTML">
  {{if .Orphan}}<p class="rail-orphan">&#x1F3DC; orphan — no inbound links</p>{{end}}
  {{if .Backlinks}}
  <ul class="rail-link-list">
    {{range .Backlinks}}<li><a class="ctx-link" hx-get="/page/{{.}}" hx-target="#main-pane" hx-push-url="true" href="/page/{{.}}">{{.}}</a></li>
    {{end}}
  </ul>
  {{else if not .Orphan}}<p class="rail-empty">no backlinks</p>{{end}}
</div>
<div id="rail-graph-content" hx-swap-oob="innerHTML">
  <div id="rail-cy-{{.CyID}}" class="rail-cy-container"
       data-rail-graph-url="/graph/data?node={{.PageEncoded}}&amp;depth=1"
       data-focus-node="{{.Page}}"></div>
</div>`

// railTmplFuncs provides the "not" helper used in the template.
var railTmplFuncs = template.FuncMap{
	"not": func(b bool) bool { return !b },
}

var railFragmentTmpl = template.Must(
	template.New("rail-fragment").Funcs(railTmplFuncs).Parse(railFragmentTmplStr),
)

// railTmplData is the data passed to railFragmentTmpl.
type railTmplData struct {
	Page        string
	PageEncoded string // URL-encoded page path for the fetch call
	CyID        string // unique-per-render ID to avoid collisions when OOB-swapped
	Orphan      bool
	Backlinks   []string
	Outbound    []Edge
}

// NewRailHandler returns an http.Handler for /rail/<relpath>.
// It renders three OOB fragments for the right rail using the prebuilt Graph g.
// Traversal outside root and pages absent from the graph yield 404.
func NewRailHandler(root string, g *Graph) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/rail/")
		if relPath == "" || relPath == "/" {
			http.NotFound(w, r)
			return
		}

		// Path-traversal guard.
		_, ok := safeResolve(root, relPath)
		if !ok {
			http.NotFound(w, r)
			return
		}

		// Graph-membership check: page must be a known .md file (O(1) via nodeSet).
		rel := normRelPath(relPath)
		if !g.Has(rel) {
			http.NotFound(w, r)
			return
		}

		// Use the cyID to disambiguate concurrent mini-graph containers —
		// slashes replaced with hyphens produce a valid HTML id suffix.
		cyID := strings.ReplaceAll(rel, "/", "-")
		cyID = strings.ReplaceAll(cyID, ".", "-")

		data := railTmplData{
			Page:        rel,
			PageEncoded: rel, // forward-slash paths are safe in query params
			CyID:        cyID,
			Orphan:      g.IsOrphan(rel),
			Backlinks:   g.Backlinks(rel),
			Outbound:    g.Outbound(rel),
		}

		var buf bytes.Buffer
		if err := railFragmentTmpl.Execute(&buf, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
	})
}
