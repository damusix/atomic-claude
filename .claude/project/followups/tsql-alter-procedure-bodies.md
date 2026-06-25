---
id: tsql-alter-procedure-bodies
title: T-SQL extractor skips standalone ALTER PROCEDURE/FUNCTION bodies (CREATE-stub + ALTER-body idiom invisible)
created: "2026-06-24"
origin: |
    #70 phase-2 CP5 real-repo eval (Brent Ozar First Responder Kit)
kind: finding
severity: nit
review_by: "2026-08-23"
status: open
---

Surfaced during #70 phase-2 CP5 validation. Indexing the Brent Ozar First
Responder Kit (real-world T-SQL, ~139K LOC) produced only 60 nodes / 46 edges —
near-zero temp-table and column lineage despite heavy `#temp` usage.

Two root causes, both pre-existing (NOT regressions from the #70 phase-2 work):

1. **`ALTER PROCEDURE` bodies are not scanned.** The kit defines procedures with
   a two-step idiom: a `CREATE PROCEDURE` stub inside a dynamic-SQL string
   (`EXEC sp_executesql N'CREATE PROCEDURE [dbo].[sp_X] AS'`) followed by a bare
   `ALTER PROCEDURE [dbo].[sp_X] ... AS BEGIN ... END` carrying the real body. The
   standalone SQL extractor scans `CREATE [OR ALTER] PROCEDURE/FUNCTION` bodies but
   not a standalone `ALTER PROCEDURE/FUNCTION`, so those bodies (and all their
   temp tables, writes, and column refs) are invisible.

2. **Dynamic SQL in `N'...'` strings is stripped** (correct behavior — it is not
   executable SQL at parse time). Much of the kit's logic is built as NVARCHAR
   strings, so it is legitimately out of scope.

Cause 1 is the actionable gap: add `ALTER PROCEDURE`/`ALTER FUNCTION` as
body-bearing routine definitions in the extractor (mirror the `CREATE` routine
arms; the `AS BEGIN ... END` body extraction is identical). This would light up a
large class of real DBA/utility T-SQL. Cause 2 is by-design.

The phase-2 features (temp tables, OUTPUT INTO, PIVOT, column lineage) were
validated instead against a conventional `CREATE PROCEDURE` fixture
(`scripts/code-eval/fixtures/tsql-lineage/sales_ops.sql`) — 29 edges, two Haiku
verifiers, 0 false / 0 missing.
