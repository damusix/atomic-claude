# Serve live reload

## Goal

`atomic serve` reflects filesystem changes (new/edited/deleted files) in the nav tree, link graph, rail, and system view without a restart or manual refresh — with near-zero idle cost when no browser tab is watching. An open page preserves scroll position; an open system view preserves node positions and viewport.

## Non-goals

- fsnotify or any file-watcher dependency — stat-polling only.
- WebSockets or the htmx SSE extension — plain `EventSource` on the client.
- Per-page or per-subtree fingerprints — the whole-realm fingerprint remains the unit of change detection; the event's `changed` list is a manifest diff, not per-page state.
- A user-facing tick-interval flag — the tick interval and quiet window are constructor-injectable as a test seam only.
- Rename detection — a rename is treated as delete + create; the node's cached layout position is lost.
- Unifying the `external.go` and `search_md.go` walkers into the snapshot — they stay query-scoped, not snapshot-shaped.
- Multi-user coordination — localhost, single user.
- Code-intel index watching — separate subsystem, unaffected by this work.

## Success criteria

- [ ] A single walk produces the realm fingerprint, nav paths, and link graph — no independent per-request nav walk and no startup-frozen link graph remain; the full-view graph JSON and the provenance DAG stay lazily assembled on `/graph/data` demand, unchanged from today.
- [ ] The tick path, when the fingerprint is unchanged, performs a stat-only walk only — no content reads, no provenance work, no git subprocess calls. On a fingerprint change, a tick rebuilds nav paths and the link graph only; it does not assemble graph JSON or the provenance DAG.
- [ ] A rebuild still in flight when the next tick fires causes that tick to skip (non-blocking check) rather than queue a second rebuild.
- [ ] A file whose mtime is within the quiet window (~2s) of now is excluded from the fingerprint manifest; the quiet window and tick interval are constructor parameters, not a user-facing flag.
- [ ] A file deleted between stat and content read during a rebuild is skipped without error for that rebuild; the deletion is reflected on the following tick.
- [ ] The realm snapshot (fp + nav paths + link graph) is one immutable struct published by a single atomic pointer swap; a handler reads the pointer once per request, making a torn read (new fp, stale graph) impossible.
- [ ] The ticker, lazy request-path validation, and the startup warm all call the same snapshot accessor; concurrent triggers collapse to one rebuild, deduped by rebuild generation rather than fingerprint value.
- [ ] A file created, edited, or deleted while the server runs is reflected in nav, rail (OUT/IN), wikilink resolution, and (on the next `/graph/data` fetch) the system-view graph, within one tick, without restarting the server.
- [ ] A request made while the ticker is parked (0 subscribers) still reflects an on-disk change made moments earlier, via the same lazy fingerprint validation.
- [ ] `/events` (SSE) is reachable as a new route and does not collide with the existing `/status` health route.
- [ ] Exactly one ticker goroutine runs for the server's lifetime, started at server start and stopped by the server context; it is never started or stopped on subscribe/unsubscribe edges.
- [ ] With zero SSE subscribers, the ticker body performs no walk (no periodic background work when no tab has a page open).
- [ ] A new subscription, including a reconnect, receives an immediate fingerprint check-and-push rather than waiting for the next tick.
- [ ] Each subscriber connection has its own buffered slot that coalesces to the latest event, plus a write deadline; a slow or dead subscriber never blocks the ticker or other subscribers.
- [ ] The `/events` handler returns promptly on server shutdown; Ctrl-C with an open tab exits within the existing 5s graceful window at exit code 0.
- [ ] The SSE payload is `{fp, changed: [relpaths]}`, a manifest diff capped at ~100 entries; over the cap the field is omitted and clients treat everything as changed; the fp-equal check remains the outer no-op guard.
- [ ] In page mode, the pane and rail refetch only when the displayed page's relpath is in `changed` (or the field is omitted); nav refetches on any change, and an SSE-triggered nav refetch skips `computeStaleness`.
- [ ] SSE-triggered swaps carry the `live-swap` marker and bypass the `htmx:after:swap` scroll reset; scroll position is unchanged after a page-mode refetch.
- [ ] In system mode, elements are diffed by id: removed nodes/edges are removed, added ones are seeded at a connected neighbor's position and laid out with a scoped pass only; the existing viewport is untouched and pre-existing positions do not move.
- [ ] When `(added + removed) / (nodes + edges currently mounted) > 0.5`, or no Cytoscape instance is mounted, the system view falls back to a full re-layout.
- [ ] The IndexedDB layout cache is keyed by a hash of the sorted mounted element-id set, not the realm fingerprint; the entry for the superseded key is pruned on re-key.
- [ ] A connectivity indicator reflects the `EventSource`'s live / reconnecting / disconnected state.
- [ ] `go test ./...` from `atomic/` is green, `go vet ./...` is clean, and the new concurrency surface in `atomic/internal/serve` passes under `go test -race`.

