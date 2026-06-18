# atomic serve

`atomic serve` starts a local, read-only HTTP server that renders a wiki realm (or a single repo) as a navigable, Obsidian-style knowledge graph in the browser. It is a presentation layer only — it reads what already exists (wiki summaries, code-intel indexes, bucket manifests) and never writes, re-indexes, or re-stamps anything.


## Usage

```bash
atomic serve [path] [--port <N>] [--open]
```

| Flag | Default | Description |
|------|---------|-------------|
| `path` | current directory | Wiki realm root, member repo, or bare repo. A relative path is resolved to an absolute one. |
| `--port` | `4500` | Port to bind on `127.0.0.1`. `--port 0` picks a free port and prints the chosen one. |
| `--open` | off | Open the browser automatically after the server starts (best-effort; never fatal). |

The server shuts down cleanly on SIGINT. It prints the URL on start.


## Scope resolution

`atomic serve` resolves scope from the `path` argument (or `cwd`) using the same `realm.Resolve` logic as `atomic code`:

| Detected shape | Scope | What is served |
|---------------|-------|----------------|
| Registered `<wikis>` realm root | **Realm** | Full realm: nav tree (Realm / Repos / Concerns / Knowledge / Buckets / External), the whole-system graph, federated code search across all members. The landing page is the realm index (`wiki/index.md`). |
| Inside a member repo with a wiki | **Member** | Single-repo nav, pages, and code intelligence; realm chrome absent. |
| Bare repo with a code index but no wiki | **Repo** | Pages and code intelligence, no wiki chrome. The landing page is the repo `README.md`. |

The resolved scope is shown in the top-bar breadcrumb.


## The interface

The UI is a single persistent shell — navigating never reloads it; only the focused content and its surrounding context change.

- **Top bar** — a breadcrumb (`realm › member › page`) and a search trigger that opens the command-palette dialog (`⌘K`).
- **Left nav** — the collapsible tree (`/nav`).
- **Middle** — the focused page, or the whole-system graph. A `[ page | system ]` toggle switches between them.
- **Right rail** — three slots tracking the focused page: its local link graph, its outbound links, and its inbound links (backlinks).
- **Code modal** — a source file opens in an overlay: highlighted source on the left, code intelligence on the right.

### Page view (the default)

The middle pane renders the focused markdown page; the right rail shows that page's context. Navigating to another page updates the content, the breadcrumb, and all three rail slots in one round-trip.

