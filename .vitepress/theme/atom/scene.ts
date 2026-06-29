// Builds the dynamic SVG for the atom and hands back typed references.
// The static skeleton (groups, defs, nucleus) lives in AtomCore.vue; this fills it in.

import { ATOM } from './config'
import { cross, normalize, type Vec3, type Point } from './math'

const SVG_NS = 'http://www.w3.org/2000/svg'

// Create a typed SVG element with attributes. instanceof narrows without a cast.
function svgEl<T extends SVGElement>(
    tag: string,
    ctor: new () => T,
    attrs: Record<string, string | number> = {},
): T {
    const node = document.createElementNS(SVG_NS, tag)
    if (!(node instanceof ctor)) throw new Error(`atom: cannot create <${tag}>`)
    for (const key in attrs) node.setAttribute(key, String(attrs[key]))
    return node
}

// Find a required element within the root, narrowed to its concrete type.
function query<T extends Element>(root: ParentNode, selector: string, ctor: new () => T): T {
    const node = root.querySelector(selector)
    if (node instanceof ctor) return node
    throw new Error(`atom: element not found: ${selector}`)
}

// One electron line = a great circle on the unit sphere plus its per-line motion offsets.
export interface ElectronLine {
    u: Vec3 // orthonormal basis of the line's plane
    v: Vec3
    phase: number // electron's starting angle around the line
    radiusScale: number // nested-shell size (0.62–1.0)
    breathPhase: number // 0 or π → two antiphase breathing groups
    lineDelay: number // beat before the line fades in during the intro
}

// The DOM nodes for one electron/line/proton triple plus its zap.
export interface LineParts {
    line: SVGPathElement
    lineFlash: SVGPathElement
    gradient: SVGLinearGradientElement
    gradStart: SVGStopElement
    gradEnd: SVGStopElement
    electron: SVGCircleElement
    proton: SVGCircleElement
    protonRadius: number
    zap: SVGPathElement
    zapSpark: SVGCircleElement
    zapLabel: SVGTextElement
}

export interface Scene {
    lines: ElectronLine[]
    parts: LineParts[]
    // proximity connecting lines
    electronPairs: [number, number][]
    electronConnections: SVGLineElement[]
    protonConnections: SVGLineElement[] // one per electron, to its nearest proton
    // per-frame screen positions
    electronPos: { x: number; y: number; depth: number }[]
    protonPos: Point[]
    // nucleus reaction targets
    nucleus: SVGGElement
    nucleusGlow: SVGCircleElement
    pulse: SVGCircleElement
}

let instanceId = 0

// Distribute the line plane-normals on a Fibonacci spiral so the lines spread evenly.
export function buildElectronLines(): ElectronLine[] {
    const { electronCount: n, lineDelayMin, lineDelaySpan } = ATOM
    const golden = Math.PI * (3 - Math.sqrt(5))
    const lines: ElectronLine[] = []
    for (let i = 0; i < n; i++) {
        const y = 1 - ((i + 0.5) / n) * 2
        const r = Math.sqrt(Math.max(0, 1 - y * y))
        const theta = i * golden
        const normalVec: Vec3 = [r * Math.cos(theta), y, r * Math.sin(theta)]
        const ref: Vec3 = Math.abs(normalVec[1]) < 0.9 ? [0, 1, 0] : [1, 0, 0]
        const u = normalize(cross(ref, normalVec))
        const v = cross(normalVec, u)
        lines.push({
            u,
            v,
            phase: (i / n) * Math.PI * 2,
            radiusScale: 0.62 + Math.random() * 0.38,
            breathPhase: (i % 2) * Math.PI,
            lineDelay: lineDelayMin + Math.random() * lineDelaySpan,
        })
    }
    return lines
}

function lightningBlue(): string {
    const h = (200 + Math.random() * 16).toFixed(0)
    const s = (85 + Math.random() * 15).toFixed(0)
    const l = (56 + Math.random() * 28).toFixed(0)
    return `hsl(${h} ${s}% ${l}%)`
}

