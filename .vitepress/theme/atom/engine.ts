// Runtime for the atom: builds the scene, drives the per-frame render, fires zaps,
// and manages the lifecycle (intro, mouse parallax, pause when off-screen/hidden).

import { animate, cubicBezier, engine } from 'animejs'
import { ATOM, LANGUAGES, SQL_FAMILY, COLOR } from './config'
import { rotate, closedPath, jaggedPath, easeOutFade, type Vec3 } from './math'
import { buildElectronLines, buildScene } from './scene'

export interface AtomHandle {
    dispose(): void
}

export function createAtom(root: SVGSVGElement): AtomHandle {
    const {
        cx, cy, radius, electronCount, tilt: baseTilt, lineSamples,
        rotationPeriod, electronTravelPeriod, tiltWobblePeriod,
        breathMin, breathMax, breathPeriod, protonChurnPeriod, protonChurnAmp,
        pairStart, pairStagger, pairFade, lineFade, introMs,
        electronConnectDist, electronConnectMax, protonConnectDist, protonConnectMax,
        flickerInterval, flickerDur, flickerStrobeFreq,
        zapGapMin, zapGapSpan, zapCountMin, zapCountSpan, zapCascadeBase, zapCascadeSpan,
        parallaxEase, parallaxYaw, parallaxTilt,
    } = ATOM

    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches
    const lines = buildElectronLines()
    const scene = buildScene(root, lines)
    const travelEase = cubicBezier(0.12, 0.5, 0.88, 0.5) // linger mid-line, dart at the ends

    // ── state ──
    let rafId = 0
    let startTime = 0
    let pausedClock = 0
    let zapTimer: ReturnType<typeof setTimeout> | null = null
    const burstTimers: ReturnType<typeof setTimeout>[] = []
    let langIndex = 0
    let running = false
    let onScreen = true
    let pageVisible = true
    let anims: ReturnType<typeof animate>[] = []
    let mouseTargetX = 0
    let mouseTargetY = 0
    let mouseX = 0
    let mouseY = 0

    const track = (anim: ReturnType<typeof animate>) => {
        anims.push(anim)
        if (anims.length > 90) anims = anims.slice(-45)
    }

    const drawConnection = (
        line: SVGLineElement, ax: number, ay: number, bx: number, by: number, opacity: number,
    ) => {
        line.setAttribute('x1', ax.toFixed(1))
        line.setAttribute('y1', ay.toFixed(1))
        line.setAttribute('x2', bx.toFixed(1))
        line.setAttribute('y2', by.toFixed(1))
        line.style.opacity = opacity.toFixed(3)
    }
    const hideConnection = (line: SVGLineElement) => {
        if (line.style.opacity !== '0') line.style.opacity = '0'
    }

    // ── per-frame render ──
    function render(elapsed: number) {
        const t = reduced ? 1e6 : elapsed // reduced-motion → everything already revealed
        scene.nucleus.style.opacity = Math.min(1, t / 300).toFixed(3) // nucleus ignites first

        // mouse parallax: ease the rotation axis toward the cursor
        mouseX += (mouseTargetX - mouseX) * parallaxEase
        mouseY += (mouseTargetY - mouseY) * parallaxEase
        const yaw = (elapsed / rotationPeriod) * Math.PI * 2 + mouseX * parallaxYaw
        const tilt = baseTilt + Math.sin(elapsed / tiltWobblePeriod) * 0.12 + mouseY * parallaxTilt
        const travel = travelEase((elapsed % electronTravelPeriod) / electronTravelPeriod) * Math.PI * 2

        // pick one electron line to flicker this interval (false-contact look)
        const bucket = Math.floor(elapsed / flickerInterval)
        const flickerLine = (bucket * 7) % electronCount
        const flickerElapsed = elapsed - bucket * flickerInterval

        lines.forEach((line, i) => {
            const part = scene.parts[i]

            // intro: electron+proton fade in, then (after a beat) the line fades in
            const pairBirth = pairStart + i * pairStagger
            const pairOn = easeOutFade(t - pairBirth, pairFade)
            const lineOn = easeOutFade(t - (pairBirth + line.lineDelay), lineFade)

            // breathing radius for this line (two antiphase groups via breathPhase)
            const breath = breathMin + (breathMax - breathMin) *
                (0.5 + 0.5 * Math.sin((elapsed / breathPeriod) * Math.PI * 2 + line.breathPhase))
            const ri = radius * breath * line.radiusScale

            // rotate the line's basis once; path, depth gradient and electron all derive from it
            const u: Vec3 = rotate([ri * line.u[0], ri * line.u[1], ri * line.u[2]], yaw, tilt)
            const v: Vec3 = rotate([ri * line.v[0], ri * line.v[1], ri * line.v[2]], yaw, tilt)

            // line path
            const points = []
            for (let k = 0; k < lineSamples; k++) {
                const a = (k / lineSamples) * Math.PI * 2
                const c = Math.cos(a)
                const s = Math.sin(a)
                points.push({ x: cx + c * u[0] + s * v[0], y: cy - (c * u[1] + s * v[1]) })
            }
            const d = closedPath(points)
            part.line.setAttribute('d', d)
            part.lineFlash.setAttribute('d', d)

            // false-contact flicker: only the chosen line, only inside its brief burst, post-intro
            let gate = 1
            if (!reduced && t > introMs && i === flickerLine && flickerElapsed < flickerDur) {
                gate = Math.sin(elapsed * flickerStrobeFreq) > 0 ? 1 : 0
            }
            part.line.style.opacity = (lineOn * gate).toFixed(3)

            // depth gradient: z(a) is a sinusoid, so near/far points are analytic (smooth fade)
            const amp = Math.hypot(u[2], v[2])
            const phi = Math.atan2(v[2], u[2])
            const ex = Math.cos(phi) * u[0] + Math.sin(phi) * v[0]
            const ey = Math.cos(phi) * u[1] + Math.sin(phi) * v[1]
            const nearX = cx + ex
            const nearY = cy - ey
            const farX = cx - ex
            const farY = cy + ey
            const contrast = 0.26 * Math.min(1, amp / ri)
            const mid = 0.36
            let nx = nearX
            let ny = nearY
            if (Math.hypot(nx - farX, ny - farY) < 0.5) nx = farX + 0.5
            part.gradient.setAttribute('x1', farX.toFixed(1))
            part.gradient.setAttribute('y1', farY.toFixed(1))
            part.gradient.setAttribute('x2', nx.toFixed(1))
            part.gradient.setAttribute('y2', ny.toFixed(1))
            part.gradStart.setAttribute('stop-opacity', (mid - contrast).toFixed(3))
            part.gradEnd.setAttribute('stop-opacity', (mid + contrast).toFixed(3))

            // electron — same basis, so its depth matches the line's gradient at that point
            const angle = line.phase + travel
            const ex2 = Math.cos(angle)
            const ey2 = Math.sin(angle)
            const px = cx + ex2 * u[0] + ey2 * v[0]
            const py = cy - (ex2 * u[1] + ey2 * v[1])
            const pz = ex2 * u[2] + ey2 * v[2]
            const depth = (pz / ri + 1) / 2 // 0 far .. 1 near
            scene.electronPos[i] = { x: px, y: py, depth }
            part.electron.setAttribute('cx', px.toFixed(2))
            part.electron.setAttribute('cy', py.toFixed(2))
            part.electron.setAttribute('r', (2.6 + 3 * depth).toFixed(2))
            part.electron.style.opacity = ((0.3 + 0.7 * depth) * pairOn).toFixed(3)

            // paired proton — tracks the electron's bearing inside the nucleus, with a gentle churn
            const bx = px - cx
            const by = py - cy
            const m = Math.hypot(bx, by) || 1
            const pr = part.protonRadius + protonChurnAmp * Math.sin(elapsed / protonChurnPeriod + i * 1.7)
            const ppx = cx + (bx / m) * pr
            const ppy = cy + (by / m) * pr
            part.proton.setAttribute('cx', ppx.toFixed(2))
            part.proton.setAttribute('cy', ppy.toFixed(2))
            part.proton.style.opacity = pairOn.toFixed(3)
            scene.protonPos[i] = { x: ppx, y: ppy }
        })

        const showConnections = t > introMs

        // electron ↔ electron connecting lines (fade with distance)
        scene.electronPairs.forEach(([a, b], k) => {
            const line = scene.electronConnections[k]
            const pa = scene.electronPos[a]
            const pb = scene.electronPos[b]
            const dist = Math.hypot(pa.x - pb.x, pa.y - pb.y)
            if (showConnections && dist < electronConnectDist) {
                drawConnection(line, pa.x, pa.y, pb.x, pb.y, (1 - dist / electronConnectDist) * electronConnectMax)
            } else {
                hideConnection(line)
            }
        })

        // proton ↔ electron connecting lines: each electron to its nearest proton when close
        for (let i = 0; i < electronCount; i++) {
            const e = scene.electronPos[i]
            let best = Infinity
            let bx = cx
            let by = cy
            for (let j = 0; j < electronCount; j++) {
                const dist = Math.hypot(e.x - scene.protonPos[j].x, e.y - scene.protonPos[j].y)
                if (dist < best) {
                    best = dist
                    bx = scene.protonPos[j].x
                    by = scene.protonPos[j].y
                }
            }
            const line = scene.protonConnections[i]
            if (showConnections && best < protonConnectDist) {
                drawConnection(line, e.x, e.y, bx, by, (1 - best / protonConnectDist) * protonConnectMax)
            } else {
                hideConnection(line)
            }
        }
    }

    // ── a single zap on electron `i` ──
    function zap(i: number) {
        const part = scene.parts[i]
        const pos = scene.electronPos[i]
        const lang = LANGUAGES[langIndex % LANGUAGES.length]
        langIndex++
        const isSql = SQL_FAMILY.has(lang) // SQL wedge → bigger beat
        const big = isSql ? 1.6 : 1

        // white-hot lightning bolt, nucleus → electron
        part.zap.setAttribute('d', jaggedPath(cx, cy, pos.x, pos.y))
        part.zap.style.strokeWidth = (2 * big).toFixed(2)
        const len = part.zap.getTotalLength()
        part.zap.style.strokeDasharray = `${len}`
        part.zap.style.strokeDashoffset = `${len}`
        track(animate(part.zap, { strokeDashoffset: [len, 0], duration: isSql ? 160 : 130, ease: 'outQuad' }))
        track(animate(part.zap, {
            opacity: [
                { to: 1, duration: 60 }, { to: 0.5, duration: 40 },
                { to: 1, duration: 35 }, { to: 0, duration: isSql ? 260 : 160 },
            ],
            ease: 'linear',
        }))

        // spark at the strike point
        part.zapSpark.setAttribute('cx', pos.x.toFixed(1))
        part.zapSpark.setAttribute('cy', pos.y.toFixed(1))
        part.zapSpark.style.opacity = '1'
        track(animate(part.zapSpark, { r: [3, 16 * big], opacity: [0.9, 0], duration: 520, ease: 'outQuad' }))

        // struck electron flashes red, then fades back to amber over ~a quarter second
        track(animate(part.electron, { fill: [COLOR.electronZap, COLOR.electron], duration: 260, ease: 'outQuad' }))

        // its line flashes green
        track(animate(part.lineFlash, {
            opacity: [{ to: isSql ? 1 : 0.85, duration: 90, ease: 'outQuad' }, { to: 0, duration: 640, ease: 'inQuad' }],
        }))

        // paired proton pops
        track(animate(part.proton, { scale: [1, 1.9, 1], duration: 420, ease: 'outQuad' }))

        // shockwave pulse + nucleus-glow flare
        scene.pulse.style.opacity = '1'
        track(animate(scene.pulse, { r: [34, 130 * big], opacity: [0.5, 0], duration: isSql ? 900 : 720, ease: 'outQuad' }))
        track(animate(scene.nucleusGlow, {
            opacity: [{ to: 1, duration: 90 }, { to: 0.55, duration: 520 }], ease: 'outQuad',
        }))

        // language label (bigger for the SQL wedge), slow taper out
        const label = part.zapLabel
        label.textContent = lang
        label.classList.toggle('is-sql', isSql)
        const ux = pos.x - cx
        const uy = pos.y - cy
        const m = Math.hypot(ux, uy) || 1
        label.setAttribute('x', (pos.x + (ux / m) * 24).toFixed(1))
        label.setAttribute('y', (pos.y + (uy / m) * 24).toFixed(1))
        track(animate(label, {
            opacity: [0, 1, 0.7, 0.3, 0],
            scale: isSql ? [0.8, 1.35, 1.25, 1.25, 1.25] : [0.7, 1.06, 1, 1, 1],
            duration: isSql ? 2600 : 2100,
            ease: 'outQuad',
        }))
    }

    // ── zap scheduling ──
    function pickElectrons(k: number): number[] {
        const picks: number[] = []
        while (picks.length < k) {
            const i = Math.floor(Math.random() * electronCount)
            if (!picks.includes(i)) picks.push(i)
        }
        return picks
    }

    function zapBurst() {
        const count = zapCountMin + Math.floor(Math.random() * zapCountSpan)
        pickElectrons(count).forEach((i, n) => {
            burstTimers.push(setTimeout(() => zap(i), n * (zapCascadeBase + Math.random() * zapCascadeSpan)))
        })
    }

    function scheduleZap() {
        zapTimer = setTimeout(() => {
            zapBurst()
            scheduleZap()
        }, zapGapMin + Math.random() * zapGapSpan)
    }

    // ── lifecycle ──
    function loop(now: number) {
        if (!startTime) startTime = now
        render(now - startTime)
        rafId = requestAnimationFrame(loop)
    }

    function start() {
        stop()
        startTime = 0
        langIndex = 0
        running = true
        pageVisible = !document.hidden
        scene.nucleusGlow.style.opacity = '0.55'
        rafId = requestAnimationFrame(loop)
        // first burst lands as the atom finishes assembling, then the cadence continues
        burstTimers.push(setTimeout(() => {
            zapBurst()
            scheduleZap()
        }, introMs + 150))
    }

    function stop() {
        if (rafId) cancelAnimationFrame(rafId)
        rafId = 0
        if (zapTimer) clearTimeout(zapTimer)
        zapTimer = null
        burstTimers.forEach(clearTimeout)
        burstTimers.length = 0
        anims.forEach((a) => {
            try {
                a.pause?.()
                a.revert?.()
            } catch {
                // animation already disposed — nothing to undo
            }
        })
        anims = []
    }

    // Pause/resume without losing place: freeze rAF + scheduling + the anime engine,
    // and shift startTime on resume so the render clock stays continuous.
    function pauseAll() {
        if (!running) return
        running = false
        if (rafId) cancelAnimationFrame(rafId)
        rafId = 0
        if (zapTimer) clearTimeout(zapTimer)
        zapTimer = null
        burstTimers.forEach(clearTimeout)
        burstTimers.length = 0
        pausedClock = performance.now()
        try {
            engine.pause()
        } catch {
            // engine not pausable in this build — animations simply keep their state
        }
    }

    function resumeAll() {
        if (running) return
        running = true
        startTime += performance.now() - pausedClock
        try {
            engine.resume()
        } catch {
            // engine not resumable — frames will catch up on the next tick
        }
        rafId = requestAnimationFrame(loop)
        scheduleZap()
    }

    function updateRun() {
        if (reduced) return
        if (onScreen && pageVisible) resumeAll()
        else pauseAll()
    }

    // ── interactions ──
    const onPointerMove = (e: PointerEvent) => {
        const rect = root.getBoundingClientRect()
        const centerX = rect.left + rect.width / 2
        const centerY = rect.top + rect.height / 2
        mouseTargetX = Math.max(-1, Math.min(1, (e.clientX - centerX) / (window.innerWidth * 0.5)))
        mouseTargetY = Math.max(-1, Math.min(1, (e.clientY - centerY) / (window.innerHeight * 0.5)))
    }
    const onVisibility = () => {
        pageVisible = !document.hidden
        updateRun()
    }
    const observer = new IntersectionObserver(
        (entries) => {
            onScreen = entries[0].isIntersecting
            updateRun()
        },
        { threshold: 0.05 },
    )

    // ── boot ──
    if (reduced) {
        scene.nucleusGlow.style.opacity = '0.55'
        render(0)
        showStaticLabels()
    } else {
        start()
        window.addEventListener('pointermove', onPointerMove)
        document.addEventListener('visibilitychange', onVisibility)
        observer.observe(root)
    }

    function showStaticLabels() {
        const demo: Record<number, string> = { 0: 'TypeScript', 5: 'T-SQL', 9: 'Rust' }
        for (const key in demo) {
            const i = Number(key)
            const pos = scene.electronPos[i]
            const ux = pos.x - cx
            const uy = pos.y - cy
            const m = Math.hypot(ux, uy) || 1
            const label = scene.parts[i].zapLabel
            label.textContent = demo[i]
            label.classList.toggle('is-sql', SQL_FAMILY.has(demo[i]))
            label.setAttribute('x', (pos.x + (ux / m) * 24).toFixed(1))
            label.setAttribute('y', (pos.y + (uy / m) * 24).toFixed(1))
            label.style.opacity = '1'
        }
    }

    return {
        dispose() {
            stop()
            window.removeEventListener('pointermove', onPointerMove)
            document.removeEventListener('visibilitychange', onVisibility)
            observer.disconnect()
        },
    }
}
