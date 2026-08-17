// goldmark inline support for Obsidian-style [[wikilinks]], which goldmark has
// no native syntax for.
//
// Nothing is resolved here: the renderer reads the focused page's already
// resolved outbound edges, so the nearest-then-alphabetical rule and the
// broken/ambiguous classification live only in graph.go, and the body and the
// rail cannot disagree.
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

var kindWikilink = ast.NewNodeKind("Wikilink")

// wikilinkNode is an inline AST node for [[page]] / [[page|alias]].
type wikilinkNode struct {
	ast.BaseInline
	// Page is what resolution matches on.
	Page string
	// Alias is the display text, defaulting to Page.
	Alias string
}

func (n *wikilinkNode) Kind() ast.NodeKind { return kindWikilink }

func (n *wikilinkNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Page":  n.Page,
		"Alias": n.Alias,
	}, nil)
}

// wikilinkInlineParser gets first crack at '[' by outranking goldmark's link
// parser. It returns nil on a plain markdown link and never advances the
// reader unless it commits a node, which is what makes the fallthrough safe.
type wikilinkInlineParser struct{}

func (p *wikilinkInlineParser) Trigger() []byte { return []byte{'['} }

func (p *wikilinkInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	// "[[x]]" is the shortest possible wikilink; line[0] is '[' by trigger.
	if len(line) < 5 || line[1] != '[' {
		return nil
	}
	rest := line[2:]
	close := bytes.Index(rest, []byte("]]"))
	if close < 0 {
		return nil
	}
	inner := rest[:close]
	if len(bytes.TrimSpace(inner)) == 0 || bytes.ContainsAny(inner, "[]") {
		return nil
	}
	page, alias := splitWikilinkInner(string(inner))
	if page == "" {
		return nil
	}
	block.Advance(2 + close + 2)
	return &wikilinkNode{Page: page, Alias: alias}
}

// splitWikilinkInner mirrors mdlink.parseWikilink: an absent or empty alias
// falls back to the page name, so a link always has display text.
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

// wikilinkResolver resolves one page name. broken means nothing matched;
// ambiguous means several did and the nearest-then-alphabetical winner is
// returned.
type wikilinkResolver func(page string) (resolved string, broken, ambiguous bool)

// wikilinkResolverFromGraph returns nil for a nil graph, and callers then skip
// wikilink wiring entirely, leaving [[…]] as literal text.
func wikilinkResolverFromGraph(g *Graph, pageRelPath string) wikilinkResolver {
	if g == nil {
		return nil
	}
	index := make(map[string]Edge)
	for _, e := range g.Outbound(pageRelPath) {
		if e.Kind != mdlink.Wikilink {
			continue
		}
		// First edge wins; duplicates resolve identically anyway.
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

// wikilinkRenderer emits a plain href for a resolved link and a non-navigable
// span for a broken one, so a dead wikilink reads as dead rather than as prose.
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

	// Unreachable in the graph-wired path, which registers this renderer only
	// alongside a resolver.
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
	_, _ = w.WriteString(`" href="`)
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

var _ goldrenderer.NodeRenderer = (*wikilinkRenderer)(nil)
