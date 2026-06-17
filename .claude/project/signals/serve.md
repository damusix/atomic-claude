# serve

## What it does

`atomic serve [path] [--port N] [--open]` starts a localhost-only, read-only HTTP server (default port 4500) that renders a wiki realm or a single repo as a navigable graph in the browser. Scope is resolved via `realm.Resolve`: registered realm root → Realm scope, inside a member → Member scope, bare repo → Repo scope. Binds to `127.0.0.1` only; no write operations of any kind. Shuts down cleanly on SIGINT/SIGTERM.

The UI is a single persistent Obsidian-style shell: top bar (breadcrumb + `md|code` search), left nav tree, middle content pane with `[page|system]` toggle, right rail (this-page mini-graph + OUT/IN link lists). The standalone `/graph` full page was removed in FE3; the system view is now the middle-pane toggle loading `/graph/data` directly. Markdown page rendering uses `RenderMarkdownWithGraph`, which resolves Obsidian-style `[[page]]` / `[[page|alias]]` wikilinks in the body as in-shell htmx navigations using the same graph edges as the right rail; broken wikilinks render as `<span class="wikilink-broken">`.

## Artifacts

- [`docs/spec/atomic-serve.md`](../../../docs/spec/atomic-serve.md) — implementation spec (success criteria SC1–SC11, non-goals, security contract)
- [`docs/design/atomic-serve.md`](../../../docs/design/atomic-serve.md) — design deliberation (scope model, asset vendoring decisions, route map)
- [`docs/reference/serve.md`](../../../docs/reference/serve.md) — user-facing reference: flags, scope table, all routes and views

## CLI code

- [`atomic/internal/serve/`](../../../atomic/internal/serve) — leaf package; 32 files. Key files:
  - `serve.go` — `Run(args, stdout, stderr) int` (os.Exit entry), `RunWithContext(ctx, opts) int` (testable entry), `Options` struct, `DisplayScope` enum (`DisplayScopeRepo / Realm / Member`), `ResolveDisplayScope`; `parseFlags` normalizes the positional target-dir arg to an absolute path via `filepath.Abs`; [`/`](../../..) root serves the Obsidian shell template; `/healthz` liveness probe; `/status` realm-health dashboard (was `/health` in CP6)
  - `render.go` — goldmark + chroma markdown-to-HTML renderer; mermaid fenced-block pass-through. Three entry points: `RenderMarkdown` (bare), `RenderMarkdownWithLinks` (link rewriting), `RenderMarkdownWithGraph` (link rewriting + in-body wikilink resolution via the page's graph edges). `renderMarkdown` is the shared implementation; when `wikiResolve` is non-nil it wires the `wikilinkInlineParser` at priority 150 (above goldmark's default link parser at 200) and the matching `wikilinkRenderer`.
  - `wikilink.go` — goldmark inline parser + renderer for Obsidian-style `[[page]]` / `[[page|alias]]` wikilinks. `wikilinkInlineParser` triggers on `[`, commits only on `[[…]]` syntax, advances the reader by `2+close+2` bytes. `wikilinkResolverFromGraph` derives a `wikilinkResolver` from the focused page's outbound `Graph.Outbound` edges filtered to `mdlink.Wikilink` kind — reuses the resolution computed in `graph.go` so the body and the right rail can never disagree. `wikilinkRenderer` emits: resolved → `<a class="wikilink" hx-get="/page/…" hx-target="#main-pane">` (or `wikilink wikilink-ambiguous` class); broken → `<span class="wikilink-broken">`.
  - `nav.go` — left-nav tree builder; calls `wiki.ReadBucketEntries` for bucket badges; calls `wiki.ReadScanMembers` for member list
  - `graph.go` — link-graph builder from `mdlink.ExtractLinks` edges; `Edge` struct with `CodeFile bool` field (non-.md source files rendered as `/file/` links, not broken); `Graph` carries `nodeSet map[string]bool` for O(1) `Has()` membership test
  - `rail_handler.go` — `NewRailHandler` for `GET /rail/<relpath>`; renders three htmx OOB fragments: `#rail-out-content` (outbound links with broken/codefile/ambiguous/external annotations), `#rail-in-content` (backlinks + orphan note), `#rail-graph-content` (Cytoscape mini-graph seeded by `/graph/data?node=<page>&depth=1`); uses `data-rail-graph-url` attribute + `htmx.onLoad` pattern (inline `<script>` in OOB swaps unreliable in htmx 2)
  - `search_md.go` — `NewMdSearchHandler` for `GET /search/md?q=`; literal case-insensitive substring grep across all `.md` files under `NavRoot`; cap=50 results, snippet truncated to 120 chars; empty query returns empty fragment (200); each result carries `/page/<relpath>` navigation hook
  - `graphoverlay.go` — `/graph/data` Cytoscape elements JSON (CP9, SC11); no longer serves a standalone `/graph` page (removed in FE3)
  - `codesearch.go` — federated code search (`/code/search`); fans out via `engine.NewWithDBPath` per member
  - `codeexplorer.go` — per-repo Code Explorer routes under `/code/*`: `node`, `callers`, `callees`, `impact`, `files`, `schema`; plus `/code/file?path=<relpath>` (FE-new) which lists all symbols in a source file using `GetNodesInFile`
  - `external.go` — external-link registry (`/external`)
  - `health.go` — realm health page handler (now mounted at `/status`)
  - `provenance.go` — provenance DAG (`/provenance`); reads `reflects:` / `sources:` frontmatter; uses `wiki.ResolveFingerprint`
  - `stale.go` — stale-badge helpers
  - `walk.go` — scoped disk walk for nav tree construction
  - `context_handler.go` — production page handler; calls `RenderMarkdownWithGraph` (not `RenderMarkdownWithLinks`) so all page renders receive in-body wikilink resolution when a `*Graph` is available
  - `assets/app.css` — base CSS (embedded); includes `.md-content a.wikilink`, `.md-content a.wikilink-ambiguous`, `.md-content .wikilink-broken` rules
  - `assets/vendor/` — vendored JS: `htmx.min.js`, `mermaid.min.js`, `cytoscape.min.js`, `elk.bundled.js`, `cytoscape-elk.min.js` (embedded via `go:embed assets templates`)
  - `templates/layout.html` — single Obsidian-style HTML shell (top bar · left nav · middle pane with page/system toggle · right rail)
