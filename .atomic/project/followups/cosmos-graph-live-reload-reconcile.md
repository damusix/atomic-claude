---
id: cosmos-graph-live-reload-reconcile
title: Rebuild graph-mode live-reload reconcile on the cosmos engine
created: "2026-07-08"
origin: |
    PR #123 merge resolution (cosmos engine swap vs serve-live-reload #122)
kind: plan
review_by: "2026-09-06"
status: open
file: atomic/internal/serve/frontend/src/pages/Graph/Graph.tsx
---

In graph mode, live-reload events do nothing. Page mode works fully, and the server side is
intact — the snapshot store and `/events` are unaffected, and `/graph/data` reflects a realm
change on its next fetch. Only the in-place patch of a mounted graph is missing.

serve-live-reload CP5 had implemented this (SSE-triggered `/graph/data` refetch, id-diff
patch, neighbor-seeded positions, IndexedDB re-key), but it was written against the old
Cytoscape system-graph client. PR #123 replaced that client wholesale with the cosmos.gl
engine, so CP5 had no surviving attachment point and was dropped at the merge. The change log
in `docs/spec/serve-live-reload.md` records the gap.

**Rebuild target (updated 2026-07-25 — the original entry pointed at
`atomic/internal/serve/templates/layout.html`, deleted in the React SPA cutover):** the work
now lives in the React frontend. `src/hooks/useLiveReload.ts` already owns the SSE subscription
that page mode consumes; `src/pages/Graph/Graph.tsx` mounts the carried engine from
`public/graph-core.js` plus the `system-graph.js` / `code-graph.js` profiles via the `window`
contracts. Reconcile means subscribing the graph page to the same hook, diffing a fresh
`/graph/data` against the mounted set, and patching or remounting under the settle-then-pause
policy with a layout-cache re-key. The cache key is fingerprint-derived, so invalidation is
already handled.

Both graph views need it — docs (`/graph/data`) and code (`/code/graph/data`).
