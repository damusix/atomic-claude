# Should design docs be authored or rendered as HTML? (issue #114 part 1)


Status: RESOLVED — recommendation adopted 2026-07-03; no new HTML surface; spec change-tree + flows shipped in the same branch (`docs/spec/spec-change-tree-flows.md`).


Issue #114 asks whether `docs/design/<topic>.md` should be authored or rendered as HTML so a
human can inspect it with navigable sections, real diagrams, and side-by-side approach
comparisons. This note answers that question; issue #114's other half — making specs carry a
change tree and implementation flows so blast radius and behavior are visible before code exists
— is a concrete change, not research, and lives in `docs/spec/spec-change-tree-flows.md`.


## Problem


Design docs are read by humans deciding whether to approve a plan. Plain markdown in a diff view
or editor is functional but flat: Mermaid diagrams render as fenced code, not diagrams; there is
no way to see two approaches side by side; a long design doc has no navigation aside from
scrolling. The issue asks whether HTML — authored directly or rendered from markdown — would fix
that.


## Evidence


Two systems already exist that are relevant. Neither is design-doc-specific; both were built for
other purposes and happen to cover the gap.


### `atomic serve` already renders every repo `.md` file as HTML, including `docs/design/*.md`


- `atomic/internal/serve/serve.go:106-120` — `Run` is the process entry point; parses flags and
  starts `RunWithContext`.
- `atomic/internal/serve/serve.go:122-388` — `RunWithContext` builds the route table, binds a
  listener, and blocks until shutdown. It is a general-purpose repo/wiki-realm markdown+code
  viewer, not scoped to any doc kind.
- `atomic/internal/serve/serve.go:210` — `mux.Handle("/page/", NewPageHandlerWithGraph(...))`
  wires every markdown file under the served root, `docs/design/<topic>.md` included, to
  `/page/docs/design/<topic>.md`. No route or template branches on path (confirmed by grep — no
  `docs/design` special-casing anywhere in `internal/serve/`, aside from a test comment noting
  `docs/design/serve.md` renders "with no context," i.e. as a plain page like any other).
- `atomic/internal/serve/serve.go:481-483` — default flags: `-port 4500`, `-host 127.0.0.1`,
  `-open` (best-effort browser launch).
- `atomic/internal/serve/render.go:3-4` (doc comment) and `:290`
  (`goldmark.WithExtensions(extension.GFM)`) — GFM tables, strikethrough, autolinks, tasklists
  render properly, not as raw markdown syntax.
- `atomic/internal/serve/render.go:159-211` — `mermaidCodeRenderer`: a fenced block with language
  `mermaid` emits `<pre class="mermaid">…raw…</pre>` for client-side rendering; every other fenced
  block is chroma-highlighted. Design docs that already use ` ```mermaid ` fences (several do —
  see `docs/design/`) get real rendered diagrams for free.
- `atomic/internal/serve/render.go:245-260` (`RenderMarkdownWithGraph`, the production entry
  point) and `:299-307` (frontmatter stripped before goldmark parses, inside the shared
  `renderMarkdown` helper) — Obsidian-style `[[wikilink]]` resolution and YAML frontmatter
  handling both work on design docs the same as any other page.
- `atomic/internal/serve/context_handler.go:214-237` (`NewPageHandlerWithGraph` doc + wiring) and
  `:262-294` (directory-index resolution and the markdown render path) — navigation (breadcrumb,
  right rail: outbound/inbound links, mini-graph) is generic, not doc-kind-aware.
- `docs/reference/serve.md` documents all of this as the shipped, general-purpose behavior:
  goldmark+chroma rendering, client-side Mermaid via `mermaid.min.js`, and three supported
  in-page link forms (bundle-relative, relative, Obsidian wikilinks).

Net: "navigable sections, real diagrams" is already solved with zero new code. Open
`atomic serve` and browse to `/page/docs/design/<topic>.md`.

**Not covered by `atomic serve`:** side-by-side approach comparison, a design-vs-spec diff/drift
view, or checkpoint-specific callouts. These would be new, design-doc-specific UI — confirmed
absent by grep across `internal/serve/` (no match for "design.*compar", "approach.*compar",
"design-vs-spec", or "checkpoint" outside the one unrelated test comment above).


### `atomic-visual-options` is the existing throwaway-HTML pattern, for the side-by-side case specifically


- `skills/atomic-visual-options/SKILL.md:56-60` — writes ONE self-contained HTML file to
  `.claude/.scratchpad/<YYYY-MM-DD>-<topic>/options.html`.
- `skills/atomic-visual-options/SKILL.md:62-68` — constraints: inline CSS only (no external
  stylesheets), no external requests (no CDN, no remote fonts/images — base64 `data:` URI if a
  raster image is genuinely needed), no client-side JavaScript, `prefers-color-scheme` dark mode.
- `skills/atomic-visual-options/SKILL.md:72-74` — hands off via a printed `file://` path; does not
  auto-open the browser; ends the turn for the user to look and reply with typed codes.
