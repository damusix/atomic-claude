import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

// Read the latest released version from the changelog release-please maintains.
// The docs site rebuilds on every push to main, so this nav badge always tracks
// the newest release without a manual bump.
function latestVersion(): string {
    try {
        const here = dirname(fileURLToPath(import.meta.url))
        const changelog = readFileSync(resolve(here, '../atomic/CHANGELOG.md'), 'utf-8')
        const match = changelog.match(/##\s*\[?(\d+\.\d+\.\d+)\]?/)
        return match ? `v${match[1]}` : ''
    } catch {
        return ''
    }
}

const version = latestVersion()

const defined_html_tags = new Set([
    'a', 'abbr', 'address', 'area', 'article', 'aside', 'audio', 'b', 'base',
    'bdi', 'bdo', 'blockquote', 'body', 'br', 'button', 'canvas', 'caption',
    'cite', 'code', 'col', 'colgroup', 'data', 'datalist', 'dd', 'del',
    'details', 'dfn', 'dialog', 'div', 'dl', 'dt', 'em', 'embed', 'fieldset',
    'figcaption', 'figure', 'footer', 'form', 'h1', 'h2', 'h3', 'h4', 'h5',
    'h6', 'head', 'header', 'hgroup', 'hr', 'html', 'i', 'iframe', 'img',
    'input', 'ins', 'kbd', 'label', 'legend', 'li', 'link', 'main', 'map',
    'mark', 'menu', 'meta', 'meter', 'nav', 'noscript', 'object', 'ol',
    'optgroup', 'option', 'output', 'p', 'picture', 'pre', 'progress', 'q',
    'rp', 'rt', 'ruby', 's', 'samp', 'script', 'search', 'section', 'select',
    'slot', 'small', 'source', 'span', 'strong', 'style', 'sub', 'summary',
    'sup', 'table', 'tbody', 'td', 'template', 'textarea', 'tfoot', 'th',
    'thead', 'time', 'title', 'tr', 'track', 'u', 'ul', 'var', 'video', 'wbr',
])

// Vue components intentionally embedded in markdown (PascalCase). The escape pass
// below leaves these alone so they render as components, not literal text. Add a
// component here when you use it inside a .md page.
const vue_components = new Set(['SessionPlayer'])

function escapeTags(text: string): string {
    // Handle backslash-escaped angle brackets: \< and \>
    let result = text.replace(/\\</g, '&lt;').replace(/\\>/g, '&gt;')
    // Escape <...> where the tag name is NOT a known HTML element
    result = result.replace(/<(\/?[a-zA-Z][^>]*)>/g, (match, inner) => {
        const tagName = inner.replace(/^\//, '').split(/[\s/|\\,[\](){}.:;!?='"]/)[0]
        if (vue_components.has(tagName)) return match
        if (defined_html_tags.has(tagName.toLowerCase())) return match
        return `&lt;${inner}&gt;`
    })
    return result
}

function escapeMustaches(text: string): string {
    return text.replace(/\{\{/g, '&#123;&#123;').replace(/\}\}/g, '&#125;&#125;')
}

function escapeVueSyntax(src: string): string {
    const lines = src.split('\n')
    let inFence = false
    const result: string[] = []

    for (const line of lines) {
        if (line.trimStart().startsWith('```')) {
            inFence = !inFence
            result.push(line)
            continue
        }
        if (inFence) {
            result.push(line)
            continue
        }
        result.push(escapeTags(escapeMustaches(line)))
    }
    return result.join('\n')
}

const fills = ['#FF6B6B', '#FECA57', '#48DBFB', '#FF9FF3', '#1DD1A1']
const ink = '#F0E8D8'
const line = '#8C7E70'
const label = '#B8A888'
const bg = '#141210'
const shadow = 'rgba(0, 0, 0, 0.85)'
const nodeShape = ':is(rect, polygon, path, circle):not(.state-start)'

// The plugin forces `theme: 'dark'` under the dark class, so no `theme` key here;
// themeVariables and themeCSS still apply on top of it.
const mermaid = {
    // Mermaid sizes node boxes by measuring labels in this font; page CSS cannot set it.
    fontFamily: '"Geist", ui-sans-serif, system-ui, sans-serif',
    // Orthogonal edges with rounded corners; the loaders register in theme/index.ts.
    layout: 'elk',
    flowchart: { padding: 16, diagramPadding: 20 },
    sequence: { diagramMarginX: 20, diagramMarginY: 20 },
    themeVariables: {
        fontSize: '15px', background: bg,
        primaryColor: fills[0], primaryBorderColor: '#000', primaryTextColor: '#000',
        mainBkg: fills[0], nodeBorder: '#000', lineColor: line, textColor: ink,
        clusterBkg: bg, clusterBorder: '#000', edgeLabelBackground: bg,
        actorBkg: fills[1], actorBorder: '#000', actorTextColor: '#000', actorLineColor: line,
        signalColor: line, signalTextColor: ink, labelBoxBkgColor: fills[0], labelBoxBorderColor: '#000',
        labelTextColor: '#000', loopTextColor: ink, noteBkgColor: fills[2], noteBorderColor: '#000',
        noteTextColor: '#000', activationBkgColor: fills[3], activationBorderColor: '#000',
        transitionColor: line, transitionLabelColor: label, stateLabelColor: '#000',
        labelBackgroundColor: bg, specialStateColor: fills[0],
    },
    // Scoped under each svg's id, so it cannot reach the <svg> itself; the canvas is in custom.css.
    themeCSS: `
        .node ${nodeShape}, rect.actor, rect.note, .cluster rect, .labelBox {
            stroke-width: 3px;
            rx: 8px;
            ry: 8px;
            filter: drop-shadow(3px 5px 8px ${shadow});
        }
        ${fills.map((f, i) => `
        .node:nth-child(${fills.length}n+${i + 1}) ${nodeShape} { fill: ${f}; }`).join('')}
        .nodeLabel { font-weight: 700; color: #000; }
        .state-start { stroke: #000 !important; stroke-width: 3px; filter: drop-shadow(2px 3px 5px ${shadow}); }
        .flowchart-link, .transition, .messageLine0, .messageLine1 { stroke-width: 2.5px; }
        .actor-line { stroke-width: 2px; stroke-dasharray: 6 4; }
        .loopLine { stroke: ${line}; stroke-width: 2px; }
        marker:is([id$="-pointEnd"], [id$="-pointStart"]) { overflow: visible; }
        marker[id$="-pointEnd"] .arrowMarkerPath { transform-origin: 10px 5px; transform: translateX(-1px) scale(1.6); }
        marker[id$="-pointStart"] .arrowMarkerPath { transform-origin: 0 5px; transform: translateX(1px) scale(1.6); }
        .edgeLabel, .edgeLabel p, .labelBkg { background-color: ${bg}; color: ${label}; font-weight: 600; }
    `,
}

export default withMermaid(defineConfig({
    markdown: {
        theme: {
            dark: 'vesper',
            light: 'kanagawa-lotus',
        },
        config(md) {
            // Fix double-escaping: our Vite plugin escapes <tag> to &lt;tag&gt; in raw source,
            // then markdown-it escapes & to &amp; inside code spans, producing &amp;lt;tag&amp;gt;.
            // Undo the double-escape in rendered code_inline output.
            const defaultCodeInline = md.renderer.rules.code_inline!
            md.renderer.rules.code_inline = (tokens, idx, options, env, self) => {
                const result = defaultCodeInline(tokens, idx, options, env, self)
                return result
                    .replace(/&amp;lt;/g, '&lt;').replace(/&amp;gt;/g, '&gt;')
                    .replace(/&amp;#123;/g, '&#123;').replace(/&amp;#125;/g, '&#125;')
            }
        },
    },
    vite: {
        plugins: [
            {
                name: 'vitepress-escape-vue-syntax',
                enforce: 'pre',
                transform(code, id) {
                    if (!id.endsWith('.md')) return
                    return escapeVueSyntax(code)
                },
            },
        ],
    },
    appearance: 'force-dark',
    mermaid,
    // Not "mermaid": @mermaid-js/layout-elk bundles a second Mermaid copy whose
    // window-load auto-run re-renders every `.mermaid` element from its SVG text.
    mermaidPlugin: { class: 'ac-mermaid' },
    title: 'Atomic Claude',
    description: 'Code graphs, wikis, and research-backed agentic coding. Deterministic tools for nondeterministic workflows, in one opinionated Claude Code config.',
    srcDir: 'docs',
    base: '/',
    lastUpdated: true,
    sitemap: {
        hostname: 'https://atomic.alonso.network',
    },
    // Internal contract docs — kept in-repo for contributors, excluded from the public site.
    srcExclude: ['spec/**', 'design/**', 'wiki/**'],
    head: [
        ['link', { rel: 'icon', type: 'image/png', href: '/logo.png' }],
        ['meta', { property: 'og:image', content: '/share-image.png' }],
        ['meta', { property: 'og:image:width', content: '1200' }],
        ['meta', { property: 'og:image:height', content: '630' }],
        ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
        ['meta', { name: 'twitter:image', content: '/share-image.png' }],
        ['script', { async: '', src: 'https://www.googletagmanager.com/gtag/js?id=G-S57FJE24NJ' }],
        ['script', {}, "window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments)}gtag('js',new Date());gtag('config','G-S57FJE24NJ')"],
    ],
    themeConfig: {
        logo: '/logo.png',
        nav: [
            { text: 'Guides', link: '/guides/install' },
            { text: 'Reference', link: '/reference/concepts' },
            {
                text: 'Links',
                items: [
                    { text: 'GitHub', link: 'https://github.com/damusix/atomic-claude' },
                    { text: 'Releases', link: 'https://github.com/damusix/atomic-claude/releases' },
                    { text: 'Credits', link: '/credits' },
                ],
            },
            ...(version
                ? [{ text: version, link: `https://github.com/damusix/atomic-claude/releases/tag/${version}` }]
                : []),
        ],
        sidebar: [
            {
                text: 'Guides',
                items: [
                    { text: 'Install', link: '/guides/install' },
                    { text: 'Getting Started', link: '/guides/getting-started' },
                    { text: 'Knowledge Base', link: '/guides/knowledge-base' },
                    { text: 'Code-intel MCP', link: '/guides/code-intel-mcp' },
                    { text: 'Contributing', link: '/guides/contributing' },
                    { text: 'Evaluations', link: '/guides/evaluations' },
                ],
            },
            {
                text: 'Reference',
                items: [
                    { text: 'Concepts', link: '/reference/concepts' },
                    { text: 'Workflow', link: '/reference/workflow' },
                    { text: 'Commands', link: '/reference/commands' },
                    { text: 'Skills', link: '/reference/skills' },
                    { text: 'Agents', link: '/reference/agents' },
                    { text: 'Output Style', link: '/reference/output-style' },
                    { text: 'Repo Wiki', link: '/reference/repo-wiki' },
                    { text: 'Realm Wiki', link: '/reference/realm-wiki' },
                    { text: 'Code Intelligence', link: '/reference/code-intel' },
                    { text: 'Serve', link: '/reference/serve' },
                    { text: 'Inter-session Bus', link: '/reference/bus' },
                    { text: 'REPL', link: '/reference/repl' },
                    { text: 'Retro', link: '/reference/retro' },
                    { text: 'Docs index', link: '/reference/docs-index' },
                    { text: 'Repo config (atomic.toml)', link: '/reference/atomic-toml' },
                    { text: 'Conventions', link: '/reference/conventions' },
                ],
            },
        ],
        socialLinks: [
            { icon: 'github', link: 'https://github.com/damusix/atomic-claude' },
        ],
        search: {
            provider: 'local',
        },
        footer: {
            message: 'Released under the MIT License.',
        },
    },
}))
