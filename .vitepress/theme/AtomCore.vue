<script setup lang="ts">
// Animated atom for the home hero. The static skeleton lives here; the engine
// (loaded client-side only) fills in the dynamic electrons, lines, protons and zaps.
import { onMounted, onBeforeUnmount, ref } from 'vue'

const root = ref<SVGSVGElement | null>(null)
let handle: { dispose(): void } | null = null

onMounted(async () => {
    if (!root.value) return
    const { createAtom } = await import('./atom/engine')
    handle = createAtom(root.value)
})

onBeforeUnmount(() => {
    handle?.dispose()
    handle = null
})
</script>

<template>
    <svg
        ref="root"
        class="atom"
        viewBox="0 0 480 480"
        role="img"
        aria-label="An animated atom: electrons orbit a glowing nucleus on a slowly rotating sphere of lines, firing the names of languages Atomic Claude indexes."
    >
        <defs>
            <radialGradient id="atom-core-glow" cx="50%" cy="50%" r="50%">
                <stop offset="0%" stop-color="#ffe9b0" stop-opacity="0.55" />
                <stop offset="45%" stop-color="#f5c050" stop-opacity="0.28" />
                <stop offset="100%" stop-color="#e8a020" stop-opacity="0" />
            </radialGradient>
        </defs>

        <g class="atom-electron-lines"></g>
        <g class="atom-electron-line-flashes"></g>

        <g class="atom-nucleus">
            <circle class="atom-nucleus-glow" cx="240" cy="240" r="62" fill="url(#atom-core-glow)" />
            <circle class="atom-nucleus-ring" cx="240" cy="240" r="15" />
            <g class="atom-protons"></g>
            <circle class="atom-nucleus-heart-glow" cx="240" cy="240" r="15" fill="url(#atom-core-glow)" />
            <circle class="atom-nucleus-heart" cx="240" cy="240" r="5.5" />
        </g>

        <g class="atom-connections"></g>
        <g class="atom-electrons"></g>
        <g class="atom-zaps"></g>
        <g class="atom-zap-sparks"></g>
        <circle class="atom-zap-pulse" cx="240" cy="240" r="34" />
        <g class="atom-zap-labels"></g>
    </svg>
</template>

<!-- Not scoped: the engine appends elements at runtime, which Vue's scoped
     attribute would not reach. Everything is namespaced under .atom instead. -->
<style>
.atom {
    /* palette — kept in sync with COLOR in atom/config.ts */
    --atom-electron: #f5c050;
    --atom-line: #2ea8ff;
    --atom-line-glow: rgba(70, 168, 255, 0.85);
    --atom-line-flash: #22e66a;
    --atom-connection: #9be88f;
    --atom-proton-glow: rgba(70, 168, 255, 0.7);
    --atom-hot: #fff6e0; /* zap bolt, spark, nucleus heart */
    --atom-nucleus: #f5c050; /* nucleus ring + shockwave pulse */
    --atom-label-sql: #fcd878;
    --atom-bg: #141210; /* zap-label outline */

    display: block;
    width: 100%;
    max-width: 460px;
    height: auto;
    margin-inline: auto;
    overflow: visible;
}

/* electron lines — depth comes from a per-line gradient set each frame */
.atom .atom-electron-line {
    fill: none;
    stroke-width: 2;
    stroke-linecap: round;
    filter: drop-shadow(0 0 3px var(--atom-line-glow));
}
.atom .atom-electron-line-flash {
    fill: none;
    stroke: var(--atom-line-flash);
    stroke-width: 2.5;
    stroke-linecap: round;
    opacity: 0;
    filter: drop-shadow(0 0 5px rgba(34, 230, 106, 0.85));
}

/* connecting lines (electron↔electron and proton↔electron) */
.atom .atom-connection {
    stroke: var(--atom-connection);
    stroke-width: 1.2;
    opacity: 0;
    filter: drop-shadow(0 0 3px rgba(155, 232, 143, 0.65));
}

.atom .atom-electron {
    fill: var(--atom-electron);
}

/* protons — fill is set per-proton (varied lightning blues) */
.atom .atom-proton {
    transform-box: fill-box;
    transform-origin: center;
    filter: drop-shadow(0 0 2px var(--atom-proton-glow));
}

/* nucleus */
.atom .atom-nucleus-ring {
    fill: none;
    stroke: var(--atom-nucleus);
    stroke-width: 1.4;
    opacity: 0.5;
    stroke-dasharray: 3 7;
    stroke-linecap: round;
    transform-box: fill-box;
    transform-origin: center;
    animation: atom-spin 32s linear infinite;
}
.atom .atom-nucleus-heart {
    fill: var(--atom-hot);
    transform-box: fill-box;
    transform-origin: center;
    animation: atom-beat 6.8s ease-in-out infinite;
}
.atom .atom-nucleus-heart-glow {
    transform-box: fill-box;
    transform-origin: center;
    animation: atom-beat 6.8s ease-in-out infinite;
}

/* zaps */
.atom .atom-zap {
    fill: none;
    stroke: var(--atom-hot);
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
    opacity: 0;
}
.atom .atom-zap-spark {
    fill: none;
    stroke: var(--atom-hot);
    stroke-width: 2;
    opacity: 0;
}
.atom .atom-zap-pulse {
    fill: none;
    stroke: var(--atom-nucleus);
    stroke-width: 2;
    opacity: 0;
}
.atom .atom-zap-label {
    font-family: var(--vp-font-family-mono);
    font-weight: 500;
    font-size: 15px;
    fill: var(--atom-electron);
    text-anchor: middle;
    dominant-baseline: middle;
    opacity: 0;
    transform-box: fill-box;
    transform-origin: center;
    paint-order: stroke;
    stroke: var(--atom-bg);
    stroke-width: 3px;
}
.atom .atom-zap-label.is-sql {
    font-weight: 700;
    fill: var(--atom-label-sql);
}

@keyframes atom-spin {
    to {
        transform: rotate(360deg);
    }
}
@keyframes atom-beat {
    0%,
    100% {
        transform: scale(0.9);
        opacity: 0.88;
    }
    50% {
        transform: scale(1.2);
        opacity: 1;
    }
}

@media (prefers-reduced-motion: reduce) {
    .atom .atom-nucleus-ring,
    .atom .atom-nucleus-heart,
    .atom .atom-nucleus-heart-glow {
        animation: none;
    }
}
</style>
