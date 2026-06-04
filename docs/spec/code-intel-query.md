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

- [ ] Graph queries (`callers`/`callees`/`impact`/`path`) correct on a fixture
      with known call structure; `impact` excludes `contains`.
- [ ] FTS search returns results in the same rank order as the reference;
      field filters (`kind:`/`lang:`/`path:`/`name:`) work; the 3-tier
      FTS→LIKE→fuzzy fallback fires on a miss.
- [ ] Context-builder markdown has the stable section headings, the JSON shape
      matches, and output is **reproducible** (serialization sorts by a stable
      key — Go map iteration is non-deterministic).

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **(master CP17) Graph traversal + query manager**: BFS/DFS with batched neighbor fetch + edge-priority sort, callers/callees/impact (exclude `contains`)/path/hierarchy/deadcode/cycles. | `internal/codeintel/graph`; ref `src/graph/{traversal,queries}.ts` (COPY); appendix I | callers/callees/impact/path correct on a known-structure fixture |
| 2 | **(master CP18) Search**: query parser (`kind:`/`lang:`/`path:`/`name:` fields), FTS query build + escaping, BM25 weights `(0,20,5,1,2)`, 3-tier FTS→LIKE→fuzzy, scoring helpers. | `internal/codeintel/search`; ref `src/search/*.ts`, `src/db/queries.ts` search fns (COPY); appendix J | Ranked results; field filters; fuzzy fallback fires on miss |
| 3 | **(master CP19) Context builder + formatter**: `findRelevantContext` multi-channel gather → BFS expand → diversity caps, `buildContext`, markdown + JSON format, call-paths, low-confidence marker. Serialization sorts by stable key. | `internal/codeintel/codectx`; ref `src/context/*.ts` (COPY format); appendix I (traversal reused) | Markdown has the stable section headings; JSON shape matches; output reproducible |

## Risks

Inherited from the umbrella: **R-F** (modernc vs node:sqlite FTS5/bm25 ranking
drift — CP2 asserts rank order on a known corpus; `ORDER BY score, nodes.id`
tiebreaker from appendix J; pin both SQLite versions). Stable-sort-on-serialize
(design §Go conventions) is load-bearing for reproducible context output.

## Change log

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP17–CP19 (graph traversal,
search, context builder) from `docs/spec/code-intel-engine.md`. The full search
contract (appendix J) is owned here; part 1 references only its BM25-weights
slice for FTS parity.

**Why:** Split the 25-checkpoint monolith into five dependency-ordered parts.
