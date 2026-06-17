// render.go — goldmark+chroma markdown renderer and chroma source-file viewer.
//
// RenderMarkdown converts markdown bytes to an HTML fragment string using
// goldmark with GFM extensions (tables, strikethrough, autolinks, tasklists).
// Fenced code blocks with language "mermaid" are emitted as
// <pre class="mermaid">…raw…</pre>; all others are chroma-highlighted.
//
// NewPageHandler returns an http.Handler for /page/* that resolves paths
// relative to a root directory, enforces a path-traversal guard, renders
// markdown via RenderMarkdown, and responds with a full HTML page including
// a conditional mermaid script.
//
// NewFileHandler returns an http.Handler for /file/* that resolves paths
// relative to a root directory, enforces the same traversal guard, and
// responds with chroma-highlighted HTML with per-line id="L<n>" anchors.
package serve

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	chroma "github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldrenderer "github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// chromaStyleName is the chroma style used for all syntax highlighting.
// "monokai" pairs well with the dark app.css theme.
const chromaStyleName = "monokai"

// chromaFmt is the shared chroma HTML formatter (inline styles, no line numbers —
// line numbers for /file/* are added manually via wrapWithLineAnchors).
var chromaFmt = chromahtml.New(
	chromahtml.TabWidth(4),
)