Markdown renders via [goldmark](https://github.com/yuin/goldmark) (GitHub Flavored Markdown) with [chroma](https://github.com/alecthomas/chroma) syntax highlighting. Fenced ` ```mermaid ` blocks render client-side via vendored `mermaid.min.js`.

In-page links are resolved server-side against the realm root, not the browser's current URL. Three link forms are supported:

- **Bundle-relative** (`/path/to/page.md`) — a leading slash is resolved against the served root (OKF §5.1 recommended form). When the target exists under root it becomes an in-shell navigable route, exactly like a relative link. This is how cross-links between OKF concept pages (`knowledge/`, `concerns/`) render.
- **Relative** (`../concerns/x.md`, `./other.md`) — resolved from the source page's directory.
- **Obsidian wikilinks** (`[[page]]`, `[[page|alias]]`) — resolved by nearest-then-alphabetical rule; kept for back-compat tolerance.

In all three cases, resolved routes become `/page/<relpath>` for markdown pages or folders (navigated via htmx, so clicking never reloads the shell or loses your place), or `/file/<relpath>` for source files (which open the code modal). External `http(s)` links open in a new tab; in-page `#anchor` links and any link that would escape the realm are left untouched. A link to a page that does not exist still routes through `/page/`, so it lands on the in-shell "not found" fragment rather than a full-page navigation to a dead URL.

### Right rail (`/rail/<page>`)

For the focused page, a single request populates four slots:

- **Properties** — YAML frontmatter key-value pairs, rendered as a table at the top of the rail. Scalar values pass through as-is; list values are comma-joined. The slot is hidden when no frontmatter is present. A frontmatter `resource:` key (or any property whose value is an `http(s)://` URL) is rendered as a clickable link — the OKF recommended form for surfacing an underlying asset or canonical source.
- **This-page graph** — a depth-1 local link graph rendered as a compact Cytoscape mini-graph (data from `/graph/data?node=<page>&depth=1`). Nodes are colored by type, using the same hybrid resolver as the system graph.
- **OUT links** — outbound links the page contains, with broken / ambiguous / external annotations. Links to source files open the code modal.
- **IN links** — backlinks; an orphan note appears when nothing links to the page.

Links and backlinks come from `mdlink.ExtractLinks`, which parses markdown links `[text](path)` and Obsidian wikilinks `[[page]]` / `[[page|alias]]` (fenced code spans excluded). Wikilinks resolve by a nearest-then-alphabetical rule; ambiguous resolutions are surfaced.

### System graph

The `[ page | system ]` toggle swaps the middle pane to the whole-realm graph (Cytoscape + ELK, fed by `/graph/data`) and collapses the right rail. Tapping a node returns to page view focused on that node.

- Nodes are colored by OKF concept type. The type is resolved via a hybrid strategy: frontmatter `type:` (title-case values `Knowledge`, `Concern`, `Repo Summary` mapped to short lowercase classes) takes priority, then path-convention fallback (`wiki/repos/` → `repo`, `wiki/concerns/` → `concern`, `wiki/knowledge/` → `knowledge`, `wiki/.buckets/` → `bucket`, `http(s)://` hrefs → `external`), then `page` as a default.
- A **type legend** appears below the graph. Each chip shows the type name and its count of visible nodes. Clicking a chip toggles that type's nodes on or off, so you can isolate concerns, or hide repos to see only knowledge pages and the edges between them.
- Edges are drawn in three classes: markdown links, wikilinks, and fingerprint/provenance links (dashed). A provenance edge whose recorded fingerprint differs from the live content hash is drawn red — the drift signal from the `reflects:` / `sources:` chain.
- Code edges are per-member sub-graphs; no cross-repo edges are drawn (federation, not merging).

The vendored graph scripts (`cytoscape.min.js`, `elk.bundled.js`, `cytoscape-elk.min.js`) load once in the shell, in that load-bearing order, and power both the rail mini-graph and the system view.

### Code modal

Clicking a source-file link — in page content, in the rail, or in a search/code result — opens a modal over the dimmed page:

- **Source** (`/file/<path>`) — chroma-highlighted, per-line anchors; a `file:line` reference scrolls to the line.
- **Code intelligence** (`/code/file?path=<path>`) — the symbols defined in the file, each a chip that drills into its callers, callees, and impact radius. In a realm the file path is mapped to its owning member, that member's own index is opened, and it is queried with the member-relative path; the drill-down links carry a `member=` parameter so callers/callees/impact stay within the same member's index. When the file's repo is not indexed, the modal shows source only with a brief note.

The modal closes on `Esc`, the close button, or a backdrop click.

### Search (command-palette dialog + `/search` page)

Search is a dialog, not an inline dropdown. The top-bar trigger — or `⌘K` / `Ctrl K`, or `/` when the focus isn't a text field — opens a centered command palette holding the `md | code` toggle and a debounced live-results list. The toggle flips the source:

- **md** (`/search/md?q=`) — a literal, case-insensitive grep over the served markdown files. Results are `file:line` matches with a snippet; selecting one loads that page. The query is only ever a search substring, never a path.
- **code** (`/code/search?q=`) — the federated symbol search (below). Selecting a result opens the code modal at that symbol's file.

Pressing `Enter` (or "View all results") opens the dedicated **`/search?q=&src=`** page: a full, URL-addressable, shell-wrapped results view with `All | Markdown | Code` tabs — quick-jump in the dialog, browse everything on the page. The dialog closes on `Esc` or a backdrop click.

The page **streams** results over Server-Sent Events (`/search/stream`): the markdown block arrives first (a fast local grep), then each realm member's code results stream in as that member's index query finishes. Members are searched **concurrently** — a bounded goroutine pool — so one large repo doesn't hold up the rest, and a terminal event clears the loading spinner. While anything is in flight a spinner shows; when a realm has no code index, the code section says so (`run atomic code index`) instead of sitting blank.

### Federated code search (`/code/search?q=…`)

Resolves realm members, opens each member's index with `engine.NewWithDBPath`, calls `SearchNodes`, and groups results under `[key]` headers. A member with no index is skipped with a visible "not indexed" note rather than aborting other members. `only` and `exclude` query params filter the member set. In repo or member scope the search targets the single index.

Members come from two sources, unioned: realm **federation** (a `<code-index>` block in CLAUDE.md plus per-member dbs at `<realm>/.atomic/<key>.db`) and per-member **self-indexes** — a member indexed the natural way, `cd <member> && atomic code index`, which writes `<member>/.claude/.atomic-index/atomic.db`. So code search (and the code modal) work in any wiki realm whose members were individually indexed, with no federation setup. Result links are prefixed with the member's realm-relative path so they resolve through the realm's `/file/` route. Members searched concurrently — see the streaming search above.

### Code intelligence routes

The code modal and code search build on the per-repo query routes, each composing existing `engine` queries (no new analysis):

- `/code/node` — node detail (signature, file:line, metadata) from `engine.GetNode`.
- `/code/callers`, `/code/callees`, `/code/impact` — rendered from `engine.Subgraph` as clickable edge chips; edge kind shown (`calls / references / writes / contains`).
- `/code/files` — the indexed file list.
- `/code/file?path=` — the symbols defined in one file (`engine.GetNodesInFile`); the modal's intelligence pane.
- `/code/schema` — for indexes containing `table` / `view` nodes: tables and views with their `column` children, an FK graph from `references` edges, and a writers-vs-readers split from `writes` edges. Derived from graph nodes and edges — there is no `atomic code schema` verb.

### External-link registry (`/external`)

Lists every outbound `http(s)` URL across the realm: the URL, the source pages that cite it, and a first-seen date (git history when available, file mtime otherwise). Reachable from the nav External group.

### Status (`/status`)

The realm-health view, reachable but no longer the landing page. Renders `wiki.Stale` / `wiki.CheckStaleness` (DRIFT / STALE / STALE bucket) plus aggregate code-index health (worst severity across member repos, naming only repos that need action). No new staleness computation — staleness also surfaces ambiently as badges in the nav. `/healthz` is a separate plain-text liveness probe.


## Static assets

All assets (htmx, base CSS, the html/template layout, vendored JS) are embedded via `go:embed` and served from memory. No network fetch, no file dependency outside the binary, no build step.


## Security

- Binds to `127.0.0.1` only. Not an API surface; no auth.
- Every served path is resolved against the scope root and rejected (404) if it escapes via path traversal (`../` or absolute). `os.ReadFile` is never called on an unvalidated request path. The markdown-search query is treated purely as a substring, never a path.
- No write operations of any kind. Serve observes; mutation stays in `/refresh-wiki`, `atomic code index`, and `atomic wiki` subcommands.