## Approach

Approach C: a subscriber-gated server ticker publishing one realm snapshot (fp + nav paths + link graph) over a plain-`EventSource` SSE endpoint, with lazy per-request fp validation as the correctness backstop. See `docs/design/serve-live-reload.md`.

## Change tree

```
atomic/internal/serve/
├── snapshot.go            A  realm snapshot type + atomic pointer + rebuild funnel + quiet-window fingerprint walk
├── events.go               A  /events SSE handler + subscriber registry + ticker goroutine
├── graphcache.go           M  graph-JSON assembly sources the link graph and fp from the snapshot store; own fingerprint walk removed
├── graphoverlay.go         M  GraphDataHandler construction takes the snapshot store instead of a static *Graph
├── nav.go                  M  NewNavHandler reads nav paths from the snapshot; SSE-triggered requests skip computeStaleness
├── context_handler.go      M  page handler resolves the link graph via the snapshot store
├── rail_handler.go         M  rail handler resolves the link graph via the snapshot store
├── serve.go                M  startup constructs the snapshot store, warms it, registers /events, starts the ticker bound to the server context
└── templates/
    └── layout.html          M  EventSource boot, live-swap marker, page-mode reconcile, system-mode diff/patch, IndexedDB re-key, connectivity indicator
```

## Outline

### atomic/internal/serve/snapshot.go

- `realmSnapshot` — immutable {fp, navPaths, graph} triple for one realm state
- `snapshotStore` — owns the atomic pointer, rebuild funnel, quiet window + tick interval (test seam)
  - `current` — returns the currently published snapshot
  - `ensureFresh` — lazy fp validation; triggers a rebuild when stale, deduped by rebuild generation
  - `rebuild` — walks nav paths + link graph, computes the manifest diff, publishes the new snapshot via one atomic pointer swap
  - `fingerprint` — quiet-window-filtered, stat-only manifest walk
- `newSnapshotStore` — constructs a store rooted at a given path with a given tick interval + quiet window

### atomic/internal/serve/events.go

- `subscriberRegistry` — tracks connected `/events` clients and their coalescing buffered slots
  - `subscribe` — registers a new subscriber, returns its channel + unsubscribe func
  - `broadcast` — pushes an event to every subscriber's slot, coalescing to the latest
  - `count` — current subscriber count, read by the ticker's gate
- `changeEvent` — `{fp, changed}` wire payload; `changed` omitted over the ~100-entry cap
- `NewEventsHandler` — `/events` SSE handler: subscribe, immediate fp check-and-push, stream until context done, per-write deadline
- `startTicker` — the single ticker goroutine: gated on subscriber count, calls `ensureFresh`, broadcasts on change, stops on context cancellation

### atomic/internal/serve/graphcache.go

- `graphDataCache.assemble` — reshaped to read the link graph and fp from `store.current()` instead of a stored startup graph and its own fingerprint walk

### atomic/internal/serve/graphoverlay.go

- `NewGraphDataHandlerWithGraph` — reshaped to accept a `*snapshotStore`
  - `GraphDataHandler.ServeHTTP` — resolves the current graph via `store.current()`

### atomic/internal/serve/nav.go

- `NewNavHandler` — reshaped to read nav paths from the snapshot store; accepts an SSE-triggered flag that skips `computeStaleness`

### atomic/internal/serve/context_handler.go

- `NewPageHandlerWithGraph` — reshaped to resolve the link graph via `store.current()` instead of a fixed `*Graph`

### atomic/internal/serve/rail_handler.go

- `NewRailHandler` — reshaped to resolve the link graph via `store.current()`

### atomic/internal/serve/serve.go

- `RunWithContext` — constructs the `snapshotStore`, warms it once, wires `/events`, starts the ticker bound to the same context that drives `srv.Shutdown`

### atomic/internal/serve/templates/layout.html

- EventSource boot script — opens `/events`, tracks last-seen fp, drives the connectivity indicator
  - page-mode reconcile handler — nav/pane/rail refetch decision, `live-swap` marker application
  - system-mode reconcile handler (`mountSystemGraph` area) — id-diff, neighbor-seeded add, scoped layout, degenerate fallback, IndexedDB re-key
- `htmx:after:swap` scroll-reset listener — reshaped to skip `live-swap`-marked swaps

## Flows

### 1. Tick flow

