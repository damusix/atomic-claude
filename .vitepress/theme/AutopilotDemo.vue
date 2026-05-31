<script setup>
// Static, theme-matched terminal showing a hands-off /autopilot run.
// No image asset — real text, crisp at any DPI, animates on load.
const steps = [
    { label: 'plan', detail: 'spec written' },
    { label: 'implement', detail: '4 checkpoints · TDD' },
    { label: 'review', detail: 'findings fixed' },
    { label: 'ship', detail: 'squash · merge' },
]
</script>

<template>
    <div class="ac-term" role="img" aria-label="An /autopilot run taking GitHub issue 142 from plan to implement to review to a squashed, merged PR — hands-off.">
        <div class="ac-term__bar">
            <span class="ac-term__dots"><i></i><i></i><i></i></span>
            <span class="ac-term__title">~/app</span>
        </div>
        <div class="ac-term__body">
            <div class="ac-term__cmd ac-row" style="--d: 0.05s">
                <span class="ac-prompt">❯</span>
                <span class="ac-cmd">/autopilot <span class="ac-arg">142</span> squash-and-merge</span>
            </div>
            <div class="ac-term__steps">
                <div
                    v-for="(s, i) in steps"
                    :key="s.label"
                    class="ac-step ac-row"
                    :style="{ '--d': 0.5 + i * 0.42 + 's' }"
                >
                    <span class="ac-mark">◇</span>
                    <span class="ac-label">{{ s.label }}</span>
                    <span class="ac-detail">{{ s.detail }}</span>
                    <span class="ac-check" :style="{ '--d': 0.5 + i * 0.42 + 0.28 + 's' }">✓</span>
                </div>
            </div>
            <div class="ac-term__done ac-row" style="--d: 2.5s">
                issue <span class="ac-arg">#142</span> closed
            </div>
        </div>
    </div>
</template>

<style scoped>
.ac-term {
    width: 100%;
    max-width: 460px;
    margin-inline: auto;
    border: 1px solid var(--vp-c-border);
    border-radius: 12px;
    background: #0f0d0a;
    box-shadow: 0 24px 56px -24px rgba(0, 0, 0, 0.7);
    font-family: var(--vp-font-family-mono);
    text-align: left;
    overflow: hidden;
    user-select: none;
}

.ac-term__bar {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.6rem 0.85rem;
    border-bottom: 1px solid var(--vp-c-divider);
}

.ac-term__dots {
    display: inline-flex;
    gap: 0.4rem;
}

.ac-term__dots i {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: #3a3028;
}

.ac-term__dots i:first-child {
    background: #6b4f1e;
}

.ac-term__title {
    font-size: 0.72rem;
    letter-spacing: 0.02em;
    color: var(--vp-c-text-3);
}

.ac-term__body {
    padding: 1.1rem 1.15rem 1.25rem;
    font-size: 0.82rem;
    line-height: 1.5;
}

.ac-prompt {
    color: #f5c050;
    margin-right: 0.5rem;
    font-weight: 600;
}

.ac-cmd {
    color: var(--vp-c-text-1);
}

.ac-arg {
    color: #f5c050;
}

.ac-term__steps {
    margin: 0.85rem 0;
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
}

.ac-step {
    display: grid;
    grid-template-columns: 1.1em 6.5em 1fr auto;
    align-items: baseline;
    column-gap: 0.5rem;
}

.ac-mark {
    color: var(--vp-c-text-3);
}

.ac-label {
    color: var(--vp-c-text-1);
}

.ac-detail {
    color: var(--vp-c-text-3);
}

.ac-check {
    color: #f5c050;
    font-weight: 600;
    opacity: 0;
    animation: ac-check-in 0.4s ease-out var(--d, 0s) forwards;
}

.ac-term__done {
    color: var(--vp-c-text-2);
}

/* Staged reveal — each row settles in sequence, "alive" on load. */
.ac-row {
    opacity: 0;
    animation: ac-row-in 0.45s ease-out var(--d, 0s) forwards;
}

@keyframes ac-row-in {
    from {
        opacity: 0;
        transform: translateY(5px);
    }
    to {
        opacity: 1;
        transform: none;
    }
}

@keyframes ac-check-in {
    0% {
        opacity: 0;
        transform: scale(0.5);
    }
    60% {
        transform: scale(1.18);
    }
    100% {
        opacity: 1;
        transform: scale(1);
    }
}

@media (prefers-reduced-motion: reduce) {
    .ac-row,
    .ac-check {
        animation: none;
        opacity: 1;
    }
}
</style>
