# atomic serve

`atomic serve` starts a local HTTP server that renders a wiki realm (or a single repo) as a navigable, Obsidian-style knowledge graph in the browser. It is a presentation layer for that content: it reads what already exists (wiki summaries, code-intel indexes, bucket manifests) and never writes, re-indexes, or re-stamps any of it. The one exception is the bus chat page (`/bus`), which operates `atomic bus` rooms and stays loopback-only regardless of `--host`. See Bus chat below.

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
| Inside a member repo with a wiki | **Member** | Realm nav and realm chrome, rooted at the realm root; code search targets the single member's index. |
| Bare repo (code index optional) | **Repo** | Pages, plus code intelligence when an index exists — docs-only when absent. No wiki chrome. The landing page is the repo `README.md`. |

The resolved scope reaches the client as `GET /api/status`'s `isRealmScope` field and `GET /api/nav`'s `scope` field, consumed by the Status page and the nav tree.


## What gets enumerated

The nav tree, markdown search, the docs graph, and the change-detection fingerprint all walk the same tree and share one exclusion rule.

Skipped: dot-directories, `node_modules`, `vendor`, `tmp`, and anything git ignores. `.claude` is the deliberate exception to the dot-directory rule — it holds servable project docs that wiki links cite across members, so skipping it would break valid links.

Git-ignored is not on its own enough to skip, because a realm gitignores its member repositories: from the realm's point of view they carry their own history and are disposable, yet they are the very thing a realm exists to serve. So an ignored directory is still walked when it is a repository in its own right — when it holds a `.git` **directory**. What that leaves out is a second copy of content already reachable somewhere else:

| Ignored path | `.git` entry | Enumerated |
|---|---|---|
| a realm's member repo | directory | yes — it is its own repository |
| `.claude/worktrees/<branch>/` | file | no — a second checkout of the same repo |
| `bin/`, build mirrors, caches | none | no |

A linked worktree carries `.git` as a file pointing back at the repository it duplicates, which is what tells it apart from a member repo. Before this rule, a repo with eight worktrees showed every doc nine times in nav, search, and the graph.

This governs enumeration only. An ignored file still renders when you navigate to it by path — it simply does not show up in a listing, a search result, or the graph.

## The interface

The UI is a single persistent shell — navigating never reloads it; only the focused content and its surrounding context change.

- **Top bar** — a breadcrumb, a search trigger (`⌘K`), a network-view button that routes to `/graph`, and a light/dark theme toggle.
- **Left nav** — the collapsible folder tree.
- **Middle** — the focused page, or graph mode, which holds the whole-realm docs graph and a per-repo code graph behind a nested Docs | Code toggle.
- **Right rail** — four slots tracking the focused page: its frontmatter properties, its local link graph, its outbound links, and its backlinks.
- **Code modal** — a source file opens as an overlay: highlighted source on the left, code intelligence on the right.

### Page view (the default)

The middle pane renders the focused markdown page; the right rail shows that page's context. Navigating to another page updates the content, the breadcrumb, and all four rail slots at once.

Markdown is GitHub Flavored, rendered server-side with syntax highlighting. Fenced ` ```mermaid ` blocks render as diagrams.

In-page links are resolved against the realm root, not the browser's current URL. Three link forms are supported:

- **Bundle-relative** (`/path/to/page.md`) — a leading slash is resolved against the served root (OKF §5.1 recommended form). When the target exists under root it becomes an in-shell navigable route, exactly like a relative link. This is how cross-links between OKF concept pages (`knowledge/`, `concerns/`) render.
- **Relative** (`../concerns/x.md`, `./other.md`) — resolved from the source page's directory.
- **Obsidian wikilinks** (`[[page]]`, `[[page|alias]]`) — resolved by nearest-then-alphabetical rule; kept for back-compat tolerance.

A markdown page or folder navigates in-shell, so clicking never reloads the app or loses your place; a source file opens the code modal instead. External `http(s)` links open in a new tab, and `#anchor` links or anything that would escape the realm are left untouched. A link to a page that does not exist still routes in-shell, landing on a "not found" view rather than a dead URL.

### Right rail

Four panels track the focused page:

- **Properties** — frontmatter as a table, hidden when the page has none. A `resource:` key, or any property whose value is a URL, renders as a clickable link.
- **This-page graph** — the page's depth-1 link neighborhood as a compact mini-graph, colored by type.
- **OUT links** — outbound links, annotated broken / ambiguous / external. Links to source files open the code modal.
- **IN links** — backlinks, with an orphan note when nothing links here.

