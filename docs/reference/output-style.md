# Output style


`output-styles/atomic.md` defines atomic style. Drop filler, articles, pleasantries, and hedging. Fragments are fine. Short synonyms preferred. Technical terms stay exact. Code blocks and error strings are never compressed. Style applies to Claude's TUI replies, not to source files or docs — those follow the codebase's own conventions.

Three intensity levels:

- **lite** — drop filler and hedging, keep articles and full sentences.
- **full** — drop articles, fragments OK, short synonyms. Default.
- **ultra** — abbreviate prose words (DB/auth/req/res/fn), arrows for causality (X → Y), one word when one word suffices.

Switch by saying "atomic lite", "atomic full", or "atomic ultra". Security warnings and irreversible-action confirmations revert to full prose automatically.
