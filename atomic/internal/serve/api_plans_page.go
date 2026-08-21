// The /api/plans/page endpoint: resolves a worktree id + relative path
// against the plans registry (api_plans.go), never against render.go's
// served-root allow-list — a worktree can sit anywhere on disk, and the
// only thing that may map an id to a filesystem root is the resolver the
// aggregator itself built. See docs/design/serve-plans-page.md "What
// aggregation actually does" for why that allow-list stays untouched.
package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// resolvePlansPath resolves relPath under root — one of N worktree roots the
// aggregator enumerated, never the single served root render.go's content
// routes are scoped to — via the same containment algorithm safeResolve
// uses. A separate call rather than a shared one, because widening
// safeResolve's allowed root would relax it at every other call site in
// this package for the benefit of one surface.
func resolvePlansPath(root, relPath string) (string, bool) {
	return resolveContained(root, relPath)
}

// plansContentType picks the raw-mode Content-Type from the aggregator's
// classification (kind — never a client-supplied extension), which is a
// floor, not a ceiling: http.DetectContentType may narrow a non-HTML type
// (e.g. text/plain -> text/plain; charset=utf-8), but a sniff that lands on
// text/html or any XML type is clamped rather than trusted, so a file
// classified "file" or "markdown" whose bytes happen to open an HTML
// signature stays inert.
func plansContentType(kind string, data []byte) string {
	if kind == "html" {
		return "text/html; charset=utf-8"
	}
	sniffed := http.DetectContentType(data)
	if isHTMLOrXML(sniffed) {
		if kind == "file" {
			return "application/octet-stream"
		}
		return "text/plain; charset=utf-8"
	}
	return sniffed
}

func isHTMLOrXML(contentType string) bool {
	base, _, _ := strings.Cut(contentType, ";")
	base = strings.TrimSpace(base)
	return base == "text/html" || base == "application/xhtml+xml" ||
		strings.HasSuffix(base, "/xml") || strings.HasSuffix(base, "+xml")
}

// plansPageHandler serves GET /api/plans/page?worktree=<id>&path=<relpath>[&raw=1].
func plansPageHandler(registry *plansRegistry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		worktreeID := q.Get("worktree")
		relPath := q.Get("path")
		raw := q.Get("raw") == "1"

		if worktreeID == "" || relPath == "" {
			writeAPIError(w, http.StatusBadRequest, "missing worktree or path")
			return
		}

		root, ok := registry.resolveWorktree(worktreeID)
		if !ok {
			writeAPIError(w, http.StatusNotFound, "unknown worktree: "+worktreeID)
			return
		}

		abs, ok := resolvePlansPath(root, relPath)
		if !ok {
			writeAPIError(w, http.StatusNotFound, "path traversal rejected: "+relPath)
			return
		}

		info, statErr := os.Stat(abs)
		if statErr != nil || info.IsDir() {
			writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
			return
		}

		data, err := os.ReadFile(abs) //nolint:gosec // path validated by resolvePlansPath
		if err != nil {
			writeAPIError(w, http.StatusNotFound, "not found: "+relPath)
			return
		}

		if raw {
			kind := classifyBundleFile(filepath.Base(relPath))
			w.Header().Set("Content-Type", plansContentType(kind, data))
			// The iframe sandbox the page applies is the primary
			// containment, but this URL is reachable by direct navigation
			// or a shared link, bypassing the iframe — and the same origin
			// serves unauthenticated /api/bus/ write routes.
			w.Header().Set("Content-Security-Policy", "sandbox")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}

		renderRelPath := normRelPath(relPath)
		bodyHTML, hasMermaid, renderErr := RenderMarkdownWithGraph(data, root, renderRelPath, nil)
		if renderErr != nil {
			writeAPIError(w, http.StatusInternalServerError, "render failed: "+renderErr.Error())
			return
		}
		writeAPIJSON(w, apiPageResponse{
			HTML:       bodyHTML,
			Title:      baseName(renderRelPath),
			RelPath:    renderRelPath,
			HasMermaid: hasMermaid,
			Breadcrumb: breadcrumbSegmentsData(renderRelPath),
		})
	})
}