Links come from both markdown links `[text](path)` and Obsidian wikilinks `[[page]]` / `[[page|alias]]`, ignoring fenced code. A wikilink resolves nearest-then-alphabetical, and an ambiguous resolution is surfaced rather than silently picked.

### Graph mode

The `[ page | system ]` toggle swaps the middle pane into graph mode and collapses the right rail. Graph mode holds two views behind a nested **Docs | Code** toggle: the whole-realm docs graph, and a per-repo code graph.

Both views run on the same GPU-simulated engine, so everything below behaves identically in each; only the data differs.

The layout is a continuous physics simulation, not a one-shot pass. It settles and pauses on first open, and the settled positions are cached, so reopening an unchanged graph replays the same layout instantly with no visible motion. The code graph's cache is keyed to that repo's index fingerprint, so re-indexing never replays a stale layout against fresh data.

**Interaction is Shift-gated.** With no modifier the pointer is a camera: drag pans, even over dense clusters, scroll zooms, and a click on a node opens it. Holding Shift makes the graph itself interactive, so hovering highlights a node's neighborhood and shows a preview card, and dragging moves the node and saves its new position. A corner hint names the gesture.

- **Shift-click pins** a node's neighborhood highlight so it survives mouse-out and releasing Shift, which is what makes studying a node's relationships hands-free. A second Shift-click opens the node. The pin clears on a background click, on pinning another node, or when the legend hides that node's type.
- **The legend filters.** Each chip names a type and its count of visible nodes, and clicking one toggles that type off. Hide repos to see only knowledge pages and the edges between them.
- **Labels and sizes track zoom.** Labels fade in as you zoom in and out as you zoom out, and node size scales with how connected a node is, shrinking toward a floor as you pull back, so a fitted view of a dense graph reads as structure rather than a solid mass. A hovered node always shows its label.

What differs between the two:

| | Docs graph | Code graph |
|---|---|---|
| Nodes | pages, colored by concept type | symbols, colored by kind |
| Edges | markdown links, wikilinks, provenance (dashed) | `contains` (faint), `calls`, `imports`, all others as one tier |
| Shift-hover card | title, description, snippet | name, kind, `file:line` |
| Opening a node | the page, inline over the dimmed graph | the code-explorer view for that symbol |
| Scope | the whole realm at once | one repo, chosen with a member picker |

A page's concept type comes from frontmatter `type:` first, then a path convention (`repos/`, `concerns/`, `knowledge/`), then `page` as the default. A provenance edge whose recorded fingerprint no longer matches the live content is drawn red — the drift signal from the `reflects:` / `sources:` chain.

Opening a docs node shows the page in a modal over the dimmed graph rather than navigating away, with an "Open full page →" button for when you want more context; graph state survives throughout. The selected view lives in the URL, so a link to a specific graph reopens that same one. In a realm the selected member is the browser-held repo pick shared with Schema and Plans (see Plans below), never part of the URL.

There is one code graph per repo and no merged cross-repo graph, the same federation-not-merging boundary federated code search follows. A repo with no index shows a message naming `atomic code index` rather than an empty pane.

**WebGL2 is required** for graph mode, in both views. Without it the toggle says so instead of showing a blank pane or a stuck spinner. The rail's mini-graph needs no WebGL2 and works in any browser.

### Code modal

Clicking a source-file link — in page content, in the rail, or in a search/code result — opens an Ark UI `Dialog` over the dimmed page:

- **Source** (`GET /api/file/<path>`) — chroma-highlighted, per-line anchors; a `file:line` reference scrolls to the line.
- **Code intelligence** (`GET /api/code/file?path=<path>`) — the symbols defined in the file, each a chip that drills into its callers, callees, and impact radius. In a realm the file path is mapped to its owning member, that member's own index is opened, and it is queried with the member-relative path; the drill-down links carry a `member=` parameter so callers/callees/impact stay within the same member's index. When the file's repo is not indexed, the modal shows source only with a brief note.

Intel-pane drill actions push onto the modal's back-stack; Back pops the stack and re-syncs the source pane to the popped entry's file/line, deduping same-file hops (scroll-to-line only, no re-fetch). The modal closes on `Esc`, the close button, or a backdrop click, which clears the stack.

### Plans

The fifth `IconRail` mode, with no scope gate. `/plans` lists every slug's committed docs (`docs/design/<slug>.md`, `docs/spec/<slug>.md`) and its uncommitted scratchpad bundle, aggregated across every git worktree of the repo — a checkout elsewhere on disk still counts, since worktrees are enumerated with `git worktree list --porcelain` rather than a glob over the conventional path. A root that is not a git repository, such as a realm root that is a plain directory, counts as one checkout of its own, so its `docs/design`, `docs/spec`, and scratchpad still appear. In realm scope, a picker on the page's own title line — repo scope renders none — switches between one member's view at a time; there is no cross-member union. The realm root appears under the realm's name.

