import DefaultTheme from 'vitepress/theme'
import { h, nextTick, onMounted, watch } from 'vue'
import { useRoute } from 'vitepress'
import SessionPlayer from './SessionPlayer.vue'
import { SESSION } from './session-script'
import { setupScrollAnimations } from './scroll-animate'
import '@fortawesome/fontawesome-free/css/solid.min.css'
import './custom.css'

export default {
    extends: DefaultTheme,
    enhanceApp() {
        // Mark the document as JS-capable as early as possible so the CSS
        // initial-hidden states (gated on `.ac-js`) apply before first paint.
        if (!import.meta.env.SSR) {
            document.documentElement.classList.add('ac-js')
        }
    },
    Layout: {
        setup() {
            const route = useRoute()
            // Run after the page content has rendered; the rAF re-run covers the
            // case where the DOM isn't fully settled on the first nextTick.
            const run = () =>
                nextTick(() => {
                    setupScrollAnimations()
                    requestAnimationFrame(setupScrollAnimations)
                })
            onMounted(run)
            watch(() => route.path, run)
            return () =>
                h(DefaultTheme.Layout, null, {
                    // Scripted, navigable Atomic Claude session as the hero image.
                    'home-hero-image': () => h(SessionPlayer, { session: SESSION }),
                })
        },
    },
}
