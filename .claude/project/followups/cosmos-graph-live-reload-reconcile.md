---
id: cosmos-graph-live-reload-reconcile
title: Rebuild graph-mode live-reload reconcile on the cosmos engine
created: "2026-07-08"
origin: |
    PR #123 merge resolution (cosmos engine swap vs serve-live-reload #122)
kind: plan
review_by: "2026-09-06"
status: open
file: atomic/internal/serve/templates/layout.html
---

serve-live-reload CP5 implemented graph-mode reconcile (SSE-triggered /graph/data refetch, id-diff patch, neighbor-seeded positions, IndexedDB re-key) against the old Cytoscape system-graph client. The cosmos engine swap (PR #123) replaced that client wholesale, so the capability was dropped at merge: in graph mode, live-reload events currently do nothing (page mode fully works; the server-side snapshot store is intact and /graph/data reflects changes on the next fetch). Rebuilding it against cosmos means: listen for atomic:live-reload in graph-core or the profiles, diff fresh /graph/data against the mounted set, patch or remount with the settle-then-pause policy and layout-cache re-key (fingerprint changes with the data, so the cache key machinery already handles invalidation). Spec docs/spec/serve-live-reload.md change log records the gap.