export function buildScene(root: SVGSVGElement, lines: ElectronLine[]): Scene {
    const { cx, cy, electronCount: n } = ATOM
    const uid = instanceId++

    const defs = query(root, 'defs', SVGDefsElement)
    const gLines = query(root, '.atom-electron-lines', SVGGElement)
    const gFlashes = query(root, '.atom-electron-line-flashes', SVGGElement)
    const gProtons = query(root, '.atom-protons', SVGGElement)
    const gConnections = query(root, '.atom-connections', SVGGElement)
    const gElectrons = query(root, '.atom-electrons', SVGGElement)
    const gZaps = query(root, '.atom-zaps', SVGGElement)
    const gSparks = query(root, '.atom-zap-sparks', SVGGElement)
    const gLabels = query(root, '.atom-zap-labels', SVGGElement)

    const parts: LineParts[] = []
    const electronPos: Scene['electronPos'] = []
    const protonPos: Point[] = []

    lines.forEach((_, i) => {
        // Each electron line is a path stroked with its own depth gradient (set per frame).
        const gradId = `atom-line-grad-${uid}-${i}`
        const gradient = svgEl('linearGradient', SVGLinearGradientElement, {
            id: gradId,
            gradientUnits: 'userSpaceOnUse',
            x1: cx - 100, y1: cy, x2: cx + 100, y2: cy,
        })
        const gradStart = svgEl('stop', SVGStopElement, {
            offset: '0', 'stop-color': '#2ea8ff', 'stop-opacity': '0.12',
        })
        const gradEnd = svgEl('stop', SVGStopElement, {
            offset: '1', 'stop-color': '#2ea8ff', 'stop-opacity': '0.6',
        })
        gradient.append(gradStart, gradEnd)
        defs.appendChild(gradient)

        const line = svgEl('path', SVGPathElement, {
            class: 'atom-electron-line', stroke: `url(#${gradId})`,
        })
        gLines.appendChild(line)

        const lineFlash = svgEl('path', SVGPathElement, { class: 'atom-electron-line-flash' })
        gFlashes.appendChild(lineFlash)

        const electron = svgEl('circle', SVGCircleElement, {
            class: 'atom-electron', r: 4, cx, cy,
        })
        gElectrons.appendChild(electron)

        const proton = svgEl('circle', SVGCircleElement, { class: 'atom-proton', r: 3, cx, cy })
        proton.setAttribute('fill', lightningBlue())
        gProtons.appendChild(proton)

        const zap = svgEl('path', SVGPathElement, { class: 'atom-zap' })
        gZaps.appendChild(zap)

        const zapSpark = svgEl('circle', SVGCircleElement, { class: 'atom-zap-spark', r: 3, cx, cy })
        gSparks.appendChild(zapSpark)

        const zapLabel = svgEl('text', SVGTextElement, { class: 'atom-zap-label', x: cx, y: cy })
        gLabels.appendChild(zapLabel)

        parts.push({
            line, lineFlash, gradient, gradStart, gradEnd, electron, proton,
            protonRadius: 3.5 + 8 * (i / (n - 1)), // tighter cluster: 3.5–11.5
            zap, zapSpark, zapLabel,
        })
        electronPos.push({ x: cx, y: cy, depth: 0 })
        protonPos.push({ x: cx, y: cy })
    })

    // electron ↔ electron connecting lines: one per unique pair
    const electronPairs: [number, number][] = []
    const electronConnections: SVGLineElement[] = []
    for (let a = 0; a < n; a++) {
        for (let b = a + 1; b < n; b++) {
            electronPairs.push([a, b])
            electronConnections.push(
                gConnections.appendChild(
                    svgEl('line', SVGLineElement, { class: 'atom-connection', x1: cx, y1: cy, x2: cx, y2: cy }),
                ),
            )
        }
    }

    // proton ↔ electron connecting lines: one per electron, to its nearest proton
    const protonConnections: SVGLineElement[] = []
    for (let i = 0; i < n; i++) {
        protonConnections.push(
            gConnections.appendChild(
                svgEl('line', SVGLineElement, { class: 'atom-connection', x1: cx, y1: cy, x2: cx, y2: cy }),
            ),
        )
    }

    return {
        lines,
        parts,
        electronPairs,
        electronConnections,
        protonConnections,
        electronPos,
        protonPos,
        nucleus: query(root, '.atom-nucleus', SVGGElement),
        nucleusGlow: query(root, '.atom-nucleus-glow', SVGCircleElement),
        pulse: query(root, '.atom-zap-pulse', SVGCircleElement),
    }
}
