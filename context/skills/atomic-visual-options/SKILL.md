---
name: atomic-visual-options
description: >
  Planning-phase visual comparison aid. Renders 2-4 side-by-side variants per decision
  dimension as a single throwaway, self-contained HTML file and captures the user's pick as
  typed terminal codes (e.g. "A2 B3"). Auto-fires on phrases like "show me a few options",
  "mock up some variants", "let me see this side by side", "visual options for",
  "compare these layouts", "show layout options", "wireframe some choices". Also invoked
  by /atomic-plan just-in-time when a design question is genuinely visual. Scope is strictly
  visual choices — mockup, layout, color, spacing, hierarchy, diagram — not conceptual or
  text decisions, which stay in the terminal.
---

Render the visual in the browser; make the decision in the terminal.

<trigger>

Auto-fire on:

- "show me a few options", "show me some options"
- "mock up some variants", "mock up some options"
- "let me see this side by side"
- "visual options for …", "compare these layouts"
- "wireframe some choices", "show layout options"
- "what would X look like", "can you visualize …"

Also fires when `/atomic-plan` encounters a design question that passes the see-it-over-read-it gate (see below).

</trigger>

## The gate (just-in-time, see-it-over-read-it)

Only render visuals when the question is genuinely visual. The test: *would the user understand it better seeing it than reading it?*

A question that is merely **about** a UI topic is not automatically visual:

- "What should the settings wizard do?" — conceptual, answer in the terminal.
- "Which of these three settings wizard layouts works?" — visual, render in the browser.

Apply this test per question, not per session. A planning conversation might have five conceptual questions and one visual one; only the visual question gets a rendered file. When the answer is "no," stay in the terminal with a plain list or prose.

## Workflow

<workflow>

### 1. Compose

Identify the decision dimensions. Each dimension becomes a **panel** (e.g. Layout, Color scheme, Navigation placement). For each panel:

- Assign a letter starting at A (panel A, panel B, panel C…).
- Generate 2–4 options. Label each with the panel letter + option number: `A1`, `A2`, `A3`. No more than four options per panel — beyond four the comparison stops being useful.
- Assign each option a short, descriptive title (e.g. "A2 — Two-column with sidebar").

### 2. Write

Write ONE self-contained HTML file to the scratchpad:

```
.claude/.scratchpad/<YYYY-MM-DD>-<topic>/options.html
```

The file must satisfy these constraints:

- Starts with `<!DOCTYPE html>`.
- All CSS inline in a `<style>` block — no external stylesheets.
- No external requests: no CDN links, no web fonts loaded from a remote host, no remote images. Default to CSS geometry and SVG for wireframe placeholders so the file renders fully offline. If a real raster image is genuinely required for fidelity, embed it as a base64 `data:` URI — the file must remain self-contained and work without a network connection.
- No client-side JavaScript. The selection is typed in the terminal; the page is pure display.
- Honors `prefers-color-scheme` for light and dark mode via a `@media (prefers-color-scheme: dark)` block.

### 3. Hand off

Print the `file://` path to the file. Do not auto-open the browser. End the turn.

Tell the user: open the file, pick one code per panel (e.g. `A2 B3`), and reply with those codes.

### 4. Read the pick

Next turn, parse the codes the user typed. Accept space- or comma-separated, one code per panel. Map each code back to its option title and description.

### 5. Iterate

If the user wants a panel changed or wants to see a different variant, overwrite the same file and tell them to refresh the browser. No versioning — the file is throwaway scratch.

### 6. Record

**Inside a `/atomic-plan` session:** write the chosen codes and what each option meant into `docs/design/<topic>.md`. Example entry:

```
Chose A2 (two-column layout with sidebar), B3 (high-contrast blue/white palette), C1 (top navigation bar).
```

**Standalone (not inside a plan):** state the chosen codes and what each option meant directly in the terminal so the decision is captured in the conversation history. If a `docs/design/<topic>.md` for the topic already exists, append the decision there too.

The HTML file is disposable and will be cleaned up with the scratchpad. The decision is what persists.

</workflow>

## HTML scaffold reference

