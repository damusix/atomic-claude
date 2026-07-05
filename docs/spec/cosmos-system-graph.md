# Cosmos system graph

## Goal

Replace the `atomic serve` system-graph engine (Cytoscape canvas 2D + one-shot
cola layout) with cosmos.gl (GPU simulation + GPU rendering), giving the
Network View continuous, Obsidian-grade physics at current scale and
effectively unlimited headroom for future large graphs, with full parity to
today's interaction surface and zero runtime build step.

## Non-goals

- Rail mini-graph migration — stays on Cytoscape (`concentric` layout).
- Any change to the `/graph/data` JSON contract or its consumers (rail,
  provenance overlay).
- Symbol/code-graph rendering — this work builds engine headroom only.
- 3D view, clustering, or community detection.
- A typed-array or LOD graph-data payload — the existing cytoElements JSON is
  reused as-is.
- Load-testing at 100k+ node scale — headroom is a property of the engine
  choice, not independently verified by this work.
- Live reload / file-watch refresh of the graph — a separate planned feature.
- WebGL context-loss auto-recovery — deferred.
- No graph-side search affordance — find-by-name at low zoom is served by the
  existing top-bar md search.

## Success criteria

- [ ] **SC1** — Motion policy holds: a fresh mount (no cached positions) runs
      the simulation to rest and then pauses; a cached open under an unchanged
      realm fingerprint seeds the cached positions before the first
      simulation tick and pauses immediately for an exact, zero-motion
      replay; a cached entry that does not carry the current cache-format
      marker is discarded and never fed into the seed, even under a matching
      fingerprint; while paused and the viewport is idle, the view adds zero
      per-frame work of its own — no position readback, no DOM label writes,
      no JS-side animation loops (cosmos.gl's internal requestAnimationFrame
      redraw of the static scene continues; its public API exposes no way to
      stop the frame loop, an accepted upstream ceiling);
      dragging a node applies a bounded local reheat that leaves the rest of
      the graph in place, and releasing the drag cools the simulation back to
      a pause and writes a full position snapshot to the cache. The cache is
      written exactly twice-per-cause: once when a fresh mount's simulation
      settles (the cache-miss path — this is what makes a later cache hit
      possible at all), and on each drag release; a cache-hit open never
      rewrites the cache. A cache entry is treated as a hit only when it
      covers the current node set in full — partial coverage falls through to
      the fresh-mount path. Continuous
      pan/zoom/drag interaction sustains ~60fps — checked manually (e.g. a
      browser devtools FPS overlay) showing no sustained drops below ~55fps.
- [ ] **SC2** — `cola.min.js` and `cytoscape-cola.js` are removed from
      `assets/vendor/` and no longer referenced by any template or script; the
      rail mini-graph's `concentric` layout and behavior are unaffected.
      Shell tests that assert vendor script presence/order (e.g. the cola
      triplet load order and `cytoscape.use` registration) are updated in the
      same checkpoint to assert the cosmos.gl bundle instead.
- [ ] **SC3** — A committed, single-file, vendored cosmos.gl bundle is loaded
      by the system-graph view with no runtime or CI build step; a committed
      provenance record adjacent to the bundle names the exact
      package@version, resolved dependency pins, esbuild version, the exact
      bundling command, and the output checksum.
- [ ] **SC4** — Every behavior in the design doc's Parity inventory table
      holds under cosmos.gl: hover preview card (anchored to its node as the
      node moves), click → page modal, legend type-filter chips (filtered
      types alpha-0-recolored, excluded from hover/click, layout does not
      reflow on toggle), zoom/pan recorded to and restored from the URL,
      theme-flip re-styling, degree-based node sizing, OKF node-type coloring,
      distinct provenance/drift edge styling, position persistence across
      opens keyed by the realm fingerprint, htmx mount/teardown lifecycle
      (`mode-system` body class, history-restore survival), lifecycle guards
      (double-mount guard, instance destroy and GL-context release on
      swap-out, mid-fetch teardown safety), and the loading indicator (clears
      on any mount outcome, success or failure).
