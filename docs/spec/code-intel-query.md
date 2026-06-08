# Code-intelligence engine — query core (part 4/5)

Part 4 of the 5-part code-intelligence engine port. **Umbrella:**
`docs/spec/code-intel-engine.md` (goal, roadmap, dependency DAG, authoritative
**reference appendix A–O**). **Design:** `docs/design/code-intel-engine.md`.

**Depends on:** parts 1–3 green — needs the resolved edge graph + FTS index.
**Blocks:** part 5 (surfaces) — the facade, CLI, and MCP adapters call these
query operations.

Brand bindings (from the umbrella): commands `atomic code <verb>`. Never emit the
reference implementation's product name.

## Scope

The structural/flow query operations over the resolved graph: graph traversal +
query manager (master CP17), search (CP18), and the context builder + formatter
(CP19).

**Contracts (authoritative, in the umbrella appendix):** I (graph traversal —
BFS batching, edge-priority sort, callers/callees/impact/path/deadcode/cycles), J
(search — query-parser fields, FTS query build + escaping, BM25 weights, 3-tier
FTS→LIKE→fuzzy, scoring helpers, `kindBonus`; the **full** search contract — part
1 used only the BM25-weights slice for its FTS-parity test).

## Success criteria

- [x] Graph queries (`callers`/`callees`/`impact`/`path`) correct on a fixture
      with known call structure; `impact` excludes `contains`.
- [x] FTS search returns results in the same rank order as the reference;
      field filters (`kind:`/`lang:`/`path:`/`name:`) work; the 3-tier
      FTS→LIKE→fuzzy fallback fires on a miss.
- [x] Context-builder markdown has the stable section headings, the JSON shape
      matches, and output is **reproducible** (serialization sorts by a stable
      key — Go map iteration is non-deterministic).

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **(master CP17) Graph traversal + query manager**: BFS/DFS with batched neighbor fetch + edge-priority sort, callers/callees/impact (exclude `contains`)/path/hierarchy/deadcode/cycles. | `internal/codeintel/graph`; ref `src/graph/{traversal,queries}.ts` (COPY); appendix I | callers/callees/impact/path correct on a known-structure fixture |
| 2 | **(master CP18) Search**: query parser (`kind:`/`lang:`/`path:`/`name:` fields), FTS query build + escaping, BM25 weights `(0,20,5,1,2)`, 3-tier FTS→LIKE→fuzzy, scoring helpers. | `internal/codeintel/search`; ref `src/search/*.ts`, `src/db/queries.ts` search fns (COPY); appendix J | Ranked results; field filters; fuzzy fallback fires on miss |
| 3 | **(master CP19) Context builder + formatter**: `findRelevantContext` multi-channel gather → BFS expand → diversity caps, `buildContext`, markdown + JSON format, call-paths, low-confidence marker. Serialization sorts by stable key. | `internal/codeintel/codectx`; ref `src/context/*.ts` (COPY format); appendix I (traversal reused) | Markdown has the stable section headings; JSON shape matches; output reproducible |

## Field-only queries (no free-text term)

A query that carries only field filters and no free-text remainder — e.g.
`kind:route`, `kind:function lang:go`, `lang:python` — must return the nodes
matching those filters. The FTS tier requires a MATCH term, so a field-only query
skips FTS/LIKE/fuzzy entirely and runs a **metadata-only listing**: fetch
candidates by `kind:` (`GetNodesByKind`) when a kind is set, else `GetAllNodes`;
then apply the remaining filters (`kind`/`lang`/`path`/`name`) in memory via the
shared `applyFilters` step; score without bm25 (`kindBonus` + path relevance +
exported bonus, plus a name-match bonus when `name:` is set), sort, and limit.
Results report a dedicated tier (`TierFilter`).

An empty query with **no** filters still returns no results (unchanged).

## Risks

Inherited from the umbrella: **R-F** (modernc vs node:sqlite FTS5/bm25 ranking
drift — CP2 asserts rank order on a known corpus; `ORDER BY score, nodes.id`
tiebreaker from appendix J; pin both SQLite versions). Stable-sort-on-serialize
(design §Go conventions) is load-bearing for reproducible context output.

## Change log

### 2026-06-06 — field-only queries return filtered nodes (metadata tier)

**What changed:** `Searcher.Search` now handles queries with field filters but no
free-text term (`kind:route`, `kind:function lang:go`, `lang:python`) via a
metadata-only listing path instead of falling through to LIKE/fuzzy on the literal
query string. Added a `TierFilter` tier. See the new "Field-only queries" section.

