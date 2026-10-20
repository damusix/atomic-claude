// Layout choice and post-render svg adjustments, shared with atomic serve: the
// logic lives in its frontend's utils and is imported from there.
import type { Mermaid } from 'mermaid'
import { usesElk, withElk } from '../../atomic/internal/serve/frontend/src/utils/mermaidLayout'
import { chipLabels, makeRoomForPadding } from '../../atomic/internal/serve/frontend/src/utils/mermaidSvg'

// The plugin passes one config for every diagram, so ELK, which routes edges
// orthogonally, is switched on per diagram here, and dropped if it fails.
export function routeMermaidLayouts(mermaid: Mermaid) {
    const render = mermaid.render.bind(mermaid)
    mermaid.render = async (id: string, text: string, container?: Element) => {
        let type: string | null = null
        try {
            type = mermaid.detectType(text)
        } catch {
            type = null
        }
        if (!usesElk(type)) return render(id, text, container)
        try {
            return await render(id, withElk(text), container)
        } catch {
            return render(id, text, container)
        }
    }
}

// The plugin renders and re-renders diagrams on its own schedule, so each new
// svg is adjusted as it appears.
export function setupMermaidLabels() {
    const pending = '.ac-mermaid svg:not([data-label-chips])'
    const adjustNew = () => {
        const fill = getComputedStyle(document.documentElement).getPropertyValue('--vp-c-bg').trim()
        for (const svg of document.querySelectorAll<SVGSVGElement>(pending)) {
            svg.setAttribute('data-label-chips', '')
            makeRoomForPadding(svg)
            chipLabels(svg, fill)
        }
    }
    new MutationObserver(adjustNew).observe(document.body, { childList: true, subtree: true })
    adjustNew()
}