- [ ] **SC5** — Node labels render as a DOM overlay that fades out below a
      zoom threshold and back in above it (Obsidian-style), except the
      hovered node's label, which always shows regardless of zoom. The label
      set is produced by a pure function — viewport, per-node degree, and
      zoom in; the label set to render out — exported from the system-graph
      asset so it is unit-testable outside a browser; a fixed numeric cap
      (150 rendered label elements by default, tunable by the implementer)
      bounds the DOM label count as the realm grows.
- [ ] **SC6** — `/graph/data`'s JSON shape, its `?node=&depth=` local-subgraph
      contract, and the `X-Graph-Fingerprint` header are unchanged; existing
      server-side tests for the endpoint pass without modification.
- [ ] **SC7** — `docs/spec/atomic-serve.md` and `docs/reference/serve.md`
      describe the system graph as cosmos.gl-powered, not Cytoscape/cola; the
      amendment follows the spec-currency amendment rules (body rewritten to
      current truth, dated change-log entry on that file).
- [ ] **SC8** — `go test ./...` (from `atomic/`) passes; `make render` and
      `make -C atomic bundle` produce no `git diff --exit-code` drift.
- [ ] **SC9** — WebGL2-unavailable failure mode: when the browser lacks
      WebGL2, the mount detects this before invoking cosmos.gl, clears the
      loading indicator, and shows a message naming the WebGL2 requirement in
      the existing error affordance — never a hung spinner and never a
      silent blank pane.

## Approach

cosmos.gl (`@cosmos.gl/graph`) replaces Cytoscape+cola for the system-graph
view only; full rationale and rejected alternatives in
`docs/design/cosmos-system-graph.md`.

## Change tree

    atomic/internal/serve/
    ├── assets/
    │   ├── vendor/
    │   │   ├── cosmos-graph.js ............... A  (vendored @cosmos.gl/graph esbuild bundle)
    │   │   ├── cosmos-graph.provenance.txt ... A  (package@version, dependency pins, esbuild version, command, checksum)
    │   │   ├── cola.min.js .................... D  (sole consumer removed)
    │   │   └── cytoscape-cola.js .............. D  (sole consumer removed)
    │   ├── system-graph.js .................... A  (mount lifecycle, data adapter, motion-policy sim control, label overlay + culling, WebGL2 detection)
    │   └── app.css ............................ M  (system-graph container/legend styles reshaped; label-overlay styles added)
    ├── templates/layout.html .................. M  (vendor <script> tags; inline system-graph script trimmed to tag + mount delegation; AtomicGraphUI made engine-neutral; rail call sites updated)
    ├── serve.go ................................ M  (stale "mount Cytoscape" doc-comment fix)
    ├── rail_test.go ............................ M  (TestShellLoadsGraphScriptsInOrder rewritten for the cosmos bundle)
    └── graphoverlay_test.go .................... M  (TestShellSystemModeToggleWiring, TestShellContainsAtomicCyStyleFunction, TestShellContainsFingerprintStyleInSharedFunction premise/comment fixes)
    docs/
    ├── spec/atomic-serve.md ................... M  (system-view description rewritten to cosmos.gl truth)
    └── reference/serve.md ..................... M  (system-graph description rewritten to cosmos.gl truth)

