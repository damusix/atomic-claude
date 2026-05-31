import DefaultTheme from 'vitepress/theme'
import { h, nextTick, onMounted, watch } from 'vue'
import { useRoute } from 'vitepress'
import AutopilotDemo from './AutopilotDemo.vue'
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
                    // Replace the hero image with a live terminal demo instead of a static asset.
                    'home-hero-image': () => h(AutopilotDemo),
                })
        },
    },
}
