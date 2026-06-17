# atomic serve — spec


## Goal

Ship `atomic serve` — a local, read-only HTTP server that renders a wiki realm (and a
bare repo, and a single member) as a navigable graph in the browser. Presentation only:
every view wraps an engine that already exists (wiki link parser, wiki staleness, bucket
diff, the code-intel realm resolver and query layer). No new analysis. CGO-free, no JS
build step — assets vendored via `go:embed`.

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

- [ ] **SC1** — `atomic serve [path]` starts an HTTP server bound to `127.0.0.1:<port>`
      (`--port`, default 4500), prints the URL, opens the browser when `--open` is set
      (best-effort; never fatal), and shuts down cleanly on SIGINT. `--port 0` picks a
      free port and prints the chosen one.
- [ ] **SC2** — Scope is resolved by `realm.Resolve`: a registered `<wikis>` realm root →
      realm scope; inside a member → member scope; a bare repo with no wiki → repo scope.
      The resolved scope is shown in the UI header. A bare repo with a code index but no
      wiki is servable (code views + its `docs/`, no realm chrome).
- [ ] **SC3** — Static assets (htmx, base CSS, html/template layout) are embedded via
      `go:embed` and served from memory; no network fetch, no file dependency outside the
      binary. The root route renders the persistent shell.
      **Superseded 2026-06-14 (see change log):** the shell is the Obsidian model — top bar
      (breadcrumb + `md|code` search), left nav, middle content with a `[page | system]`
      toggle, right rail (this-page graph ▸ OUT links ▸ IN links), code-file modal. The
      earlier "left nav · center · right context" three-pane is replaced. See *Frontend
      rework* below and the design doc § "Frontend interaction model".
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
- [ ] **SC11** — Graph overlay: `cytoscape.min.js`, `elk.bundled.js`, and
      `cytoscape-elk.min.js` are vendored via `go:embed` and loaded in that order
      (`cytoscape.use(cytoscapeElk)` after). `/graph` renders a global realm graph and a
      local depth-1–2 view from a node. Three edge classes — md-link, wikilink, and
      fingerprint/provenance (dashed) — are drawn distinctly. Code edges are per-member
      sub-graphs entered from a repo node; no cross-repo edges are drawn.
- [ ] **SC12** — Provenance DAG walk: a new frontmatter reader extracts `reflects:` /
      `sources:` from concern and knowledge pages; the concern → knowledge → bucket-file
      chain is walkable; a stamp whose recorded fingerprint differs from the live content
      hash flags the node and draws its edge red. Reuses `wiki` fingerprint resolution; no
      re-stamping.
- [ ] **SC13** — Artifact checklist complete: `serve` registered in
      `cliusage/cliusage.go` (flags `--port`, `--open`); `CLAUDE.md` workflow + registry
      mention; `README.md` + `docs/reference/` updated; `/atomic-help` cli topic row +
      tour stage updated; `atomic validate artifacts` passes; `make render` +
      `make -C atomic bundle` produce zero `git diff --exit-code`; signals refreshed.


## Approach

Decided in `docs/design/atomic-serve.md`: one `atomic serve` verb, a presentation-only
leaf package (`internal/serve/`) importing wiki + code-intel and imported by neither;
goldmark + chroma + mermaid render; htmx UI; Cytoscape.js + ELK graph; all assets
vendored via `go:embed`; scope resolution shared with `atomic code` via `realm.Resolve`.


## Checkpoints

