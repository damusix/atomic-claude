<script setup lang="ts">
// Hero session player. Content lives in session-script.ts; this only drives it:
// types the command char-by-char, reveals the session line-by-line with Claude
// Code's transcript chrome (⏺ actions, ⎿ output), and lets the viewer play/pause,
// jump between sections, and scroll. Pausable + cancellable.
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { type SessionSlide, type OutputLine } from './session-script'

// The session to play is supplied by the host (home hero, concepts page, …).
const props = defineProps<{ session: SessionSlide[] }>()

const body = ref<HTMLDivElement | null>(null)
const current = ref(0)
const playing = ref(true)

let token = 0 // bumped to cancel the running animation
let rafId = 0
let disposed = false
const reduced = !import.meta.env.SSR && matchMedia('(prefers-reduced-motion: reduce)').matches

// rAF countdown that freezes while paused and bails when cancelled.
function wait(ms: number, gen: number): Promise<void> {
    return new Promise((resolve) => {
        let remaining = ms
        let last = performance.now()
        const step = (now: number) => {
            if (disposed || gen !== token) return resolve()
            const dt = now - last
            last = now
            if (playing.value) remaining -= dt
            if (remaining <= 0) return resolve()
            rafId = requestAnimationFrame(step)
        }
        rafId = requestAnimationFrame(step)
    })
}

function clearBody() {
    if (body.value) body.value.innerHTML = ''
}
function span(cls: string, text: string): HTMLSpanElement {
    const el = document.createElement('span')
    el.className = cls
    el.textContent = text
    return el
}
function toBottom() {
    if (body.value) body.value.scrollTop = body.value.scrollHeight
}

// Build one output row with the right prefix chrome for its kind.
function buildLine(line: OutputLine): HTMLDivElement {
    const row = document.createElement('div')
    row.className = 'sp-row'
    const kind = line.kind ?? 'std'
    if (kind === 'gap') {
        row.classList.add('sp-gap')
        return row
    }

    if (kind === 'say' || kind === 'tool') {
        row.append(span('sp-dot', '⏺'), document.createTextNode(' '))
    } else if (kind === 'out') {
        row.append(span('sp-bracket', '⎿'), document.createTextNode('  '))
    } else if (kind === 'cont') {
        row.append(document.createTextNode('     '))
    }

    const text = line.text ?? ''
    const tone = line.tone ? ' sp-' + line.tone : ''

    if (kind === 'tool') {
        const paren = text.indexOf('(')
        if (paren !== -1) {
            row.append(span('sp-toolname' + tone, text.slice(0, paren)), span('sp-toolargs' + tone, text.slice(paren)))
        } else {
            row.append(span('sp-toolname' + tone, text))
        }
        return row
    }

    const base = kind === 'say' ? 'sp-say' : kind === 'std' ? 'sp-std' : 'sp-out'
    row.append(span(base + tone, text))
    return row
}

function commandRow(slide: SessionSlide): { verbEl: HTMLSpanElement; restEl: HTMLSpanElement; cursor: HTMLSpanElement } {
    const row = document.createElement('div')
    row.className = 'sp-row sp-cmd'
    const verbEl = span('sp-verb', '')
    const restEl = span('sp-rest', '')
    const cursor = span('sp-cursor', '▋')
    row.append(span('sp-prompt', '❯ '), verbEl, restEl, cursor)
    body.value?.appendChild(row)
    return { verbEl, restEl, cursor }
}

async function typeInto(el: HTMLElement, text: string, gen: number) {
    for (const ch of text) {
        if (gen !== token) return
        el.textContent += ch
        toBottom()
        await wait(26 + Math.random() * 22, gen)
    }
}

async function animate(idx: number, gen: number) {
    clearBody()
    const slide = props.session[idx]
    const sp = slide.command.indexOf(' ')
    const verb = sp === -1 ? slide.command : slide.command.slice(0, sp)
    const rest = sp === -1 ? '' : slide.command.slice(sp)

    const { verbEl, restEl, cursor } = commandRow(slide)
    await typeInto(verbEl, verb, gen)
    await typeInto(restEl, rest, gen)
    if (gen !== token) return
    await wait(420, gen)
    cursor.remove()

    await wait(260, gen)
    for (const line of slide.output) {
        if (gen !== token) return
        body.value?.appendChild(buildLine(line))
        toBottom()
        await wait((line.kind === 'gap' ? 30 : 78) + Math.random() * 50, gen)
    }
}

function renderStatic(idx: number) {
    clearBody()
    const slide = props.session[idx]
    const sp = slide.command.indexOf(' ')
    const { verbEl, restEl, cursor } = commandRow(slide)
    cursor.remove()
    verbEl.textContent = sp === -1 ? slide.command : slide.command.slice(0, sp)
    restEl.textContent = sp === -1 ? '' : slide.command.slice(sp)
    for (const line of slide.output) body.value?.appendChild(buildLine(line))
}

async function loop(startIdx: number) {
    const gen = ++token
    let idx = startIdx
    while (gen === token && !disposed) {
        current.value = idx
        await animate(idx, gen)
        if (gen !== token) return
        await wait(6000, gen) // hold the finished section
        if (gen !== token) return
        idx = (idx + 1) % props.session.length
    }
}

