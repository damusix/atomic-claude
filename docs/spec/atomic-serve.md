# atomic serve — spec


## Goal

Ship `atomic serve` — a local HTTP server, read-only with respect to realm and repo
content, that renders a wiki realm (and a bare repo, and a single member) as a navigable
graph in the browser. Presentation only: every view wraps an engine that already exists
(wiki link parser, wiki staleness, bucket diff, the code-intel realm resolver and query
layer). No new analysis. CGO-free, no JS build step — assets vendored via `go:embed`.

**Bus chat exception.** `POST /api/bus/*` targets the bus daemon's own state domain, not
realm or repo content — it is loopback-only regardless of `--host`. Full contract:
`docs/spec/serve-bus-chat.md`.

Design: `docs/design/atomic-serve.md` — all deliberation settled there. This spec carries
only what gets built.


## Non-goals

- **Cross-repo code edges.** Federation, not merging — a call from member A into member
  B stays unresolved. Serve renders what the per-member graphs contain.
- **Write operations.** No editing the wiki, re-stamping, or re-indexing from the UI.
  Serve observes; mutation stays in `/refresh-wiki`, `atomic code index`, `atomic wiki`.
- **Remote exposure / auth.** localhost bind only. Not an API surface, no auth.
- **A JS toolchain.** No npm, no SPA build, no bundler. Vendored UMD/IIFE JS + html/template.
- **MCP realm awareness.** Unchanged; serve is a separate CLI verb.
- **Changes to `atomic wiki` / `atomic code` subcommands** or the `<wiki-scan>` /
  `<code-index>` / `code.toml` formats. Serve reads them; it does not change them.


## Success criteria

- [ ] **SC1** — `atomic serve [path]` starts an HTTP server bound to `<host>:<port>`
      (`--port`, default 4500; `--host`, default `127.0.0.1`), prints the URL, opens the
      browser when `--open` is set (best-effort; never fatal), and shuts down cleanly on
      SIGINT. `--port 0` picks a free port and prints the chosen one. `--host 0.0.0.0` (or
      `::`) exposes the read-only viewer on the LAN and prints every reachable address.
- [ ] **SC2** — Scope is resolved by `realm.Resolve`: a registered `<wikis>` realm root →
      realm scope; inside a member → member scope; a bare repo with no wiki → repo scope.
      The resolved scope is shown in the UI header. A bare repo with a code index but no
      wiki is servable (code views + its `docs/`, no realm chrome).
- [ ] **SC3** — The React SPA build output (`frontend/dist/`) — a Bun-toolchained React +
      TypeScript workspace, package.json/tsconfig committed, `dist/` gitignored, built by
      `make frontend` and embedded via `go:embed` — is served from memory; no network fetch, no file
      dependency outside the binary, no runtime Bun/Node invocation. Every non-API,
      non-static, non-carried-JS-endpoint GET falls back to the embedded `index.html`; React
      Router resolves the requested path client-side and renders the Obsidian shell — top bar
      (breadcrumb + `md|code` search), left nav, middle content with a `[page | system]`
      toggle, right rail (this-page graph ▸ OUT links ▸ IN links), code-file modal. See the
      *React SPA frontend* section below and `docs/spec/serve-react-frontend.md`.
