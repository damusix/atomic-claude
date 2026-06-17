// wikilink.go — goldmark inline support for Obsidian-style [[wikilinks]].
//
// goldmark has no native wikilink syntax, so a bare [[page]] / [[page|alias]] in
// a markdown body renders as literal text. The realm link graph already parses
// and resolves these (mdlink.ExtractLinks + resolveWikilink), which is why the
// right rail shows the OUT/IN links — but the page body never linked them. This
// file closes that gap: an inline parser turns [[…]] into a wikilinkNode, and a
// renderer resolves it through the *same* graph edges the rail uses, so the body
// and the rail can never disagree.
//
// Resolution is not recomputed here. wikilinkResolverFromGraph reads the focused
// page's already-resolved outbound edges, so the nearest-then-alphabetical rule
// (and ambiguity/broken classification) lives in exactly one place: graph.go.
package serve

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	goldrenderer "github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/damusix/atomic-claude/atomic/internal/mdlink"
)

// kindWikilink is the AST node kind for an Obsidian-style wikilink.
var kindWikilink = ast.NewNodeKind("Wikilink")

// wikilinkNode is an inline AST node for [[page]] / [[page|alias]].
type wikilinkNode struct {
	ast.BaseInline
	// Page is the raw page name (left of '|'), used for resolution.
	Page string
	// Alias is the display text (right of '|', or Page when no alias).
	Alias string
}

func (n *wikilinkNode) Kind() ast.NodeKind { return kindWikilink }

func (n *wikilinkNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Page":  n.Page,
		"Alias": n.Alias,
	}, nil)
}

// ─── inline parser ───────────────────────────────────────────────────────────

// wikilinkInlineParser recognises [[page]] / [[page|alias]] on the '[' trigger.
// Registered at a higher priority (lower number) than goldmark's default link
// parser (200) so it gets first crack at '['; on a single '[' (a normal markdown
// link) it returns nil and the default link parser runs with the reader position
// restored by the goldmark dispatch loop. It never advances the reader unless it
// commits a node, which keeps the nil-then-fallthrough contract safe.
type wikilinkInlineParser struct{}

func (p *wikilinkInlineParser) Trigger() []byte { return []byte{'['} }

func (p *wikilinkInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	// Shortest possible wikilink is "[[x]]" (5 bytes). line[0] is '[' by trigger.
	if len(line) < 5 || line[1] != '[' {
		return nil
	}
	rest := line[2:]
	close := bytes.Index(rest, []byte("]]"))
	if close < 0 {
		return nil
	}
	inner := rest[:close]
	// Reject empty ([[]]) and inner brackets (not a clean wikilink).
	if len(bytes.TrimSpace(inner)) == 0 || bytes.ContainsAny(inner, "[]") {
		return nil
	}
	page, alias := splitWikilinkInner(string(inner))
	if page == "" {
		return nil
	}
	// Consume "[[" + inner + "]]".
	block.Advance(2 + close + 2)
	return &wikilinkNode{Page: page, Alias: alias}
}

// splitWikilinkInner splits "page" or "page|alias" into its parts, mirroring
// mdlink.parseWikilink: the alias defaults to the page name, and an empty alias
// (e.g. "page|") falls back to the page name so the link always has display text.
func splitWikilinkInner(inner string) (page, alias string) {
	if i := strings.IndexByte(inner, '|'); i != -1 {
		page = strings.TrimSpace(inner[:i])
		alias = strings.TrimSpace(inner[i+1:])
		if alias == "" {
			alias = page
		}
		return page, alias
	}
	page = strings.TrimSpace(inner)
	return page, page
}

// ─── resolver ────────────────────────────────────────────────────────────────

// wikilinkResolver maps a raw wikilink page name to its resolution for the
// page being rendered: the realm-root-relative target, plus broken/ambiguous
// flags. broken means no realm file matched; ambiguous means more than one did
// and the nearest-then-alphabetical winner is returned.
type wikilinkResolver func(page string) (resolved string, broken, ambiguous bool)

