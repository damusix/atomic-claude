# atomic serve — visual refresh exploration


The current `atomic serve` shell is functional but plain. This is a design exploration of a more
modern, appealing look, before any change lands in `layout.html` / `assets/app.css`.


Three directions, each a self-contained HTML mockup under `mockups/`, rendered against the same
page (the OKF "Caching Patterns" knowledge page) so they are directly comparable. The mockups
restyle the existing shell — top bar, left nav, content pane with `[page|system]` toggle, right
rail (Properties / this-page graph / out / in links) — they do not change the information
architecture.


| # | Direction | File | Feel |
|---|-----------|------|------|
| A | Refined dark | `mockups/a-refined-dark.html` | Polished dark dev-tool (Linear / Vercel-dark) |
| B | Light editorial | `mockups/b-light-editorial.html` | Premium reading-first docs (Stripe / Tailwind docs) |
| C | Modern app | `mockups/c-modern-app.html` | Deep-indigo, gradient accents, command-style search (Raycast / Arc) |


The node-type color identity from the shipped app is preserved across all three: knowledge `#f38ba8`,
repo `#a6e3a1`, concern `#cba6f7`, page `#89b4fa`, external `#6c7086`.


Open any file directly in a browser to interact, or see the rendered screenshots posted on the PR.
These mockups are throwaway exploration aids; once a direction is chosen, the real implementation
lands in `atomic/internal/serve/templates/layout.html` + `assets/app.css`, and this directory can be
removed.
