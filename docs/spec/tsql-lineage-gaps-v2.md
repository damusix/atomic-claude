# T-SQL lineage gaps v2 — temp tables, OUTPUT INTO, PIVOT, column-level

## Goal

Close the four remaining T-SQL lineage gaps from issue #70 with real, false-positive-free lineage:
proc-scoped temp tables / table variables, `OUTPUT … INTO` write edges, `PIVOT`/`UNPIVOT` source
preservation, and object→column references via alias resolution that resolve to the specific column
node. Done = all four produce correct, resolved edges, the standalone suite is green, and a real-repo
eval confirms the new edges resolve without false lineage.

Parent extractor contract: `docs/spec/sql-language-support.md`. Continues `docs/spec/tsql-lineage-gaps.md`
(issue #70).

## Non-goals

- **Column→column derivation lineage** (output column derives from input columns). Needs
  view-output-column extraction + SELECT-expression parsing; separate epic.
- **Unqualified column references** (`SELECT id FROM acct`) — ambiguous; only qualified `alias.col` emit.
- **New NodeKind or EdgeKind.** Temp tables reuse `table` + `Metadata`; column refs reuse `references`.
- **Scalar variable edges.** Only `@x` declared `TABLE(…)` is a relation.
- **Resolver redesign.** Gaps 1–3 touch no resolver code. Gap 4 adds two bounded, SQL-scoped tweaks to
  `name_matcher.go` (qualified-column routing + exact-QName preference) — not a new resolution model.
- **Column type semantics**, query-plan analysis, stored-proc control flow (inherited from parent).

## Success criteria

- [ ] A routine declaring `#tmp` (via `CREATE TABLE #tmp` or `SELECT … INTO #tmp`) and reading it emits
      a `writes` then a `references` edge from the routine to a single temp node whose `Name` is a
      synthetic routine-scoped string (no `.`/`/`/`::`) and whose `Metadata` carries `{"temp":"local"}`
      plus the written token `#tmp`.
- [ ] Two procedures in the **same file** that each declare `#tmp` produce **two distinct** temp nodes;
      neither procedure's reads/writes resolve to the other's `#tmp` (synthetic name encodes the routine).
- [ ] A `DECLARE @t TABLE(…)` table variable inserted into and selected from emits `writes` +
      `references` to a routine-scoped node; a scalar `DECLARE @id INT` emits **no** edge.
- [ ] A global `CREATE TABLE ##g` written in one procedure and read in another **in the same file**
      resolves both edges to one shared `##g` node; a file declaring `##g` twice yields **one** node
      (file-level dedup), not two.
- [ ] `INSERT/UPDATE/DELETE/MERGE … OUTPUT <list> INTO <target>` emits a `writes` edge to `<target>`
      (real table, or routine-scoped `@tvar`/`#temp`). The match bounds the OUTPUT→INTO gap to an
      output-list character run, so a statement boundary (FROM/SELECT/VALUES/`;`) between them prevents
      a match.
- [ ] `OUTPUT <list>` **without** `INTO` emits no edge, and `A OUTPUT inserted.* INTO Log; INSERT INTO B …`
      links only `Log` and `B` to their own statements — no cross-statement false edge.
- [ ] `FROM <tbl> PIVOT (…)` and `FROM (subquery) UNPIVOT (…)` keep the source `references` edge and emit
      no edge to `PIVOT`/`UNPIVOT`, the aggregate, or the IN-list value columns. A test asserts this and
      is committed regardless of whether a guard proves necessary.
- [ ] A qualified column reference `a.id` where `FROM acct a` is in scope emits a `references` edge from
      the routine/view that **resolves to the `acct.id` column node**; a schema-qualified `dbo.acct`
      resolves to `dbo.acct.id`; an unqualified `id` emits no column edge.
- [ ] Resolver tweaks are SQL-scoped: a non-SQL single-dot `receiver.method` reference resolves exactly
      as before (existing resolution tests across languages stay green).
- [ ] Existing standalone suite stays green (APPLY, FLATTEN, COPY, FROM/JOIN, EXEC/CALL, MERGE, dbt,
      Snowflake, CTE-shadow — all unaffected); full `go test ./...` green.
- [ ] Real-repo eval: index a T-SQL repo exercising temp tables / OUTPUT-INTO / PIVOT / qualified
      columns; Haiku batch verification (≥2 agents) confirms emitted edges match source and finds no
      false lineage; counts recorded.

## Approach

Synthetic routine-scoped node names (mirroring the `__dbt_ref_` precedent) + reuse of
`table`/`references`, with object→column slice **4a only** (qualified refs via an alias→table map plus
two bounded SQL-scoped resolver tweaks). Gaps 1–3 are `sql.go`-only; gap 4 also touches
`resolution/name_matcher.go`. See `docs/design/tsql-lineage-gaps-v2.md`.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Temp tables + table variables — body declaration scan, synthetic routine-scoped nodes (`Name` = synthetic, written token in `Metadata`), read/write edges incl. `SELECT … INTO`; global `##g` file-deduped | `extraction/standalone/sql.go` (+ `sql_test.go`) | atomic-implementer (mode: feature) | ~2 | Routine-local `#tmp`/`@t` emit `writes`+`references` to a distinct node resolving via `byExactName`; two same-file procs' `#tmp` stay distinct; scalar `@id` → nothing; `##g` shared cross-proc same-file + deduped; `Metadata{"temp":…}`; suite green |
| 2 | `OUTPUT … INTO <target>` write edges (reuse CP1 scoping for `@tvar`/`#temp`) | `extraction/standalone/sql.go` (+ `sql_test.go`) | atomic-implementer (mode: surgical) | ~2 | `OUTPUT … INTO t` → `writes` t bounded by output-list char run; OUTPUT-without-INTO → no edge; no cross-statement false link; temp targets resolve routine-scoped |
| 3 | `PIVOT` / `UNPIVOT` object-level test (source ref preserved, no false edges); add a guard only if a false edge is found | `extraction/standalone/sql.go` (+ `sql_test.go`) | atomic-implementer (mode: surgical) | 1-2 | Committed test: `FROM tbl PIVOT(…)` and `FROM (subquery) UNPIVOT(…)` keep source `references`; no edge to PIVOT/UNPIVOT/aggregate/IN-list values |
| 4 | Column-level lineage 4a — alias→table map from FROM/JOIN; qualified `alias.col` → column `references`; + SQL-scoped resolver tweaks (single-dot SQL → qualified routing; `byQualifiedName` prefers exact full-QName) | `extraction/standalone/sql.go`, `resolution/name_matcher.go` (+ tests) | atomic-implementer (mode: feature) | ~3 | `a.id` with `FROM acct a` resolves to `acct.id` column node; `dbo.acct` → `dbo.acct.id`; unqualified/ambiguous skipped; alias map honors `JOIN`; non-SQL `receiver.method` resolution unchanged; table-level refs unchanged |
| 5 | Eval + real-repo validation | `scripts/code-eval/` (corpus/fixtures), real T-SQL repo | atomic-implementer (mode: feature) | ~2 | Index a T-SQL repo; non-zero temp/OUTPUT-INTO/column edges; Haiku batch verification (≥2 agents) confirms edges match source, no false lineage; counts recorded |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `@scalar` mistaken for table variable | med | Strict gate on `DECLARE @x TABLE(…)` set; scalars skipped |
| `OUTPUT … INTO` over-matches a later statement's `INTO` | med | Restrict OUTPUT→INTO gap to output-list char class `[\w.\[\]*,\s]+`; a statement boundary breaks the run → no match; negative test asserts it |
| Resolver tweak 1 leaks into non-SQL resolution | med | Gate strictly on `ref.Language == LanguageSQL`; cross-language resolution tests stay green |
| Resolver tweak 2 (prefer-exact) regresses other languages | low | Prefer-exact only narrows when an exact full-QName candidate exists; full suite + eval guard |
| Synthetic temp nodes clutter `code search` | low | Reuse `table` kind + `Metadata{"temp":…}`; written token in metadata |
| Qualified column ref resolves to a same-named column in another schema | low | Emit table-qualified-as-written; exact-QName preference + same-file bonus; documented limitation, silent degrade |
| Global `##g` declared twice in one file → two nodes | low | File-level dedup of global temp nodes by name |

## Implementation log

- 2026-06-24 — Built across 5 checkpoints on branch `tsql-lineage-gaps-v2` (subagent
  implement→review loop, commit per green). CP1 temp tables/table variables (synthetic
  routine-scoped names; two same-file `#tmp` stay distinct; scalar `@x` excluded; `##g`
  file-deduped). CP2 OUTPUT…INTO writes (output-list-bounded guard, ghost-target boundary
  tests). CP3 PIVOT/UNPIVOT confirmed object-level no-op (source via FROM). CP4 column-level
  4a (alias→table map + qualified `alias.col` refs) + two SQL-scoped resolver tweaks
  (single-dot→qualified routing, `byQualifiedName` prefer-exact).
- 2026-06-24 — CP5 real-repo eval surfaced a third resolver gap completing SC8: the pipeline
  pre-filter (`hasAnyPossibleMatch`) checked the full ref name against a bare-name cache, so
  emitted qualified column refs (`dbo.Account.account_id`) were dropped before reaching
  `byQualifiedName` — column refs emitted but never resolved. Fixed with an SQL-scoped
  simple-name fallback in `resolveOne`; added an end-to-end pipeline test (the unit tests
  bypassed the pre-filter by calling `matchReference` directly). Validated on a conventional
  `CREATE PROCEDURE` fixture (`scripts/code-eval/fixtures/tsql-lineage/sales_ops.sql`): 29
  lineage edges, two Haiku verifiers, 0 false / 0 missing; `account_id` (in 3 tables) resolved
  to the correct table's column via prefer-exact. Real-repo finding (ALTER PROCEDURE bodies +
  dynamic SQL not scanned) filed as follow-up `tsql-alter-procedure-bodies` — pre-existing, not
  a regression.

**Squashed to d54034f — 2026-06-24.** Per-checkpoint SHAs are historical (unreachable from any branch). **Merged into main as d54034f — 2026-06-24.**

## Change log

<!-- Populated on first amendment after approval. -->
