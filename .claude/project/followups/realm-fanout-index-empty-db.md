---
id: realm-fanout-index-empty-db
title: Realm-fanout atomic code index reports success but writes 0-byte member db
created: "2026-07-19"
origin: |
    sql-string-match taxgentic validation 2026-07-19
kind: finding
severity: risk
review_by: "2026-09-17"
status: open
---

Reproduced twice on taxgentic: when 'atomic code index' runs in realm-fanout mode ('[server]'-prefixed output), it reports 'indexed: 462 files, 10060 nodes, 18107 edges' but leaves member .claude/.atomic-index/atomic.db as a 0-byte file. Running the same binary directly from inside the member directory (no fanout) persists correctly (~22MB). Suspect the fanout path's db handle/close or a wrong working-dir for the SQLite file.

**Root cause found (2026-08-08, taxgentic re-index session):** not data loss. Realm-mode `atomic code index` writes each member DB to `<realm>/.atomic/<key>.db` by design (SC 3, `cli/realm.go:indexRealmAll`) — the member-local `.claude/.atomic-index/atomic.db` is simply never the destination, and a 0-byte member-local file left by other codepaths (engine open before realm redirect) reads as "index lost". Two real sharp edges remain: (1) success output never names the DB path, so the mislocation is invisible; (2) `IndexAll` against an existing realm DB is incremental — unchanged files keep stale extraction after a binary upgrade, so post-upgrade "re-index" silently retains old-format nodes/edges (observed: mixed basename/full-specifier import names, surviving self-loop edges). Fix shape: print the target DB path in fan-out output, and add a force-full-reindex path (or version-stamp the DB and auto-full-reindex on binary schema/extraction change).