File/area references ground in the verified seams from the evidence pass. Agent column is
a dispatch hint, not a hard roster.


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
| 9 | **Graph overlay** — vendor `cytoscape.min.js`+`elk.bundled.js`+`cytoscape-elk.min.js` (load order load-bearing) via `go:embed`; `/graph` global + local depth-1–2; 3 edge classes styled (md-link/wikilink/fingerprint-dashed); code sub-graph entered via repo node | `internal/serve/` (graph routes, JSON for cytoscape), vendored assets | builder | SC11 |
| 10 | **Provenance DAG** — frontmatter reader for `reflects:`/`sources:`; concern→knowledge→bucket-file walk; live-hash vs stamped mismatch → red edge + node flag; reuse `wiki` fingerprint resolution | `internal/serve/`, `wiki/stamp.go` resolution (resolveFingerprint:91), new frontmatter reader | builder | SC12 |
| 11 | **Artifact checklist + docs + parity** — cliusage flags; `CLAUDE.md` registry+workflow; `README.md`; `docs/reference/serve.md` (+ commands table); `/atomic-help` cli row + tour; `atomic validate artifacts`; `make render` + `make -C atomic bundle` clean; signals refresh | `cliusage.go`, `CLAUDE.md`, `README.md`, `docs/reference/`, `templates/commands/atomic-help.md`, `docs/reference/commands.md` | surgeon | SC13 |


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
- [ ] **FE-SC3** — System graph mode: the `[page | system]` toggle swaps the middle pane to
      the whole-realm Cytoscape/ELK graph (reusing the existing graph data); the right rail
      collapses; clicking a node returns to page view focused on it. The standalone `/graph`
      view is reachable only through this toggle, not a separate nav destination.
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
| FE3 | **System graph mode** — `[page|system]` toggle swaps middle to the realm graph (reuse `/graph/data`); node click → page view; rail collapses in system mode | `internal/serve/graphoverlay.go`, `layout.html`, `app.css` | builder | FE-SC3 |
| FE4 | **Code modal** — code node / `file:line` / source-link opens a modal: chroma source + code-intel relations (imports/exports/callers/callees via `codeexplorer`); clickable jumps; degrade to source-only | `internal/serve/codeexplorer.go`, `internal/serve/render.go`, `layout.html`, `app.css` | builder | FE-SC4 |
| FE5 | **Search dialog + page** — search is a command-palette dialog (`#search-modal`, opened by the top-bar trigger / `⌘K` / `/`) with the `md\|code` toggle + live results; selecting navigates (md→`#main-pane`, code→code modal); `Enter` / "view all" opens the dedicated `/search?q=&src=` page (`search_page.go`, shell-wrapped, `All\|Markdown\|Code` tabs) which composes the `/search/md` + `/code/search` fragments. `md` grep handler `search_md.go`; federated `codesearch.go` | `internal/serve/search_page.go`, `search_md.go`, `codesearch.go`, `layout.html`, `app.css`, `serve.go` | builder | FE-SC5 |
| FE6 | **Parity + docs** — render/bundle clean; `docs/reference/serve.md` + `/atomic-help` row reflect the Obsidian UI; signals refresh; full verify | `docs/reference/serve.md`, `templates/commands/atomic-help.md`, signals | surgeon | FE-SC7 |


## Open questions

None.


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Vendored JS (~5.3 MB: mermaid 3.24, elk 1.57, cytoscape 0.43) inflates the `atomic` binary | High (certain) | Accepted per design (graph is the point). Embed via `go:embed`; consider gzip-at-rest + serve decompressed only if binary size becomes a complaint. Documented, not silent. |
| Cytoscape+ELK load order wrong → silent "ELK is undefined" | Medium | CP9 spec pins the order `cytoscape → elk.bundled → cytoscape-elk`, then `cytoscape.use`. Verify the graph actually lays out, not just that the page loads. |
| `mdlink.ExtractLinks` diverges from `Linkify`'s fence handling → links matched inside code spans | Medium | CP4 reuses the existing fence-tracking internals rather than a fresh regex; test with fenced/inline-code fixtures. |
| Run scope is large (11 checkpoints) — partial completion | High | Commit-per-green: each checkpoint lands committed and independently valuable. Foundation (CP1–6) is usable without the code/graph layers. Report remaining checkpoints honestly. |
| Path-traversal / arbitrary file read via `/page/*` or `file:line` route | Medium | CP1/CP2: every served path is resolved against the scope root and rejected (404) if it escapes; never `os.ReadFile` an unvalidated request path. localhost bind limits blast radius. |
| `/atomic-help` hard rule missed — `serve` not discoverable | Medium | CP11 Verifies requires the cli topic row + tour; `CLAUDE.local.md` MISSING-scan catches it. |
| Release-please type mislabel hides the feature from the changelog | Medium | Ship as `feat:` — new user-visible verb, no breaking change. |


## Change log

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