The pick is held by the browser, not the URL. One value, shared with the Graph and Schema pickers, is persisted in the `atomic-member` cookie, keyed inside it by the served realm or repo (`realm:<name>` or `repo:<name>`), so a rail click, a reload, or a serve of the same realm on another port all land on the same repo, and a different realm served later on the same port reads its own entry. Only an explicit pick writes it; a page that cannot show the picked member, such as Graph when that member has no code index, falls back to its first member without changing the pick.

The two halves of a row collapse differently, because only one of them ever repeats identically. A committed doc dedups by content SHA: several worktrees holding byte-identical bytes render as one version, labelled by the checkout on the repository's default branch when one holds it, else by the most recently modified. A scratchpad bundle never dedups — one checkout, one bundle, attributed to the worktree that holds it, because nothing merges it.

Opening a slug (`/plans/:slug/*`) renders one file in the middle pane; the right rail carries a version picker, a navigation over the bundle's parts (design, spec, brief, state, followups, findings, options — only the ones present), and the open file's own headings, mirroring how the right rail works for any other page.

The version picker is a type-ahead over checkout names, not a tab strip — a repo with a dozen worktrees would wrap a tab strip into uselessness, and it renders nothing at all when a file has exactly one version. With no picked name, a file opens at its newest version by mtime; the merged version keeps its label and its filled dot and is one keystroke away. A picked name persists as you move between files and is re-resolved against each file's own version set. A bundle file that lives in only one checkout — a `findings/` note from a swarm run on another branch — still opens from any selection: it renders at its newest version by mtime, and the picker updates to name that checkout rather than the request being refused.

Picking a version is a property of reading a file, not of viewing the list — there is no worktree selector above the row list and no page-level version control.

```mermaid
flowchart TB
    accTitle: How the Plans page resolves a version when the sticky selection does not hold a file
    accDescr: Opening a file whose selected checkout does not have it renders that file's default version instead of refusing, and moves the picker to name the checkout now on screen.
    A["reader opens a file<br/>selected checkout = W"] --> B{"does W hold<br/>this file?"}
    B -->|yes| C["render W's version<br/>picker still reads W"]
    B -->|no| D["render this file's default:<br/>newest by mtime"]
    D --> E["selection := that checkout<br/>picker updates to say so"]
```

Navigation always wins; the selection yields rather than blocks.

The top bar says where the open file lives. On `/plans/:slug/*` in realm scope the breadcrumb reads `plans › <member> › <slug> › <file>`, and after the file a muted `<branch> · <path>` names the checkout on screen: the path is relative to the served root, absolute when the worktree lives outside it. It comes from the same resolution the version picker uses, so the two cannot disagree.

A bundle file renders by its classified kind: `markdown` through the existing markdown pipeline, `html` (an `atomic-visual-options` artifact) inside an `<iframe>` sandboxed with `allow-scripts` alone — the mock runs its own scripts in an opaque origin and is never injected into the app's own document or stylesheet — and `file` as a download link with no inline preview.

`⌘K` gains a third `source: "plans"` tab alongside `md` and `code`, filtering the already-fetched `/api/plans` payload by title and description client-side; there is no separate plans search endpoint.

| Route | Serves |
|-------|--------|
| `GET /api/plans[?member=]` | One row per slug — the doc version sets and the attributed scratchpad bundle. No file content travels in this response. |
| `GET /api/plans/page?worktree=<id>&path=<relpath>[&raw=1]` | A single doc or bundle file, resolved through a worktree id issued by the aggregator. Without `raw`, the same rendered HTML-in-JSON shape `/api/page` returns; with `raw=1`, the file's own content-type and raw bytes. |
| `GET /api/plans/members` | The member list backing the realm picker — declared and wiki-scanned members, including one with no code index, plus the realm root itself. |

Cross-worktree reads never widen `safeResolve`'s allowed-root set. The client sends an opaque worktree id and a relative path, never a filesystem path, so nothing in the request can influence which roots are reachable; an unknown or stale id is rejected. Every `raw=1` response carries `Content-Security-Policy: sandbox`, with `allow-scripts` added for kind `html` so a bundle mock can run, and its content-type is decided by the aggregator's classified `kind` alone — a sniff may narrow a non-HTML type further but is clamped before it can promote anything to `text/html` or an XML type. The same origin serves the unauthenticated `/api/bus/*` and `/api/code/index` write routes, and a sandboxed frame on the serving machine passes their loopback gate, so every POST route refuses a browser request whose `Origin` is not the server's own (an opaque-origin frame sends `Origin: null`) or whose `Sec-Fetch-Site` is not `same-origin`. CLI callers send neither header and are unaffected.

