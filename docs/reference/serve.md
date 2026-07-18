# atomic serve

`atomic serve` starts a local, read-only HTTP server that renders a wiki realm (or a single repo) as a navigable, Obsidian-style knowledge graph in the browser. It is a presentation layer only — it reads what already exists (wiki summaries, code-intel indexes, bucket manifests) and never writes, re-indexes, or re-stamps anything.

The UI is a React single-page app: Go serves a JSON API plus a handful of carried, unreshaped endpoints (`/graph/data`, `/code/graph/data`, `/code/graph/members`, `/events`, `/healthz`); React Router resolves every other path client-side.


## Usage

```bash
atomic serve [path] [--port <N>] [--host <addr>] [--open]
```

| Flag | Default | Description |
|------|---------|-------------|
| `path` | current directory | Wiki realm root, member repo, or bare repo. A relative path is resolved to an absolute one. |
| `--port` | `4500` | Port to bind. `--port 0` picks a free port and prints the chosen one. |
| `--host` | `127.0.0.1` | Interface to bind. `0.0.0.0` (or `::`) exposes the server on the LAN and prints every reachable address. Still read-only, still no auth. |
| `--open` | off | Open the browser automatically after the server starts (best-effort; never fatal). |

The server shuts down cleanly on SIGINT. It prints the URL on start; under a wildcard bind (`--host 0.0.0.0`) it prints the loopback URL plus every reachable LAN address.


## Scope resolution

`atomic serve` resolves scope from the `path` argument (or `cwd`) using the same `realm.Resolve` logic as `atomic code`:

| Detected shape | Scope | What is served |
|---------------|-------|----------------|
| Registered `<wikis>` realm root | **Realm** | Full realm: nav tree (Realm / Repos / Concerns / Knowledge / Buckets / External), graph mode (the whole-realm docs graph, and a code graph per member via a picker), federated code search across all members. The landing page is the realm index (`wiki/index.md`). |
| Inside a member repo with a wiki | **Member** | Single-repo nav, pages, and code intelligence; realm chrome absent. |
| Bare repo with a code index but no wiki | **Repo** | Pages and code intelligence, no wiki chrome. The landing page is the repo `README.md`. |

The resolved scope is surfaced via `GET /api/status`, consumed by the top-bar breadcrumb.


## Architecture

`atomic/internal/serve/frontend/` is a Bun-toolchained React + TypeScript workspace: Bun is the package manager, bundler, and test runner (`bun install`, `bun test`, `bun run build.ts`). The build output (`frontend/dist/`) is committed and embedded into the `atomic` binary via `go:embed` (`frontend_dist.go`) — `go build ./internal/serve/...` needs no Bun or Node invocation, and `make frontend` regenerates `dist/` from source with a `git diff --exit-code` drift gate (same pattern as the `commands/`/`agents/` render pipeline).

The Go server exposes two kinds of routes:

- **`/api/*`** — JSON endpoints the React app fetches: page content, rail data, nav tree, search, code-intel queries, status, external-link registry. Every response that carries pre-rendered HTML (markdown, chroma-highlighted source) names the field `html` (or `*Html`); everything else is plain data the client renders. One error envelope: non-200 status + `{"error": "<message>"}`.
- **Carried endpoints** — `/graph/data`, `/code/graph/data`, `/code/graph/members`, `/events`, `/healthz` keep their pre-React paths and shapes unchanged; the cosmos.gl graph engine (`graph-core.js`, `system-graph.js`, `code-graph.js`, vendored `cosmos-graph.js`) is carried into `frontend/public/` and mounted from React via `window` contracts (`GraphCore`, `AtomicGraphUI`, `AtomicCodeExplorer`) rather than rewritten.

Every other GET falls through to the embedded `index.html` SPA shell; React Router resolves the requested path client-side (`/page/<relpath>`, `/graph`, `/search`, `/status`, `/external`, `/code/schema`, and the `/` landing route). All fetches in the React app go through one shared `FetchEngine` instance (`@logosdx/fetch`, `utils/api`) — no bare `fetch` in components; resilience (retry, dedupe, caching) is engine configuration.