// wikilinkResolverFromGraph derives a resolver from the focused page's outbound
// graph edges. It reuses the resolution the graph already computed (graph.go's
// resolveWikilink), so the body and the right rail are guaranteed to agree.
// Returns nil when g is nil — callers skip wikilink wiring entirely in that case,
// leaving [[…]] as literal text (the prior behaviour for the graphless path).
func wikilinkResolverFromGraph(g *Graph, pageRelPath string) wikilinkResolver {
	if g == nil {
		return nil
	}
	index := make(map[string]Edge)
	for _, e := range g.Outbound(pageRelPath) {
		if e.Kind != mdlink.Wikilink {
			continue
		}
		// First edge wins; duplicate [[same]] links resolve identically anyway.
		key := strings.ToLower(strings.TrimSpace(e.Target))
		if _, seen := index[key]; !seen {
			index[key] = e
		}
	}
	return func(page string) (string, bool, bool) {
		e, ok := index[strings.ToLower(strings.TrimSpace(page))]
		if !ok || e.Broken || e.ResolvedPath == "" {
			return "", true, false
		}
		return e.ResolvedPath, false, e.Ambiguous
	}
}

// ─── renderer ────────────────────────────────────────────────────────────────

// wikilinkRenderer renders a wikilinkNode using the per-page resolver. Resolved
// links become in-shell htmx navigations to /page/<target> (mirroring the rail
// and the markdown-link rewriter); broken links render as a visible non-navigable
// span so a dead wikilink reads as dead rather than as plain prose.
type wikilinkRenderer struct {
	resolve wikilinkResolver
}

func (r *wikilinkRenderer) RegisterFuncs(reg goldrenderer.NodeRendererFuncRegisterer) {
	reg.Register(kindWikilink, r.render)
}

func (r *wikilinkRenderer) render(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	node, ok := n.(*wikilinkNode)
	if !ok {
		return ast.WalkContinue, nil
	}

	// No resolver: degrade to plain display text (should not happen in the
	// graph-wired path, which only registers this renderer alongside a resolver).
	if r.resolve == nil {
		_, _ = w.WriteString(template.HTMLEscapeString(node.Alias))
		return ast.WalkContinue, nil
	}

	resolved, broken, ambiguous := r.resolve(node.Page)
	if broken {
		_, _ = w.WriteString(`<span class="wikilink-broken" title="unresolved wikilink: `)
		_, _ = w.WriteString(template.HTMLEscapeString(node.Page))
		_, _ = w.WriteString(`">`)
		_, _ = w.WriteString(template.HTMLEscapeString(node.Alias))
		_, _ = w.WriteString(`</span>`)
		return ast.WalkContinue, nil
	}

	href := "/page/" + resolved
	class := "wikilink"
	if ambiguous {
		class = "wikilink wikilink-ambiguous"
	}
	_, _ = w.WriteString(`<a class="`)
	_, _ = w.WriteString(class)
	_, _ = w.WriteString(`" hx-get="`)
	_, _ = w.WriteString(template.HTMLEscapeString(href))
	_, _ = w.WriteString(`" hx-target="#main-pane" hx-swap="innerHTML" hx-push-url="true" href="`)
	_, _ = w.WriteString(template.HTMLEscapeString(href))
	_ = w.WriteByte('"')
	if ambiguous {
		_, _ = w.WriteString(` title="ambiguous: multiple files match"`)
	}
	_ = w.WriteByte('>')
	_, _ = w.WriteString(template.HTMLEscapeString(node.Alias))
	_, _ = w.WriteString(`</a>`)
	return ast.WalkContinue, nil
}

// Ensure the renderer satisfies the goldmark interface.
var _ goldrenderer.NodeRenderer = (*wikilinkRenderer)(nil)