### Search

`⌘K` (or `Ctrl K`, or `/` when focus is not a text field) opens a command palette with an `md | code | plans` toggle:

- **md** — a literal, case-insensitive search over the served markdown. Results are `file:line` matches with a snippet; selecting one loads that page. The query is only ever a substring, never a path.
- **code** — symbol search across the code index. Selecting a result opens the code modal at that symbol's file.
- **plans** — a client-side filter over the slugs the Plans view already holds, by title, description, and slug. Selecting one opens that slug. There is no plans search endpoint; the tab exists so plans never pollute md or code results.

`Enter` opens the full `/search?q=&src=` page: URL-addressable, with `All | Markdown | Code` tabs. Use the palette to jump, the page to browse.

Results stream in rather than landing at once. The markdown block arrives first, then each realm member's code results as that member's index finishes, searched concurrently so one large repo does not hold up the rest. A realm with no code index says so and names `atomic code index` as the fix, instead of sitting blank.

### Federated code search

In realm scope, code search spans every member and groups results under `[key]` headers. A member with no index is skipped with a visible "not indexed" note rather than aborting the others, and `only` / `exclude` filter the member set. In repo or member scope it targets the single index.

Members come from two sources, unioned: realm federation (a `<code-index>` block in CLAUDE.md, with per-member dbs under `<realm>/.atomic/`) and per-member self-indexes, written by a plain `cd <member> && atomic code index`. Code search and the code modal therefore work in any realm whose members were indexed individually, with no federation setup at all.

### SQL schema view

For an index holding tables and views, `/code/schema` renders each with its columns, its foreign-key sources, and the routines that write to it. It is derived from the graph rather than computed on request, which is why there is no `atomic code schema` verb to run.

### External links

The nav's External group lists every outbound `http(s)` URL across the realm: the URL, the pages that cite it, and a first-seen date (git history when available, file mtime otherwise).

### Status

The realm-health view reports wiki staleness (DRIFT / STALE / STALE bucket) alongside code-index health, naming only the repos that need action. Staleness also shows ambiently as badges in the nav, so this page summarizes rather than being the only signal. `/healthz` is a separate plain-text liveness probe.


## Bus chat

`/bus` operates `atomic bus` rooms from the browser: watch a room's traffic live, speak into it as the operator, end a member's session, close a room, and open the Claude Code session behind any member. The page is titled **Message Bus**, because what it shows is a chat; `bus` alone named the transport rather than the thing on screen. The CLI verb and the Go package are still `bus`.

The room list polls `GET /api/bus/rooms` and shows each room's member count and halted state. Opening a room backfills the transcript from the room's durable log (`GET /api/bus/log`), then follows a live `GET /api/bus/tail` Server-Sent Events stream, deduplicated by envelope id. Each message shows its sender, its kind, and either its addressees or `fyi` for a room-wide status message.

The composer sends as a web member named by position, the same `<realm>-<repo>-web` naming `atomic where` reports, with `kind: human`, so halt blocks agents and never this member. Typing `@` opens a dropdown of the room's members; picking one, or completing a mention with a space, turns it into a removable chip. The textarea grows as you type. Enter sends, Shift+Enter inserts a newline. Halt and resume buttons set and clear the room's halt flag.

Opening a channel with no daemon running starts one, the same auto-spawn `atomic bus join` triggers from a terminal. The read-only requests (`status`, `rooms`, `who`, `log`, `tail`) never spawn a daemon: with none running they report an empty or not-running state instead.

### Ending a session, closing a room

Two controls stop listeners rather than pause them, so both confirm first.

The `×` on a member's chip (`POST /api/bus/end`) evicts that member: it delivers one closing envelope to that member's own stream, drops the membership, and closes the stream, which is what stops the agent's `Monitor`. The room and everyone else in it carry on. The operator's own chip carries no control — cutting your own listener from the page you are using has no use.

**Close** (`POST /api/bus/close`) ends the room for everyone: every listener is closed and the room is dropped. Unlike halt, there is no resume — the room is gone, and reopening it means joining again.

The eviction envelope reaches only the evicted member. `closing` is read as "the last envelope this stream will ever see", so delivering it to a peer that is staying would leave that peer primed to treat the next daemon restart as a shutdown and stop reconnecting. Peers learn of the departure from the roster. The room log still records the eviction, so it is auditable after the fact.