## Outline

    atomic/internal/serve/assets/vendor/cosmos-graph.js
      None — vendored third-party output, no authored pieces

    atomic/internal/serve/assets/vendor/cosmos-graph.provenance.txt
      None — flat provenance record (package, pins, command, checksum), no nameable pieces

    atomic/internal/serve/assets/vendor/cola.min.js
      None — deleted, nothing created or reshaped

    atomic/internal/serve/assets/vendor/cytoscape-cola.js
      None — deleted, nothing created or reshaped

    atomic/internal/serve/assets/system-graph.js
      mount/teardown lifecycle — double-mount guard, instance construction, GL-context release on swap-out, mid-fetch teardown safety
      data adapter — cytoElements JSON to point/link index arrays, ID<->index maps
      motion-policy sim control — fresh-mount rest-then-pause, seed-and-pause cache replay, cache-format versioning, bounded drag reheat, URL view-state read/write
      styling + interaction parity — legend filter (alpha-0 recolor, hover/click guard, no-reflow), provenance/drift edge styling, degree-based sizing, OKF type coloring, theme-flip re-style, hover/click event wiring into AtomicGraphUI
      label overlay — DOM projection from screen-space coordinates, zoom-threshold fade, hover-always-shows exception
        culling function (exported, pure) — viewport + degree + zoom in, label set out, fixed numeric cap
      WebGL2 detection — availability check before instance construction, failure-mode messaging

    atomic/internal/serve/templates/layout.html
      vendor <script> tags — cosmos.gl bundle added, cola triplet removed, cytoscape.min.js retained for the rail
      system-graph mount delegation — thin htmx.onLoad hook into system-graph.js's exported mount
      AtomicGraphUI — engine-neutral preview/modal helper module
        showPreviewCard — takes plain node data (id, type, meta, screen coords) instead of a Cytoscape node
        openPageModal — engine-neutral node-id lookup
      mountRailGraph — call sites updated to the reshaped AtomicGraphUI contract
      atomicCyStyle — fingerprint/fingerprint-drift edge selectors removed (relocated to system-graph.js)

    atomic/internal/serve/serve.go
      None — doc-comment-only fix, no created or reshaped code pieces

    atomic/internal/serve/rail_test.go
      TestShellLoadsGraphScriptsInOrder — assertions + doc-comment rewritten for the cosmos bundle

    atomic/internal/serve/graphoverlay_test.go
      TestShellSystemModeToggleWiring — premise rewritten for the code-home move
      TestShellContainsAtomicCyStyleFunction — doc-comment fix only (rail-only coverage)
      TestShellContainsFingerprintStyleInSharedFunction — assertions + doc-comment rewritten for the new styling home

    atomic/internal/serve/assets/app.css
      system-graph container/legend styles — reshaped for the cosmos mount
      label-overlay styles — new (DOM label divs, fade transition)

    docs/spec/atomic-serve.md
      Approach paragraph — rewritten to cosmos.gl truth
      FE-SC3 — rewritten to cosmos.gl truth
      FE3 checkpoint row — rewritten to cosmos.gl truth

    docs/reference/serve.md
      system-graph description — rewritten to cosmos.gl truth

