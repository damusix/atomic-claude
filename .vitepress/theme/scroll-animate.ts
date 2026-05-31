// IntersectionObserver-driven scroll reveals for the home page:
//   • feature cards rise + fade as they enter the viewport
//   • home code blocks reveal their lines in sequence, like the hero console
//
// Idempotent and safe to re-run on client-side route changes — each element is
// marked once via a data attribute, so repeated calls only pick up new nodes.
// All initial-hidden states are gated on the `.ac-js` class (added in
// theme/index.ts), so a no-JS or SSR render shows everything normally.

let observer: IntersectionObserver | null = null

function getObserver(): IntersectionObserver {
    if (!observer) {
        observer = new IntersectionObserver(
            (entries, obs) => {
                for (const entry of entries) {
                    if (entry.isIntersecting) {
                        entry.target.classList.add('ac-inview')
                        obs.unobserve(entry.target)
                    }
                }
            },
            { threshold: 0.15, rootMargin: '0px 0px -8% 0px' },
        )
    }
    return observer
}

function register(el: HTMLElement, revealNow: boolean): void {
    if (el.dataset.acObserved) return
    el.dataset.acObserved = '1'
    el.classList.add('ac-reveal')
    if (revealNow) {
        el.classList.add('ac-inview')
    } else {
        getObserver().observe(el)
    }
}

export function setupScrollAnimations(): void {
    if (typeof window === 'undefined') return

    // When motion is unwanted or the API is missing, reveal immediately and skip
    // observing — the CSS reduced-motion guard zeroes out the animations too.
    const revealNow =
        !('IntersectionObserver' in window) ||
        window.matchMedia('(prefers-reduced-motion: reduce)').matches

    // Feature cards — rise + fade, with a slight per-column cascade when a whole
    // row enters together.
    document.querySelectorAll<HTMLElement>('.VPFeature').forEach((card, i) => {
        card.style.setProperty('--ac-i', String(i % 3))
        register(card, revealNow)
    })

    // Home code blocks — terminal chrome (via CSS) + staggered line reveal. The
    // box stays visible; its lines type in, matching the hero console. Line
    // indexes are (re)assigned every run so they survive a re-render.
    document
        .querySelectorAll<HTMLElement>('.home-extra div[class*="language-"]')
        .forEach((block) => {
            block.classList.add('ac-term-block')
            block
                .querySelectorAll<HTMLElement>('.line')
                .forEach((line, i) => line.style.setProperty('--ac-i', String(i)))
            register(block, revealNow)
        })
}