**Why:** `code search "kind:route"` (the obvious way to list routes) returned
nothing — and so did `kind:function` and every other pure-field query. Root cause:
when `ParsedQuery.FTSText` was empty the FTS tier was skipped and the LIKE tier ran
on `opts.Query` verbatim (`"kind:route"`), which matches no node name. Surfaced by
real-repo eval after framework route extraction landed (25 route nodes existed but
`kind:route` found none).

### 2026-06-06 — CP19 context builder + formatter implemented

**What changed:** New package `internal/codeintel/codectx`. Implements:
- `Builder.FindRelevantContext(ctx, query, Options) (types.Subgraph, string, error)` — FTS/LIKE/fuzzy
  search via `search.Searcher` → exact-name channel fallback → BFS callee (depth=1) + caller (depth=2)
  expansion via `graph.Manager` → diversity capping (`DefaultMaxPerFile=5`, `DefaultMaxPerKind=8`)
  with depth-priority sort (callees survive over callers when per-file budget is tight); returns
  combined `types.Subgraph` and source-tier string ("fts"/"like"/"fuzzy").
- `Builder.BuildContext(ctx, types.Subgraph, BuildOptions) (types.Context, error)` — renders
  `Content` string in markdown or JSON format; populates `NodeCount`, `EdgeCount`, `Source`,
  `Truncated`.
- Markdown formatter: 4 stable sections (`# Context: <query>`, `## Symbols`,
  `## Call paths`, `## Relationships`); heuristic edges marked `(heuristic)` in Relationships.
- JSON formatter: `JSONOutput` with nodes sorted by ID, edges sorted by composite key
  `source+\x00+target+\x00+kind`, roots sorted; `provenance` field on every edge.
- Reproducibility: `SubgraphSortedNodes` for node iteration, `sortEdges` for edge order,
  `sort.Strings` for roots — no raw map iteration in any serialization path; verified by
  10-round byte-identical output test.
- 9 test cases: GatherAndBFS, SourceTierPropagates, DiversityCap, StableHeadings,
  HeuristicEdgeMarker, JSON StableShape, EdgesSortedByCompositeKey, NodeCountEdgeCount,
  Reproducibility.

**Why:** CP19 completes the query-core part (CP17–CP19); unblocks part 5 (surfaces —
facade, CLI, MCP adapters).

### 2026-06-06 — CP18 search package implemented

**What changed:** New package `internal/codeintel/search`. Implements:
- `ParseQuery(raw) ParsedQuery` — parses `kind:`/`lang:`/`language:`/`path:`/`name:` field
  prefixes from a raw query string; invalid kind/lang values fall through to FTS text.
- `Searcher.Search(ctx, SearchOptions) ([]SearchResult, Tier, error)` — 3-tier pipeline:
  FTS (over-fetch 5×, rescore, filter) → LIKE (GetAllNodes, substring ladder) → fuzzy
  (bounded Damerau-Levenshtein, early-exit at maxDist). Each tier reports itself via `Tier`.
- `KindBonus(NodeKind) float64` — exact appendix-J table.
- `ScorePathRelevance(Node) float64` — deterministic depth-based path centrality.
- Final score = `−bm25 + kindBonus + pathRelevance + nameMatchBonus`; test-file −15
  downrank unless query contains "test"; ties broken by node ID (stable sort).
- 47 test cases covering all criteria: parse fields, kindBonus table, rank order, field
  filters, test-file downrank, LIKE tier fires on FTS miss, fuzzy tier fires on LIKE miss,
  maxDist boundary, bounded no-hang, deterministic tiebreaker, nameMatchBonus.

**Why:** CP18 is the prerequisite for CP19 (context builder uses Search) and the CP20+
facade (engine.SearchNodes delegates to this layer).

### 2026-06-06 — CP17 graph traversal + query manager implemented

**What changed:** `internal/codeintel/graph` package created. Implements `Manager`
with `GetCallees`, `GetCallers`, `GetImpactRadius`, `FindPath`, `GetTypeHierarchy`,
`FindDeadCode`, `FindCircularDependencies`. BFS batches neighbor fetch (one
`GetNodesByIds` call per frontier level, no N+1); edges sorted contains(0) <
calls(1) < other(2) before expansion. All operations verified on a known-structure
fixture (A→B→C call chain, class→interface heritage, 2-file import cycle, unexported
uncalled function).

**Why:** CP17 is the prerequisite for CP19 (context builder) and the CP20+ facade.
The `Manager` struct is the call target for those layers.

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP17–CP19 (graph traversal,
search, context builder) from `docs/spec/code-intel-engine.md`. The full search
contract (appendix J) is owned here; part 1 references only its BM25-weights
slice for FTS parity.

**Why:** Split the 25-checkpoint monolith into five dependency-ordered parts.
