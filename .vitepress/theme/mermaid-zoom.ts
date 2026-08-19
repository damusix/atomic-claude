/**
 * Click-to-fullscreen lightbox for rendered mermaid diagrams, with wheel /
 * pinch zoom and drag pan. Installed once via document-level click delegation,
 * so it needs no coordination with the plugin's async render: by the time a
 * diagram is clickable, its SVG exists.
 */

const SCALE_MIN = 0.1
const SCALE_MAX = 40
const CLICK_SLOP_PX = 5

interface PointerPosition {
    x: number
    y: number
}

function naturalSize(svg: SVGSVGElement): { width: number; height: number } {
    const viewBox = svg.viewBox.baseVal
    if (viewBox && viewBox.width > 0 && viewBox.height > 0) {
        return { width: viewBox.width, height: viewBox.height }
    }
    const rect = svg.getBoundingClientRect()
    return { width: Math.max(rect.width, 1), height: Math.max(rect.height, 1) }
}

function openLightbox(svg: SVGSVGElement) {
    const { width, height } = naturalSize(svg)

    const overlay = document.createElement('div')
    overlay.className = 'ac-mermaid-lightbox'
    overlay.setAttribute('role', 'dialog')
    overlay.setAttribute('aria-label', 'Diagram viewer')

    const stage = document.createElement('div')
    stage.className = 'ac-mermaid-lightbox-stage'

    // A plain cloneNode would keep the original's id. Mermaid re-renders clean
    // up by id (and the diagram's internal style selectors and marker url(#…)
    // refs are all id-prefixed), so a page re-render — even the one triggered
    // by this overlay hiding the scrollbar — would clobber the clone. Rewrite
    // every occurrence of the id in the serialized markup instead.
    const markup = svg.id
        ? svg.outerHTML.split(svg.id).join(`${svg.id}-lightbox`)
        : svg.outerHTML
    stage.innerHTML = markup

    const clone = stage.querySelector('svg')
    if (!clone) return
    clone.style.width = `${width}px`
    clone.style.height = `${height}px`
    clone.style.maxWidth = 'none'

    const close = document.createElement('button')
    close.className = 'ac-mermaid-lightbox-close'
    close.setAttribute('aria-label', 'Close diagram viewer')
    close.textContent = '×'

    const hint = document.createElement('div')
    hint.className = 'ac-mermaid-lightbox-hint'
    hint.textContent = 'scroll or pinch to zoom · drag to pan · double-click to reset · esc to close'

    overlay.append(stage, close, hint)

    let scale = 1
    let tx = 0
    let ty = 0

    const apply = () => {
        stage.style.transform = `translate(${tx}px, ${ty}px) scale(${scale})`
    }

    const fit = () => {
        scale = Math.min(
            (window.innerWidth * 0.92) / width,
            (window.innerHeight * 0.92) / height,
        )
        tx = (window.innerWidth - width * scale) / 2
        ty = (window.innerHeight - height * scale) / 2
        apply()
    }

    const zoomAt = (clientX: number, clientY: number, factor: number) => {
        const next = Math.min(SCALE_MAX, Math.max(SCALE_MIN, scale * factor))
        const applied = next / scale
        tx = clientX - (clientX - tx) * applied
        ty = clientY - (clientY - ty) * applied
        scale = next
        apply()
    }

    const pointers = new Map<number, PointerPosition>()
    let dragDistance = 0

    const onWheel = (event: WheelEvent) => {
        event.preventDefault()
        zoomAt(event.clientX, event.clientY, Math.exp(-event.deltaY * 0.002))
    }

    const onPointerDown = (event: PointerEvent) => {
        overlay.setPointerCapture(event.pointerId)
        pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
        if (pointers.size === 1) dragDistance = 0
    }

    const onPointerMove = (event: PointerEvent) => {
        const previous = pointers.get(event.pointerId)
        if (!previous) return
        const current = { x: event.clientX, y: event.clientY }

        if (pointers.size === 1) {
            tx += current.x - previous.x
            ty += current.y - previous.y
            dragDistance += Math.hypot(current.x - previous.x, current.y - previous.y)
            apply()
        } else if (pointers.size === 2) {
            const other = [...pointers.entries()].find(([id]) => id !== event.pointerId)
            if (other) {
                const [, anchor] = other
                const previousDistance = Math.hypot(previous.x - anchor.x, previous.y - anchor.y)
                const currentDistance = Math.hypot(current.x - anchor.x, current.y - anchor.y)
                if (previousDistance > 0) {
                    const midX = (current.x + anchor.x) / 2
                    const midY = (current.y + anchor.y) / 2
                    zoomAt(midX, midY, currentDistance / previousDistance)
                }
            }
            dragDistance = Number.POSITIVE_INFINITY
        }
        pointers.set(event.pointerId, current)
    }

    const onPointerEnd = (event: PointerEvent) => {
        pointers.delete(event.pointerId)
    }

    const onClick = (event: MouseEvent) => {
        if (dragDistance > CLICK_SLOP_PX) return
        if (event.target instanceof Element && event.target.closest('svg')) return
        teardown()
    }

    const onKeyDown = (event: KeyboardEvent) => {
        if (event.key === 'Escape') teardown()
    }

    const teardown = () => {
        window.removeEventListener('keydown', onKeyDown)
        window.removeEventListener('resize', fit)
        overlay.remove()
        document.documentElement.style.removeProperty('overflow')
    }

    overlay.addEventListener('wheel', onWheel, { passive: false })
    overlay.addEventListener('pointerdown', onPointerDown)
    overlay.addEventListener('pointermove', onPointerMove)
    overlay.addEventListener('pointerup', onPointerEnd)
    overlay.addEventListener('pointercancel', onPointerEnd)
    overlay.addEventListener('dblclick', fit)
    overlay.addEventListener('click', onClick)
    close.addEventListener('click', teardown)
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('resize', fit)

    document.documentElement.style.overflow = 'hidden'
    document.body.appendChild(overlay)
    fit()
}

export function setupMermaidZoom() {
    document.addEventListener('click', (event) => {
        if (!(event.target instanceof Element)) return
        const container = event.target.closest('.mermaid')
        if (!container || container.closest('.ac-mermaid-lightbox')) return
        const svg = container.querySelector('svg')
        if (!svg) return
        openLightbox(svg)
    })
}