Markdown rendering (goldmark + chroma + wikilink resolution) stays entirely server-side and single-sourced between the page body and the rail's edge data — `/api/page/*` and `/api/rail/*` resolve links identically. React never renders markdown or server-side content; it injects the HTML the API returns.


## The interface

The UI is a single persistent shell — navigating never reloads it; only the focused content and its surrounding context change.

- **Top bar** — a breadcrumb (`realm › member › page`), a search trigger that opens the command-palette dialog (`⌘K`), and a light/dark theme toggle.
- **Left nav** — the collapsible folder tree (`GET /api/nav`), rendered as an Ark UI `TreeView`.
- **Middle** — the focused page, or graph mode, addressed by React Router. A `[ page | system ]` toggle switches between them; graph mode itself holds the whole-realm docs graph and a per-repo code graph behind a nested Docs | Code toggle.
- **Right rail** — four slots tracking the focused page: its YAML frontmatter properties, its local link graph, its outbound links, and its inbound links (backlinks).
- **Code modal** — a source file opens in an Ark UI `Dialog` overlay: highlighted source on the left, code intelligence on the right.

### Page view (the default)

The middle pane renders the focused markdown page; the right rail shows that page's context. Navigating to another page updates the content, the breadcrumb, and all three rail slots via `GET /api/page/<relpath>` and `GET /api/rail/<relpath>`.

