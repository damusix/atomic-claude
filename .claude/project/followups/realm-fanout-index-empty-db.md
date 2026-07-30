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
