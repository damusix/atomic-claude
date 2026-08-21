// Server-side markdown rendering (goldmark + chroma) behind /api/page and the
// chroma source-file view behind /api/file. Rewritten links are plain hrefs;
// the client router intercepts in-shell navigation.
package serve

import (
	"bytes"
	"fmt"
	"html/template"
	"path/filepath"
	"regexp"
	"strings"

	chroma "github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldrenderer "github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// chromaStyleName only supplies the token set chroma walks; with WithClasses
// the style's colors are never emitted. The palette lives in app.css.
const chromaStyleName = "monokai"

// chromaFmt emits token classes rather than inline colors, so one rendered
// document can follow the theme toggle — an inline color cannot. It emits no
// line numbers; wrapWithLineAnchors adds them for /api/file, and the fence
// window numbers its lines with a CSS counter so the digits stay out of a copy.
var chromaFmt = chromahtml.New(
	chromahtml.TabWidth(4),
	chromahtml.WithClasses(true),
	chromahtml.WithLineNumbers(false),
)

// chromaHighlight highlights code, degrading to escaped plain text on error.
func chromaHighlight(lang, code string) string {
	style := styles.Get(chromaStyleName)
	if style == nil {
		style = styles.Fallback
	}
	// Explicit fence language only — lexers.Analyse would mis-detect an
	// unlabeled fence (a terminal transcript, say) and colour it as code.
	lexer := lexers.Get(lang)
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

// chromaHighlightLines renders code as a table whose rows carry id="L<n>"
// anchors.
func chromaHighlightLines(lang, code string) string {
	style := styles.Get(chromaStyleName)
	if style == nil {
		style = styles.Fallback
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Match("file." + lang)
	}
	if lexer == nil {
		// Plaintext, not lexers.Analyse: content guessing colours .txt/.mod/
		// LICENSE files as code.
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

// wrapWithLineAnchors turns chroma's <pre> block into anchored table rows.
func wrapWithLineAnchors(highlighted string) string {
	inner := highlighted
	if preStart := strings.Index(inner, ">"); strings.HasPrefix(strings.TrimSpace(inner), "<pre") && preStart >= 0 {
		inner = inner[preStart+1:]
	}
	trimmed := strings.TrimSpace(inner)
	if strings.HasSuffix(trimmed, "</pre>") {
		inner = trimmed[:len(trimmed)-len("</pre>")]
	}

	rawLines := strings.Split(inner, "\n")
	for len(rawLines) > 0 && strings.TrimSpace(rawLines[len(rawLines)-1]) == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	var sb strings.Builder
	sb.WriteString(`<table class="file-view"><tbody>`)
	for i, line := range rawLines {
		n := i + 1
		// Unescaped %s is safe: chroma already emits escaped HTML.
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

// mermaidCodeRenderer routes a "mermaid" fence to a raw <pre class="mermaid">
// for client-side rendering; every other fence goes through chroma.
type mermaidCodeRenderer struct {
	hasMermaid *bool
}

// RegisterFuncs implements goldrenderer.NodeRenderer.
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

	// An unlabeled fence is still a block someone set apart: a file tree, a
	// spec outline, a transcript. It gets the same window as labeled code,
	// named for what it is. The plaintext lexer colors nothing, which is the
	// point — the frame and the line numbers are what the block is here for.
	if strings.TrimSpace(lang) == "" {
		lang = "text"
	}

	_, _ = w.WriteString(codeWindow(lang, chromaHighlight(lang, code)))
	_ = w.WriteByte('\n')
	return ast.WalkContinue, nil
}

// codeWindow frames a highlighted fence as a titled window: the language label
// names what the block is without the reader inferring it from the syntax, and
// the frame marks the block as a quoted artifact rather than page furniture.
func codeWindow(lang, highlighted string) string {
	return `<figure class="code-window">` +
		`<figcaption class="code-window-bar">` +
		`<span class="code-window-dots" aria-hidden="true"></span>` +
		`<span class="code-window-lang">` + template.HTMLEscapeString(lang) + `</span>` +
		`</figcaption>` +
		highlighted +
		`</figure>`
}

var _ goldrenderer.NodeRenderer = (*mermaidCodeRenderer)(nil)

// RenderMarkdown converts markdown to an HTML fragment with GFM enabled,
// leaving link destinations verbatim. The bool reports whether the page
// contains a mermaid block.
func RenderMarkdown(src []byte) (string, bool, error) {
	return renderMarkdown(src, nil, nil)
}

// RenderMarkdownWithLinks is RenderMarkdown with every relative destination
// rewritten to a server route (see resolvePageHref), so a click stays in the
// shell instead of the browser resolving the raw href against the wrong base.
// External links, anchors, and realm-escaping paths are left alone.
func RenderMarkdownWithLinks(src []byte, root, pageRelPath string) (string, bool, error) {
	rewrite := func(raw string) (string, bool, bool) {
		return resolvePageHref(root, pageRelPath, raw)
	}
	return renderMarkdown(src, rewrite, nil)
}

// RenderMarkdownWithGraph is RenderMarkdownWithLinks plus [[wikilink]]
// resolution, reusing the page's own graph edges so the body and the rail
// cannot disagree. A nil g leaves [[…]] as literal text, since resolution
// needs the realm-wide basename index the graph carries.
func RenderMarkdownWithGraph(src []byte, root, pageRelPath string, g *Graph) (string, bool, error) {
	rewrite := func(raw string) (string, bool, bool) {
		return resolvePageHref(root, pageRelPath, raw)
	}
	return renderMarkdown(src, rewrite, wikilinkResolverFromGraph(g, pageRelPath))
}

// markdownLinkRewriter rewrites one link destination. A nil rewriter leaves
// hrefs verbatim.
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
		// Priority must beat goldmark's link parser (200) so [[…]] wins over a
		// single '['. Parser and renderer are wired together — goldmark errors
		// on a node kind with no renderer.
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

	// Without this, a leading "---" becomes a thematic break and the key:value
	// lines below it collapse into a bogus setext <h2>. A parse failure falls
	// through with the original source so no content is lost.
	body := src
	if _, bodyStr, err := frontmatter.Parse(string(src)); err == nil {
		body = []byte(bodyStr)
	}
	body = stripLeadingMetaTags(body)

	var out bytes.Buffer
	if err := md.Convert(body, &out); err != nil {
		return "", false, fmt.Errorf("goldmark convert: %w", err)
	}
	return out.String(), hasMermaid, nil
}

// metaTagLine matches the opening of a line like "<scan-sha>e7f83d…</scan-sha>".
// RE2 has no backreferences, so isMetaTagLine checks the closing tag instead.
var metaTagLine = regexp.MustCompile(`^<([a-z][a-z0-9-]*)>`)

// isMetaTagLine reports whether line is a single complete metadata element.
func isMetaTagLine(line []byte) bool {
	m := metaTagLine.FindSubmatch(line)
	if m == nil {
		return false
	}
	return bytes.HasSuffix(line, []byte("</"+string(m[1])+">"))
}

// stripLeadingMetaTags removes the metadata block some wiki pages open with
// (<wiki-type>, <scan-sha>, <wiki-schema>). These are not YAML, so goldmark
// drops the tags as unsafe raw HTML but keeps their text, surfacing as a stray
// paragraph above the title. Only a leading run is stripped — the same tag
// further down the page is prose or an example.
func stripLeadingMetaTags(body []byte) []byte {
	lines := bytes.Split(body, []byte("\n"))

	cut := 0
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			cut++
			continue
		}
		if !isMetaTagLine(trimmed) {
			break
		}
		cut++
	}
	if cut == 0 {
		return body
	}
	return bytes.Join(lines[cut:], []byte("\n"))
}

// linkRewriteRenderer replaces goldmark's <a> rendering so the destination can
// be rewritten. Link children still render through their own renderers.
type linkRewriteRenderer struct {
	rewrite markdownLinkRewriter
}

// RegisterFuncs implements goldrenderer.NodeRenderer.
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

	href, _, external := r.rewrite(string(node.Destination))

	_, _ = w.WriteString(`<a href="`)
	_, _ = w.WriteString(template.HTMLEscapeString(href))
	_ = w.WriteByte('"')
	if len(node.Title) > 0 {
		_, _ = w.WriteString(` title="`)
		_, _ = w.WriteString(template.HTMLEscapeString(string(node.Title)))
		_ = w.WriteByte('"')
	}
	if external {
		_, _ = w.WriteString(` target="_blank" rel="noopener noreferrer"`)
	}
	_ = w.WriteByte('>')
	return ast.WalkContinue, nil
}

var _ goldrenderer.NodeRenderer = (*linkRewriteRenderer)(nil)

// safeResolve joins relPath onto root, rejecting absolute paths and any ..
// escape. This is the containment guard for every path the server serves.
func safeResolve(root, relPath string) (string, bool) {
	return resolveContained(root, relPath)
}

// resolveContained resolves relPath under root, rejecting any path that
// would escape it: no absolute path, no ".." segment after Clean, and the
// joined result must stay under root after EvalSymlinks on both sides. The
// one containment algorithm every root-scoped content route in this package
// relies on — safeResolve's allowed root is render.go's single served root;
// api_plans_page.go calls this directly under a worktree-issued root
// instead, since widening safeResolve's root would relax it at every other
// call site for the benefit of one surface.
func resolveContained(root, relPath string) (string, bool) {
	cleaned := filepath.Clean(relPath)
	if filepath.IsAbs(cleaned) ||
		cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	joined := filepath.Join(root, cleaned)

	// Both sides go through EvalSymlinks so a root that resolves differently
	// (macOS /var↔/private/var) does not reject valid paths.
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal, err = filepath.Abs(root)
		if err != nil {
			return "", false
		}
	}
	joinedReal, err := filepath.EvalSymlinks(joined)
	if err != nil {
		joinedReal, err = filepath.Abs(joined)
		if err != nil {
			return "", false
		}
		// EvalSymlinks failing means the path does not exist, and the ".."
		// guard above already rejected traversal, so this can only be a
		// non-existent child of root. Rebasing onto rootReal keeps the prefix
		// check below honest when root itself resolves through a symlink.
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