- [`atomic/cmd/atomic/main.go`](../../../atomic/cmd/atomic/main.go) — dispatches `atomic serve` at line 131–132 via `serve.Run(args[1:], ...)`
- [`atomic/internal/cliusage/cliusage.go`](../../../atomic/internal/cliusage/cliusage.go) — `serve` verb entry: path `["serve"]`, args `[path]`, flags `["--port", "--open"]`

## Docs

- [`docs/spec/atomic-serve.md`](../../../docs/spec/atomic-serve.md) — success criteria, non-goals, security contract
- [`docs/design/atomic-serve.md`](../../../docs/design/atomic-serve.md) — design decisions: scope model, asset vendoring, route list, Cytoscape+ELK choice
- [`docs/reference/serve.md`](../../../docs/reference/serve.md) — user-facing reference for all flags, scope resolution table, every route

## Coupling

- **wiki domain**: `serve` calls `wiki.ReadBucketEntries`, `wiki.ReadScanMembers`, `wiki.Stale`, `wiki.CheckStaleness`, `wiki.ResolveFingerprint`, and `wiki.FileSHA256`. Changes to wiki's staleness or bucket-diff APIs break serve's health page and nav badges.
- **code-intel domain**: `serve` imports `codeintel/realm` (for `realm.Resolve`) and opens per-member indexes via `engine.NewWithDBPath`. Changes to `realm.Resolve` scope types or `engine.NewWithDBPath` signature require matching changes in `serve.go` and `codesearch.go`. `codeexplorer.go` uses `GetNodesInFile` from `CodeEngine`; changes to that method signature propagate here.
- **mdlink domain**: `serve` depends on `mdlink.ExtractLinks` for graph and backlink construction. `Edge.CodeFile` is populated from `mdlink.Link` fields; changes to `Link` struct or wikilink resolution rules affect `graph.go`, `rail_handler.go`, and `graphoverlay.go`. `wikilink.go` imports `mdlink` for the `mdlink.Wikilink` edge-kind constant used to filter outbound edges in `wikilinkResolverFromGraph`; a rename of that constant breaks the resolver.
- **cliusage / doctor domain**: `atomic validate artifacts` lints `atomic serve` citations against `cliusage.go`. Adding or removing serve flags requires updating `cliusage.go` or artifact linting false-positives.

## Conventions worth knowing

- `Run` / `RunWithContext` split: `Run` owns signal handling and calls `os.Exit`; `RunWithContext` is the testable seam — tests inject a context and `Options` directly.
- All static assets (CSS, vendored JS, HTML template) are embedded at compile time via `//go:embed assets templates`; no network fetch and no file dependency outside the binary at runtime.
- Path traversal is rejected at the handler level — every served path is resolved against the scope root and 404'd if it escapes; `safeResolve` is the shared guard used by page, rail, and file handlers.
- `parseFlags` normalizes the positional target-dir arg to an absolute path via `filepath.Abs` so downstream handlers can resolve request paths against the root regardless of invocation form (`atomic serve .` or a relative path).
- Cytoscape+ELK load order is fixed: `cytoscape.min.js` → `elk.bundled.js` → `cytoscape-elk.min.js`; `cytoscape.use(cytoscapeElk)` is called after all three are loaded.
- `--port 0` triggers OS-assigned port; the chosen port is printed to stdout so callers (tests, scripts) can parse it.
- `DisplayScopeRepo` covers both "repo with a code index" and "bare repo with no index" — docs-only mode is valid; a code index is not required to start.
- Right-rail mini-graph uses `data-rail-graph-url` attribute + `htmx.onLoad` delegation for Cytoscape init; inline `<script>` tags in OOB `innerHTML` swaps are not reliably executed by htmx 2.
- `/healthz` is the liveness probe (plain text `ok`); `/status` is the user-facing realm-health dashboard (moved from `/health` in CP6).
- The link graph is built once at server startup via `BuildLinkGraph(navRoot)`; the graph is passed into page and rail handlers so they remain stateless. `Graph.Has(rel)` is O(1) via `nodeSet`.
- Wikilink resolution is single-source: `wikilinkResolverFromGraph` reads the already-computed outbound edges from `Graph.Outbound(pageRelPath)` filtered to `mdlink.Wikilink` kind. The body and the right rail use the same resolution — no second resolution pass. When `g` is nil (e.g. tests calling `RenderMarkdown` or `RenderMarkdownWithLinks`), the wikilink parser is not registered and `[[…]]` renders as literal text.
- The `wikilinkInlineParser` priority is 150, below the goldmark default block parsers but above the default link parser (200). On a single `[` that does not form `[[`, it returns nil and does not advance the reader — the default link parser takes over.
