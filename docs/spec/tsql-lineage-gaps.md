# T-SQL APPLY lineage

Extend the standalone SQL extractor (`atomic/internal/codeintel/extraction/standalone/sql.go`) so that
`CROSS APPLY` and `OUTER APPLY` table-valued-function invocations in a routine/view body produce a
`calls` edge to the invoked function. This closes the highest-value T-SQL lineage gap identified in the
2026-06-09 coverage audit (issue #70): APPLY-driven table-valued-function lineage was previously invisible.

Parent spec: `docs/spec/sql-language-support.md` (the extractor, its taxonomy, and the body-edge engine
`scanBodyEdges`). This spec amends only the body-edge behavior; nothing else in the parent changes.

## Why

`scanBodyEdges` captures `FROM`/`JOIN` (references), `INSERT`/`UPDATE`/`DELETE`/`MERGE` (writes), and
`EXEC`/`CALL` (calls). It deliberately does **not** run general function-call-in-expression scanning over
routine bodies — that was found too noisy (see F-7 in the parent: fn-call capture is scoped to specific
clauses). As a result, a table-valued function invoked through `CROSS APPLY dbo.GetLines(o.id)` produced
no edge at all. APPLY is the canonical T-SQL idiom for correlated table-valued-function joins, so its
absence left a class of proc→function lineage uncaptured.

APPLY is the cheapest, lowest-false-positive win of the audit's gap list: the invoked target is always a
parenthesized invocation, so it can be matched precisely without the noise that blanket expression
scanning would introduce.

## Approach

One regex, one dispatch block in `scanBodyEdges`, mirroring the existing `bodyExecCallRE` → `calls` path.

- New package-level regex:

      var bodyApplyRE = regexp.MustCompile(`(?i)\b(?:CROSS|OUTER)\s+APPLY\s+(` + sqlQNameRaw + `)\s*\(`)

  The trailing `\s*\(` is required: it restricts matches to **table-valued-function invocations**
  (`APPLY name(...)`), which is the lineage-relevant case. A correlated-derived-table apply
  (`CROSS APPLY (SELECT ... FROM real_table) x`) does not match — its capture group would have to start
  with `(`, which `sqlQNameRaw` cannot — and that is correct: the inner `FROM real_table` is already
  captured by the existing `FROM`/`JOIN` scan, so no lineage is lost.

- New dispatch block in `scanBodyEdges`, placed after the `EXEC`/`CALL` block, following the identical
  shape used by every other body-edge kind:

      // CROSS APPLY / OUTER APPLY <tvf>( → calls
      for _, m := range bodyApplyRE.FindAllStringSubmatchIndex(body, -1) {
          rawName := body[m[2]:m[3]]
          _, name := parseQName(rawName)
          if name != "" {
              addRef(name, types.EdgeKindCalls, m[2])
          }
      }

  Reuses `parseQName` (so `dbo.GetLines` resolves to `GetLines`) and `addRef` (so keyword filtering, CTE
  shadowing, and per-name+kind deduplication all apply unchanged).

`EdgeKindCalls` is the correct kind: a parenthesized invocation is a call, consistent with how
`EXEC`/`CALL` targets are emitted. Built-in TVFs (`STRING_SPLIT`, `OPENJSON`, …) will not resolve to a
node and the unresolved reference is dropped by the resolver — the same graceful-degradation behavior the
extractor already relies on for unknown targets.

## Contract / success criteria

1. `CROSS APPLY dbo.GetLines(o.id)` in a routine or view body emits a `calls` edge to `GetLines`.
2. `OUTER APPLY GetTags(t.id)` emits a `calls` edge to `GetTags` (both APPLY flavors covered).
3. A schema-qualified target (`CROSS APPLY analytics.fn_rollup(x)`) emits a `calls` edge to the bare
   function name `fn_rollup` (schema stripped by `parseQName`, matching every other body edge).
4. The table on the left of the APPLY keeps its existing edge: in
   `FROM Orders o CROSS APPLY dbo.GetLines(o.id) l`, `Orders` still emits a `references` edge.
5. No spurious edge to the keywords `CROSS`, `OUTER`, or `APPLY`.
6. A correlated derived-table apply (`CROSS APPLY (SELECT ... FROM src) x`) emits **no** APPLY-derived
   call edge; the inner `FROM src` still emits its `references` edge.
7. Existing behavior is unchanged: the full standalone extractor test suite stays green (`OUTER JOIN`,
   LATERAL, CTE shadow, EXEC/CALL, MERGE, etc. are unaffected — APPLY requires the literal `APPLY`
   keyword).

## Non-goals (explicitly out of scope — do not implement here)

These remaining gaps from issue #70 are **not** addressed by this change and must not be added to it. They
are not regex additions; they require a name-scoping decision the current name-based global resolver does
not support, and pulling them in would broaden the change past its reviewed contract:

- **Temp tables (`#tmp`, `##global`) and table variables (`@t`).** The resolver matches references to
  nodes by global name; two different procedures' `#tmp` are distinct objects but would collide under
  name-based resolution. Making intra-proc lineage correct needs proc-local scoping, not a regex — a
  separate design.
- **`OUTPUT ... INTO <target>`** write edges. Tractable for real-table targets but carries an
  INTO-disambiguation false-positive risk (OUTPUT without INTO scanning forward to an unrelated INTO);
  deferred to keep this change false-positive-free.
- **`PIVOT` / `UNPIVOT`.**
- **Column-level lineage.** Object-level only, per the parent spec; the audit flags this as an
  industry-wide weak spot, out of scope.

Issue #70 and the `tsql-lineage-gaps` follow-up remain open to track these.

## Checkpoints

A single checkpoint — one regex plus one dispatch block in `scanBodyEdges`, with tests.

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | CROSS/OUTER APPLY → `calls` edge | `atomic/internal/codeintel/extraction/standalone/sql.go` (+ `sql_test.go`) | `bodyApplyRE` dispatch emits a `calls` edge to the invoked TVF; schema stripped (`dbo.GetLines` → `GetLines`); left-of-APPLY table keeps its `references` edge; derived-table apply excluded; `CROSS`/`OUTER`/`APPLY` never become edges; full standalone suite stays green |

## Test plan

Add T-SQL-style fixtures and tests to `sql_test.go`, following the existing `const <name>Fixture` +
`Test<Name>` + WHY-comment convention and using the `hasUnresolvedRef` / `countUnresolvedRefs` helpers:

- `CROSS APPLY` TVF → `calls` edge (criterion 1).
- `OUTER APPLY` TVF → `calls` edge (criterion 2).
- Schema-qualified APPLY target → `calls` edge to bare name (criterion 3).
- Combined fixture: `FROM Orders o CROSS APPLY dbo.GetLines(o.id) l` asserts both the `Orders`
  `references` edge and the `GetLines` `calls` edge (criterion 4).
- Negative: `CROSS`/`OUTER`/`APPLY` never appear as reference or call targets (criterion 5).
- Negative: derived-table apply emits no APPLY call edge, but its inner `FROM` table does emit a
  `references` edge (criterion 6).

## Implementation log

- 2026-06-23 — Implemented on branch `tsql-apply-lineage` (autopilot, issue #70). One regex
  (`bodyApplyRE`) + one dispatch block in `scanBodyEdges` (`sql.go`); 6 tests in `sql_test.go`
  (`TestTSQLCrossApplyCalls`, `TestTSQLOuterApplyCalls`, `TestTSQLApplySchemaStripped`,
  `TestTSQLApplyKeywordsNotEdges`, `TestTSQLApplyDerivedTableNoCallEdge`). Reviewer: PASS (1 🔵 nit on the
  derived-table fixture, fixed in-iteration). Standalone package suite green; module-wide `go vet`,
  `gofmt`, `go build ./...`, and `atomic validate` clean.

## Change log

- 2026-06-24 — Added the `## Checkpoints` table required by `atomic validate` rule S5 (the spec predated S5
  enforcement). No content change — the single checkpoint restates the existing Approach/Contract.
- 2026-06-23 — Created. Scopes issue #70 to its highest-value win (CROSS/OUTER APPLY → `calls`); the other
  audit gaps are listed as explicit non-goals with rationale.