function jump(i: number) {
    current.value = i
    if (reduced) {
        renderStatic(i)
        return
    }
    playing.value = true
    loop(i)
}

function togglePlay() {
    if (reduced) return
    playing.value = !playing.value
}

onMounted(() => {
    if (reduced) {
        renderStatic(0)
        return
    }
    loop(0)
})

onBeforeUnmount(() => {
    disposed = true
    token++
    cancelAnimationFrame(rafId)
})
</script>

<template>
    <div class="sp-term">
        <div class="sp-bar">
            <span class="sp-dots"><i></i><i></i><i></i></span>
            <span class="sp-title">~/atomic-claude</span>
        </div>

        <div ref="body" class="sp-body"></div>

        <div class="sp-nav">
            <button
                v-if="!reduced"
                class="sp-play"
                :aria-label="playing ? 'Pause' : 'Play'"
                @click="togglePlay"
            >
                {{ playing ? '❚❚' : '▶' }}
            </button>
            <div class="sp-tabs">
                <button
                    v-for="(slide, i) in session"
                    :key="slide.id"
                    class="sp-tab"
                    :class="{ active: i === current }"
                    @click="jump(i)"
                >
                    {{ slide.label }}
                </button>
            </div>
        </div>
    </div>
</template>

<style scoped>
.sp-term {
    width: 100%;
    max-width: 500px;
    margin-inline: auto;
    border: 1px solid var(--vp-c-border);
    border-radius: 12px;
    background: #0f0d0a;
    box-shadow: 0 24px 56px -24px rgba(0, 0, 0, 0.7);
    font-family: var(--vp-font-family-mono);
    text-align: left;
    overflow: hidden;
}

.sp-bar {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.6rem 0.85rem;
    border-bottom: 1px solid var(--vp-c-divider);
    user-select: none;
}
.sp-dots {
    display: inline-flex;
    gap: 0.4rem;
}
.sp-dots i {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: #3a3028;
}
.sp-dots i:first-child {
    background: #6b4f1e;
}
.sp-title {
    font-size: 0.72rem;
    letter-spacing: 0.02em;
    color: var(--vp-c-text-3);
}

.sp-body {
    padding: 0.9rem 1rem 1rem;
    height: 22rem;
    overflow-y: auto;
    font-size: 0.75rem;
    line-height: 1.55;
    scrollbar-width: thin;
}
.sp-row {
    white-space: pre;
    min-height: 1.2em;
}
.sp-gap {
    min-height: 0.55em;
}

/* the typed command — marked as input with a left rule */
.sp-cmd {
    margin: 0.05rem 0 0.45rem;
    padding-left: 0.5rem;
    border-left: 2px solid #6b4f1e;
}
.sp-prompt,
.sp-verb {
    color: #f5c050;
}
.sp-prompt {
    font-weight: 600;
}
.sp-rest {
    color: #f0e8d8;
}
.sp-cursor {
    color: #f5c050;
    animation: sp-blink 1s steps(1) infinite;
}
@keyframes sp-blink {
    50% {
        opacity: 0;
    }
}

/* session chrome */
.sp-dot {
    color: #f5c050;
}
.sp-bracket {
    color: #5a4d3a;
}
.sp-say {
    color: #e8e0d0;
}
.sp-std {
    color: #cfc6b6;
}
.sp-out {
    color: #a89c86;
}
.sp-toolname {
    color: #d4c6a8;
    font-weight: 600;
}
.sp-toolargs {
    color: #8c7858;
}

/* tone overrides — declared last so they win over the kind colour */
.sp-ok {
    color: #f5c050;
}
.sp-warn {
    color: #e0915a;
}
.sp-muted {
    color: #8c7858;
}

.sp-nav {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.5rem 0.7rem;
    border-top: 1px solid var(--vp-c-divider);
    user-select: none;
}
/* plain links, no chrome — the active one just turns amber */
.sp-play {
    flex: none;
    background: none;
    border: 0;
    padding: 0 0.25rem 0 0.1rem;
    color: var(--vp-c-text-3);
    font-size: 0.7rem;
    cursor: pointer;
    transition: color 0.15s;
}
.sp-play:hover {
    color: #f5c050;
}
.sp-tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 0.7rem;
}
.sp-tab {
    background: none;
    border: 0;
    padding: 0;
    font-family: var(--vp-font-family-mono);
    font-size: 0.72rem;
    color: var(--vp-c-text-3);
    cursor: pointer;
    transition: color 0.15s;
}
.sp-tab:hover {
    color: var(--vp-c-text-2);
}
.sp-tab.active {
    color: #f5c050;
}
/* no stray box on mouse click; keep a subtle cue for keyboard focus */
.sp-play:focus,
.sp-tab:focus {
    outline: none;
}
.sp-play:focus-visible,
.sp-tab:focus-visible {
    outline: none;
    color: #f5c050;
    text-decoration: underline;
}

@media (prefers-reduced-motion: reduce) {
    .sp-cursor {
        animation: none;
    }
}
</style>
