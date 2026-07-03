# Design: legible viewing options for responses


GitHub issue #113. The atomic output style optimizes for terseness in the TUI: fragments, ASCII trees, compact tables. For short answers that works. For longer analyses, plans, comparisons, or multi-part findings, a dense terminal reply becomes hard to scan — the reader has one rendering (monospace markdown in the terminal) and no way to ask for a more legible view of the same content.


## Problem


The style currently has one axis: compress harder, or fall back to Auto-Clarity prose. Legibility problems on long replies are about the *medium*, not the compression level. A 60-line comparison matrix is hard to read in a terminal no matter how it is compressed.


## Shape


Two pieces, one contract:


1. **A presentation ladder in `output-styles/atomic.md`.** The existing "Structure over prose" section becomes an explicit three-rung ladder, chosen by content shape:
   - Rung 1 — prose: ≤2 entities, simple answers.
   - Rung 2 — structure: table for comparison, tree for hierarchy, ASCII flow for sequencing (the existing rules, kept).
   - Rung 3 — rendered page: when the reply is dense enough that a terminal rendering stops being scannable, the TUI reply stays an atomic summary and the full legible view goes to a rendered HTML page via the `atomic-legible` skill.

2. **A new skill, `atomic-legible`.** The re-render mechanism. Auto-fires on natural phrases ("show me that legibly", "render that", "make that readable", "show that as a page") and is invoked by the ladder's third rung. Writes ONE self-contained HTML file to the scratchpad and prints its `file://` path. The TUI reply stays atomic; the page is an additional surface, not a replacement.


The skill is the *how*, the ladder rung is the *when* (axiom 5 corollary). Precedent: `atomic-visual-options` already establishes the file mechanics — self-contained HTML in `.claude/.scratchpad/`, no external requests, no JS, `prefers-color-scheme` support, print the path, never commit.


## Rules


### Rung 3 trigger (density threshold)


Escalation is content-shape-driven, with concrete signals so the rule is inspectable:

- 3+ headed sections in one reply, or
- a table past ~6 columns or ~20 rows, or
- ~50+ lines of structured content (trees, tables, multi-part findings combined).


### Offer-first, produce-on-shape


Rung 3 **offers** by default: the dense TUI reply ends with one line offering the rendered view. The skill produces the page on acceptance or on a direct phrase.

Produce directly (no offer round-trip) only when:

- the request itself is presentation-shaped ("give me a report on…", "write up a comparison of…"), or
- the user already accepted a render earlier in the session.

**Why offer-first:** writing the full HTML alongside every dense reply roughly doubles the output cost of every long answer and accumulates scratch files nobody asked for. The offer is one line; the round-trip is one word.


### Re-render is re-presentation, not re-derivation


"Show me that legibly" reformats what was already said — expands compressed fragments into full sentences, turns ASCII trees into real markup, keeps every fact and number identical. It never re-runs the analysis or adds new findings. The page may be *fuller* than the TUI reply (atomic compression relaxed — the page is a file, and files follow their own voice), but it must not diverge from it.


### File contract (inherited from `atomic-visual-options`)


- ONE file: `.claude/.scratchpad/<YYYY-MM-DD>-<slug>/legible.html`.
- Self-contained: `<!DOCTYPE html>`, all CSS inline, no external requests (no CDN, no web fonts, no remote images), no client-side JavaScript. Inline SVG allowed.
- Light/dark via `@media (prefers-color-scheme: dark)`.
- Print the `file://` path; do not auto-open the browser.
- Throwaway scratch: never committed, cleaned up with the scratchpad. Iteration overwrites the same file.


## Approaches considered


| Approach | Verdict |
|----------|---------|
| Skill (auto-fire on natural phrases) | **Chosen.** "Show me that legibly" is language the user naturally types — axiom 5 says skill. |
| Slash command `/legible` | Rejected as the primary surface: forces the user to remember an invocation for a conversational ask. (Skills are user-invocable by name anyway.) |
| Auto-produce the page on every dense reply | Rejected: doubles output cost per long answer, litters scratch. Offer-first keeps the cost opt-in. |
| Markdown file instead of HTML | Rejected: a `.md` file re-opens in the same medium class (monospace editor buffer); the issue is the medium. HTML in a browser is the legibility jump, and the self-contained-HTML mechanics are already proven by `atomic-visual-options`. |
| Auto-open via `open`/`xdg-open` | Rejected: permission-prompt risk in unattended runs, and `atomic-visual-options` precedent is print-the-path. |


## Out of scope (from the issue)


- File contents / docs voice — the two-voices boundary is unchanged.
- Wiki/serve browser rendering (#51 covers that surface).
- Any change to how subagents respond.