// chromaHighlight returns an HTML string of the highlighted code.
// Falls back to plain HTML-escaped text on any chroma error.
func chromaHighlight(lang, code string) string {
	style := styles.Get(chromaStyleName)
	if style == nil {
		style = styles.Fallback
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iter, err := lexer.Tokenise(nil, code)
	if err != nil {
		return template.HTMLEscapeString(code)
	}
	var buf bytes.Buffer
	if err := chromaFmt.Format(&buf, style, iter); err != nil {
		return template.HTMLEscapeString(code)
	}
	return buf.String()
}

// chromaHighlightLines returns a <table class="file-view"> with chroma-highlighted
// code split into rows, each row having id="L<n>" for anchor navigation.
func chromaHighlightLines(lang, code string) string {
	style := styles.Get(chromaStyleName)
	if style == nil {
		style = styles.Fallback
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		// Try matching by filename extension, e.g. "go" → Go lexer.
		lexer = lexers.Match("file." + lang)
	}
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iter, err := lexer.Tokenise(nil, code)
	if err != nil {
		return buildPlainLineView(code)
	}
	var buf bytes.Buffer
	if err := chromaFmt.Format(&buf, style, iter); err != nil {
		return buildPlainLineView(code)
	}
	return wrapWithLineAnchors(buf.String())
}

// wrapWithLineAnchors strips the outer <pre>…</pre> that chroma emits,
// splits on newlines, and wraps each line in a <tr id="L<n>"> row.
func wrapWithLineAnchors(highlighted string) string {
	// Strip the outer <pre …> tag (chroma always emits one).
	inner := highlighted
	if preStart := strings.Index(inner, ">"); strings.HasPrefix(strings.TrimSpace(inner), "<pre") && preStart >= 0 {
		inner = inner[preStart+1:]
	}
	trimmed := strings.TrimSpace(inner)
	if strings.HasSuffix(trimmed, "</pre>") {
		inner = trimmed[:len(trimmed)-len("</pre>")]
	}

	rawLines := strings.Split(inner, "\n")
	// Drop trailing empty entry from the final newline.
	for len(rawLines) > 0 && strings.TrimSpace(rawLines[len(rawLines)-1]) == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	var sb strings.Builder
	sb.WriteString(`<table class="file-view"><tbody>`)
	for i, line := range rawLines {
		n := i + 1
		// %s is intentional: chroma already emits escaped HTML, so no further escaping is needed here.
		fmt.Fprintf(&sb, `<tr id="L%d"><td class="ln"><a href="#L%d">%d</a></td><td class="ld">%s</td></tr>`, n, n, n, line)
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String()
}

// buildPlainLineView is the fallback when chroma fails.
func buildPlainLineView(code string) string {
	lines := strings.Split(code, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var sb strings.Builder
	sb.WriteString(`<table class="file-view"><tbody>`)
	for i, line := range lines {
		n := i + 1
		fmt.Fprintf(&sb, `<tr id="L%d"><td class="ln"><a href="#L%d">%d</a></td><td class="ld">%s</td></tr>`,
			n, n, n, template.HTMLEscapeString(line))
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String()
}

// ─── mermaid-aware goldmark code-block renderer ──────────────────────────────

// mermaidCodeRenderer is a goldmark NodeRenderer that handles FencedCodeBlock
// nodes. Language "mermaid" → raw <pre class="mermaid">; others → chroma.
type mermaidCodeRenderer struct {
	hasMermaid *bool
}

// RegisterFuncs registers the FencedCodeBlock rendering function.
func (r *mermaidCodeRenderer) RegisterFuncs(reg goldrenderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCode)
}

func (r *mermaidCodeRenderer) renderFencedCode(
	w util.BufWriter,
	source []byte,
	n ast.Node,
	entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	node, ok := n.(*ast.FencedCodeBlock)
	if !ok {
		return ast.WalkContinue, nil
	}
	lang := string(node.Language(source))

	// Collect raw code text from lines.
	var buf bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		buf.Write(seg.Value(source))
	}
	code := buf.String()

	if strings.EqualFold(lang, "mermaid") {
		*r.hasMermaid = true
		_, _ = w.WriteString(`<pre class="mermaid">`)
		_, _ = w.WriteString(template.HTMLEscapeString(code))
		_, _ = w.WriteString("</pre>\n")
		return ast.WalkContinue, nil
	}

	// Chroma-highlight the block.
	_, _ = w.WriteString(chromaHighlight(lang, code))
	_ = w.WriteByte('\n')
	return ast.WalkContinue, nil
}

// Ensure mermaidCodeRenderer implements goldrenderer.NodeRenderer.
var _ goldrenderer.NodeRenderer = (*mermaidCodeRenderer)(nil)

// ─── RenderMarkdown ──────────────────────────────────────────────────────────

// RenderMarkdown converts markdown source to an HTML fragment string.
// GFM extensions are enabled: tables, strikethrough, autolinks, tasklists.
// Fenced code blocks with language "mermaid" are emitted as
// <pre class="mermaid">…raw…</pre>; all other fenced blocks are chroma-highlighted.
//
// Link destinations are left verbatim — use RenderMarkdownWithLinks to rewrite
// in-page links into server routes resolved against the realm root.
//
// Returns the HTML string, hasMermaid (true if any mermaid block is present),
// and any error.
func RenderMarkdown(src []byte) (string, bool, error) {
	return renderMarkdown(src, nil, nil)
}

// RenderMarkdownWithLinks is RenderMarkdown plus server-side link rewriting.
//
// Every relative link destination on the page (located at pageRelPath, a
// realm-root-relative path) is resolved against the realm root and rewritten
// into a real server route so clicking it navigates inside the shell rather
// than triggering a full-page browser navigation (which loses the user's
// place, and 404s when the browser resolves the raw href against the wrong
// base URL). See resolvePageHref for the routing rules. External links and
// in-page anchors are preserved; realm-escaping links are left untouched.
func RenderMarkdownWithLinks(src []byte, root, pageRelPath string) (string, bool, error) {
	rewrite := func(raw string) (string, bool, bool) {
		return resolvePageHref(root, pageRelPath, raw)
	}
	return renderMarkdown(src, rewrite, nil)
}

// RenderMarkdownWithGraph is RenderMarkdownWithLinks plus in-body wikilink
// resolution. Obsidian-style [[page]] / [[page|alias]] links in the body are
// turned into in-shell htmx navigations, resolved through the focused page's
// already-computed graph edges (the same resolution the right rail uses), so the
// body and the rail can never disagree. A broken wikilink renders as a visible
// non-navigable span.
//
// g may be nil; nil leaves [[…]] as literal text (the RenderMarkdownWithLinks
// behaviour) since wikilink resolution needs the realm-wide basename index the
// graph carries.
func RenderMarkdownWithGraph(src []byte, root, pageRelPath string, g *Graph) (string, bool, error) {
	rewrite := func(raw string) (string, bool, bool) {
		return resolvePageHref(root, pageRelPath, raw)
	}
	return renderMarkdown(src, rewrite, wikilinkResolverFromGraph(g, pageRelPath))
}

// markdownLinkRewriter maps a raw markdown link destination to (href, htmxPage,
// external): href is the rewritten destination; htmxPage requests in-shell htmx
// navigation; external requests a new-tab link. A nil rewriter disables
// rewriting (hrefs render verbatim).
type markdownLinkRewriter func(rawHref string) (href string, htmxPage bool, external bool)

func renderMarkdown(src []byte, rewrite markdownLinkRewriter, wikiResolve wikilinkResolver) (string, bool, error) {
	hasMermaid := false
	codeRenderer := &mermaidCodeRenderer{hasMermaid: &hasMermaid}

	renderers := []util.PrioritizedValue{util.Prioritized(codeRenderer, 1)}
	if rewrite != nil {
		renderers = append(renderers, util.Prioritized(&linkRewriteRenderer{rewrite: rewrite}, 1))
	}

	parserOpts := []parser.Option{parser.WithAutoHeadingID()}
	if wikiResolve != nil {
		// Register the wikilink inline parser above goldmark's default link parser
		// (priority 200) so [[…]] is recognised before a single '[' link, and the
		// matching node renderer so the AST node has a renderer (goldmark errors on
		// an unrendered node kind). The two are always wired together.
		parserOpts = append(parserOpts, parser.WithInlineParsers(
			util.Prioritized(&wikilinkInlineParser{}, 150),
		))
		renderers = append(renderers, util.Prioritized(&wikilinkRenderer{resolve: wikiResolve}, 1))
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parserOpts...),
		goldmark.WithRendererOptions(
			goldhtml.WithHardWraps(),
			goldhtml.WithXHTML(),
			goldrenderer.WithNodeRenderers(renderers...),
		),
	)

	var out bytes.Buffer
	if err := md.Convert(src, &out); err != nil {
		return "", false, fmt.Errorf("goldmark convert: %w", err)
	}
	return out.String(), hasMermaid, nil
}

// ─── link-rewriting goldmark renderer ────────────────────────────────────────

// linkRewriteRenderer is a goldmark NodeRenderer that fully controls <a> output
// for inline links so it can rewrite the destination and attach htmx navigation
// (or new-tab) attributes. It replaces goldmark's default link rendering; link
// children (text, code spans, emphasis) still render via their own renderers
// between the opening and closing tags.
type linkRewriteRenderer struct {
	rewrite markdownLinkRewriter
}

// RegisterFuncs registers the inline-link rendering function.
func (r *linkRewriteRenderer) RegisterFuncs(reg goldrenderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
}

func (r *linkRewriteRenderer) renderLink(
	w util.BufWriter,
	_ []byte,
	n ast.Node,
	entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</a>")
		return ast.WalkContinue, nil
	}
	node, ok := n.(*ast.Link)
	if !ok {
		return ast.WalkContinue, nil
	}

	href, htmxPage, external := r.rewrite(string(node.Destination))

	_, _ = w.WriteString(`<a href="`)
	_, _ = w.WriteString(template.HTMLEscapeString(href))
	_ = w.WriteByte('"')
	if len(node.Title) > 0 {
		_, _ = w.WriteString(` title="`)
		_, _ = w.WriteString(template.HTMLEscapeString(string(node.Title)))
		_ = w.WriteByte('"')
	}
	switch {
	case htmxPage:
		_, _ = w.WriteString(` hx-get="`)
		_, _ = w.WriteString(template.HTMLEscapeString(href))
		_, _ = w.WriteString(`" hx-target="#main-pane" hx-swap="innerHTML" hx-push-url="true"`)
	case external:
		_, _ = w.WriteString(` target="_blank" rel="noopener noreferrer"`)
	}
	_ = w.WriteByte('>')
	return ast.WalkContinue, nil
}

