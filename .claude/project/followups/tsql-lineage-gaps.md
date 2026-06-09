---
id: tsql-lineage-gaps
title: Close T-SQL lineage gaps in standalone SQL extractor
created: "2026-06-09"
origin: |
    code-intel SQL coverage audit 2026-06-09
kind: plan
review_by: "2026-08-08"
status: open
file: atomic/internal/codeintel/extraction/standalone/sql.go
---

Audit (2026-06-09) of sql.go against T-SQL: definitions are strong (brackets, GO, CREATE OR ALTER, CREATE TYPE FROM/AS TABLE, SYNONYM, computed cols) and object-level body edges cover FROM/JOIN, EXEC/CALL, MERGE INTO, INSERT/UPDATE/DELETE, FK. The regex extractor degrades silently (misses edges, never chokes). Remaining gaps, in rough priority:

- CROSS APPLY / OUTER APPLY: 0 coverage — table-valued-function lineage missed. (~1 regex + test, highest value.)
- Table variables (@t TABLE(...)) and temp tables (#tmp): 0 — intra-proc lineage invisible.
- OUTPUT clause (MERGE/INSERT ... OUTPUT): 0 — output-target lineage missed.
- PIVOT / UNPIVOT: 0.
- Column-level lineage: none — object-level only (this is the industry-wide weak spot for T-SQL per the research, not just ours).

Strategic note: per the 2026-06-09 research, no general code-graph tool graphs T-SQL source at all, so even partial closure here is differentiating. APPLY + table-vars are the cheap, high-value wins.