- `skills/atomic-visual-options/SKILL.md:86-92` — the chosen codes and what each meant are
  recorded back into `docs/design/<topic>.md`; the HTML file itself is never committed.

This is a side-by-side comparison renderer already, scoped to *visual* decisions (layout, color,
diagram shape) rather than design-doc approaches generally. It is the scaffold to extend if a
literal approach-comparison view is ever wanted — see Recommendation.


## Recommendation


**No new HTML surface.** `atomic serve` already delivers "design doc as navigable HTML" with zero
new code — Mermaid renders as diagrams, GFM tables/lists render properly, and navigation
(breadcrumb, backlinks, mini-graph) works today because design docs are ordinary markdown files
under the served root. The human-legibility gap that *is* real — a design/spec being hard to scan
for blast radius and behavior before approving — is addressed by the change-tree + flows sections
added to `docs/spec/spec-change-tree-flows.md` in this same branch: a sketch-level file tree with
A/M/D markers and numbered actor → step sequences, readable directly in the markdown body, no
rendering required.

Revisit a bespoke design-review view (side-by-side approach comparison, a design-vs-spec alignment
view) only if real usage shows `atomic serve` + the new spec sections insufficient. If that
happens, `atomic-visual-options`' throwaway-HTML pattern (self-contained file, `file://` handoff,
typed-code decision capture) is the starting scaffold — not a new system from scratch.


## Answers to the three open questions


**1. Author HTML directly vs. render markdown from source?** Render, never author. Fresh-context
subagents (`/subagent-implementation`, `/atomic-plan`'s spec loop) read `docs/design/<topic>.md`
and `docs/spec/<topic>.md` verbatim as markdown — that is the machine-readable contract
(`rules/specs/spec-currency.md`). Authoring HTML directly would fork the source of truth: HTML
source is far more token-expensive for a subagent to read, `git diff` on HTML markup is far less
reviewable than a markdown diff, and two representations of the same content drift. Markdown stays
the single source subagents read; HTML is not a subagent-facing format — it is a read-only view
generated from markdown, exactly like `atomic serve` already does for every other repo document.

**2. Where does the artifact live?** Nowhere new — no committed HTML file, no new render pipeline.
Two existing paths already cover the need: persistent browsing via `atomic serve` (point it at the
repo, open `/page/docs/design/<topic>.md`), and, if a per-review throwaway view is ever wanted
beyond what serve gives for free, the `atomic-visual-options` scratchpad pattern
(`.claude/.scratchpad/<date>-<topic>/options.html`, self-contained, `file://` handoff) is the
scaffold to extend — not a new system.

**3. Does subagent consumption change if the source format changes?** No, because the source
format does not change. Specs and design docs stay plain markdown, read verbatim by fresh-context
subagents. Nothing in `/subagent-implementation`, `/atomic-plan`, or `rules/specs/spec-currency.md`
needs to change as a result of this research.
