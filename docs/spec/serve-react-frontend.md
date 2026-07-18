# Serve React frontend


## Goal


Replace `atomic serve`'s htmx fragment-swap UI with a React SPA — Go stays the data and markdown-render backend, the cosmos.gl graph engine and the visual design carry over unchanged — delivered via an additive-then-cutover migration: `/api/*` endpoints land alongside the existing routes, the React app is built screen-by-screen against them, and one final checkpoint flips the default route and deletes the htmx layer.


## Non-goals


- No visual redesign — same look, same light/dark themes, same three-pane layout.
- No new features beyond parity. Graph-pane live-reload reconcile becomes *attachable* (React owns the pane's mount lifecycle) but its implementation stays follow-up `cosmos-graph-live-reload-reconcile`.
- No SSR / Next.js — the server renders markdown; it never renders React.
- No change to the security contract: read-only, localhost-default, path-traversal guards intact.
- No rewrite of the graph engine internals or vendored libraries (`graph-core.js`, `system-graph.js`, `code-graph.js`, vendored `cosmos-graph.js`, Cytoscape, mermaid stay carried, not rebuilt).
- No Windows support work (repo policy).
- No API-usage or version-pinning prescription beyond what the design already decided — React Router is fixed (design D4-A); exact React/router API usage, the state-management library choice, and version pinning are the implementer's call at build time.
- Accepted regressions at cutover (deliberate, per owner triage): plain-HTTP/no-JS readability of `/page/*` ends — content requires the SPA; unmatched non-API GETs return 200 + shell instead of 404 (traversal guards enforce at `/api/*`); a browser tab opened pre-cutover misrenders until manually reloaded; post-release rollback is forward-fix or pinning the prior binary.


## Success criteria


- [ ] Every row of `docs/design/serve-react-frontend.md`'s blast-radius inventory (36 screens/features, client-asset table, endpoint table) is accounted for in the shipped diff: `carry` rows land unchanged, `reshape`/`rebuild` rows are reimplemented in React/JSON, `delete` rows are removed.
- [ ] `atomic/internal/serve/frontend/` is a Bun-toolchained React + TypeScript workspace (Bun is the package manager, bundler, and test runner; layout and component conventions per `frontend/CLAUDE.md`); `make frontend` builds it via `bun`, the committed `dist/` matches a fresh build (`git diff --exit-code`), `bun test` runs the frontend suite, and `go build ./internal/serve/...` succeeds referencing the embedded dist with zero Bun/Node invocation.
- [ ] All reshaped endpoints land under `/api/*`; the carried-JS endpoints (`/graph/data`, `/code/graph/data`, `/code/graph/members`, `/events`, `/healthz`) keep their current paths unchanged.
- [ ] Markdown rendering (goldmark + chroma + wikilink resolution) stays server-side and single-sourced between page body and rail edges — `/api/page/*` and `/api/rail/*` responses resolve links identically.
- [ ] After cutover, every non-API, non-static, non-carried-JS-endpoint GET serves the SPA shell; deep links (`/page/<relpath>`, `/graph?view=&member=`, `/search?q=&src=`) resolve to the equivalent screen client-side.
- [ ] `templates/layout.html`, the htmx vendor bundle, the OOB fragment handlers, and the pre-cutover HTML-fragment code paths are deleted in the cutover checkpoint — not left dead alongside the new code.
- [ ] `scripts/graph-gates.mjs` targets the React shell's selectors; all 5 gates pass against the cut-over build.
- [ ] Every reshaped handler's test coverage moves from HTML-fragment assertions to JSON assertions with no coverage dropped in the move.
- [ ] `docs/reference/serve.md`, `docs/spec/atomic-serve.md`, `docs/wiki/serve.md` describe the React SPA architecture, correcting the graph-live-reload overstatement flagged in the design's tooling table.
- [ ] No new runtime network dependency — the built binary serves the SPA with no outbound fetch at runtime (single-binary property intact).
- [ ] `@ark-ui/react` is the only UI-primitive dependency: the left-nav folder tree (TreeView), modals (Dialog), search tabs (Tabs), connection tooltip (Tooltip), and ⌘K palette (Combobox) all build on it — no second primitive suite in `package.json`.


## Approach


Additive-then-cutover migration to a Bun-toolchained React + TypeScript SPA, committed `dist/` embedded via `go:embed` with a render/bundle-style drift gate, hybrid HTML-in-JSON API under `/api/*`, React Router preserving today's URL scheme, Ark UI as the sole UI-primitive library — see `docs/design/serve-react-frontend.md`.


## Change tree


    atomic/internal/serve/
    ├── frontend/ ........................... A  (Bun + React + TS workspace)
    │   ├── CLAUDE.md ....................... A  (workspace conventions: Bun-only toolchain, domain-scoped layout, component-folder rules)
    │   ├── package.json .................... A
    │   ├── bunfig.toml ..................... A  (bun install/test config; dev API proxy to atomic serve)
    │   ├── tsconfig.json ................... A
    │   ├── index.html ...................... A  (before-paint theme init inline script)
    │   ├── src/ ............................. A  (layouts, pages, components, hooks, utils — domain-scoped, see Outline)
    │   ├── public/ ........................... A  (carried assets, copied here at CP1: app.css [htmx-specific selectors pruned at cutover], graph-core.js, system-graph.js, code-graph.js, vendor/cosmos-graph.js, vendor/cytoscape.min.js, vendor/mermaid.min.js, logo.png, fonts — NOT htmx.min.js, which is not carried)
    │   └── dist/ ............................. A  (committed build output)
    ├── frontend_dist.go ..................... A  (go:embed source for frontend/dist)
    ├── context_handler.go ................... M  (adds /api/page JSON; pre-cutover HTML path removed at cutover)
    ├── rail_handler.go ....................... M  (adds /api/rail JSON; OOB fragments removed at cutover)
    ├── nav.go ................................ M  (adds /api/nav JSON tree + badges)
    ├── search_md.go .......................... M  (adds /api/search/md JSON)
    ├── search_stream.go ...................... M  (SSE payloads become JSON; gains normalizeSearchSrc, relocated from search_page.go at cutover)
    ├── codesearch.go ......................... M  (adds /api/code/search JSON)
    ├── codeexplorer.go ....................... M  (adds /api/code/{node,callers,callees,impact,files,schema,file} JSON)
    ├── health.go .............................. M  (adds /api/status JSON — NewHealthHandler; /healthz unchanged)
    ├── external.go ............................ M  (adds /api/external JSON)
    ├── render.go .............................. M  (template split: HTML-in-JSON; renderer emits plain hrefs)
    ├── wikilink.go ............................ M  (renderer emits plain hrefs, not hx-get)
    ├── serve.go ................................ M  (/api/* mounts; SPA fallback flip + embed source swap at cutover)
    ├── templates/layout.html .................. D  (removed at cutover)
    ├── search_page.go ........................ D  (removed at cutover — NewSearchPageHandler, renderSearchPageFragment, and searchBreadcrumbOOB die with it, superseded by the SPA fallback + React SearchRoute; normalizeSearchSrc relocates to search_stream.go first — the file's logic survives, only its handler shell dies)
    └── assets/ ................................ D  (removed at cutover — every surviving asset was already carried into frontend/public/ at CP1; htmx.min.js is not carried, it dies with the directory)
    scripts/graph-gates.mjs .................... M  (selectors retargeted to the React shell)
    atomic/Makefile ............................ M  (new `frontend` target)
    .githooks/pre-commit ........................ M  (new render-parity stage for frontend/dist)
    .github/workflows/ci.yml .................... M  (next-targeting push/PR triggers; frontend drift gate + bun test)
    docs/reference/serve.md ..................... M  (React SPA architecture)
    docs/spec/atomic-serve.md ................... M  (React SPA architecture; corrects live-reload overstatement)
    docs/wiki/serve.md ........................... M  (signals refresh, out of band from implementer checkpoints)


## Outline


    atomic/internal/serve/frontend/src/
      App — entry: router + Shell mount
      layouts/
        Shell — three-pane app shell: top bar slot, nav pane, content pane, rail pane
      pages/
        Page — page view + rail composition for /page/*
        Graph — graph mode: docs|code switcher, carried-engine mount
        Search — /search?q=&src= page: All|Markdown|Code tabs
        Status — /status dashboard
        External — /external registry view
      components/
        ui/ — generic app-agnostic primitives, barrel-exported from ui/index.ts
        nav/ — top bar (breadcrumb, search trigger, theme toggle, connection indicator) + left nav tree with realm groups and stale/drift badges
        rail/ — Properties, this-page mini-graph, OUT/IN panels
          railCytoscapeStyle — atomicCyStyle Cytoscape stylesheet factory (rail mini-graph only), built from typeColors
        search/ — command palette dialog: md|code toggle, debounced results
        code-modal/ — source pane, intel pane, back-stack
        schema/ — code schema tables/views/FK graph
      hooks/
        useLiveReload — /events SSE → emits realm.changed on the observer bus; reconcile logic (nav-always, page-conditional) subscribes there; scroll preservation
        useTheme — toggle + retheme cascade (mermaid, cosmos, rail Cytoscape) via theme.changed observer event
      utils/
        api — the shared FetchEngine instance (@logosdx/fetch: baseUrl /api, retry, dedupePolicy) + its createFetchContext pair; error-envelope handling; every component fetch goes through it via attempt() tuples
        events — typed ObserverEngine (@logosdx/observer) + createObserverContext pair: the cross-cutting event bus (realm.changed from live-reload, theme.changed for the retheme cascade)
        graphEngineAdapter — mount/teardown glue honoring window.GraphCore / AtomicGraphUI / AtomicCodeExplorer contracts
        graphUI — rebuild of the AtomicGraphUI shared block (hover preview card, page modal, navigate); exposed as window.AtomicGraphUI so the carried profiles' hooks keep working; consumed by the rail mini-graph and both graph views
        typeColors — TYPE_HUE/ramp/atomicCyTypeColors: single OKF type→color source reading CSS custom properties; exposed as a window global so the carried system-graph.js/code-graph.js profiles keep their existing call site unmodified; imported directly by React components and by railCytoscapeStyle

    atomic/internal/serve/
      frontend_dist.go
        embeddedFrontend — go:embed var over frontend/dist
      context_handler.go
        handlePage — JSON {html, relpath, breadcrumb, hasMermaid, ...}
      rail_handler.go
        handleRail — JSON {props, out, in, graphURL}
      nav.go
        handleNav — JSON nav tree + badges
      search_md.go
        handleSearchMD — JSON results
      search_stream.go
        handleSearchStream — SSE JSON events
        normalizeSearchSrc — src-param clamp, relocated from search_page.go at cutover
      codesearch.go
        handleCodeSearch — JSON
      codeexplorer.go
        handleCodeNode / handleCodeCallers / handleCodeCallees / handleCodeImpact / handleCodeFiles / handleCodeSchema / handleCodeFile — JSON
      health.go / external.go
        handleStatus / handleExternal — JSON dashboard payloads
      render.go
        template split — HTML-in-JSON delivery, plain hrefs
      wikilink.go
        renderer — plain href output (no hx-get)
      serve.go
        mux — /api/* registration; SPA fallback + embed source swap at cutover
      search_page.go
        (normalizeSearchSrc moves to search_stream.go — see above; the only piece that survives)
        NewSearchPageHandler / renderSearchPageFragment / searchBreadcrumbOOB — dead at cutover, no reshape; the document-load shell-wrap and htmx fragment composition are superseded by the SPA fallback and by React's SearchRoute composing the already-reshaped /api/search/md, /api/search/stream, and /api/code/search endpoints directly


## API contracts


Cross-checkpoint contract: the API checkpoints (CP2–CP4) implement these shapes; the screen checkpoints (CP5–CP11) consume them. Field names are normative (camelCase); additive fields are free, renames/removals are spec amendments. Shapes derive from the handlers' existing view-model structs — cited per endpoint.

Conventions (every `/api/*` endpoint):

- `Content-Type: application/json`; Go `encoding/json` default HTML-escaping stays enabled.
- One error envelope: non-200 status + `{"error": "<message>"}` (adopts `writeGraphError`, codegraph.go). 404 = missing target or traversal rejection; 400 = bad/missing params; 500 = render/query failure. Soft per-member states inside composite responses (not-indexed members, degraded intel) are data fields, not errors.
- Fields carrying pre-rendered HTML are named `html` (or `*Html`); every other field is plain data the client renders.
- All fetches in the React app go through one shared `FetchEngine` instance (`@logosdx/fetch`, `utils/api`) exposed via `createFetchContext` — no bare `fetch` in components. Resilience (retry, dedupe, caching) is engine config, never hand-rolled; every call sits behind an `attempt()` tuple (`@logosdx/utils`), with the error envelope handled in the `!res.ok` branch. Live-reload-triggered refetches must bypass or invalidate any `cachePolicy` so a reconcile never serves a stale cached page.

| Endpoint | Shape (source struct) |
|----------|----------------------|
| `GET /api/page/<relpath>` | `{html, title, relpath, hasMermaid, breadcrumb: [{label, path, folder}]}` — from `pageWithGraphData` (context_handler.go:202) with breadcrumb reshaped from HTML to segments (the `data-nav-folder` behavior moves client-side). Directory URL → `{dir: true, relpath, entries: [{name, relpath, folder}]}`. Missing → 404 envelope; client renders the not-found view from `{error, relpath}` |
| `GET /api/file/<relpath>` | `{html, title, path}` — chroma line-table HTML (render.go:604) |
| `GET /api/rail/<relpath>` | `{relpath, orphan, properties: [{key, value, isURL, isJSON}] \| null, out: [edge], in: [{path}], graphDataURL}`; `edge = {target, resolvedPath, kind, broken, ambiguous, codeFile, external}` — from `railTmplData`/`propKV` (rail_handler.go:41,118) + `Edge` (graph.go:34) |
| `GET /api/nav` | `{scope: "realm"\|"repo", groups: [{name, items: [navNode]}]}`; `navNode = {label, relpath?, stale?, children?: [navNode]}` — folder nodes carry `children`, leaves carry `relpath`; badges from the staleness sets (nav.go) |
| `GET /api/search/md?q=` | `{query, truncated, cap, results: [{relpath, line, snippet}]}` — from `mdMatch` + the 50-cap truncation flag (search_md.go:57,88) |
| `GET /api/code/search?q=&only=&exclude=` | `{members: [{key, prefix, indexed, results: [nodeRef]}]}`; `nodeRef = {id, name, kind, filePath, startLine}` — member grouping per codesearch.go; un-indexed members appear with `indexed: false`, empty results |
| `GET /api/search/stream?q=&src=` | SSE, named events (search_stream.go:12): `md` → the `/api/search/md` payload; `code` → one `{member: {key, prefix, indexed}, results: [nodeRef]}` per member; `end` → `{}` terminal. `src ∈ {all, md, code}` (`normalizeSearchSrc`). Exception to the 400 convention: empty/missing `q` streams a single terminal `end` event with 200 — SSE has no mid-stream status channel, and this mirrors the pre-existing stream's empty-query handling |
| `GET /api/code/node?id=&member=` | `{member, node}`; `node = {id, name, kind, filePath, startLine, signature, language, docstring}` — `types.Node` |
| `GET /api/code/{callers,callees,impact}?id=&member=` | `{member, root: node, edges: [{kind, source, target}], nodes: {<id>: node}}` — `types.Subgraph` |
| `GET /api/code/files?member=` | `{files: [{path, language, nodeCount}]}` — `types.FileRecord` |
| `GET /api/code/schema?member=` | `{tables: [{node, columns: [node], fkSources: [node], writers: [node]}]}` — `tableSchema` (codeexplorer.go:580) |
| `GET /api/code/file?path=&member=` | `{path, member, nodes: [{id, name, kind, startLine}], degraded?: "<reason>"}` — degraded carries today's inline not-indexed/no-intel messages (codeexplorer.go:837) |
| `GET /api/status` | `{isRealmScope, wiki: {staleRepos, staleConcerns, staleBuckets, bucketDiffKeys, allFresh}, index: {severity, detail, freshCount, staleMembers, notIndexed}}` — `healthData` (health.go:250) |
| `GET /api/external` | `{entries: [{url, sources: [relpath], firstSeen \| null}]}` — `ExternalEntry` (external.go:94) |
| `GET /events` | unchanged carried contract: unnamed events, `{fp, changed?}` (changed capped at 100, omitted over cap) |


## Flows


Flow: SPA boot

1. browser requests `/` (or any non-API, non-static, non-carried-JS-endpoint path)
2. Go's SPA fallback serves the embedded `index.html`
3. the before-paint inline script sets the theme class from localStorage, falling back to OS preference, before React mounts
4. React mounts; React Router resolves the current URL to a route
5. the route effect fetches its `/api/*` data and renders

Flow: page navigation

1. user clicks an in-body wikilink, a nav-tree entry, or a breadcrumb segment
2. React Router intercepts the click and updates the URL without a full reload
3. `PageRoute` calls `/api/page/<relpath>` and `/api/rail/<relpath>`
4. the server resolves the link (bundle-relative, relative, or wikilink form), renders markdown through the goldmark/chroma/wikilink pipeline, and returns HTML-in-JSON
5. React injects the returned HTML and the rail panels; the mermaid effect mounts fenced blocks

Flow: rail fetch

1. `Rail` requests `/api/rail/<relpath>`
2. the server returns ordered Properties, OUT links, IN backlinks, and a mini-graph URL
3. `Rail` renders the Properties/OUT/IN panels; the mini-graph child requests `/graph/data?node=<relpath>&depth=1` and mounts the carried Cytoscape stylesheet factory

Flow: code modal drill-down

1. user clicks a source link (page body, rail OUT/IN annotation, or a code-graph node)
2. `CodeModal` opens and fetches `/api/code/file` (source pane) and `/api/code/node` (intel pane) for the target
3. intel-pane drill actions (callers/callees/impact) call the matching `/api/code/*` endpoint and push onto the modal's back-stack
4. Back pops the stack; the source pane re-syncs to the popped entry's file/line, deduping same-file hops (scroll-to-line only, no re-fetch)
5. closing the modal clears the stack

Flow: streaming search

1. user opens the search dialog or navigates to `/search?q=&src=`
2. the search UI opens an SSE connection to `/api/search/stream?q=&src=`
3. the server streams per-member JSON result events (federated code search: bounded concurrent pool; markdown search: literal grep)
4. React appends results as events arrive, shows a spinner until the stream closes, and renders "not indexed" notes for un-indexed members

Flow: live-reload reconcile

1. `useLiveReload` opens `EventSource('/events')`
2. the server ticks on a quiet-window fingerprint change, broadcasting `{fp, changed}` (list capped at 100; omitted over cap means refetch-all)
3. the hook always refetches nav data; it refetches the open page/rail only when the current relpath is in `changed` (or the list was omitted)
4. React re-renders the affected data in place, with scroll position preserved as a natural property of re-rendering content rather than swapping panes
5. the connection indicator reflects live / reconnecting / disconnected state

Flow: graph-mode mount via the carried engine

1. user navigates to `/graph?view=&member=`
2. `GraphRoute` fetches `/graph/data` (docs view) or `/code/graph/data` + `/code/graph/members` (code view) — carried endpoints, untouched paths
3. `GraphRoute` calls `graphEngineAdapter`, which invokes `window.GraphCore.mount(container, profile)` with the carried `system-graph.js` or `code-graph.js` profile
4. the carried cosmos.gl engine renders and simulates unchanged; hover/click handlers call back into `window.AtomicGraphUI` / `window.AtomicCodeExplorer` to open the page or code modal
5. on route change or unmount, the adapter calls the engine's teardown to release the WebGL context

Flow: theme toggle retheme cascade

1. user toggles light/dark via `useTheme`
2. the toggle flips the CSS custom-property theme class and persists the choice to localStorage
3. the retheme effect re-invokes every `typeColors`-derived consumer: `railCytoscapeStyle` (`window.__railCy.style(...)`), the carried graph engine's retheme entry point, and the mermaid retheme effect
4. each consumer re-reads the now-flipped CSS variables and repaints — no data re-fetch


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Frontend build pipeline scaffold: Bun+React+TS workspace (conventions per `frontend/CLAUDE.md`), `go:embed` source, `make frontend` target + CI/pre-commit drift gate, dev API proxy | `atomic/internal/serve/frontend/` (workspace + carried public assets), `atomic/internal/serve/frontend_dist.go`, `atomic/Makefile`, `.githooks/pre-commit`, `.github/workflows/ci.yml` | atomic-implementer (mode: feature) | ~10 (excl. generated dist) | `make frontend && git diff --exit-code` clean; `go build ./internal/serve/...` succeeds against the committed dist with no Bun invocation; `bun test` scaffold smoke test green and wired into CI for PRs targeting `next` |
| 2 | `/api/*` content endpoints: page, file, rail, nav — additive JSON alongside existing htmx routes | `context_handler.go`, `rail_handler.go`, `nav.go`, `serve.go` (route registration) + tests | atomic-implementer (mode: feature) | ~8 | existing HTML-fragment tests untouched and green; new tests assert `/api/page`, `/api/file`, `/api/rail`, `/api/nav` JSON shapes |
| 3 | `/api/*` search + streaming endpoints — additive JSON alongside existing htmx routes | `search_md.go`, `search_stream.go`, `codesearch.go`, `serve.go` (route registration) + tests | atomic-implementer (mode: feature) | ~6 | existing HTML-fragment tests untouched and green; new tests assert `/api/search/md`, `/api/search/stream` (JSON events), `/api/code/search` JSON shapes; federated search concurrency/bounding behavior unchanged |
| 4 | `/api/*` code-intel + dashboard endpoints — additive JSON alongside existing htmx routes | `codeexplorer.go`, `health.go`, `external.go`, `serve.go` (route registration) + tests | atomic-implementer (mode: feature) | ~8 | existing HTML-fragment tests untouched and green; new tests assert `/api/code/{node,callers,callees,impact,files,schema,file}`, `/api/status`, `/api/external` JSON shapes |
| 5 | React shell: routing, theme (toggle + before-paint init + retheme hook seam), top bar, left nav as an Ark TreeView folder tree (collapsible branches, keyboard nav, stale/drift badges), single-source type→color module | `frontend/src/App`, `layouts/Shell`, `pages/` skeleton, `components/nav/`, `hooks/useTheme`, `utils/typeColors` | atomic-implementer (mode: feature) | ~11 | frontend test suite green against CP2's nav JSON; theme toggle covers persisted + OS-fallback cases; connection indicator renders from a stubbed live-reload state; Back/Forward restores scroll position and `location.hash` scrolls to its anchor on mount/update; all fetches route through `utils/api`; `typeColors` returns the same colors as today's `atomicCyTypeColors` for a given CSS-var set and is exported as the sole color source (no independent color derivation elsewhere in the diff) |
| 6 | React page view + rail: HTML-in-JSON injection, wikilink click interception (all link forms + broken/ambiguous/external/codefile), mermaid mount, directory/404, rail Properties/mini-graph/OUT/IN, `utils/graphUI` rebuild (hover preview card + page modal, `window.AtomicGraphUI` contract) | `frontend/src/pages/Page`, `components/rail/` (incl. `railCytoscapeStyle`), `utils/graphUI` | atomic-implementer (mode: feature) | ~10 | frontend test suite green for page render, link-form interception, mermaid mount, directory/404 fallback, rail panels; a skeleton state renders in the content pane until `/api/page` resolves (no blank flash); rail mini-graph hover shows the preview card and click opens the page modal via `utils/graphUI`; rail mini-graph's Cytoscape stylesheet is built from `typeColors` (`railCytoscapeStyle`), not a re-derived color map |
| 7 | React search: command palette (Ark Combobox: shortcuts, md\|code toggle, debounce) and search page (Ark Tabs, SSE stream, "not indexed" notes) | `frontend/src/components/search/`, `pages/Search` | atomic-implementer (mode: feature) | ~6 | frontend test suite green for shortcut triggers, tab switching, streamed-result rendering, not-indexed notes |
| 8 | React graph mode: route + engine adapter mounting carried `system-graph.js`/`code-graph.js`, Docs\|Code switcher, member picker | `frontend/src/pages/Graph`, `utils/graphEngineAdapter` | atomic-implementer (mode: feature) | ~5 | frontend test suite green for mount/teardown calling `window.GraphCore.mount`; switcher and member-picker (hidden single-member) behavior verified against a mocked engine; carried profiles read colors via the `typeColors` window global — no color logic duplicated in the adapter; profiles' hover/click hooks resolve through the `window.AtomicGraphUI` contract provided by `utils/graphUI` (built in CP6) |
| 9 | React code modal (Ark Dialog): source pane (chroma HTML-in-JSON, line anchors, scroll-to-line), intel pane (symbols/node/callers/callees/impact), back-stack, same-file fetch dedup | `frontend/src/components/code-modal/` | atomic-implementer (mode: feature) | ~5 | frontend test suite green for source-pane render + scroll-to-line, intel-pane drill-down, back-stack navigation, dedup on same-file hops |
| 10 | React dashboards: code schema view, external-link registry, status view | `frontend/src/components/schema/`, `pages/Status`, `pages/External` | atomic-implementer (mode: feature) | ~5 | frontend test suite green rendering `/api/code/schema`, `/api/external`, `/api/status` JSON |
| 11 | Live-reload reconcile + retheme cascade | `frontend/src/hooks/useLiveReload`, `hooks/useTheme` (retheme effect) | atomic-implementer (mode: feature) | ~4 | frontend test suite green for quiet-window reconcile (nav-always, page-conditional, capped-list-omitted → refetch-all), scroll preservation, and retheme firing against mermaid/cosmos/rail-Cytoscape mocks |
| 12 | Cutover: flip SPA default route, relocate `normalizeSearchSrc` into `search_stream.go`, delete `layout.html`/`search_page.go`'s handler shell/the old `assets/` directory/OOB handlers/pre-cutover HTML paths, prune the carried `app.css`'s htmx selectors, retarget and re-run `scripts/graph-gates.mjs` | `serve.go`, every reshaped handler file (mechanical deletion of its pre-cutover HTML path — same operation repeated per file), `search_stream.go` (receives `normalizeSearchSrc`), `templates/layout.html` (D), `search_page.go` (D, minus the relocated helper), `atomic/internal/serve/assets/` (D), `frontend/public/app.css`, `scripts/graph-gates.mjs` | atomic-implementer (mode: feature) | ~17 | `go test ./internal/serve/...` green with reshaped handlers' tests JSON-only and `normalizeSearchSrc` covered under its new home; every non-API, non-static, non-carried-JS-endpoint GET serves the SPA shell; `git grep` for `NewSearchPageHandler`/`renderSearchPageFragment`/`searchBreadcrumbOOB`/`atomic/internal/serve/assets/` returns nothing; the post-cutover carried copy `frontend/public/app.css` carries no htmx-specific selectors (`grep -c 'hx-\|htmx' atomic/internal/serve/frontend/public/app.css` returns 0); all 5 `scripts/graph-gates.mjs` gates pass |
| 13 | Docs amendment: architecture description + live-reload overstatement correction | `docs/reference/serve.md`, `docs/spec/atomic-serve.md`, `docs/wiki/serve.md` | atomic-implementer (mode: feature) | 3 | content matches the shipped architecture; reviewer confirms no stale htmx/fragment-swap claims remain |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Committed `dist/` produces noisy diffs on every frontend change (hashed filenames) | high | Same accepted tradeoff as `commands/`/`agents/`/embedded bundle; the CI drift gate (`make frontend && git diff --exit-code`) is the enforcement, not diff hygiene |
| Server-rendered HTML injected via `dangerouslySetInnerHTML` widens the perceived XSS surface | med | Content is local-filesystem markdown, read-only, same trust domain as today's server-rendered fragments — no new external input crosses this boundary; the cutover checkpoint documents this framing explicitly |
| `scripts/graph-gates.mjs` selectors drift from the React shell's actual DOM structure | med | Selector retargeting is its own checkpoint step; all 5 gates re-run before cutover ships |
| A screen checkpoint's expectations diverge from its API checkpoint's actual JSON shape (additive phase, built in separate iterations) | med | Each screen checkpoint tests directly against the same-shape API checkpoint; a divergence surfaces as a failing screen test, not silent drift |
| Contributor Bun requirement for any frontend change | low | Documented in the contributing guide; `go build` alone still works without Bun because `dist/` is committed |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->

### 2026-07-17 — Bun toolchain + baseline workspace layout

**What changed:** The frontend workspace toolchain is Bun (package manager, bundler, test runner) instead of Vite + npm; `vite.config.ts` becomes `bunfig.toml`. The `src/` layout is fixed as domain-scoped `layouts/pages/components/hooks/utils` (was `routes/` + flat `components/` + `engine/` + `colors/`), with component-folder rules (per-component folder with `style.css`, `components/ui/` barrel for generic primitives, no barrels for app-specific components) codified in a new `frontend/CLAUDE.md` that the workspace scaffold ships. Baseline scaffold (tree + CLAUDE.md + package.json + tsconfig.json + ui barrel) committed ahead of CP1; CP1 retains build-pipeline/embed/gate wiring.

**Why:** Owner decision — Bun collapses install/bundle/test into one tool with the fewest dependencies, and the layout conventions need to exist before fresh-context checkpoint subagents start building screens.

**Superseded:** Vite + React + TS workspace with `vite.config.ts` and the `routes/`-based outline structure.

### 2026-07-17 — Ark UI as the sole primitive library

**What changed:** `@ark-ui/react` is the single UI-primitive dependency: TreeView renders the left nav as a folder-dropdown tree (collapsible branches, keyboard navigation), Dialog backs the code/page modals, Tabs the search-results view, Tooltip the connection indicator, Combobox the ⌘K palette. New success criterion pins it as the only primitive suite; CP5/CP7/CP9 name the components. `frontend/CLAUDE.md` carries the never-add-a-second-suite rule.

**Why:** Owner decision after a `/gather-evidence` pass: one lineage covers every primitive need including the tree and palette; verified to bundle clean under `bun build` (zero ESM/"use client" errors, ~49 kB gz over React for the five primitives — probe at `tmp/ark-probe/`). Evidence and rejected alternatives in the design's D5 table.

### 2026-07-17 — API contracts + behavior ownership

**What changed:** New `## API contracts` section pins the cross-checkpoint JSON/SSE contracts (shapes derived from the handlers' existing view-model structs, cited per endpoint), one error envelope (`{"error": …}` + status, adopting `writeGraphError`), the html-field naming convention, and a mandatory shared fetch helper (`utils/api`). Ownership fixes: the `AtomicGraphUI` shared block (hover preview card, page modal, navigate) is rebuilt as `utils/graphUI` in CP6 and consumed by CP8's profiles via the preserved window contract; CP5 gains Back/Forward scroll restoration + `location.hash` anchor scrolling; CP6 gains a no-blank-flash skeleton gate; CP1 wires `bun test` into CI for `next`-targeting PRs and the change tree records the `ci.yml` trigger fix. Non-goals gains the explicitly accepted cutover regressions (no-JS readability, 200-on-unmatched-paths, stale pre-cutover tabs, forward-fix rollback).

**Why:** Challenge-swarm findings, owner-triaged: fresh-context checkpoint subagents need pinned boundary contracts to build coherently; several existing behaviors had no owning checkpoint; the accepted regressions were implicit.

### 2026-07-17 — LogosDX data/reactivity layer

**What changed:** `utils/api` is a shared `FetchEngine` instance (`@logosdx/fetch`) exposed through `createFetchContext` — resilience (retry/dedupe/cache) is engine config, hand-rolled loops and caches are prohibited; all I/O sits behind `@logosdx/utils` `attempt()` tuples (no try-catch). New `utils/events` — a typed `ObserverEngine` (`@logosdx/observer`) as the cross-cutting event bus: `useLiveReload` emits `realm.changed`, `useTheme` emits `theme.changed`, consumers subscribe via the `@logosdx/react` context hooks. Live-reload refetches must bypass/invalidate any fetch cache.

**Superseded:** the hand-rolled `utils/api` fetch helper.

**Why:** Owner decision — LogosDX is the house stack; FetchEngine ships the exact resilience the API layer needs as config. Verified to bundle clean under `bun build` (~29 kB gz over React for fetch+utils+observer+react — probe at `tmp/ark-probe/logos.tsx`).

### 2026-07-17 — Stream empty-query exception

**What changed:** Correction: `/api/search/stream` contract row now documents that an empty/missing `q` yields a 200 with a single terminal `end` event, not the 400 envelope.

**Why:** CP3 implementation + review — SSE has no mid-stream status channel; behavior mirrors the pre-existing stream handler. Flagged by the iteration-3 reviewer as undocumented implementer discretion.