// Ensure linkRewriteRenderer implements goldrenderer.NodeRenderer.
var _ goldrenderer.NodeRenderer = (*linkRewriteRenderer)(nil)

// ─── safeResolve ─────────────────────────────────────────────────────────────

// safeResolve resolves relPath relative to root, rejecting any attempt to
// escape the root via .. components or absolute paths.
// Returns ("", false) if the path escapes root.
func safeResolve(root, relPath string) (string, bool) {
	cleaned := filepath.Clean(relPath)
	// An absolute path or one that starts with ".." after Clean is invalid.
	if filepath.IsAbs(cleaned) ||
		cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	joined := filepath.Join(root, cleaned)

	// Use EvalSymlinks on both sides so macOS /var↔/private/var mismatches
	// don't cause false-negative rejections when root resolves differently.
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal, err = filepath.Abs(root)
		if err != nil {
			return "", false
		}
	}
	// For the joined path, try EvalSymlinks first (works when the file exists);
	// fall back to Abs (for not-yet-created paths the guard still works because
	// the path contains no symlinks — we only created it from root + clean).
	joinedReal, err := filepath.EvalSymlinks(joined)
	if err != nil {
		joinedReal, err = filepath.Abs(joined)
		if err != nil {
			return "", false
		}
		// Normalise the joinedReal to the same symlink-resolved base as rootReal
		// so the prefix comparison is consistent.
		// Safe: EvalSymlinks failed, meaning the path does not exist on disk.
		// The ".." guard above already rejected any traversal, so the only paths
		// that reach here are non-existent children of root. Rewriting the
		// rootPrefix segment to rootReal keeps the subsequent prefix check
		// consistent when root itself resolves through a symlink (e.g. macOS
		// /var → /private/var), preventing false-negative rejections.
		rootPrefix, _ := filepath.Abs(root)
		if rootPrefix != rootReal && strings.HasPrefix(joinedReal, rootPrefix) {
			joinedReal = rootReal + joinedReal[len(rootPrefix):]
		}
	}
	if joinedReal != rootReal && !strings.HasPrefix(joinedReal, rootReal+string(filepath.Separator)) {
		return "", false
	}
	return joinedReal, true
}