## Flows

    Flow: fresh mount (no cached position)
    1. user opens the [system] toggle in the shell
    2. shell fetches /graph/data, reads X-Graph-Fingerprint
    3. system-graph.js checks IndexedDB for a cached entry under that fingerprint — finds none
    4. system-graph.js detects WebGL2, constructs the cosmos.gl instance, adapts the JSON to typed arrays
    5. the simulation starts and runs continuously
    6. the simulation reaches rest and pauses; the loading indicator clears; legend and labels render

    Flow: cached open (unchanged fingerprint)
    1. user opens the [system] toggle
    2. shell fetches /graph/data, reads X-Graph-Fingerprint
    3. system-graph.js finds a cached entry under that fingerprint and checks its cache-format marker
    4. marker matches the current format -> positions are seeded before the first simulation tick, and the sim pauses immediately
    5. marker is stale (pre-swap format) -> the entry is discarded and the fresh-mount flow runs instead
    6. an exact, zero-motion replay renders; the loading indicator clears

    Flow: drag
    1. user presses and drags a node
    2. system-graph.js pins the non-dragged neighborhood and reheats the simulation locally, within a bounded energy budget
    3. the dragged node follows the pointer; the rest of the graph stays in place
    4. user releases the drag
    5. the simulation cools back to a pause
    6. the dragged node's new position is written to the IndexedDB cache, keyed by the current fingerprint and format marker

    Flow: WebGL2-unavailable mount
    1. user opens the [system] toggle
    2. system-graph.js checks WebGL2 availability before constructing the cosmos.gl instance
    3. WebGL2 is unavailable -> the loading indicator clears and an error message naming the WebGL2 requirement renders in the existing error affordance
    4. no cosmos.gl instance is constructed; no fetch-chain .catch handling is relied on

    Flow: legend filter toggle
    1. user clicks a legend chip for a node type
    2. system-graph.js recolors the matching points to alpha-0 (still simulating, not removed)
    3. a hover/click guard is enabled on those points — mouseover/click on an alpha-0 point produces no preview card or modal
    4. the simulation and layout continue unchanged — no reflow
    5. user clicks the chip again -> the points are recolored back and the guard is lifted

    Flow: label zoom fade
    1. viewport zoom or pan changes (including simulation-tick movement)
    2. system-graph.js's exported culling function receives the current viewport, per-node degree, and zoom level
    3. the culling function returns the label set to render, bounded by the fixed cap
    4. labels below the zoom threshold fade out, labels above it fade in, and the hovered node's label is always included regardless of zoom
    5. the DOM label overlay positions update to the newly projected screen coordinates

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|-----------|-------------|-------|-----------|----------|
| 1 | **Vendor cosmos.gl + remove cola** — one-time `esbuild --bundle` of `@cosmos.gl/graph` into a committed single-file bundle under `assets/vendor/`, with a committed provenance record alongside it (package@version, resolved dependency pins, esbuild version, exact command, output checksum); delete `cola.min.js` + `cytoscape-cola.js`; update the vendor `<script>` tags in `layout.html` (remove the cytoscape→cola→cytoscape-cola load-order block, add the cosmos.gl bundle) while keeping `cytoscape.min.js` (still needed by the rail); rewrite `TestShellLoadsGraphScriptsInOrder` to assert the cosmos.gl bundle is present instead of the removed cola triplet, and correct its doc-comment's stale claim that the rail mini-graph uses cola (the rail uses `concentric`) | `atomic/internal/serve/assets/vendor/`, `atomic/internal/serve/templates/layout.html` (script tags), `atomic/internal/serve/rail_test.go` | builder | 5 | SC2, SC3; `go build` succeeds; rail mini-graph still mounts; no reference to `cola` remains in the vendor `<script>` tags or in `assets/vendor/` (the system-graph mount body's cola layout config is CP2's territory — it is deleted wholesale when the mount moves to `system-graph.js`, so the system view is expectedly non-functional between the CP1 and CP2 commits; the rail is unaffected); `TestShellLoadsGraphScriptsInOrder` passes asserting the cosmos.gl bundle; a local Node import-smoke of the vendored bundle succeeds (constructs/imports it outside the browser — not a CI gate) |
| 2 | **Mount lifecycle on cosmos.gl** — create `atomic/internal/serve/assets/system-graph.js`: adapt the existing `/graph/data` cytoElements JSON into cosmos's point/link index arrays (ID↔index maps for later event handling); detect WebGL2 availability before constructing the cosmos.gl instance — on failure, clear the loading indicator and show a message naming the WebGL2 requirement; on success, start the continuous simulation, running to rest and pausing on a fresh mount; wire the initial zoom/fit to match today's fit-on-open behavior; guard against double-mount, destroy the instance and release its GL context on swap-out, and make mid-fetch teardown safe; trim `layout.html`'s inline script down to the vendor `<script>` tag plus the htmx-delegated mount call into `system-graph.js`; fix the stale "mount Cytoscape" comment at `serve.go:33-35`; rewrite `TestShellSystemModeToggleWiring`'s premise — its identifier/URL-string assertions against the "/" shell response no longer hold once the mount body moves into `system-graph.js`, so it must assert the new seam (the script tag and the delegated mount call) instead | `atomic/internal/serve/assets/system-graph.js` (new), `atomic/internal/serve/templates/layout.html`, `atomic/internal/serve/serve.go`, `atomic/internal/serve/graphoverlay_test.go` | builder | 4 | SC1 (fresh-mount rest-then-pause and idle zero-work clauses), SC4 (lifecycle-guards clause), SC9; `TestShellSystemModeToggleWiring` passes against its rewritten premise |
| 3 | **Position cache + drag reheat + URL view state** — restore the IndexedDB position cache as seed-and-pause: seed the cached positions before the first simulation tick and pause immediately on an unchanged `X-Graph-Fingerprint`, for an exact, zero-motion replay; version the cache value format and discard any entry lacking the current marker under an identical fingerprint rather than feeding it to the seed; wire node drag to a bounded local reheat (pin the non-dragged neighborhood, reheat locally — the design names `setPinnedPoints` and `start(alpha)` as the verified cosmos.gl primitives for this) that cools back to a pause on release and writes a full position snapshot to the cache on release; the fresh-mount path writes one full snapshot when the simulation settles (cache-miss path only — a cache-hit open never writes); the cache-hit gate requires full coverage of the current node set, with partial entries falling through to the fresh path; recreate the URL zoom/pan read/write behavior inside `system-graph.js` (equivalent semantics to the removed shell helpers: restore view from URL params on mount, record zoom/pan to the URL debounced via replaceState), wired to the new instance's viewport events | `atomic/internal/serve/assets/system-graph.js` | builder | 1 | SC1 (seed-and-pause replay, cache-format discard, and drag-reheat/save-on-release clauses), SC6; manual verification of exact-replay-with-zero-motion on a second open and of drag reheat staying local |
| 4 | **Interaction + styling parity** — make `AtomicGraphUI`'s helpers engine-neutral (plain data: node id, type, meta, screen coordinates, rather than Cytoscape objects) and update the rail's call sites in the same change; re-wire hover → `AtomicGraphUI.showPreviewCard` (anchored to its node as the node moves) and click → `AtomicGraphUI.openPageModal`; port the legend type-filter chips so a filtered-out type is alpha-0-recolored (not removed from the simulation), excluded from hover/click by a guard, and never triggers a layout reflow; port theme-flip re-styling, degree-based node sizing, and OKF type coloring (`atomicCyTypeColors()`) onto the cosmos path; move the distinct `fingerprint`/`fingerprint drift` provenance-edge styling into `system-graph.js` (its current home is the shared `atomicCyStyle()` in `layout.html`); fix `TestShellContainsAtomicCyStyleFunction`'s doc-comment, which claims system-graph coverage — after this checkpoint `atomicCyStyle()` is rail-only; rewrite `TestShellContainsFingerprintStyleInSharedFunction`'s assertions to check the provenance-edge styling's new home in `system-graph.js` instead of the shell HTML, and fix its doc-comment's stale claim that "the style must live in the shell, not in the removed /graph page" | `atomic/internal/serve/templates/layout.html` (`AtomicGraphUI` helpers, rail call sites), `atomic/internal/serve/assets/system-graph.js`, `atomic/internal/serve/assets/app.css`, `atomic/internal/serve/graphoverlay_test.go` | builder | 4 | SC4 (remaining parity clauses); SC1 (the ~60fps / no sustained drops below ~55fps smoothness clause, completed here since drag, hover, and click are all live) — checked manually via a browser devtools FPS overlay during continuous pan/zoom/drag; `TestShellContainsFingerprintStyleInSharedFunction` passes against its rewritten premise; rail hover/click behavior verified manually after the `AtomicGraphUI` signature change |
| 5 | **DOM label overlay** — render node labels as positioned DOM elements projected from cosmos.gl screen-space coordinates, updated on every simulation/viewport tick; export a pure culling function from `system-graph.js` (viewport + per-node degree + zoom in, the label set to render out) with a fixed numeric cap (150 by default) on rendered label elements; fade labels out below a zoom threshold and back in above it, except the hovered node's label, which always renders regardless of zoom | `atomic/internal/serve/assets/system-graph.js`, `atomic/internal/serve/assets/app.css` | builder | 2 | SC5; a unit test of the exported culling function (pure, runnable outside a browser) plus manual verification of fade-in/out and the hover-always-shows exception at current realm scale |
| 6 | **Amend serve docs + full verify** — rewrite the Cytoscape/cola description of the system view in `docs/spec/atomic-serve.md` (Approach paragraph, FE-SC3, FE3 checkpoint row) to cosmos.gl truth, with a dated change-log entry on that file per the spec-currency amendment rules; update `docs/reference/serve.md`'s system-graph description; run `go test ./...`, `make render`, `make -C atomic bundle`, and signals refresh | `docs/spec/atomic-serve.md`, `docs/reference/serve.md` | surgeon | 2 | SC7, SC8 |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| cosmos.gl's ESM-only, index-based API (no built-in labels, ordinal point/edge indices rather than IDs) makes the JSON-to-cosmos adapter and event-to-node-ID mapping more intricate than the Cytoscape call sites it replaces | Medium | Keep the adapter as one isolated function (JSON → typed arrays + ID↔index maps) inside `system-graph.js`; reuse the same maps for every event handler (hover, click, drag) so ID resolution has one source of truth |
| Vendored bundle size (cosmos.gl + luma.gl) inflates `assets/vendor/` beyond an acceptable footprint | Low | Acceptance bar is anything under the already-vendored `mermaid.min.js` (3.3MB); measure after the first `esbuild --bundle` in checkpoint 1 before proceeding |
| Continuous GPU simulation drifts previously-stable node positions on every open, defeating the "stable mental map" the position cache exists to preserve | Medium | Seed-and-pause: warm-start from cached positions before the first simulation tick and pause immediately for an exact, zero-motion replay under an unchanged fingerprint; a cache-format marker discards pre-swap entries rather than feeding them into the seed |
| WebGL2 is unavailable (remote desktop, VMs, policy-managed browsers) | Medium | SC9's detect-and-message failure mode — `getContext('webgl2')` returns null rather than throwing, so the mount must explicitly detect it before the fetch-chain's `.catch` would ever fire |
| CI has no browser or GPU, so client runtime motion and interaction behavior cannot be exercised by automated tests | High | Accepted as structural; verified manually at implementation time. Mitigated in scope by keeping Go-test premises accurate (they check structure/strings, not motion), extracting the culling policy as a unit-testable pure function, and a local bundle import-smoke — none of which substitutes for browser verification |