Adapt the following structure for each new options file. It is intentionally compact while remaining genuinely functional — two panels, two or three options each, a usage instruction header, inline CSS with light/dark support, and a large readable code badge per option. Replace the SVG placeholders and descriptive text with content appropriate to the specific decision.

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Visual Options</title>
<style>
    /* ── Base ────────────────────────────────────────── */
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
        font-family: system-ui, sans-serif;
        background: #f5f5f5;
        color: #111;
        padding: 2rem;
    }
    h1 { font-size: 1.1rem; margin-bottom: 0.4rem; }
    .instruction {
        font-size: 0.95rem;
        background: #fffbe6;
        border: 1px solid #e0c800;
        border-radius: 6px;
        padding: 0.6rem 1rem;
        margin-bottom: 2rem;
        max-width: 680px;
    }

    /* ── Panel ───────────────────────────────────────── */
    .panel { margin-bottom: 2.5rem; }
    .panel-title {
        font-size: 0.8rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.08em;
        color: #666;
        margin-bottom: 0.8rem;
    }
    .options {
        display: flex;
        flex-wrap: wrap;
        gap: 1rem;
    }

    /* ── Option card ─────────────────────────────────── */
    .card {
        background: #fff;
        border: 1px solid #ddd;
        border-radius: 8px;
        padding: 1rem;
        width: 200px;
        flex-shrink: 0;
    }
    .code-badge {
        display: inline-block;
        font-size: 1.6rem;
        font-weight: 700;
        font-family: ui-monospace, monospace;
        color: #1a56db;
        margin-bottom: 0.4rem;
    }
    .card-title {
        font-size: 0.9rem;
        font-weight: 600;
        margin-bottom: 0.6rem;
    }
    .wireframe {
        border: 1px dashed #bbb;
        border-radius: 4px;
        background: #f9f9f9;
        overflow: hidden;
        margin-top: 0.4rem;
    }

    /* ── Dark mode ───────────────────────────────────── */
    @media (prefers-color-scheme: dark) {
        body     { background: #1a1a1a; color: #e0e0e0; }
        .instruction { background: #2a2a00; border-color: #777700; color: #e0e0e0; }
        .panel-title { color: #999; }
        .card    { background: #2a2a2a; border-color: #444; }
        .code-badge { color: #6ea8fe; }
        .wireframe { background: #222; border-color: #555; }
    }
</style>
</head>
<body>

<h1>Visual Options</h1>
<div class="instruction">
    Open this file in your browser. Reply in the terminal with one code per panel, e.g. <strong>A2 B1</strong>.
</div>

<!-- Panel A: Layout -->
<div class="panel">
    <div class="panel-title">Panel A — Layout</div>
    <div class="options">

        <div class="card">
            <div class="code-badge">A1</div>
            <div class="card-title">Single column</div>
            <div class="wireframe">
                <svg width="168" height="100" xmlns="http://www.w3.org/2000/svg">
                    <rect x="8" y="8" width="152" height="12" rx="2" fill="#ccc"/>
                    <rect x="8" y="26" width="152" height="60" rx="2" fill="#ddd"/>
                </svg>
            </div>
        </div>

        <div class="card">
            <div class="code-badge">A2</div>
            <div class="card-title">Two columns</div>
            <div class="wireframe">
                <svg width="168" height="100" xmlns="http://www.w3.org/2000/svg">
                    <rect x="8" y="8" width="152" height="12" rx="2" fill="#ccc"/>
                    <rect x="8" y="26" width="70" height="60" rx="2" fill="#ddd"/>
                    <rect x="90" y="26" width="70" height="60" rx="2" fill="#ddd"/>
                </svg>
            </div>
        </div>

        <div class="card">
            <div class="code-badge">A3</div>
            <div class="card-title">Sidebar + main</div>
            <div class="wireframe">
                <svg width="168" height="100" xmlns="http://www.w3.org/2000/svg">
                    <rect x="8" y="8" width="152" height="12" rx="2" fill="#ccc"/>
                    <rect x="8" y="26" width="40" height="60" rx="2" fill="#bbb"/>
                    <rect x="56" y="26" width="104" height="60" rx="2" fill="#ddd"/>
                </svg>
            </div>
        </div>

    </div>
</div>

<!-- Panel B: Color scheme -->
<div class="panel">
    <div class="panel-title">Panel B — Color scheme</div>
    <div class="options">

        <div class="card">
            <div class="code-badge">B1</div>
            <div class="card-title">Neutral gray</div>
            <div class="wireframe">
                <svg width="168" height="60" xmlns="http://www.w3.org/2000/svg">
                    <rect width="168" height="60" fill="#f0f0f0"/>
                    <rect x="8" y="8" width="60" height="44" rx="3" fill="#888"/>
                    <rect x="76" y="8" width="84" height="20" rx="3" fill="#bbb"/>
                    <rect x="76" y="34" width="84" height="18" rx="3" fill="#ccc"/>
                </svg>
            </div>
        </div>

        <div class="card">
            <div class="code-badge">B2</div>
            <div class="card-title">Blue / white</div>
            <div class="wireframe">
                <svg width="168" height="60" xmlns="http://www.w3.org/2000/svg">
                    <rect width="168" height="60" fill="#e8f0fe"/>
                    <rect x="8" y="8" width="60" height="44" rx="3" fill="#1a56db"/>
                    <rect x="76" y="8" width="84" height="20" rx="3" fill="#6ea8fe"/>
                    <rect x="76" y="34" width="84" height="18" rx="3" fill="#bfd7ff"/>
                </svg>
            </div>
        </div>

    </div>
</div>

</body>
</html>
```

<constraints>

## Rules

- Visual questions only. Route conceptual and text choices — anything the user would understand as well by reading a list — to the terminal instead. A `<question>` that happens to mention a UI component is not automatically visual. **Why:** the browser is a context switch; it earns its cost only when the content genuinely cannot be conveyed in prose.

- The file is throwaway scratch; never commit it. Record the chosen codes and what each option meant in `docs/design/<topic>.md` instead. **Why:** a committed mockup rots and misleads future readers; the chosen code line (`A2 B3`) plus a one-sentence description is the durable record.

- Self-contained and offline: no external CSS, no CDN fonts, no remote images by default, no client-side JavaScript. **Why:** the file must render anywhere a browser exists with zero dependencies and zero setup; the selection mechanism is the terminal, so client-side interactivity is not needed.

- 2–4 options per panel, maximum. **Why:** beyond four, a side-by-side comparison stops being useful — the user is no longer comparing, they are scanning a catalogue.

</constraints>