Markdown renders server-side via [goldmark](https://github.com/yuin/goldmark) (GitHub Flavored Markdown) with [chroma](https://github.com/alecthomas/chroma) syntax highlighting, delivered to the client as HTML-in-JSON (`{html, title, relpath, hasMermaid, breadcrumb}`). Fenced ` ```mermaid ` blocks render client-side via vendored `mermaid.min.js`.

In-page links are resolved server-side against the realm root, not the browser's current URL. Three link forms are supported:

- **Bundle-relative** (`/path/to/page.md`) — a leading slash is resolved against the served root (OKF §5.1 recommended form). When the target exists under root it becomes an in-shell navigable route, exactly like a relative link. This is how cross-links between OKF concept pages (`knowledge/`, `concerns/`) render.
- **Relative** (`../concerns/x.md`, `./other.md`) — resolved from the source page's directory.
- **Obsidian wikilinks** (`[[page]]`, `[[page|alias]]`) — resolved by nearest-then-alphabetical rule; kept for back-compat tolerance.

In all three cases, resolved routes become `/page/<relpath>` for markdown pages or folders (React Router navigates in-shell, so clicking never reloads the app or loses your place), or opens the code modal for source files. External `http(s)` links open in a new tab; in-page `#anchor` links and any link that would escape the realm are left untouched. A link to a page that does not exist still routes through `/page/`, so it lands on the in-shell "not found" view rather than a full-page navigation to a dead URL.

### Right rail (`GET /api/rail/<page>`)

For the focused page, a single request returns four panels as JSON:

- **Properties** — YAML frontmatter key-value pairs, rendered as a table at the top of the rail. Scalar values pass through as-is; array and object values are pretty-printed as JSON. The slot is hidden when no frontmatter is present. A frontmatter `resource:` key (or any property whose value is an `http(s)://` URL) is rendered as a clickable link — the OKF recommended form for surfacing an underlying asset or canonical source.
- **This-page graph** — a depth-1 local link graph rendered as a compact Cytoscape mini-graph (data from `/graph/data?node=<page>&depth=1`). Nodes are colored by type, using the same hybrid resolver as the docs graph.
- **OUT links** — outbound links the page contains, with broken / ambiguous / external annotations. Links to source files open the code modal.
- **IN links** — backlinks; an orphan note appears when nothing links to the page.

Links and backlinks come from `mdlink.ExtractLinks`, which parses markdown links `[text](path)` and Obsidian wikilinks `[[page]]` / `[[page|alias]]` (fenced code spans excluded). Wikilinks resolve by a nearest-then-alphabetical rule; ambiguous resolutions are surfaced.

### Graph mode

The `[ page | system ]` toggle swaps the middle pane into graph mode and collapses the right rail. Graph mode holds two views behind a nested **Docs | Code** toggle: the whole-realm docs graph, and a per-repo code graph.

**Docs graph.** The whole-realm graph, rendered by [cosmos.gl](https://cosmos.gl) (GPU simulation and GPU rendering, fed by `/graph/data`). The layout runs as a continuous physics simulation instead of a one-shot layout pass: it settles to rest and pauses on first open, and the settled positions are cached per realm, so reopening an unchanged graph replays the same layout instantly with no visible motion.

- Nodes are colored by OKF concept type. The type is resolved via a hybrid strategy: frontmatter `type:` (title-case values `Knowledge`, `Concern`, `Repo Summary` mapped to short lowercase classes) takes priority, then path-convention fallback (`wiki/repos/` → `repo`, `wiki/concerns/` → `concern`, `wiki/knowledge/` → `knowledge`, `wiki/.buckets/` → `bucket`, `http(s)://` hrefs → `external`), then `page` as a default.
- Nodes render in **A-style**: a solid background with a colored glow ring. Colors are read from CSS custom properties at render time via the single-source `typeColors` module and track the active theme automatically.
- Node labels render as a DOM overlay that fades in as you zoom in and fades out as you zoom out, so a dense graph stays readable from a distance. The hovered node's label always shows, regardless of zoom.
- Dragging a node reheats the simulation locally: the dragged node follows the pointer while the rest of the graph stays put. Releasing the drag settles the simulation back to rest and saves the new position to the cache.
- A **type legend** appears below the graph. Each chip shows the type name and its count of visible nodes. Clicking a chip toggles that type's nodes on or off, so you can isolate concerns, or hide repos to see only knowledge pages and the edges between them.
- Edges are drawn in three classes: markdown links, wikilinks, and fingerprint/provenance links (dashed). A provenance edge whose recorded fingerprint differs from the live content hash is drawn red, the drift signal from the `reflects:` / `sources:` chain.

Hovering a node shows a floating card near the pointer with a type chip, title, short description, and a snippet, taken from `title`, `description`, and `snippet` fields in the `/graph/data` JSON payload; it dismisses on pointer-leave. Clicking a node opens a modal over the dimmed graph, not a navigation away: the modal fetches the page's rendered HTML from `/api/page/<id>`, displays it inline, and offers an "Open full page →" button for when you want more context. The modal closes on Esc, the close button, or a click on the dimmed backdrop, and graph state is preserved throughout.

**Code graph.** A per-repo view of the code-intel index, fetched from `GET /code/graph/data`: the repo's symbols as nodes, and their `contains`, `calls`, and `imports` relationships as edges. It shares the docs graph's cosmos.gl engine, so the same continuous physics, drag behavior, and settle-then-pause motion apply; only the data source and styling differ.

- Nodes are colored by kind (functions, types, modules, and so on), collapsed into a small set of visual groups with a filterable legend, the same interaction as the docs graph's type legend: click a chip to toggle a group on or off.
- `contains` edges (a file containing its symbols) render fainter than `calls` and `imports`, so the structural skeleton doesn't drown out the relationships you actually care about.
- Node size scales with how connected a symbol is, within the same size window the docs graph uses.
- Hovering a node shows its name, kind, and `file:line` in place of the docs graph's title, description, and snippet. Clicking a node opens the existing code-explorer view for that symbol, the same view reached from code search and the code modal, member-aware in realm scope.
- In a wiki realm, a member picker next to the Docs | Code toggle lists the repos with a code index, and switching members swaps the graph to that repo. A single-repo or member-scoped server has only one repo to show, so no picker appears.
- If the selected repo has no code index, the pane shows a message naming `atomic code index` as the fix, instead of an empty graph.
- Positions are cached per repo, keyed to that repo's index fingerprint: re-indexing changes the fingerprint, so a stale layout is never replayed against fresh data, while reopening an unchanged index replays the cached layout with no visible motion.
- One graph per repo; there is no merged, cross-repo code graph, the same federation-not-merging rule federated code search follows.

The selected view, and in a realm the selected member, are kept in the URL, so a link to a specific graph reopens the same one.

**WebGL2 requirement.** Graph mode needs WebGL2 to run, in both the Docs and Code view. If the browser lacks it, the toggle shows a message naming the requirement instead of a blank pane or a stuck spinner. The rail mini-graph runs on Cytoscape and needs no WebGL2, so it works in any browser.

The rail mini-graph runs on the vendored `cytoscape.min.js`; graph mode runs on a separately vendored cosmos.gl bundle shared by both the Docs and Code views. Both are carried into `frontend/public/` and embedded via `go:embed` alongside the rest of `dist/`, with no runtime build step.

### Code modal

Clicking a source-file link — in page content, in the rail, or in a search/code result — opens an Ark UI `Dialog` over the dimmed page:

- **Source** (`GET /api/file/<path>`) — chroma-highlighted, per-line anchors; a `file:line` reference scrolls to the line.
- **Code intelligence** (`GET /api/code/file?path=<path>`) — the symbols defined in the file, each a chip that drills into its callers, callees, and impact radius. In a realm the file path is mapped to its owning member, that member's own index is opened, and it is queried with the member-relative path; the drill-down links carry a `member=` parameter so callers/callees/impact stay within the same member's index. When the file's repo is not indexed, the modal shows source only with a brief note.

Intel-pane drill actions push onto the modal's back-stack; Back pops the stack and re-syncs the source pane to the popped entry's file/line, deduping same-file hops (scroll-to-line only, no re-fetch). The modal closes on `Esc`, the close button, or a backdrop click, which clears the stack.

### Search (command-palette dialog + `/search` page)

Search is an Ark UI `Combobox` dialog, not an inline dropdown. The top-bar trigger — or `⌘K` / `Ctrl K`, or `/` when the focus isn't a text field — opens a centered command palette holding the `md | code` toggle and a debounced live-results list. The toggle flips the source:

- **md** (`GET /api/search/md?q=`) — a literal, case-insensitive grep over the served markdown files. Results are `file:line` matches with a snippet; selecting one loads that page. The query is only ever a search substring, never a path.
- **code** (`GET /api/code/search?q=`) — the federated symbol search (below). Selecting a result opens the code modal at that symbol's file.

Pressing `Enter` (or "View all results") opens the dedicated **`/search?q=&src=`** page: a full, URL-addressable results view with `All | Markdown | Code` tabs (Ark UI `Tabs`) — quick-jump in the dialog, browse everything on the page. The dialog closes on `Esc` or a backdrop click.

The page **streams** results over Server-Sent Events (`GET /api/search/stream`): the markdown block arrives first (a fast local grep), then each realm member's code results stream in as that member's index query finishes. Members are searched **concurrently** — a bounded goroutine pool — so one large repo doesn't hold up the rest, and a terminal event clears the loading spinner. While anything is in flight a spinner shows; when a realm has no code index, the code section says so (`run atomic code index`) instead of sitting blank. An empty or missing `q` streams a single terminal `end` event with a 200 status — SSE has no mid-stream status channel.

### Federated code search (`GET /api/code/search?q=…`)

Resolves realm members, opens each member's index with `engine.NewWithDBPath`, calls `SearchNodes`, and groups results under `[key]` headers. A member with no index is skipped with a visible "not indexed" note rather than aborting other members. `only` and `exclude` query params filter the member set. In repo or member scope the search targets the single index.

Members come from two sources, unioned: realm **federation** (a `<code-index>` block in CLAUDE.md plus per-member dbs at `<realm>/.atomic/<key>.db`) and per-member **self-indexes** — a member indexed the natural way, `cd <member> && atomic code index`, which writes `<member>/.claude/.atomic-index/atomic.db`. So code search (and the code modal) work in any wiki realm whose members were individually indexed, with no federation setup. Result links are prefixed with the member's realm-relative path so they resolve through the realm's code-file route. Members searched concurrently — see the streaming search above.

### Code intelligence routes

The code modal and code search build on the per-repo query routes under `/api/code/*`, each composing existing `engine` queries (no new analysis):

- `GET /api/code/node` — node detail (signature, file:line, metadata) from `engine.GetNode`.
- `GET /api/code/callers`, `GET /api/code/callees`, `GET /api/code/impact` — rendered as `{member, root, edges, nodes}` (`types.Subgraph`); edge kind shown (`calls / references / writes / contains`).
- `GET /api/code/files` — the indexed file list.
- `GET /api/code/file?path=` — the symbols defined in one file (`engine.GetNodesInFile`); the modal's intelligence pane.
- `GET /api/code/schema` — for indexes containing `table` / `view` nodes: tables and views with their `column` children, an FK graph from `references` edges, and a writers-vs-readers split from `writes` edges. Derived from graph nodes and edges — there is no `atomic code schema` verb.
- `GET /code/graph/data`, `GET /code/graph/members` — the code graph's data export and member list; see Graph mode above.

### External-link registry (`GET /api/external`)

Lists every outbound `http(s)` URL across the realm: the URL, the source pages that cite it, and a first-seen date (git history when available, file mtime otherwise). Reachable from the nav External group.

### Status (`GET /api/status`)

The realm-health view, reachable but no longer the landing page. Renders `wiki.Stale` / `wiki.CheckStaleness` (DRIFT / STALE / STALE bucket) plus aggregate code-index health (worst severity across member repos, naming only repos that need action). No new staleness computation — staleness also surfaces ambiently as badges in the nav. `/healthz` is a separate plain-text liveness probe.


## Live reload

While a browser tab is open, `atomic serve` reflects filesystem changes without a restart. The server keeps one realm snapshot (fingerprint, nav paths, link graph) and re-checks it with a stat-only walk every 10 seconds, but only while at least one tab is subscribed to the `/events` stream. With no tabs open the server does no periodic work.

When the realm changes, subscribed tabs receive a push carrying the new fingerprint and the list of changed files. The open page refetches its pane and rail only when the displayed file itself changed; the nav tree refreshes on any change. Scroll position is preserved as a natural property of React re-rendering content in place rather than swapping panes. The graph pane is not patched in place on a live-reload push — it reflects the change only on its next `/graph/data` or `/code/graph/data` fetch (re-entering the view, reloading, or the cosmos engine's own fingerprint-keyed layout cache invalidating on a subsequent load).

Files written moments ago are held back for a short quiet window before they are published, so a tool writing a file incrementally never renders a half-written page. A small indicator in the top bar shows the live connection state: live, reconnecting, or disconnected. Shutting the server down with tabs open exits cleanly and immediately.

Provenance hashing and the full graph JSON stay lazy: they are computed when the system view asks for them, never on the periodic check.


## Theme and visual design

`atomic serve` ships with a light and dark theme, both derived from the same CSS custom-property set.

**Theme toggle.** The top-bar sun / moon button switches themes. Before any page content paints, an inline script (in `index.html`) reads the `atomic-serve-theme` key from `localStorage` and falls back to the OS `prefers-color-scheme` media query. Toggling writes the choice back to `localStorage` and re-invokes every `typeColors`-derived consumer — the rail's Cytoscape stylesheet, the carried graph engine's retheme entry point, and the mermaid retheme effect — so every visible graph's colors update immediately without a page reload.

**Light theme.** Warm paper background, charcoal text, amber accent.

**Dark theme.** Warm charcoal background, off-white text, amber accent.

**Typography.** Newsreader (serif) for display headings; Inter for UI text; a monospace stack with ligatures explicitly disabled (`font-variant-ligatures: none; font-feature-settings: "calt" 0`) on all `code`, `pre`, `kbd`, and `samp` elements. This prevents programming-font contextual alternates (e.g. `calt`-ligated `--`, `->`, `===`) from collapsing adjacent characters visually.

**Type chip.** The `type` property in the right-rail Properties slot renders as a small colored chip rather than plain text, matching the color used for that node type in the graph.


## Static assets

The React app's build output (`frontend/dist/`), including carried CSS and vendored JS, is embedded via `go:embed` and served from memory. No network fetch, no file dependency outside the binary, no runtime build step.


## Security

- Binds to `127.0.0.1` by default; `--host 0.0.0.0` opts into LAN exposure. Read-only either way, and never an auth surface.
- Every served path is resolved against the scope root and rejected (404) if it escapes via path traversal (`../` or absolute). `os.ReadFile` is never called on an unvalidated request path. The markdown-search query is treated purely as a substring, never a path.
- No write operations of any kind. Serve observes; mutation stays in `/refresh-wiki`, `atomic code index`, and `atomic wiki` subcommands.
- Plain-HTTP/no-JS readability of `/page/*` no longer applies post-cutover — content requires the SPA to run; unmatched non-API GETs return the SPA shell (200) rather than 404, and traversal guards enforce at the `/api/*` boundary.