1. Ticker fires on its interval.
2. Ticker checks the subscriber registry's count; 0 subscribers → park, no walk, wait for next tick.
3. ≥1 subscriber → ticker calls `store.ensureFresh()`.
4. `ensureFresh` computes the quiet-window-filtered fingerprint (stat-only walk).
5. Fingerprint unchanged → no rebuild; ticker parks until next tick.
6. Fingerprint changed → a rebuild for this generation is already in flight → this call skips (non-blocking); otherwise it proceeds.
7. Rebuild walks nav paths + link graph (`.md` stat + content reads only); a file that vanishes between stat and read is skipped for this rebuild without error.
8. The new `realmSnapshot` is published via one atomic pointer swap.
9. Ticker computes the changed relpaths (manifest diff, capped at ~100; over cap the field is omitted) and broadcasts `{fp, changed}` to the registry.
10. Registry fans the event out to every subscriber's buffered slot, coalescing to the latest.

### 2. Subscribe flow

1. Browser opens an `EventSource` → `GET /events`.
2. Handler registers a new subscriber slot with the registry.
3. Handler immediately calls `store.ensureFresh()` and pushes the current fp to this subscriber only, regardless of the tick cycle — the resync push.
4. Handler streams subsequent broadcast events from the subscriber's slot until the request context is done (client disconnect or server shutdown).
5. Each write applies a write deadline; a slow or dead subscriber's slot is simply overwritten by newer events and never blocks the broadcaster or other subscribers.
6. Client's `EventSource` `onopen`/`onerror` drive the connectivity indicator (live / reconnecting / disconnected); a reconnect re-enters this flow at step 1 and receives its own resync push at step 3.

### 3. Page-mode reconcile flow

1. Client's `EventSource.onmessage` receives `{fp, changed}`.
2. Received fp equals the last-seen fp → no-op (outer guard).
3. Fp differs → nav refetches unconditionally (`GET /nav`), marked `live-swap`; the request signals the handler to skip `computeStaleness`.
4. Displayed page's relpath ∈ `changed` (or `changed` is omitted) → pane (`GET /page/<relpath>`) and rail (`GET /rail/<relpath>`) refetch, both marked `live-swap`.
5. Displayed page's relpath ∉ `changed` → pane and rail refetch are skipped.
6. `live-swap`-marked swaps bypass the `#main-pane` scroll-reset listener; scroll position is preserved.
7. Client updates its last-seen fp to the received fp.

### 4. System-mode reconcile flow

1. Client's `EventSource.onmessage` receives `{fp, changed}` while the system graph is mounted.
2. Received fp equals the last-seen fp → no-op.
3. Fp differs → client fetches `/graph/data` for the fresh elements and fingerprint header.
4. Fresh elements are diffed by id against the mounted Cytoscape instance's current elements.
5. `(added + removed) / (nodes + edges currently mounted) > 0.5`, or no instance mounted → full re-layout (may re-fit; viewport not preserved — accepted).
6. Otherwise → `cy.remove()` the removed ids; `cy.add()` the added elements seeded at a connected neighbor's position plus an offset; run a scoped layout pass on the new collection only; no `fit()`; existing viewport untouched.
7. IndexedDB layout cache re-keys from a hash of the sorted mounted element-id set; the entry for the superseded key is pruned.
8. Connectivity indicator continues reflecting `EventSource` state throughout.

### 5. Shutdown flow