// ─── page HTML template ───────────────────────────────────────────────────────

const pageTemplateStr = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/static/app.css">
<script src="/static/vendor/htmx.min.js"></script>
</head>
<body>
<div id="page-content" class="md-content">
{{.Body}}
</div>
{{if .HasMermaid -}}
<script src="/static/vendor/mermaid.min.js"></script>
<script>
document.addEventListener("DOMContentLoaded", function() {
  if (window.mermaid) {
    mermaid.initialize({ startOnLoad: false });
    mermaid.run();
  }
});
document.addEventListener("htmx:afterSwap", function() {
  if (window.mermaid) {
    mermaid.initialize({ startOnLoad: false });
    mermaid.run();
  }
});
</script>
{{- end}}
</body>
</html>`

var pageTmpl = template.Must(template.New("page").Parse(pageTemplateStr))

// pageFragmentTemplateStr is returned for htmx requests (HX-Request header present).
// It omits the outer <!DOCTYPE html> shell; htmx swaps only this fragment into
// #main-pane. When hasMermaid is true the mermaid script + run() call are included
// so diagrams render after the htmx:afterSwap event fires.
const pageFragmentTemplateStr = `<div id="page-content" class="md-content">
{{.Body}}
</div>
{{if .HasMermaid -}}
<script src="/static/vendor/mermaid.min.js"></script>
<script>
(function() {
  if (window.mermaid) {
    mermaid.initialize({ startOnLoad: false });
    mermaid.run();
  }
})();
</script>
{{- end}}`

var pageFragmentTmpl = template.Must(template.New("page-fragment").Parse(pageFragmentTemplateStr))

// ─── file HTML template ───────────────────────────────────────────────────────

// fileTemplateStr is the full HTML page returned for direct /file/* navigation
// (no HX-Request header). Styles for .file-view live in assets/app.css.
const fileTemplateStr = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/static/app.css">
</head>
<body>
<div class="file-view-wrapper">
{{.Body}}
</div>
</body>
</html>`

var fileTmpl = template.Must(template.New("file").Parse(fileTemplateStr))

// fileFragmentTemplateStr is returned for htmx requests (HX-Request header present).
// It omits the outer <!DOCTYPE html> shell; the caller swaps this fragment into
// #code-modal-source. The wrapper div is retained so the shell's CSS target works.
const fileFragmentTemplateStr = `<div class="file-view-wrapper">
{{.Body}}
</div>`

