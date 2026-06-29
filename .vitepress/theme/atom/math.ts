// Pure geometry helpers for the atom. No DOM, no state — easy to reason about and test.

export type Vec3 = [number, number, number]
export interface Point {
    x: number
    y: number
}

export function cross(a: Vec3, b: Vec3): Vec3 {
    return [
        a[1] * b[2] - a[2] * b[1],
        a[2] * b[0] - a[0] * b[2],
        a[0] * b[1] - a[1] * b[0],
    ]
}

export function normalize(a: Vec3): Vec3 {
    const m = Math.hypot(a[0], a[1], a[2]) || 1
    return [a[0] / m, a[1] / m, a[2] / m]
}

// Rotate a point: yaw around Y, then tilt around X. Used to spin the whole atom.
export function rotate(p: Vec3, yaw: number, tilt: number): Vec3 {
    const cy = Math.cos(yaw)
    const sy = Math.sin(yaw)
    const x = p[0] * cy + p[2] * sy
    const z = -p[0] * sy + p[2] * cy
    const y = p[1]
    const cx = Math.cos(tilt)
    const sx = Math.sin(tilt)
    return [x, y * cx - z * sx, y * sx + z * cx]
}

// A closed SVG path string through the given screen points.
export function closedPath(points: Point[]): string {
    let d = ''
    for (let i = 0; i < points.length; i++) {
        d += (i ? 'L' : 'M') + points[i].x.toFixed(1) + ' ' + points[i].y.toFixed(1) + ' '
    }
    return d + 'Z'
}

// A jagged "lightning" path between two points; jitter peaks mid-span.
export function jaggedPath(x1: number, y1: number, x2: number, y2: number): string {
    const segments = 7
    const dx = x2 - x1
    const dy = y2 - y1
    const len = Math.hypot(dx, dy) || 1
    const nx = -dy / len
    const ny = dx / len
    let d = `M ${x1.toFixed(1)} ${y1.toFixed(1)}`
    for (let s = 1; s < segments; s++) {
        const t = s / segments
        const amp = Math.sin(t * Math.PI) * len * 0.1
        const off = (Math.random() * 2 - 1) * amp
        d += ` L ${(x1 + dx * t + nx * off).toFixed(1)} ${(y1 + dy * t + ny * off).toFixed(1)}`
    }
    return d + ` L ${x2.toFixed(1)} ${y2.toFixed(1)}`
}

// Reveal ramp: 0 before `elapsed`, eased to 1 across `duration`. Used for the intro fades.
export function easeOutFade(elapsed: number, duration: number): number {
    if (elapsed <= 0) return 0
    if (elapsed >= duration) return 1
    const p = elapsed / duration
    return 1 - (1 - p) * (1 - p)
}
