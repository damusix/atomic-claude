import { defineConfig } from 'vitepress'

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

function escapeTags(text: string): string {
    // Handle backslash-escaped angle brackets: \< and \>
    let result = text.replace(/\\</g, '&lt;').replace(/\\>/g, '&gt;')
    // Escape <...> where the tag name is NOT a known HTML element
    result = result.replace(/<(\/?[a-zA-Z][^>]*)>/g, (match, inner) => {
        const tagName = inner.replace(/^\//, '').split(/[\s/|\\,[\](){}.:;!?='"]/)[0]
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

export default defineConfig({
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
    title: 'Atomic Claude',
    description: 'An opinionated Claude Code configuration — compressed replies, idea-to-PR workflow, clean skill/command split.',
    srcDir: 'docs',
    base: '/',
    // Internal contract docs — kept in-repo for contributors, excluded from the public site.
    srcExclude: ['spec/**', 'design/**'],
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
        ],
        sidebar: {
            '/guides/': [
                {
                    text: 'Guides',
                    items: [
                        { text: 'Install', link: '/guides/install' },
                        { text: 'Contributing', link: '/guides/contributing' },
                        { text: 'Evaluations', link: '/guides/evaluations' },
                    ],
                },
            ],
            '/reference/': [
                {
                    text: 'Reference',
                    items: [
                        { text: 'Concepts', link: '/reference/concepts' },
                        { text: 'Workflow', link: '/reference/workflow' },
                        { text: 'Commands', link: '/reference/commands' },
                        { text: 'Skills', link: '/reference/skills' },
                        { text: 'Agents', link: '/reference/agents' },
                        { text: 'Output Style', link: '/reference/output-style' },
                        { text: 'Signals Workflow', link: '/reference/signals-workflow' },
                        { text: 'Wiki Workflow', link: '/reference/wiki-workflow' },
                        { text: 'Conventions', link: '/reference/conventions' },
                    ],
                },
            ],
        },
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
})
