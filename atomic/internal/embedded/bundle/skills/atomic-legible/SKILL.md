---
name: atomic-legible
description: >
  Re-renders a previous answer (or the current one) as a single self-contained HTML page for
  legible reading; the TUI reply stays atomic. Auto-fires on phrases like "show me that legibly",
  "render that", "make that readable", "show that as a page", "pretty version of that", "re-render
  that answer". Also invoked by the atomic output style's presentation ladder (rung 3) when a
  reply crosses the density threshold. Scope is re-presentation of existing content only — it
  never re-derives analysis. Distinct from atomic-visual-options (planning-phase option comparison
  with typed pick codes); this skill presents one finished answer, no decision capture.
---

Render the answer as a page; the terminal reply stays atomic.

<trigger>

Auto-fire on:

- "show me that legibly", "render that"
- "make that readable", "show that as a page"
- "pretty version of that", "re-render that answer"

Also fires when the atomic output style's rung-3 offer is accepted, and directly (no offer) when the request itself is presentation-shaped ("give me a report on…", "write up a comparison of…") or the user already accepted a render earlier this session.

</trigger>

## Workflow

<workflow>

### 1. Identify the target

Default target is the assistant's previous substantive answer. An explicit reference from the user ("the comparison from earlier") overrides the default — find and use that answer instead.

### 2. Write

Write ONE self-contained HTML file to the scratchpad:

```
.claude/.scratchpad/<YYYY-MM-DD>-<slug>/legible.html
```

`<slug>` derives from the content's topic.

Content rule: re-presentation, not re-derivation. Every fact, number, and code block matches exactly what was said. Compressed fragments may expand to full sentences; ASCII trees and tables become real markup. Add nothing new — no fresh analysis, no additional claims.

### 3. Hand off

Print the `file://` path to the file. Do not auto-open the browser. Keep the TUI reply atomic: a short summary plus the path.

### 4. Iterate

If the user asks for changes, overwrite the same file and tell them to refresh the browser. No versioning — the file is throwaway scratch.

</workflow>

## File constraints

Same contract as `atomic-visual-options`:

- Starts with `<!DOCTYPE html>`.
- All CSS inline in a `<style>` block — no external stylesheets.
- No external requests: no CDN links, no web fonts loaded from a remote host, no remote images. Inline SVG and base64 `data:` URIs are allowed when a real image is needed.
- No client-side JavaScript. The page is pure display.
- Honors `prefers-color-scheme` for light and dark mode via a `@media (prefers-color-scheme: dark)` block.
- Code blocks render in monospace `<pre><code>` with ligatures disabled (`font-variant-ligatures: none`) — otherwise sequences like ` --` visually collapse.

## HTML scaffold reference

Adapt the following structure. It is document-shaped — title, section headings, prose, a table, a `<pre>` block — not the option-card layout of `atomic-visual-options`.

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Legible view</title>
<style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
        font-family: system-ui, sans-serif;
        background: #f5f5f5;
        color: #111;
        padding: 2rem;
        max-width: 800px;
        margin: 0 auto;
        line-height: 1.5;
    }
    h1 { font-size: 1.3rem; margin-bottom: 1rem; }
    h2 { font-size: 1.05rem; margin: 1.5rem 0 0.5rem; }
    table { border-collapse: collapse; width: 100%; margin: 0.8rem 0; }
    th, td { border: 1px solid #ddd; padding: 0.4rem 0.6rem; text-align: left; }
    pre, code {
        font-family: ui-monospace, monospace;
        font-variant-ligatures: none;
    }
    pre {
        background: #fff;
        border: 1px solid #ddd;
        border-radius: 6px;
        padding: 0.8rem;
        overflow-x: auto;
    }

    @media (prefers-color-scheme: dark) {
        body  { background: #1a1a1a; color: #e0e0e0; }
        th, td { border-color: #444; }
        pre   { background: #2a2a2a; border-color: #444; }
    }
</style>
</head>
<body>

<h1>Legible view</h1>

<h2>Section heading</h2>
<p>Prose exactly as said, expanded from any compressed fragments.</p>

<table>
    <tr><th>Column A</th><th>Column B</th></tr>
    <tr><td>value</td><td>value</td></tr>
</table>

<pre><code>original code block, unchanged</code></pre>

</body>
</html>
```

<constraints>

## Rules

- Throwaway scratch; never commit. **Why:** the file is a rendering of an already-said answer — the answer itself, not the page, is the durable record.

- Re-presentation only, never re-derivation. **Why:** the terminal reply is the decision surface; a page that adds new analysis would create two sources of truth for what was actually concluded.

- Self-contained and offline: no external CSS, no CDN fonts, no remote images by default, no client-side JavaScript. **Why:** the file must render anywhere a browser exists with zero dependencies and zero setup.

- The TUI reply stays the decision surface — atomic summary plus path, never replaced by "see the page for details." **Why:** the page is an additional surface, not a substitute; a reply that only makes sense after opening a browser breaks atomic style's promise that the terminal reply stands alone.

</constraints>