1. Server receives `ctx.Done()` (SIGINT/SIGTERM in production, cancel in tests).
2. `srv.Shutdown(shutCtx)` begins the existing 5s graceful window.
3. The ticker goroutine observes the same context cancellation and exits its loop without blocking shutdown.
4. Each open `/events` handler observes its request context's cancellation and returns promptly, releasing its connection.
5. `Shutdown` completes inside the 5s window even with subscribers connected; the process exits 0.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|-----------|--------------|-------|-----------|---------|
| 1 | Snapshot core: one realm snapshot (fp + nav paths + link graph) behind a single atomic pointer, quiet-window fingerprint walk, generation-keyed rebuild funnel | `atomic/internal/serve/snapshot.go` (new), `atomic/internal/serve/graphcache.go` (link graph + fp sourced from the store), `atomic/internal/serve/graphoverlay.go` (construction site) — reuses `atomic/internal/serve/walk.go` filters unmodified | atomic-implementer (feature) | 3 | Go test: one walk populates fp, nav paths, and link graph together; a file inside the quiet window does not flip the fp; a file vanishing mid-rebuild is skipped without error and picked up on the next rebuild; concurrent `ensureFresh` callers collapse to one rebuild (generation-keyed, not fp-keyed); graph-JSON assembly still reads the link graph from the store and remains fp-keyed, singleflight-deduped, and lazy on `/graph/data` demand as today |
| 2 | Handler migration: nav, page, rail, graph/data handlers read the snapshot; retire the startup-frozen `BuildLinkGraph` singleton and the per-request nav walk | `atomic/internal/serve/nav.go`, `atomic/internal/serve/context_handler.go`, `atomic/internal/serve/rail_handler.go`, `atomic/internal/serve/graphoverlay.go`, `atomic/internal/serve/serve.go` (route wiring, startup construction) | atomic-implementer (feature) | 5 | Go test: nav/page/rail/graph-data handlers source content from the shared snapshot; a file added after server start appears in nav, rail OUT/IN, and graph/data on the next request without restart; a wikilink to that newly created file resolves (not rendered broken) on the next page request; an SSE-triggered nav request skips `computeStaleness` while an ordinary navigation request still runs it; existing serve package tests stay green |
| 3 | SSE endpoint + subscriber-gated ticker: new `/events` route, one ticker goroutine (10s default, constructor-injectable) gated on subscriber count, quiet-window rebuild, coalesced broadcast, fast shutdown | new file `atomic/internal/serve/events.go` (SSE handler + subscriber registry + ticker), `atomic/internal/serve/serve.go` (route registration, ticker goroutine bound to server context) | atomic-implementer (feature) | 2 | `httptest.NewServer` + a real streaming client with a bounded context (not the `search_stream_test.go` `ResponseRecorder` pattern): a subscribed client receives `{fp, changed}` after an on-disk change and a tick; a new subscription receives an immediate fp push before the next tick; no rebuild occurs when the fp is unchanged; the ticker performs no work at 0 subscribers; a slow subscriber's coalesced slot never blocks broadcast to others; `Shutdown` with a live subscriber connected completes inside the 5s window at exit code 0; `go test -race ./internal/serve/...` clean |
| 4 | Client: EventSource boot + page-mode reconcile (fp compare, refetch page pane + rail + nav, `live-swap`-marked scroll preserve, connectivity indicator) | `atomic/internal/serve/templates/layout.html` (EventSource boot script, `live-swap` marker, `htmx:after:swap` scroll-reset bypass, connectivity indicator) | atomic-implementer (surgical) | 1 | No JS test rig exists for inline `layout.html` script — manual `atomic serve` scenario (accepted risk): open a page, edit the underlying file on disk, observe the pane/rail/nav refetch within one tick with scroll position unchanged; an SSE event whose fp matches the currently displayed fp produces zero refetches; an event whose `changed` list excludes the displayed page still refetches nav but not pane/rail; the connectivity dot reflects a manual server stop/restart |
| 5 | Client: system-mode graph diff/patch (id-diff add/remove, neighbor-seeded position, scoped layout, no `fit()`, degenerate full-relayout fallback, IndexedDB re-key by element-id-set hash); registers its handler on checkpoint 4's EventSource dispatcher — system mode patches via raw `/graph/data` fetch, so the `live-swap` htmx marker does not apply here | `atomic/internal/serve/templates/layout.html` (`mountSystemGraph` area: SSE-triggered `/graph/data` refetch, id-diff, IndexedDB re-key) | atomic-implementer (surgical) | 1 | No JS test rig exists for inline `layout.html` script — manual `atomic serve` scenario (accepted risk): open the system graph, add/remove files on disk, observe only the changed nodes/edges patched with viewport and existing node positions untouched; a >50%-changed edit triggers full re-layout instead; the IndexedDB entry keyed to the prior element-id-set hash is gone after the re-key |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Ticker goroutine leaks or races with graceful shutdown | Medium | Bound to the same context as the server goroutine; checkpoint 3 asserts `Shutdown` completes within the 5s window with a live subscriber, under `-race` |
| Two concurrent walkers compute different fingerprints for a changing filesystem, defeating fp-keyed dedup | Low | Rebuild funnel dedups by rebuild generation, not fp value, and is the single entry point for the ticker, lazy validation, and startup warm |
| Cytoscape id-diff patch produces visually unstable results (misplaced new nodes, stale removed edges) | Medium | Seed new nodes near a connected neighbor's position, scope the layout pass to the new collection only, fall back to full re-layout above the 50% threshold |
| SSE-triggered scroll-preserve bypass interacts with the existing unconditional `htmx:after:swap` scroll reset used by ordinary navigation | Low | Bypass is scoped to swaps carrying the `live-swap` marker only; ordinary htmx navigation keeps today's reset behavior |

## Change log