That envelope alone cannot carry the guarantee, because it travels through the member's bounded channel — and the agent most likely to be evicted, one whose reader has stalled, is exactly the one whose buffer is full. The envelope would be dropped, `recv` would see a bare close, and it would reconnect. So the daemon also refuses a `recv` from an evicted session until it rejoins. Rejoining is the way back in.

Both controls also clear the persisted roster in `~/.atomic/bus.json`, the same second step `atomic bus close` performs. Without it the daemon's next start replays that file and restores what was just removed: an evicted member would reappear in the roster with a dead listener, and a closed room would come back.

### Session rail

The right rail on `/bus` lists the room's members, each with its `kind` and staleness, plus a chip for its Claude Code session when one is found. Sessions are located by globbing `~/.claude/projects/*/<session-id>.jsonl`. Clicking a chip opens the transcript in a paginated modal, rendered as markdown through the same server-side pipeline as realm pages. The parser tolerates the drift of an internal, unversioned `.jsonl` format: unknown line types are skipped, and long blocks are truncated rather than breaking the render.

### Loopback only

Every `/api/bus/*` request is refused with 403 unless it comes from the loopback interface, regardless of `--host`. `--host 0.0.0.0` exposes the read-only realm and repo views to the LAN; it does not extend to bus chat, because sending or halting as the human operator is a capability the read-only viewer never had.

The gate checks the TCP peer address, not a header, so a request cannot claim to be local. It also cannot see through a reverse proxy: a proxy that terminates LAN connections and forwards them to `atomic serve` on `127.0.0.1` makes every forwarded request look local to the gate. Running such a proxy is a deliberate choice outside `atomic serve`'s threat model, not a gap in the gate.


## Live reload

While a browser tab is open, `atomic serve` reflects filesystem changes without a restart. The server keeps one realm snapshot (fingerprint, nav paths, link graph) and re-checks it with a stat-only walk every 10 seconds, but only while at least one tab is subscribed to the `/events` stream. With no tabs open the server does no periodic work.

When the realm changes, subscribed tabs receive a push carrying the new fingerprint and the list of changed files. The open page refetches its pane and rail only when the displayed file itself changed; the nav tree refreshes on any change. Scroll position is preserved as a natural property of React re-rendering content in place rather than swapping panes. The graph pane is not patched in place on a live-reload push — it reflects the change only on its next `/graph/data` or `/code/graph/data` fetch (re-entering the view, reloading, or the cosmos engine's own fingerprint-keyed layout cache invalidating on a subsequent load).

Files written moments ago are held back for a short quiet window before they are published, so a tool writing a file incrementally never renders a half-written page. A small indicator in the top bar shows the live connection state: live, reconnecting, or disconnected. Shutting the server down with tabs open exits cleanly and immediately.

Provenance hashing and the full graph JSON are warmed once in a background goroutine at startup and recomputed on demand when the realm fingerprint changes — never on the periodic check.


## Theme and typography

The top-bar sun / moon button switches between a light theme (warm paper, charcoal text, amber accent) and a dark one (warm charcoal, off-white text, amber accent). Your choice persists; a first visit follows the OS setting. Switching retints every visible graph immediately, with no page reload.

Headings are set in Newsreader, UI text in Inter, and code in a monospace stack with ligatures disabled, so a sequence like `--` or `->` never visually collapses the characters around it. In the right rail, a page's `type` property renders as a chip in the same color that type carries in the graph.

The whole UI is embedded in the binary and served from memory, so there is no runtime build step and no file dependency outside the binary. The one network call is for the webfonts; without network access the UI falls back to system fonts.


## Security

- Binds to `127.0.0.1` by default; `--host 0.0.0.0` opts into LAN exposure. Read-only either way with respect to realm and repo content, and never an auth surface for that content. The bus chat page is the exception: it refuses every non-loopback request regardless of `--host`. See Bus chat above.
- Every served path is resolved against the scope root and rejected (404) if it escapes via path traversal (`../` or absolute). `os.ReadFile` is never called on an unvalidated request path. The markdown-search query is treated purely as a substring, never a path.
- No write operations against realm or repo content. Mutating that content stays in `/refresh-wiki`, `atomic code index`, and `atomic wiki` subcommands. `POST /api/bus/*` is the one write surface serve exposes, and it targets the bus daemon's own state, not realm or repo content.
- Plain-HTTP/no-JS readability of `/page/*` no longer applies post-cutover — content requires the SPA to run; unmatched non-API GETs return the SPA shell (200) rather than 404, and traversal guards enforce at the `/api/*` boundary.