- [ ] **SC4** — A realm/repo markdown file renders to HTML via goldmark (GFM) with chroma
      syntax highlighting and client-side mermaid for ```mermaid blocks. A `file:line`
      reference opens a chroma-highlighted source view scrolled/anchored to the line.
      Path traversal outside the served root is rejected (404, not a file read).
      In-body Obsidian `[[page]]` / `[[page|alias]]` wikilinks render as in-shell links
      resolved through the page's link-graph edges (same resolution as the rail); broken
      wikilinks render as a visible non-navigable span.
      YAML frontmatter (a leading `---` … `---` block) is parsed via `internal/frontmatter`
      and stripped from the rendered body, so it never renders as a spurious `<hr>` +
      heading. Its key/values are surfaced in the right rail (FE-SC2), not inline.
- [ ] **SC5** — The left nav renders a collapsible tree grouped Realm / Repos / Concerns
      / Knowledge / Buckets / External, built from `wiki.ReadScanMembers` + a disk walk of
      `wiki/concerns`, `wiki/knowledge`, and the bucket registry. Stale and bucket-diff
      badges render inline where the data exists.
- [ ] **SC6** — A new exported `mdlink.ExtractLinks(content string) []Link` returns both
      markdown links `[text](path)` and Obsidian wikilinks `[[page]]` / `[[page|alias]]`
      (fenced code spans excluded, matching the existing fence tracking). Wikilinks
      resolve to a file path; a same-named page in two locations resolves by a documented
      rule (nearest-then-alphabetical) and the ambiguity is surfaced. The realm link graph
      (nodes + edges) is built from this; a page view shows backlinks, outbound links, and
      orphan status.
- [ ] **SC7** — An external-link registry page lists every outbound `http(s)` URL across
      the realm: URL, the source pages that cite it, and a first-seen date (file mtime
      fallback when git is unavailable). Reachable from the nav `External` group.
- [ ] **SC8** — A realm-health front page renders the existing staleness report
      (`wiki.Stale` / `CheckStaleness`: DRIFT / STALE / STALE bucket) plus aggregate
      code-index health (the doctor check-11 realm aggregation: worst severity, naming
      only repos needing action) as badges. No new staleness computation.
- [ ] **SC9** — Federated code search: `/code/search?q=…` resolves realm members via
      `realm.Resolve`, opens each member db with `engine.NewWithDBPath(memberPath,
      res.DBPath(key))`, calls `SearchNodes`, and renders results grouped by `[key]`. A
      member with no db is skipped with a visible "not indexed" note, never aborting the
      others. An `only`/`exclude` query param filters the member set. In repo/member
      scope the search targets the single index.
- [ ] **SC10** — Per-repo Code Explorer (under a repo's Code tab when that member is
      indexed): node detail (signature, file:line, metadata), callers / callees / impact
      rendered from `Subgraph` as clickable edge chips with the edge kind shown
      (`calls / references / writes / contains`), and a files list. SQL repos get a schema
      view: `table`/`view` nodes with their `column` children and constraints, an FK graph
      from `references` edges, and a writers-vs-readers split from `writes` edges. The
      schema view is derived from graph nodes/edges — there is no `atomic code schema` verb.
- [ ] **SC11** — Graph overlay: `cytoscape.min.js` is vendored via `go:embed` for the rail
      mini-graph. The middle-pane graph mode hosts two views behind a nested **Docs |
      Code** control. **Docs** is the whole-realm system view, rendered by cosmos.gl, a
      separately vendored bundle (rendering contract: `docs/spec/cosmos-system-graph.md`).
      `/graph` renders a global realm graph and a local depth-1–2 view from a node. Three
      edge classes — md-link, wikilink, and fingerprint/provenance (dashed) — are drawn
      distinctly. **Code** renders one repo's code-intel symbol graph (nodes + resolved
      `contains`/`calls`/`imports` edges from that repo's `atomic.db`), fetched from
      `GET /code/graph/data[?member=<prefix>]` and sharing the Docs view's cosmos.gl core
      (contract: `docs/spec/code-graph.md`). In realm scope, `GET /code/graph/members`
      backs a member picker listing code members with their indexed state; switching
      members swaps the graph. Single-repo/member scope shows no picker — one graph per
      repo, never merged (no cross-repo edges are drawn; federation, not merging). An
      unindexed member renders the message "index not available — run `atomic code
      index`" instead of a blank pane.
      Graph nodes glow in A-style with theme-aware colors read from CSS custom properties
      (no hard-coded palette). `/graph/data` node objects carry `title`, `description`, and
      `snippet` metadata (from `Graph.Meta` / `extractNodeMeta`); `/code/graph/data` node
      objects carry `label`, `kind`, `file`, `line`, `language` instead. Hovering a node
      shows a floating preview card (type chip, title, description, snippet in Docs; name,
      kind, `file:line` in Code). Clicking a node in the Docs view opens a content modal
      that fetches `/page/<id>`, renders the page over a dimmed graph backdrop, and offers
      an "Open full page →" button; closing the modal returns focus to the graph without
      navigation. The prior behavior — tap on a system-graph node navigates away to the
      page view — is superseded. Clicking a node in the Code view opens the existing
      code-explorer node modal for that symbol, member-aware.
- [ ] **SC12** — Provenance DAG walk: a new frontmatter reader extracts `reflects:` /
      `sources:` from concern and knowledge pages; the concern → knowledge → bucket-file
      chain is walkable; a stamp whose recorded fingerprint differs from the live content
      hash flags the node and draws its edge red. Reuses `wiki` fingerprint resolution; no
      re-stamping.
- [ ] **SC13** — Artifact checklist complete: `serve` registered in
      `cliusage/cliusage.go` (flags `--port`, `--host`, `--open`); `CLAUDE.md` workflow + registry
      mention; `README.md` + `docs/reference/` updated; `/atomic-help` cli topic row +
      tour stage updated; `atomic validate artifacts` passes; `make render` +
      `make -C atomic bundle` produce zero `git diff --exit-code`; signals refreshed.


## Approach

Decided in `docs/design/atomic-serve.md`: one `atomic serve` verb, a presentation-only
leaf package (`internal/serve/`) importing wiki + code-intel and imported by neither;
goldmark + chroma + mermaid render server-side, delivered as HTML-in-JSON under `/api/*`;
a Bun-toolchained React + TypeScript SPA (`frontend/`, contract:
`docs/spec/serve-react-frontend.md`) consumes the API and renders the shell client-side;
the rail mini-graph on Cytoscape.js (`concentric` layout), the system graph on cosmos.gl
(GPU simulation + GPU rendering — `docs/spec/cosmos-system-graph.md`) — both carried into
`frontend/public/` unchanged and mounted from React via `window` contracts (`GraphCore`,
`AtomicGraphUI`, `AtomicCodeExplorer`); all assets embedded via `go:embed` from the
gitignored, build-time `frontend/dist/`; scope resolution shared with `atomic code` via
`realm.Resolve`. The middle-pane graph mode hosts two views behind a nested Docs | Code
control — the whole-realm system graph above, and a per-repo code-intel symbol graph —
sharing one cosmos.gl core (`graph-core.js`) with a docs profile (`system-graph.js`) and a
code profile (`code-graph.js`); contract at `docs/spec/code-graph.md`.


## Checkpoints

File/area references ground in the verified seams from the evidence pass. Agent column is
a dispatch hint, not a hard roster.

These checkpoints (CP1–11, FE1–6) shipped the htmx-fragment shell first; that shell was
replaced wholesale by a React SPA per `docs/spec/serve-react-frontend.md` (see *React SPA
frontend* below). The rows below are the historical build order for the underlying
engines (markdown render, link graph, search, code-intel routes, graph overlay) — those
engines are unchanged; only their UI composition and transport (htmx fragments → `/api/*`
JSON + React) changed, per the newer spec.


| # | Checkpoint | Files/areas | Agent | Verifies |
|---|-----------|-------------|-------|----------|
| 1 | **Server skeleton + scope + shell** — `serve` verb, cliusage entry, `main.go` dispatch; `internal/serve/` leaf pkg; `net/http` localhost server, `--port` (default 4500, `0`=free), `--open` (best-effort), SIGINT graceful shutdown, `/healthz`; scope via `realm.Resolve`; embedded htmx + CSS + html/template three-pane shell | `atomic/cmd/atomic/main.go`, `atomic/internal/cliusage/cliusage.go`, new `atomic/internal/serve/`, `realm.Resolve` (realm/resolver.go:86) | builder | SC1, SC2, SC3 |
| 2 | **Markdown render + file view** — goldmark (GFM) + chroma HTML; `/page/*` renders a realm md file; vendored `mermaid.min.js` inits `.language-mermaid`; `file:line` → chroma-highlighted source anchored to line; path-traversal guard | `internal/serve/` (render, routes), vendored assets dir | builder | SC4 |
| 3 | **Nav tree** — Realm/Repos/Concerns/Knowledge/Buckets/External groups from `wiki.ReadScanMembers` (scan_members.go:17) + disk walk of `wiki/concerns`,`wiki/knowledge` + bucket registry; collapsible htmx tree; inline stale/bucket badges where data present | `internal/serve/`, `wiki.ReadScanMembers`, `wiki.Member` | builder | SC5 |
| 4 | **Link graph + backlinks** — exported `mdlink.ExtractLinks(content) []Link` (md links + `[[wikilink]]`/`[[alias]]`, fence-aware) + wikilink→path resolution (nearest-then-alphabetical, ambiguity surfaced); realm node/edge graph; page view backlinks/outbound/orphan | `atomic/internal/mdlink/mdlink.go` (add ExtractLinks + Link type; reuse fence tracking), `internal/serve/` graph model | builder | SC6 |
| 5 | **External-link registry** — collect every outbound http(s) URL realm-wide → page (URL, source pages, first-seen via git or mtime); nav External group links to it | `internal/serve/` (registry), reuse `mdlink.ExtractLinks` | surgeon | SC7 |
| 6 | **Realm-health front page** — render `wiki.Stale`/`CheckStaleness` (stale.go:52, staleness.go:88) + aggregate code-index health (reuse doctor `checks_code_index.go` realm aggregation) as badges; bucket-diff counts | `internal/serve/`, `wiki.Stale`, `wiki.CheckStaleness`, `doctor/checks_code_index.go` | builder | SC8 |
| 7 | **Federated code search** — `/code/search?q=` over `realm.Resolve` members; `engine.NewWithDBPath(memberPath, res.DBPath(key))` (engine.go:104) + `SearchNodes` (engine.go:451); `[key]`-grouped; cold member skipped+noted; `only`/`exclude` param; single-index in repo/member scope | `internal/serve/`, `engine` query layer, `realm` resolver | builder | SC9 |
| 8 | **Per-repo Code Explorer + SQL schema** — repo Code tab: node detail (`GetNode` engine.go:418), callers/callees/impact (`Subgraph`, engine.go:615-631) as edge-kind chips, files (`GetFiles` engine.go:530); SQL schema view from `table`/`view`/`column` nodes + `references`/`writes` edges (types/types.go:122-157) | `internal/serve/`, `engine` query layer, `types` enums | builder | SC10 |
| 9 | **Graph overlay** — vendor `cytoscape.min.js` via `go:embed` for the rail mini-graph; the middle-pane graph mode hosts the whole-realm system view (cosmos.gl, see `docs/spec/cosmos-system-graph.md`) and, behind a nested Docs\|Code control, a per-repo code-intel symbol graph (see `docs/spec/code-graph.md`); `/graph` global + local depth-1–2; 3 edge classes styled (md-link/wikilink/fingerprint-dashed); code graph is one repo at a time via the Docs\|Code control (realm member picker), never merged | `internal/serve/` (graph routes, JSON for the rail's cytoscape), `internal/serve/codegraph.go`, `internal/serve/code_graph_members.go`, `internal/serve/assets/` (cosmos.gl vendor + `graph-core.js` + `system-graph.js` + `code-graph.js`) | builder | SC11 |
| 10 | **Provenance DAG** — frontmatter reader for `reflects:`/`sources:`; concern→knowledge→bucket-file walk; live-hash vs stamped mismatch → red edge + node flag; reuse `wiki` fingerprint resolution | `internal/serve/`, `wiki/stamp.go` resolution (resolveFingerprint:91), new frontmatter reader | builder | SC12 |
| 11 | **Artifact checklist + docs + parity** — cliusage flags; `CLAUDE.md` registry+workflow; `README.md`; `docs/reference/serve.md` (+ commands table); `/atomic-help` cli row + tour; `atomic validate artifacts`; `make render` + `make -C atomic bundle` clean; signals refresh | `cliusage.go`, `CLAUDE.md`, `README.md`, `docs/reference/`, `templates/commands/atomic-help.md`, `docs/reference/commands.md` | surgeon | SC13 |


## Visual redesign — 2026-06-18


The Obsidian shell from the 2026-06-14 FE rework is restyled and the graph interaction model
is extended. All changes are additive to the existing shell; no engine changes.

### Theme toggle + editorial restyle

A light/dark theme toggle lives in the top bar (sun / moon icon). Before paint, an inline
script reads `localStorage` key `atomic-serve-theme`, falls back to `prefers-color-scheme`,
and sets `data-theme` on `<html>`. Toggling writes the choice back to `localStorage`, calls
`window.SystemGraph.retheme()` to re-push point/link colors on the cosmos.gl system graph, and
calls `.style()` on the live rail Cytoscape instance (`window.__railCy`) — so both graphs
re-theme without a page reload.

Two CSS-variable theme sets are defined in `app.css`: a warm paper light theme and a warm
charcoal dark theme. Typography: Newsreader (serif) for display headings, Inter for UI
text, a monospace stack with `font-variant-ligatures: none; font-feature-settings: "calt" 0`
to prevent programming-font ligature collapse on `--`, `->`, `===` sequences (affects all
`code, pre, kbd, samp` elements). Amber accent (`#f59e0b` family). The `type` property in
the Properties rail slot renders as a colored type-chip rather than plain text.

### A-style glowing graph nodes

Graph nodes use a glow style (A-style): solid background with a colored box-shadow
`rgba` ring. Node and edge colors are read from CSS custom properties (`getComputedStyle`)
at render time so they track the active theme without a graph rebuild.

### Node hover preview

Hovering a node in either the system graph or the rail mini-graph shows a floating preview
card anchored near the pointer. The card contains: a type chip, the node `title`, a short
`description` (first sentence of frontmatter or inferred), and a `snippet` (the opening
prose). These fields come from the `meta` object in the `/graph/data` JSON, populated by
`extractNodeMeta` / `Graph.Meta` in `graph.go`. The card dismisses on pointer-leave.

In the cosmos.gl main graph pane (both the Docs and Code views — shared `graph-core.js`),
hovering a node also highlights that node, its direct neighbors, and the edges between
them (full color/width), dimming everything else via cosmos's native
`highlightedPointIndices`/`highlightedLinkIndices` greyout. Hovering an edge (native
`onLinkMouseOver`/`onLinkMouseOut` link hit-testing) highlights the edge and both endpoint
nodes the same way, and shows a preview card reading `<source> —<kind>→ <target>` anchored
at the pointer, reusing the same preview-card machinery as node hover. Unhovering either
restores the full-color/full-opacity view. Zoom is clamped both directions: the zoom-in
ceiling (`ZOOM_MAX`) is a fixed constant derived from cosmos's own point-size zoom-scaling
curve (see `graph-core.js`'s `ZOOM_MAX` comment for the derivation); the zoom-out floor
(`effectiveZoomMin`) is fit-anchored, not node-size-derived — computed per mount from that
graph's own settled-layout bounding box (`computeFitZoomApprox() * 0.6`), since a fixed
node-size-derived floor sat an order of magnitude below any real fitted view and never
engaged (see `graph-core.js`'s `computeFitZoomApprox`/`onSimulationEnd` comments). The rail
mini-graph (Cytoscape, not cosmos.gl) is unaffected by this paragraph.

### Node-click content modal (system graph)

Clicking a node in the **system graph** opens a content modal over a dimmed graph backdrop.
The modal fetches `/page/<id>`, renders the returned HTML, and presents a primary "Open full
page →" button that navigates into the page view. Close/Esc/scrim-click dismisses the modal
and returns focus to the graph. The modal is themed for both light and dark.

**Superseded:** the prior system-graph behavior — clicking a node navigated away from the
graph to the page view, losing graph context — is replaced by this modal pattern. Clicking a
node in the **rail mini-graph** continues to navigate directly to the page view (unchanged).


## Frontend rework (Obsidian shell) — 2026-06-14


The shipped CP1–11 built the engines and a first set of route handlers, but composed them
as disjointed pages (a dead right pane, `/health` as the landing, `/graph` as a separate
destination, eight inline templates). This rework recomposes the **same engines** into one
cohesive, read-only Obsidian-style shell. No new analysis — wiring only, plus a markdown
grep. Canonical UI picture: design doc § "Frontend interaction model".


### Success criteria


- [ ] **FE-SC1** — Shell: every route renders inside one persistent layout — top bar
      (breadcrumb `realm › member › page` + a single search box with an `md|code` source
      toggle), left nav, middle content pane carrying a `[page | system]` toggle, right
      rail with three stacked slots (this-page graph, OUT links, IN links). The dead
      `context-pane` is gone; no route replaces the whole layout. Default landing is the
      page view of the realm index (or a bare repo's README/overview), not the staleness
      dashboard.
- [ ] **FE-SC2** — Right rail tracks focus: for the focused page the rail shows its local
      link graph (depth 1), its OUT links (`mdlink.ExtractLinks` of that page), and its IN
      links (backlinks). Navigating to a new page updates all three slots to the new focus.
      When the focused page carries a YAML frontmatter block, the rail also shows a
      Properties slot (`#rail-props-content`) listing its key/values in **source order**
      (parsed via `frontmatter.ParseOrdered`); a page with no frontmatter shows no
      Properties slot. List-valued keys (e.g. `sources:`) render as a comma-joined value.
- [ ] **FE-SC3** — Graph mode: the `[page | system]` toggle swaps the middle pane into
      graph mode, which hosts two views behind a nested **Docs | Code** control. **Docs**
      is the whole-realm cosmos.gl graph (reusing the existing graph data); the right rail
      collapses; clicking a node opens the content modal (SC11), not an immediate
      navigation. **Code** is a per-repo code-intel symbol graph (`docs/spec/code-graph.md`)
      fetched from `GET /code/graph/data`; in realm scope a member picker
      (`GET /code/graph/members`) lists code members with their indexed state, and
      switching members swaps the graph, while single-repo/member scope shows no picker;
      clicking a symbol node opens the existing code-explorer node modal, member-aware; an
      unindexed member shows a message naming `atomic code index` instead of an empty
      graph. The selected graph view and member persist in URL state. The standalone
      `/graph` view is reachable only through this toggle, not a separate nav destination.
- [ ] **FE-SC4** — Code modal: clicking a code node, `file:line`, or link-to-a-source-file
      opens a modal over the dimmed page — left = chroma-highlighted source, right =
      code-intel relationships (imports, exports/defs, callers/impact, callees) when the
      member is indexed; rows are clickable jumps; Esc/✕ closes; degrades to source-only
      when no index.
- [ ] **FE-SC5** — Search is a command-palette **dialog**, not an inline dropdown. A top-bar
      trigger (and `⌘K` / `Ctrl K`, or `/` when not typing) opens `#search-modal`; the dialog
      carries the `md|code` source toggle and a debounced live-results list. `md` greps the
      served markdown files (literal text, `file:line` matches); `code` runs the federated
      symbol search. Selecting a result navigates (page for md into `#main-pane`, code modal
      for code). `Enter` or "view all results" opens the dedicated **`/search?q=&src=`** page,
      a full, URL-addressable, shell-wrapped results view with `All | Markdown | Code` tabs.
      The page **streams** via SSE (`/search/stream`): the markdown block first (fast local
      grep), then one event per realm member as its DB query completes — members are searched
      **concurrently** (bounded goroutine pool), so a slow member never blocks the rest — and a
      terminal `end` event clears the loading indicator. The dialog and every other in-flight
      request show a spinner; an empty code index renders a clear "run `atomic code index`"
      note rather than a blank panel.
- [ ] **FE-SC6** — Health is ambient: staleness / code-index signals render as dots/badges
      on nav items and the breadcrumb, not as the front door. The old health dashboard
      survives only as a reachable `/status` page, not the landing.
- [ ] **FE-SC7** — Parity holds: `make render` + `make -C atomic bundle` clean; the
      `/atomic-help` serve row + `docs/reference/serve.md` describe the Obsidian UI; signals
      refreshed.


### Checkpoints


| # | Checkpoint | Files/areas | Agent | Verifies |
|---|-----------|-------------|-------|----------|
| FE1 | **Shell + page-view skeleton** — rewrite `layout.html` to the Obsidian shell (top bar breadcrumb + `md|code` search box [toggle may be inert this CP], left nav, middle content with `[page|system]` toggle, right rail with 3 slots); remove the dead context-pane; breadcrumb from the focused page; default landing = page view of the realm index; demote `/health` to `/status` | `internal/serve/templates/layout.html`, `internal/serve/serve.go`, `internal/serve/assets/app.css`, `internal/serve/health.go` | builder | FE-SC1, FE-SC6 |
| FE2 | **Right-rail compositing** — a rail endpoint (e.g. `/rail?page=`) returning this-page graph (depth-1 `BuildLinkGraph`) + OUT (`ExtractLinks`) + IN (backlinks from `context_handler`); htmx wires content nav → rail refresh | `internal/serve/context_handler.go`, `internal/serve/graph.go`, `internal/serve/render.go` | builder | FE-SC2 |
| FE3 | **Graph mode: Docs + Code views** — `[page\|system]` toggle swaps middle pane into graph mode; a nested Docs\|Code control switches between the whole-realm cosmos.gl graph (node click → content modal per SC11; rail collapses) and the per-repo code-intel symbol graph (node click → code-explorer node modal, member-aware; realm member picker; URL view+member state; not-indexed message). Docs view: `system-graph.js` — see `docs/spec/cosmos-system-graph.md`. Code view: shared `graph-core.js` + `code-graph.js` profile + `codegraph.go`/`code_graph_members.go` (`GET /code/graph/data`, `GET /code/graph/members`) — see `docs/spec/code-graph.md` | `internal/serve/graphoverlay.go`, `internal/serve/codegraph.go`, `internal/serve/code_graph_members.go`, `internal/serve/codeexplorer.go`, `internal/serve/assets/system-graph.js`, `internal/serve/assets/graph-core.js`, `internal/serve/assets/code-graph.js`, `layout.html`, `app.css` | builder | FE-SC3 |
| FE4 | **Code modal** — code node / `file:line` / source-link opens a modal: chroma source + code-intel relations (imports/exports/callers/callees via `codeexplorer`); clickable jumps; degrade to source-only | `internal/serve/codeexplorer.go`, `internal/serve/render.go`, `layout.html`, `app.css` | builder | FE-SC4 |
| FE5 | **Search dialog + page** — search is a command-palette dialog (`#search-modal`, opened by the top-bar trigger / `⌘K` / `/`) with the `md\|code` toggle + live results; selecting navigates (md→`#main-pane`, code→code modal); `Enter` / "view all" opens the dedicated `/search?q=&src=` page (`search_page.go`, shell-wrapped, `All\|Markdown\|Code` tabs) which composes the `/search/md` + `/code/search` fragments. `md` grep handler `search_md.go`; federated `codesearch.go` | `internal/serve/search_page.go`, `search_md.go`, `codesearch.go`, `layout.html`, `app.css`, `serve.go` | builder | FE-SC5 |
| FE6 | **Parity + docs** — render/bundle clean; `docs/reference/serve.md` + `/atomic-help` row reflect the Obsidian UI; signals refresh; full verify | `docs/reference/serve.md`, `templates/commands/atomic-help.md`, signals | surgeon | FE-SC7 |


## React SPA frontend — 2026-07-17

The htmx-fragment shell built above (FE1–FE6) is superseded wholesale by a Bun-toolchained
React + TypeScript SPA. Full contract, checkpoints, and change log:
`docs/spec/serve-react-frontend.md`; design deliberation: `docs/design/serve-react-frontend.md`.

Current shape: the Go server exposes a JSON API under `/api/*` (page, file, rail, nav,
search, code-intel, status, external, plans[?member=], plans/page, plans/members) plus a handful of
carried, unreshaped endpoints (`/graph/data`, `/code/graph/data`, `/code/graph/members`,
`/events`, `/healthz`). Every
other GET falls back to the embedded `index.html`; React Router resolves the request
client-side. `templates/layout.html`, the htmx vendor bundle, the OOB fragment handlers,
and every pre-cutover HTML-fragment code path are deleted — not left dead alongside the
new code. The cosmos.gl graph engine (`graph-core.js`, `system-graph.js`, `code-graph.js`,
vendored `cosmos-graph.js`) and Cytoscape (rail mini-graph) carry over unchanged, mounted
from React via `window` contracts (`GraphCore`, `AtomicGraphUI`, `AtomicCodeExplorer`)
instead of htmx `hx-*` attributes.

**Live-reload correction:** the graph pane is *not* patched in place on a live-reload
push, in either the pre-cutover htmx shell or the current React SPA. A subscribed tab's
nav tree and the currently displayed page/rail refresh on the next `/events` push; the
graph pane (Docs or Code view) reflects a realm change only on its *next* `/graph/data` or
`/code/graph/data` fetch — re-entering the view, reloading, or the cosmos engine's own
fingerprint-keyed layout cache invalidating on a subsequent load.
`docs/spec/serve-live-reload.md`'s CP5 (in-place Cytoscape id-diff/patch for the graph pane) was
built against the pre-cosmos client and had no surviving attachment point once the
cosmos.gl engine swap replaced it; it was struck and superseded, not completed. Tracked by
follow-up `cosmos-graph-live-reload-reconcile`.

## Open questions

None.


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Vendored JS (~4.6 MB: mermaid 3.31, cosmos-graph.js 0.82, cytoscape 0.44) inflates the `atomic` binary | High (certain) | Accepted per design (graph is the point). Embed via `go:embed`; consider gzip-at-rest + serve decompressed only if binary size becomes a complaint. Documented, not silent. |
| Vendor script order wrong (`system-graph.js`/`code-graph.js` reference the global `Cosmos` that `cosmos-graph.js` exports, and both profiles depend on the shared `graph-core.js` mounting before them) → the mount throws a `ReferenceError` and the graph pane renders blank | Medium | `layout.html` loads `cytoscape.min.js` (rail), then `cosmos-graph.js`, then `graph-core.js`, then `system-graph.js`, then `code-graph.js`, in that order. `TestShellLoadsGraphScriptsInOrder` asserts the scripts are present and the removed ELK/cola artifacts are gone; it does not assert relative ordering, so verify the sequence manually on any script-tag reshuffle. The committed headless-Chromium gate harness (`scripts/graph-gates.mjs`; contract: `docs/spec/code-graph.md` SC3) runs mount/settle/drag/cache/hover gates against both the Docs and Code views and catches a load-order regression at the mount gate. |
| `mdlink.ExtractLinks` diverges from `Linkify`'s fence handling → links matched inside code spans | Medium | CP4 reuses the existing fence-tracking internals rather than a fresh regex; test with fenced/inline-code fixtures. |
| Run scope is large (11 checkpoints) — partial completion | High | Commit-per-green: each checkpoint lands committed and independently valuable. Foundation (CP1–6) is usable without the code/graph layers. Report remaining checkpoints honestly. |
| Path-traversal / arbitrary file read via `/page/*` or `file:line` route | Medium | CP1/CP2: every served path is resolved against the scope root and rejected (404) if it escapes; never `os.ReadFile` an unvalidated request path. localhost bind limits blast radius. |
| `/atomic-help` hard rule missed — `serve` not discoverable | Medium | CP11 Verifies requires the cli topic row + tour; `CLAUDE.local.md` MISSING-scan catches it. |
| Release-please type mislabel hides the feature from the changelog | Medium | Ship as `feat:` — new user-visible verb, no breaking change. |


## Change log

### 2026-08-23 — `frontend/dist/` is gitignored build output

**What changed:** SC3 and the approach summary no longer describe `frontend/dist/` as committed. It is built by `make frontend` (a prerequisite of `make build|test|vet`) or `go generate ./...`, gitignored, and embedded at build time — the same arrangement the artifact bundle moved to on 2026-08-16. The CI drift gate and the pre-commit rebuild stage are gone with it.

**Why:** a committed `dist/` produced a multi-thousand-line diff on every frontend change and existed only to spare contributors a Bun install; with the bundle already gitignored and CI and goreleaser already running `go generate ./...` with Bun present, the committed copy was the only generated output still in git.

**Superseded:** "`dist/` committed and embedded via `go:embed`" in SC3; "committed `frontend/dist/` build" in the approach summary.

### 2026-08-20 — `/api/*` enumeration gains the Plans routes

**What changed:** The "React SPA frontend" section's `/api/*` enumeration adds `plans[?member=]` and `plans/page` alongside the existing page/file/rail/nav/search/code-intel/status/external routes.

**Why:** `docs/spec/serve-plans-page.md` adds `GET /api/plans[?member=]` and `GET /api/plans/page` — the read-only Plans aggregator surface — to this server; the enumeration here is the current-shape reference other specs and agents read.

### 2026-08-08 — Read-only contract narrowed for bus chat

**What changed:** The Goal section's scope statement changes from "a local, read-only
HTTP server" to "a local HTTP server, read-only with respect to realm and repo content."
A new "Bus chat exception" clause names `POST /api/bus/*` as the one write surface: it
targets the bus daemon's own state domain, not realm or repo content, and stays
loopback-only regardless of `--host`. Full contract: `docs/spec/serve-bus-chat.md`.

**Why:** `docs/spec/serve-bus-chat.md` adds serve's first POST endpoints (`join`, `send`,
`say`, `halt`, `resume`, `leave`) to operate `atomic bus` rooms from the UI. The
unqualified "read-only" claim in this spec's Goal no longer described serve's full
behavior.

**Superseded:** the Goal section's unqualified "a local, read-only HTTP server" claim.

### 2026-07-17 — React SPA replaces the htmx-fragment shell

**What changed:** SC3 and the Approach paragraph now describe the served UI as a
Bun-toolchained React + TypeScript SPA (`frontend/`, `docs/spec/serve-react-frontend.md`)
whose committed `dist/` is embedded via `go:embed`, rather than an htmx-fragment shell
rendered from `templates/layout.html`. The Go server now exposes JSON under `/api/*`
(page, file, rail, nav, search, code-intel, status, external) plus the carried,
unreshaped endpoints (`/graph/data`, `/code/graph/data`, `/code/graph/members`,
`/events`, `/healthz`); every other GET falls back to the embedded `index.html` and React
Router resolves the path client-side. `templates/layout.html`, the htmx vendor bundle, the
OOB fragment handlers, and every pre-cutover HTML-fragment code path are deleted. The
cosmos.gl graph engine and Cytoscape rail mini-graph carry over unchanged, now mounted
from React via `window` contracts instead of htmx `hx-*` attributes. A new
"React SPA frontend" body section points at the full contract; a note above the
Checkpoints table frames CP1–11/FE1–6 as the historical htmx-era build order for engines
that are otherwise unchanged. Also corrects an overstatement in this spec's live-reload
description: the graph pane is not, and never was, patched in place on a live-reload push
— `docs/spec/serve-live-reload.md`'s CP5 (in-place Cytoscape diff/patch) was struck and
superseded at the cosmos.gl engine merge, not completed; the graph pane reflects a realm
change only on its next `/graph/data`/`/code/graph/data` fetch.

**Why:** the htmx shell was replaced end-to-end by `docs/spec/serve-react-frontend.md`'s
additive-then-cutover migration; this spec's body described a UI that no longer exists,
which a fresh-context subagent would build against verbatim.

**Superseded:** SC3's htmx/`html/template`-embedded-shell description and its "root route
renders the persistent shell" claim; the Approach paragraph's "htmx UI" clause; the
Checkpoints section's implicit claim that CP1–11/FE1–6 describe the current UI transport
(they now describe historical build order only, per the added note); any prior implication
elsewhere in this spec's body that the graph pane live-reloads in place.

### 2026-07-08 — Graph pane: node/edge hover highlighting, zoom clamp, smaller node sizes, brighter edges

**What changed:** Six UX changes to the shared cosmos.gl graph layer (`graph-core.js` +
the `system-graph.js`/`code-graph.js` profiles + `app.css`), inherited by both the Docs and
Code graph views. (1) Node size range: `MIN_POINT_SIZE`/`MAX_POINT_SIZE` 13-24 → 8-14px.
(2)/(3) Zoom is clamped both directions via the `onZoom` handler (cosmos has no native
scaleExtent config). The zoom-in ceiling (`ZOOM_MAX=500`) is a fixed constant derived from
`calculatePointSize()`'s own zoom-scaling curve (a literal "80-100px apparent" reading is
not reachable under this engine's screen-space-constant sizing mode; ZOOM_MAX is the closest
node-size-derived analog, and was confirmed working empirically). The zoom-out floor
(`effectiveZoomMin`) is fit-anchored, not node-size-derived: a fixed constant derived the
same way as ZOOM_MAX (`MAX_POINT_SIZE` / typical settled edge length) was tried first and
found empirically wrong by the orchestrator's browser gate — it sat roughly an order of
magnitude below any real fitted view for this repo's docs realm and never engaged, so
wheel-out collapsed the graph toward a speck before it caught. `effectiveZoomMin` is instead
computed once per mount from that graph's own just-settled layout
(`computeFitZoomApprox() * 0.6`, `graph-core.js`), so it scales with whatever dataset is
actually mounted. (4)
Edge visibility: `--edge`/`--edge-strong` brightened in both themes (light: `#cabfae`/`#b1a48f`
→ `#9c8f74`/`#7d6f52`; dark: `#4a4330`/`#6a5f43` → `#6b5f41`/`#8f8058`), plus modest default
link-width bumps in both profiles — the code view's `contains` tier is explicitly unchanged
(stays the faintest tier). (5) Hovering a node highlights it, its direct neighbors, and the
edges between them via cosmos's native `highlightedPointIndices`/`highlightedLinkIndices`
greyout (adjacency built once per data load, no per-hover graph walk); everything else dims.
(6) Hovering an edge uses cosmos's native link hit-testing (`onLinkMouseOver`/`onLinkMouseOut`
— previously unregistered, so link hovering was inert) to highlight both endpoints and show
an `<source> —<kind>→ <target>` preview card, reusing the existing preview-card machinery.

**Why:** UX polish request (`graph-interactions` brief) — nodes read as oversized, edges as
nearly invisible, and hover offered no way to trace a node's or edge's connections.

### 2026-07-08 — Code modal: impact-radius node hydration, Back-stack via htmx events, node-view source sync

**What changed:** Three code-modal/graph-engine fixes, reproduced via a Playwright probe
(`tmp/probe-modal.mjs`).
(1) `graph.GetImpactRadius`'s container path fetches child nodes via `GetNodesByIds` but
never added them to the returned `Subgraph.Nodes` — only the per-child `impactBFS`
sub-traversal's own neighbors were hydrated — so an impact radius rendered on a container
(file/class/struct/…) showed raw node-ID fallbacks (`renderSubgraph`) for the container's
own children. `GetImpactRadius` (container and non-container paths) and the symmetric
`GetCallers`/`GetCallees` now also hydrate their own start node into the returned
`Subgraph`, so every edge endpoint resolves (`atomic/internal/codeintel/graph/graph.go`).
(2) The Back-stack forward-push in `layout.html`'s FE4 code-modal script was a
`document.addEventListener('click', …)` walking up to an `A[hx-get]` inside
`#code-modal-intel` — htmx 4's own delegated click handling consumes the click first, so
this listener never fired and the Back button never appeared after a drill-down. The push
now happens in the existing `htmx:before:request` handler (reads the request URL off
`ctx.request.action`); the dead click listener is removed.
(3) A `/code/node` view swapped into the intel pane (edge-chip or file-defines click)
updated the intel pane only — the modal's source pane and title stayed on whatever was
shown before. `renderNodeDetail` (codeexplorer.go) now stamps `data-file`/`data-line`/
`data-name` on its root element (member-aware, reusing `joinMemberPath`); a new
`htmx:after:swap` handler on `#code-modal-intel` reads those attrs and reloads
`#code-modal-source`, scrolls to the line, and updates `#code-modal-title` — list views
(callers/callees/impact chips, file-defines) carry no such attrs and are left untouched.

**Why:** Three user-reported bugs in the shipped code-graph feature (PR #123).

**Correction:** the 2026-06-15 "Code modal intel pane has a Back button" entry's
forward-push mechanism (a document-level `click` listener) never actually fired under
htmx 4 — verified empirically with the Playwright probe. The Back button existed in the
DOM but never populated its stack via a real drill-down.

### 2026-07-08 — Code graph view added to the graph pane

**What changed:** The middle-pane graph mode now hosts two views behind a nested Docs |
Code control, not the system graph alone. Docs is the existing whole-realm cosmos.gl
graph (unchanged); Code is a new per-repo code-intel symbol graph rendered by the same
cosmos.gl engine, split into a shared core (`graph-core.js`, view-agnostic mount/motion/
cache/legend/label lifecycle) plus two thin profiles — `system-graph.js` (docs, now
slimmed to the docs-specific data adapter and shell glue) and `code-graph.js` (code,
new). The server gained `GET /code/graph/data[?member=<prefix>]` (the resolved member's
full symbol graph as flat JSON — `id`/`label`/`kind`/`file`/`line`/`language` nodes,
`source`/`target`/`kind` edges — plus a content-derived `fingerprint`; an unresolved
`?member=` or unopenable index is a non-200 JSON error, never a silent local-index
fallback) and `GET /code/graph/members` (the scope's code members with each one's
indexed state, backing the realm member picker). In realm scope a member picker swaps
which repo's graph is shown — one graph per repo, never merged, matching the code-intel
engine's per-repo isolation; single-repo/member scope shows no picker. The graph view and
selected member persist in URL state (`view=`, `member=`), so a shared link reopens the
same graph. Clicking a symbol node opens the existing code-explorer node modal,
member-aware, instead of the Docs view's page-content modal. An unindexed member renders
"index not available — run `atomic code index`" instead of a blank pane. The layout cache
namespaces code-view entries `code:<member>:<fingerprint>` so they never collide with the
docs profile's own cache entries in the same IndexedDB store. SC11, FE-SC3, the FE3 and
checkpoint-9 rows, the Approach paragraph, and the vendor-script-order risk row are
rewritten to describe this current shape; none of them claim the system graph is the only
graph view, or that the rail's Cytoscape/cola powers anything beyond the rail mini-graph.
Full feature contract (endpoint shapes, styling, layout cache, drag-physics
regression-testing, the committed `scripts/graph-gates.mjs` Playwright gate harness that
verifies both views): `docs/spec/code-graph.md`.

**Why:** The code-intel engine already computes a per-repo symbol graph
(`atomic.db`/`engine`); the graph pane already had a proven cosmos.gl render/motion/cache
stack for the docs system graph. Splitting that stack into a shared core plus per-view
profiles let the code graph reuse the hand-tuned physics, cache, and interaction grammar
without re-deriving them, while keeping the two graphs — and their independently
resolvable data sources — visually and behaviorally distinct behind one toggle.

**Superseded:** SC11's "Code edges are per-member sub-graphs entered from a repo node"
clause (there was no such entry point; the mechanism is the Docs | Code control described
above) and its implicit single-view graph pane; FE-SC3's "the whole-realm cosmos.gl graph"
as the sole content of graph mode, and its "clicking a node returns to page view" claim
(superseded earlier, 2026-06-18, by the content-modal behavior — corrected here while the
clause was already being rewritten); the FE3 and checkpoint-9 rows' single-view Files/areas
scope and "code sub-graph entered via repo node" wording; the vendor-script-order risk
row's two-script load-order claim (now five scripts, `graph-core.js` and `code-graph.js`
added).

### 2026-07-04 — System graph: cosmos.gl replaces Cytoscape+cola

**What changed:** The Approach paragraph, FE-SC3, and the FE3 checkpoint row now describe the
system graph (the `[page|system]` toggle's whole-realm view) as rendered by cosmos.gl (GPU
simulation + GPU rendering) rather than Cytoscape. The rail mini-graph is unaffected — it stays
on Cytoscape.js with the `concentric` layout, unmentioned by this change. FE3's Files/areas
column now names `internal/serve/assets/system-graph.js`, the new client asset that owns the
cosmos.gl mount lifecycle, data adapter, motion policy, styling parity, and label overlay. The
"Visual redesign" section's theme-toggle paragraph is corrected to match: the toggle calls
`window.SystemGraph.retheme()` for the cosmos.gl system graph, not `.style()` on a
`window.__systemCy` Cytoscape instance (which no longer exists); the rail's
`window.__railCy.style()` call is unchanged. SC11 and the CP9 checkpoint row are corrected the
same way: SC11 no longer claims the system view vendors/loads `elk.bundled.js` +
`cytoscape-elk.min.js` (neither file exists in `assets/vendor/`) or renders via Cytoscape — it
now vendors `cytoscape.min.js` for the rail only and points the system-view rendering contract
at `docs/spec/cosmos-system-graph.md`; CP9's row drops the same false vendor/load-order
instruction and its Files/areas column adds `internal/serve/assets/` (cosmos.gl vendor +
`system-graph.js`). The Risks table's vendored-JS footprint row is re-accounted against the
actual current `assets/vendor/` contents (mermaid 3.31 MB, cosmos-graph.js 0.82 MB, cytoscape
0.44 MB — elk and cola are gone), and its Cytoscape+ELK load-order row is replaced with the
current order-sensitive risk: `system-graph.js` depends on the global `Cosmos` that
`cosmos-graph.js` exports, so the vendor `<script>` tags must load `cosmos-graph.js` before
`system-graph.js`.

**Why:** The full engine-swap contract lives in `docs/spec/cosmos-system-graph.md`
(cosmos.gl replaces Cytoscape canvas 2D + one-shot cola layout for the system-graph view only,
for continuous GPU physics and headroom at scale); this amendment points the serve spec's
system-view description at that contract instead of leaving stale Cytoscape wording in place.
The spec-currency rule applies to the whole body, not only the sections the cosmos-system-graph
checkpoint named — SC11, CP9, and the Risks table were flagged in review as additional stale
claims left over from an earlier, undocumented ELK-to-cola engine swap that predates this
migration; fixing them here keeps the body internally consistent rather than leaving three more
false claims for the next reader.

**Superseded:** the Approach paragraph's "Cytoscape.js + ELK graph" description of the graph
stack (as applied to the system view — the rail's Cytoscape usage is current and unchanged);
FE-SC3's "whole-realm Cytoscape/ELK graph" wording; FE3's `graphoverlay.go`-only file scope for
the client-side system-graph mount; the theme-toggle paragraph's `window.__systemCy` Cytoscape
instance reference; SC11's `cytoscape.min.js`+`elk.bundled.js`+`cytoscape-elk.min.js` vendor/
load-order claim for the system view; CP9's identical vendor/load-order instruction; the Risks
table's "mermaid 3.24, elk 1.57, cytoscape 0.43" footprint accounting and its "Cytoscape+ELK
load order wrong" risk row.

### 2026-06-23 — `--host` flag for LAN exposure

**What changed:** Documented the `--host` flag (default `127.0.0.1`). `--host 0.0.0.0` (or
`::`) binds all interfaces, exposing the read-only viewer on the LAN; the startup banner then
prints every reachable non-loopback address alongside the loopback URL. SC1 and the SC13
cliusage flag list updated to current truth.

**Why:** The flag shipped in `serve.go` but was absent from the spec body, from
`cliusage.go` (so `atomic --help` and `atomic validate` did not advertise it), and from the
reference docs. Surfaced by the 2026-06-23 wiki / MCP / code-intel doc-accuracy audit.

### 2026-06-19 — htmx upgraded 2.0.10 → 4.0.0-beta4 (native, no compat shim)

**What changed:** Vendored htmx bumped to the v4 beta, migrated natively (no `htmx-2-compat`
shim). The delta is small because serve's usage is inheritance-safe (every triggering element
repeats its own hx-attributes; no `hx-boost`, no parent-only `hx-target`/`hx-swap` a child
depends on). Event listeners moved to v4 colon-format reading `detail.ctx`, attached to
`document` (v4 dispatches on `document` when the source element was detached by the swap, so
`document.body` listeners miss those): `htmx:afterSettle`→`htmx:after:swap` (current-page
tracking via `ctx.target`/`ctx.request.action`), `htmx:oobAfterSwap`→`htmx:after:settle` (rail
mini-graph re-scan — v4 merged OOB into the unified swap/settle events),
`htmx:beforeRequest`→`htmx:before:request` (code-modal-intel spinner). Added `Vary: HX-Request`
to all responses via a single middleware in `serve.go`. `htmx.onLoad` and
`htmx.ajax({target,swap,headers})` are unchanged in v4. Verified in a headless browser on
4.0.0-beta4: page/nav load, current-page tracking, rail mount+hover+click, system-graph modal
open/close, ⌘K search, and Back/Forward shell-restore all pass.

**Why:** dependency currency — the latest htmx release (beta channel) requested explicitly;
plus the missing `Vary` header was a content-negotiation correctness gap (htmx best practice).

**Superseded:** the v2 shell set `htmx-config` `historyRestoreAsHxRequest=false` as belt-and-
suspenders for Back/Forward; that key is removed in v4. v4 keeps no localStorage history cache,
so a restore is a server round-trip carrying `HX-History-Restore-Request`, which `fragmentRequest`
already answers with the full shell. The obsolete `TestShell_DisablesHistoryRestoreAsHxRequest`
was dropped (intent covered by `TestHistoryRestore_ReturnsShellNotFragment`).

### 2026-06-18 — Serve visual redesign: themes + graph interactions

**What changed:** Light/dark theme toggle added to the top bar. An inline before-paint script
reads `localStorage` key `atomic-serve-theme` then falls back to OS `prefers-color-scheme`;
explicit choices are persisted. Toggling calls `.style()` on `window.__systemCy` /
`window.__railCy` so Cytoscape re-themes live. Two CSS-variable theme sets (warm paper light /
warm charcoal dark) replace the prior single-theme stylesheet; editorial restyle adds Newsreader
serif headings, Inter UI, ligature-disabled monospace, amber accent, and type-chip rendering for
the `type` property. Graph nodes adopt A-style glow (solid + colored box-shadow ring); node/edge
colors are read from `getComputedStyle` at render time so they track the theme automatically.
`/graph/data` node objects are enriched with `title`, `description`, and `snippet` fields
(from `extractNodeMeta` / `Graph.Meta` in `graph.go`). Hovering a node shows a floating preview
card with a type chip, title, description, and snippet. Clicking a node in the system graph
opens a content modal fetching `/page/<id>` over a dimmed backdrop; the modal offers "Open full
page →" and closes on Esc / backdrop click / close button. SC11 body updated to describe
current behavior.

**Why:** The shell was functional but visually flat and interactively brittle — the system graph
navigated away on node tap, destroying the user's place in the graph. The theme toggle,
editorial restyle, and glowing nodes bring visual polish; node hover previews let users orient
before committing to a page; the content modal preserves graph context on node click.

**Superseded:** SC11's implicit single-theme UI is replaced by the two-theme CSS-variable
system. The prior system-graph tap-navigates-away behavior is replaced by the content modal
(node click → modal with "Open full page →" rather than immediate navigation).

### 2026-06-17 — Frontmatter parsed out of the body, surfaced in the right rail

- **Fixed:** YAML frontmatter rendered as garbage in the page body. goldmark has no
  frontmatter syntax, so a leading `---` became a thematic break (`<hr>`) and the
  following `key: value` lines collapsed into a bogus setext `<h2>` (which also
  polluted the heading outline with a junk auto-id). The right rail never showed the
  metadata at all.
- **Added:** `renderMarkdown` now strips the frontmatter block before goldmark sees it,
  reusing `internal/frontmatter.Parse` (body preserved byte-for-byte; malformed/unclosed
  blocks fall through untouched so a real `<hr>` is never eaten). All body-render entry
  points (`RenderMarkdown`, `RenderMarkdownWithLinks`, `RenderMarkdownWithGraph`) inherit
  the strip from this one choke point.
- **Added:** `frontmatter.ParseOrdered(input) ([]KV, body, error)` — a key-order-preserving
  sibling of `Parse` (same yaml.Node walk, same date-as-string coercion guard). The rail
  needs source order; `Parse`'s `map` does not preserve it.
- **Added:** the right-rail compositor (`rail_handler.go`) reads the focused page's
  frontmatter and emits a fourth OOB fragment, `#rail-props-content`, listing key/values
  in source order; list values render comma-joined. A page with no frontmatter emits an
  empty slot (CSS hides it). `layout.html` gains the `#rail-props` slot at the top of
  `#right-rail`; `app.css` styles `.rail-props-list`.
- **Scope note:** using `title:` for the breadcrumb/page title was considered and **not**
  done here — the breadcrumb final segment carries folder-nav semantics, and the title is
  already visible as a Properties row. Left as a possible follow-up.

### 2026-06-16 — In-body wikilinks render as in-shell links

- **Fixed:** Obsidian-style `[[page]]` / `[[page|alias]]` links in a markdown body
  rendered as **literal text** — goldmark has no native wikilink syntax, and the
  render-time link rewriter (`linkRewriteRenderer`) only handled standard markdown
  `[text](url)` links. The right rail still showed the OUT/IN links (it reads the
  link graph), so the body and the rail disagreed: the rail resolved the wikilink,
  the body left it as prose. A new goldmark inline parser + renderer (`wikilink.go`)
  now turns `[[…]]` into a real link. Resolution is **not** recomputed: a resolved
  wikilink reuses the focused page's already-computed graph edges
  (`wikilinkResolverFromGraph` reads `Graph.Outbound`), so the body and the rail
  share the one nearest-then-alphabetical resolution in `graph.go`. Resolved links
  become htmx navigations to `/page/<target>` (shell preserved, matching the
  markdown-link rewriter and the rail); broken links render as a visible
  non-navigable `<span class="wikilink-broken">`; ambiguous links resolve to the
  nearest match with a warning class. The new `RenderMarkdownWithGraph` entry point
  carries the graph into the render; `NewPageHandlerWithGraph` calls it. Wikilinks
  inside inline code spans and fenced blocks stay literal (goldmark consumes those
  as raw text), matching `mdlink.ExtractLinks` fence-awareness. The graphless paths
  (`RenderMarkdown`, `RenderMarkdownWithLinks`) leave `[[…]]` literal — there is no
  realm basename index to resolve against without the graph.

### 2026-06-15 — System graph: drop code-file edges (dangling-target crash)

- **Fixed:** a markdown page linking to a real source file (e.g.
  `signals.md → search.sh`) produced a Cytoscape edge whose target was the source
  file. The system graph is a page-to-page graph — source files are not nodes (no
  `/page/`) — so the edge referenced a nonexistent target and Cytoscape aborted the
  entire `[system]` render (console: `Can not create edge … with nonexistent
  target`). `buildCytoElements` / `buildLocalSubgraph` now skip `Edge.CodeFile`
  links and, defensively, any edge whose target is not a known node. Code files
  still surface in the rail OUT list as `/file/` links. (`graphoverlay.go`.)

### 2026-06-15 — Code modal intel: loading feedback + drills stay in the pane

- **Fixed:** intel drill-downs (and Back) swapped `#code-modal-intel` with no
  loading feedback. A delegated `htmx:beforeRequest` handler now shows a spinner
  in the pane for any request targeting it.
- **Fixed:** the subgraph (`/code/callers|callees|impact`) and node-detail
  (`/code/node`) drill links targeted `#main-pane` — they escaped to the pane
  *behind* the modal. These routes are only reached through the modal, so all five
  drill links now target `#code-modal-intel`; the full defines → callers → node
  chain stays inside the modal (and is recorded by the Back stack).

### 2026-06-15 — Code modal intel pane has a Back button

- **Fixed:** drilling the code modal's intel pane (defines → callers → callees →
  node → …) swapped `#code-modal-intel` in place with no way back to the previous
  view. The intel pane is now wrapped in `#code-modal-intel-pane` with a persistent
  `← Back` button (outside the swap target so it survives). A per-modal JS stack
  records each drill-down URL; Back pops one level and reloads it. The button is
  hidden at the root (the file's defines view). (`layout.html`, `app.css`.)

### 2026-06-15 — Back/Forward no longer destroys the nav shell

- **Fixed:** the browser Back button wiped the shell. On an htmx history cache
  miss, htmx re-requests the pushed URL and replaces `<body>` with the response;
  because the page/file/search handlers return bare `#main-pane` fragments when
  `HX-Request` is set, the restore (which also carries `HX-Request`) got a fragment
  and the shell was destroyed. Fix is twofold: the shell sets
  `htmx.config.historyRestoreAsHxRequest=false` (htmx omits `HX-Request` on
  restore), and the handlers treat any `HX-History-Restore-Request` as a document
  load (`fragmentRequest` helper) and return the full shell regardless — so the
  shell survives even if the client config is overridden. The shell-less
  `/code/search` standalone form dropped `hx-push-url` (it must not push a URL that
  restores to a shell-less fragment; the canonical search surface is `/search`).

### 2026-06-15 — System-graph renders reliably + loading feedback

- **Fixed:** the `[ page | system ]` toggle's system view often showed a blank pane.
  `#system-cy` is created by an `innerHTML` swap, so Cytoscape could initialize
  against a still-zero-size container (graph rendered into a 0×0 canvas), and a
  large realm's `elk` layout takes a few seconds with no feedback. The toggle now
  shows a centered "Laying out graph…" indicator, and on `layoutstop` calls
  `cy.resize()` + `cy.fit()` so the graph is sized to the container and centered;
  a fetch error replaces the indicator with a visible message. (`layout.html`,
  `app.css` — `#main-pane { position: relative }` + `.system-graph-loading`.)

### 2026-06-15 — Code-intel discovers per-member self-indexes (realm scope)

- **Fixed:** in a wiki realm with no `<code-index>` federation, code search and the
  code modal's intel pane found nothing even after `atomic code index` — serve only
  consulted federation dbs (`<realm>/.atomic/<key>.db`) and the realm-root index,
  never a member's own `<member>/.claude/.atomic-index/atomic.db`. New shared
  resolver `discoverCodeMembers` (`code_members.go`) unions federation members with
  self-indexed members read from the wiki scan; `memberForPath` maps a realm-relative
  file path to its owning member (longest-prefix) plus the member-relative remainder.
- **Code search** (`codesearch.go`) now fans out over discovered members (federation
  ∪ self-index) and prefixes each result link with the member's realm-relative path,
  so `/file/<member>/<rel>` resolves through the realm's file route.
- **Code modal** (`codeexplorer.go`) resolves the member from the requested path,
  opens that member's db, and queries it with the member-relative path; node /
  subgraph / file-intel routes accept a `member=` query param threaded onto every
  drill-down link (and `/file/` location), so callers/callees/impact stay within the
  same member's index. Repo/member scope is unchanged (empty prefix, local index).

### 2026-06-15 — In-page links resolved server-side against the realm root

- **Fixed:** page-content markdown links rendered with their **raw** destinations
  (`../concerns/x.md`), so the browser resolved them against the shell URL and did a
  full-page navigation — losing the user's place, and 404-ing when the base URL was
  wrong. Links are now rewritten at render time (`RenderMarkdownWithLinks` +
  `linkRewriteRenderer` in `render.go`; `resolvePageHref` in `graph.go`, the render-time
  sibling of `resolveMarkdownLink`): each relative target is resolved against the realm
  root into a real route — `/page/<rel>` for markdown/folders (htmx-navigated into
  `#main-pane`, so the shell is preserved), `/file/<rel>` for source files (opens the
  code modal). External links get `target="_blank"`; in-page anchors and realm-escaping
  links are left verbatim. Unresolved-but-in-realm targets route through `/page/` so a
  dead link yields the in-shell 404 fragment, not a full-page navigation.

### 2026-06-14 — Search results stream (SSE) + loading feedback

- **Added:** the `/search` page now streams over Server-Sent Events
  (`/search/stream?q=&src=`, `search_stream.go`): a `md` event (fast local grep),
  then one `code` event per realm member, then a terminal `end`. Members are
  searched **concurrently** — `fanOutMembers` runs a bounded goroutine pool, so the
  slowest member no longer blocks the others. The dialog fetch and every `.loading`
  placeholder gained a spinner; the dialog cancels stale fetches via `AbortController`.
- **Fixed:** federated code search rendered an empty `<div>` (no feedback) when a
  realm had no code members; it now renders a clear "run `atomic code index`" note.
  The per-member search logic was extracted to `codeSearchGroups` + `searchMember` +
  `renderMemberGroup`, shared by the synchronous handler and the stream.

### 2026-06-14 — Search becomes a command-palette dialog + dedicated page

- **Superseded:** FE-SC5's inline live-results **dropdown** anchored under a top-bar
  search input, plus the top-bar `md|code` toggle. (The dropdown also shipped with no
  CSS, so results dumped unstyled — it read as "search broken".)
- **Added:** search is now a command-palette **dialog** (`#search-modal`, opened by a
  top-bar trigger, `⌘K`/`Ctrl K`, or `/` when not typing) carrying the `md|code` toggle
  and a debounced live-results list, plus a dedicated **`/search?q=&src=`** page
  (`search_page.go`, mounted in `serve.go`) that composes the existing `/search/md` and
  `/code/search` fragments into a shell-wrapped, URL-addressable results view with
  `All | Markdown | Code` tabs. The search *backends* are unchanged.
- Also in this window (bug-fix commits, same UI): `.claude` is walked so member project
  docs cited by wiki linkify resolve (not broken) and their rail no longer 404s; folder
  URLs serve an index file or a generated listing; nav member links mirror the wiki index
  (indexed→signals, pending→folder) instead of guessing a nonexistent `wiki/repos/<name>.md`;
  link color moved to a site-wide base rule; hidden dotfiles dropped from enumeration.

### 2026-06-14 — Frontend rework to the Obsidian shell

- **Superseded:** SC3's "left nav · center · right context" three-pane shell, and the
  composition implied by SC8 (`/health` as the front page) and SC11 (`/graph` as a
  standalone destination). The reused *engines* (SC4–SC12) are unchanged — only how they
  compose into the UI changed.
- **Added:** FE-SC1–FE-SC7 and checkpoints FE1–FE6 — one persistent Obsidian-style shell
  (top bar breadcrumb + `md|code` search · left nav · middle content with a `page|system`
  toggle · right rail this-page-graph ▸ OUT ▸ IN · code-file modal). Canonical picture in
  the design doc § "Frontend interaction model".
- **Why:** the first build composed the engines as disjointed pages (dead right pane,
  staleness dashboard as landing, graph as a separate page, eight inline templates). The
  author called it "30% there, disjointed." The rework recomposes the same engines, read-
  only, into one navigable graph workspace. Wiring + a markdown grep; no new analysis.


## Implementation log

### Shipped (unreleased) — 2026-06-13

Built across all 11 checkpoints via `/autopilot` (subagent implement→review loop) in an
isolated worktree (`atomic-serve`). New leaf package `internal/serve/` imports wiki +
code-intel; neither imports it back. Commits (chronological, on branch `atomic-serve`):

- `2a58118` — CP1 server skeleton, scope resolution (`realm.Resolve`), embedded three-pane shell
- `1abbf92` — CP2 markdown render (goldmark+chroma+mermaid) + `/file/*` source view + traversal guard
- `c7bc2eb` — CP3 left-nav tree + stale/bucket-diff badges (exports `wiki.BucketDiffReadOnly`, `wiki.ReadBucketEntries`)
- `8cd14ba` — CP4 realm link graph + backlinks (`mdlink.ExtractLinks`, fence-aware)
- `101a0c9` — CP5 external-link registry (git-first first-seen, mtime fallback)
- `56a7d2b` — CP6 realm-health front page (shared `parseStaleLines` for nav + health)
- `77e4b57` — CP7 federated code search (`engine.NewWithDBPath` fan-out, `[key]`-grouped)
- `403d8f2` — CP8 Code Explorer + SQL schema view (`CodeEngine` interface seam)
- `28b9ee3` — CP9 Cytoscape+ELK graph overlay (load-order guarded; shared `shouldSkipDir`)
- `72abe54` — CP10 provenance DAG + drift detection (exports `wiki.FileSHA256`, `wiki.ResolveFingerprint`)
- `b6d2d35` — CP11 discovery surfaces (CLAUDE.md, /atomic-help, README, docs/reference/serve.md) + render/bundle parity
- `b080cf7` — cliusage `--help` golden updated for the `serve` verb (caught in final verify)

**Wiki/engine seams exported for read-only reuse (the only production changes outside `internal/serve/`):**
`wiki.BucketDiffReadOnly`, `wiki.ReadBucketEntries`, `wiki.FileSHA256`, `wiki.ResolveFingerprint` —
thin wrappers over existing unexported funcs so serve hashes/diffs exactly as the CLI does.

**Reviewer findings — every one addressed in-iteration (autopilot rule 2; FOLLOWUPS ledger ended empty):**
- CP3: stale/bucket badges were template-wired but nil in production → wired `computeStaleness` + `BucketDiffReadOnly`.
- CP5: first-seen shipped mtime-only → wired git-first `GitOrMtimeDateFn`.
- CP6: duplicated `wiki.Stale` parser → extracted shared `parseStaleLines`.
- CP9: file walkers ingested `.claude`/`tmp`/`.worktrees` `.md` (found via runtime smoke test) → shared `shouldSkipDir`.
- CP10: drift edges emitted the class but had no red style → added `edge.fingerprint.drift` selector.

**Vendored assets (`go:embed`, ~5.3 MB total, accepted per design):** htmx 2.0.10 (50K), mermaid 11 (3.2M),
cytoscape 3.34 (425K), elk.bundled 0.11 (1.5M), cytoscape-elk 2.3 (3.6K). Load order
cytoscape → elk.bundled → cytoscape-elk is load-bearing and guarded by a byte-order test.

**Verification:** render + bundle parity clean; `go build`/`vet`/`gofmt` clean; `atomic validate`
(worktree binary) 0 FAIL; end-to-end smoke — all routes return 200, `/graph/data` emits valid
Cytoscape JSON, hidden dirs excluded from the walk.

**Known pre-existing failure (NOT this feature):** `internal/hooks` `TestSessionStart_*` read the real
`~/.claude` `<wikis>` block and fail on machines with registered dirty wikis (filed `hooks-tests-read-real-home`);
CI runs against a clean HOME and is unaffected.

**Deferred:** none — all 11 checkpoints shipped. The `code-web-explorer` follow-up (`kind: plan`) is now
subsumed by the Code Explorer mount and can be closed.
