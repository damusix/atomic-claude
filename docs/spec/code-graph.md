# Code graph view

## Goal

`atomic serve` renders each repo's code-intel symbol graph (nodes + resolved edges from that repo's `atomic.db`) as a cosmos.gl force-directed view in the existing graph pane, with the same motion policy, interaction grammar, and performance envelope as the docs system view. One graph per repo; realm mode selects the member repo.

## Non-goals

- Merged realm-wide code graph (per-repo only; no cross-repo edges exist in the data).
- Live re-index / file watching (separate planned feature).
- File-level aggregation lens, path filters, or query-scoped subgraphs (callers-of-X view).
- Any write path; the view is read-only over the index.
- Rendering for repos with no index beyond a clear "not indexed" message.
- New physics tuning pass — the system view's tuned constants carry over unchanged unless a success criterion fails because of them.

## Success criteria

- [ ] SC1 — **Index integrity**: `atomic code index` on this repo (which contains the two `.vitepress/theme/*.vue` files) exits 0 with no FK-constraint errors and produces `calls` edges > 0. A per-file store failure never prevents the resolution phase from running (per `IndexAll`'s documented contract). A ref whose owner node is absent from the store is skipped with the skip recorded in that file's `errors` column — never a fatal error. Regression-tested with a fixture that reproduces the dangling-owner ref.
- [ ] SC2 — **Data endpoint**: `GET /code/graph/data` returns the resolved member's full graph as JSON: a server-computed `fingerprint` (changes iff the index content changes), `nodes` (id, label, kind, file, line, language), `edges` (source, target, kind). `?member=<prefix>` resolves via the same member-resolution path as the sibling `/code/*` routes; single-repo mode needs no param. Unknown member or missing index returns a non-200 with a JSON error body. Covered by httptest against a fixture DB.
- [ ] SC3 — **Committed gate harness + system view unchanged**: a committed script under `scripts/` drives headless Chromium (Playwright) against a running `atomic serve` and checks, for a named view: mount with zero console errors, settle-then-pause within a time budget, drag reheat that resolves a forced overlap, IndexedDB cache replay with zero motion on reopen, and hover preview appearance. It exits non-zero on any gate failure and skips with a clear message when Playwright or a browser is unavailable (local tool; CI carries Go/unit coverage only). The harness passes against the docs system view *before* the shared-core refactor (baseline) and *after* it (regression gate).
- [ ] SC4 — **Code view renders at scale**: on this repo's index (~17.5k nodes, ~55k edges incl. `contains` and ~34k `calls`), the code view passes the same harness gates: mounts clean, settles and pauses, live-drag reheats locally, and reopening with an unchanged fingerprint replays the cached layout with zero motion.
- [ ] SC5 — **Styling legibility**: node color derives from a kind→group mapping (~8 visual groups) with a legend that filters groups client-side; `contains` edges render visually subordinate (fainter/thinner) to `calls`/`imports`; node size scales with degree inside the existing min/max window; DOM labels cap at 150 by degree with zoom fade. Theme toggle restyles without remount.
- [ ] SC6 — **Explorer integration**: hovering a node shows name, kind, `file:line` (signature when present); clicking opens the existing code-explorer node view for that symbol, member-aware. WebGL2-less browsers get the existing detect-and-message fallback.
- [ ] SC7 — **Per-repo selection**: in realm mode the code view offers a member picker listing code members (indexed state visible); switching members swaps the graph; single-repo mode shows no picker. View choice and member survive in URL state alongside the existing graph view state.
- [ ] SC8 — **Docs current + full verify**: `docs/spec/atomic-serve.md` amended (spec-currency) and `docs/reference/serve.md` updated to describe the code graph; `go test ./...` green from `atomic/`; culling unit test still 4/4; `atomic validate spec` clean.

## Approach

Symbol-level full graph per repo served by a lean `/code/graph/data` endpoint, rendered by the existing cosmos stack refactored into a shared core plus per-view profiles — see `docs/design/code-graph.md`.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Index integrity: ref-owner guard in `storeExtractionResult` + root-cause fix for the Vue extractor's dangling `FromNodeID` + regression tests | `atomic/internal/codeintel/indexer/`, `atomic/internal/codeintel/extraction/` (standalone Vue path), tests | atomic-implementer (mode: feature) | ~4 | SC1; `go test ./codeintel/...`; fresh `atomic code index` on repo yields calls edges |
| 2 | Graph export endpoint: engine bulk-read exposure + `/code/graph/data` handler + fingerprint | `atomic/internal/codeintel/engine/engine.go`, `atomic/internal/serve/codeexplorer.go`, new `codegraph.go` + `codegraph_test.go`, `serve.go` | atomic-implementer (mode: feature) | ~5 | SC2; httptest suite |
| 3 | Committed gate harness: headless-Chromium gates (mount, settle, drag/overlap, cache replay, hover) as a `scripts/` tool; baseline green against the current docs view | new `scripts/` harness file(s) | atomic-implementer (mode: feature) | ~2 | SC3 (harness half); baseline run green pre-refactor |
| 4 | Shared cosmos core extraction; system view behavior preserved | `atomic/internal/serve/assets/` (new core module, `system-graph.js` slimmed), `templates/layout.html` script wiring | atomic-implementer (mode: feature) | ~3 | SC3 (regression half): harness green on docs view post-refactor |
| 5 | Code view profile: fetch/adapter, kind→group palette + legend, edge-kind styling, degree sizing, labels, layout cache | `atomic/internal/serve/assets/` (new code profile), `templates/layout.html` script tag | atomic-implementer (mode: feature) | ~3 | SC4, SC5; harness gates on code view |
| 6 | UI wiring: Docs\|Code switcher, member picker (realm), URL view state, explorer hover/click hooks, not-indexed message | `atomic/internal/serve/assets/` (code profile + core touch-ups), `templates/layout.html`, `assets/app.css` | atomic-implementer (mode: feature) | ~4 | SC6, SC7; harness hover gate on code view; member picker verified manually against a realm serve (single-repo dev serve shows no picker) |
| 7 | Docs amendments + full verify | `docs/spec/atomic-serve.md`, `docs/reference/serve.md` | atomic-implementer (mode: surgical) | 2 | SC8 |

## Change tree

```
atomic/internal/codeintel/
  indexer/orchestrator.go          M  storeExtractionResult: dangling-owner ref guard → file errors column
  indexer/orchestrator_test.go     M  regression: dangling-ref file stores rest of its data; resolution still runs
  extraction/…(vue standalone)     M  stop emitting refs owned by nodes absent from the result
  engine/engine.go                 M  bulk graph read exposure (all nodes + all edges) for export
atomic/internal/serve/
  codegraph.go                     A  export handler: member-resolved engine → lean JSON + fingerprint
  codegraph_test.go                A  httptest: shape, member param, missing index, fingerprint stability
  codeexplorer.go                  M  CodeEngine interface gains bulk reads; route dispatch
  serve.go                         M  /code/graph/data registration
  assets/graph-core.js (name TBD)  A  shared cosmos mount/teardown, WebGL2 gate, motion policy, cache, drag, labels, legend
  assets/system-graph.js           M  docs profile over the shared core; public SystemGraph API unchanged
  assets/code-graph.js (name TBD)  A  code profile: fetch/adapt, kind→group palette, edge-kind styling, cache key, explorer hooks
  templates/layout.html            M  Docs|Code switcher, member picker, script tags
  assets/app.css                   M  switcher/picker styles
scripts/
  graph-gates.mjs (name TBD)       A  committed headless gate harness (Playwright): mount/settle/drag/cache/hover per view
docs/
  spec/atomic-serve.md             M  amendment: code graph view clause
  reference/serve.md               M  code graph section
```

## Outline

- `atomic/internal/codeintel/indexer/orchestrator.go`
  - `storeExtractionResult` — before inserting an unresolved ref, verify its owner node exists in this file's result (or the DB); on miss, skip the ref and append a note to the file record's `errors` column
- `atomic/internal/codeintel/extraction` (Vue standalone path)
  - the construct that emits refs with owner IDs never added to `result.Nodes` — emit the owner node or attribute the ref to an existing node (root-cause fix; exact site to be located from the two-Vue-file repro)
- `atomic/internal/codeintel/engine/engine.go`
  - `GetAllNodes` — facade pass-through to the DB bulk read
  - `GetAllEdges` — facade pass-through to the DB bulk read
- `atomic/internal/serve/codegraph.go`
  - graph-data handler — resolve member (same seam as `/code/*`), open engine, assemble lean JSON, compute fingerprint from index state (counts + last-indexed)
- `atomic/internal/serve/codeexplorer.go`
  - `CodeEngine` — interface gains the two bulk reads
- `atomic/internal/serve/assets/` shared core (new)
  - mount/teardown lifecycle — WebGL2 gate, cosmos creation, styling flush ordering, retheme
  - motion policy — settle-then-pause constants, drag reheat with repulsion boost, cache seed-and-pause replay
  - label overlay — degree-capped DOM labels with zoom fade
  - legend — group chips with client-side filtering
  - layout cache — IndexedDB store, format-versioned, full-coverage hit gate, namespaced keys
- `atomic/internal/serve/assets/` docs profile (`system-graph.js`)
  - data source + adapter for `/graph/data`; OKF type palette; existing `window.SystemGraph` API preserved
- `atomic/internal/serve/assets/` code profile (new)
  - data source + adapter for `/code/graph/data`; kind→group palette; edge-kind styling (`contains` subordinate); cache key `code:<member>:<fingerprint>`; hover/click handlers into the code explorer
- `atomic/internal/serve/templates/layout.html`
  - graph pane switcher — Docs | Code segmented control; member picker rendered in code mode when realm
- `scripts/` gate harness (new)
  - per-view gate runner — launches headless Chromium against a running serve instance; gates: clean mount, settle-then-pause budget, drag/overlap resolution, cache replay zero-motion, hover preview; non-zero exit on failure; skip-with-message when Playwright/browser absent

## Flows

1. **Open code graph (single repo)**: user toggles graph pane → picks Code → JS fetches `/code/graph/data` → server opens the repo engine, streams nodes+edges+fingerprint → adapter builds typed arrays → cache miss → simulation settles → pause → layout snapshot saved under `code:<repo>:<fingerprint>`.
2. **Reopen unchanged**: same fetch → fingerprint matches cached snapshot with full coverage → positions seeded, `render(0)` replay, zero motion.
3. **Re-index between visits**: index content changed → server fingerprint differs → cache miss → fresh settle → new snapshot replaces old key.
4. **Realm member switch**: picker change → fetch with `?member=<prefix>` → prior graph torn down, new one mounted; URL state updated.
5. **Inspect a symbol**: hover → preview card (name, kind, file:line, signature) → click → code-explorer node view for that id, member-aware.
6. **Drag**: pointer down on node → local reheat with boosted repulsion → release → restore repulsion, cool to pause → snapshot updated.
7. **Unindexed member**: fetch returns error JSON → pane shows "not indexed" message naming `atomic code index`.
8. **Index a repo containing a dangling-owner ref file** (SC1): extraction stores the file's nodes/edges, skips the bad ref recording it in the file's `errors` column → indexing reports success → resolution phase runs → calls/imports edges exist.

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Shared-core refactor regresses the hand-tuned system view | med | Harness (CP3) is committed and baseline-green before the refactor (CP4) starts; CP4's verification is the same harness green post-refactor; physics constants move verbatim |
| 17.5k-node payload/adapt jank on open | low | lean flat JSON (no nested per-element envelope); localhost; adapter already O(n) typed-array fill |
| `contains`-dominated visual (hairball) despite styling | med | edge-kind styling discipline (SC5); legend group filtering; degree sizing makes file hubs anchors; if illegible, tune edge alpha only — physics untouched |
| Vue extractor root cause deeper than the repro suggests | med | SC1's guard makes the failure non-fatal regardless; the extractor fix has a 2-file repro to bisect against |
| CI has no browser/GPU for SC3/SC4 gates | high (CI) / n/a (local) | gates run locally headless as during system-view tuning; Go tests carry CI coverage |

## Change log

### 2026-07-07 — SC4 edge count corrected to measured post-SC1 scale

**What changed:** SC4's scale figure updated from "~20k edges (approximate)" to the measured "~55k edges incl. `contains` and ~34k `calls`".

**Why:** Correction: SC1's index-integrity fix landed and a fresh index of this repo measures 17,502 nodes / 54,472 edges (33,655 `calls`) — the pre-fix estimate was extrapolated from an older, partially resolved index.

**Superseded:** the ~20k approximate edge figure.