## Change log

### 2026-07-04 — Correction: cache saves include the fresh-settle snapshot; hit gate requires full coverage

**What changed:** SC1 and CP3 now state the cache is written once when a fresh mount settles (full snapshot) and on drag release (full snapshot), and that a cache entry counts as a hit only with full node-set coverage. The prior "saves on drag release only, never on open" wording is superseded.

**Why:** CP3 review traced a 🔴: drag-only single-node saves plus a shape-only hit gate meant one drag created a permanently-hit partial cache — every later open froze the dragged node at its position and every other node at random scatter (render(0) runs zero physics steps). "Never on open" also made the cache unreachable for never-dragged graphs. The challenge-swarm finding this wording came from banned per-open *rewrites* (seed-and-cool churn); today's code always saved the fresh settle (`layout.html:943` at base) — that semantic is restored.

**Superseded:** position saves on drag release only; shape-only cache-hit validation.

### 2026-07-04 — Correction: CP3 recreates URL view state, does not rewire it

**What changed:** CP3's row no longer says to "keep the existing `readViewFromURL`/`writeViewToURL`" — those shell helpers were deleted with the old mount body in CP2. CP3 recreates the equivalent behavior inside `system-graph.js`.

**Why:** CP2's trim removes the whole inline block (correct per its scope); the CP3 wording predated that and would send a fresh implementer looking for functions that no longer exist. Caught by the CP2 review.

### 2026-07-04 — Correction: SC1 idle clause scoped to view-owned work

**What changed:** SC1's "zero per-frame work while paused and viewport idle" now covers only work the view adds (position readback, DOM label writes, JS-side animation loops). cosmos.gl's internal requestAnimationFrame redraw of the static scene is excluded as an accepted upstream ceiling.

**Why:** Discovered at CP2 implementation, verified against the unminified `@cosmos.gl/graph@3.1.0` source: `render()` starts a self-rescheduling frame loop; `pause()` stops only the simulation, and `stopFrames()` is private. No public API can idle the redraw. The original clause was unachievable as written.

### 2026-07-04 — Correction: CP1 cola-reference scope

**What changed:** CP1's Verifies clause "no reference to `cola` remains in `layout.html`" narrowed to the vendor `<script>` tags and `assets/vendor/`. The system-graph mount body's cola layout config stays until CP2 deletes the mount body wholesale; the system view is expectedly non-functional between the CP1 and CP2 commits on the isolated branch.

**Why:** Discovered at CP1 implementation — removing the mount body's cola references in CP1 would touch CP2's territory (mount-lifecycle code) without a working replacement, blurring the checkpoint boundary for no functional gain. The original wording did not account for the checkpoint split.