var fileFragmentTmpl = template.Must(template.New("file-fragment").Parse(fileFragmentTemplateStr))

// ─── NewPageHandler ───────────────────────────────────────────────────────────

// NewPageHandler returns an http.Handler that serves rendered markdown files
// from root. The path segment after "/page/" is resolved relative to root;
// traversal outside root yields 404.
//
// Deprecated: use NewPageHandlerWithGraph for rail wiring and shell support.
// NewPageHandler is kept for tests that exercise the raw renderer.
func NewPageHandler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/page/")
		if relPath == "" || relPath == "/" {
			http.NotFound(w, r)
			return
		}

		abs, ok := safeResolve(root, relPath)
		if !ok {
			http.NotFound(w, r)
			return
		}

		data, err := os.ReadFile(abs) //nolint:gosec // path validated by safeResolve
		if err != nil {
			http.NotFound(w, r)
			return
		}

		bodyHTML, hasMermaid, err := RenderMarkdownWithLinks(data, root, normRelPath(relPath))
		if err != nil {
			http.Error(w, "render error", http.StatusInternalServerError)
			return
		}

		title := filepath.Base(relPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		tplData := struct {
			Title      string
			Body       template.HTML
			HasMermaid bool
		}{
			Title:      title,
			Body:       template.HTML(bodyHTML), //nolint:gosec // goldmark output
			HasMermaid: hasMermaid,
		}

		// htmx requests receive only the inner fragment so the swap lands in
		// #main-pane without replacing the three-pane shell.
		if r.Header.Get("HX-Request") != "" {
			_ = pageFragmentTmpl.Execute(w, tplData)
			return
		}
		_ = pageTmpl.Execute(w, tplData)
	})
}

// ─── NewFileHandler ───────────────────────────────────────────────────────────

// NewFileHandler returns an http.Handler that serves source files from root
// with chroma syntax highlighting and per-line id="L<n>" anchors.
// The path segment after "/file/" is resolved relative to root;
// traversal outside root yields 404.
//
// shell, when non-nil, is used for document (non-htmx) loads to wrap the
// file view inside the full layout shell (FE8: shell is the universal envelope).
// shell may be nil; nil degrades to the legacy bare full-page template.
func NewFileHandler(root string, shell ...*ShellRenderer) http.Handler {
	var sh *ShellRenderer
	if len(shell) > 0 {
		sh = shell[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relPath := strings.TrimPrefix(r.URL.Path, "/file/")
		if relPath == "" || relPath == "/" {
			http.NotFound(w, r)
			return
		}

		isHX := fragmentRequest(r)

		abs, ok := safeResolve(root, relPath)
		if !ok {
			// Traversal guard rejected path: serve shelled 404 (consistent with /page/).
			serve404(w, r, relPath, "/file/"+relPath, isHX, sh)
			return
		}

		data, err := os.ReadFile(abs) //nolint:gosec // path validated by safeResolve
		if err != nil {
			// File not found or unreadable: serve shelled 404.
			serve404(w, r, relPath, "/file/"+relPath, isHX, sh)
			return
		}

		ext := strings.TrimPrefix(filepath.Ext(relPath), ".")
		bodyHTML := chromaHighlightLines(ext, string(data))

		title := filepath.Base(relPath)

		tplData := struct {
			Title string
			Body  template.HTML
		}{
			Title: title,
			Body:  template.HTML(bodyHTML), //nolint:gosec // chroma output
		}

		// htmx requests receive only the inner fragment so the swap lands in
		// #code-modal-source without replacing the three-pane shell.
		if isHX {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = fileFragmentTmpl.Execute(w, tplData)
			return
		}

		// Document load: render the shell with LandingURL = this file path so
		// the user retains navigation (FE8: shell is the universal envelope).
		if sh != nil {
			_ = sh.Render(w, "/file/"+relPath, http.StatusOK)
			return
		}

		// Fallback: legacy bare full-page template (no shell available).
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = fileTmpl.Execute(w, tplData)
	})
}
