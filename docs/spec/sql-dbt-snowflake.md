# dbt + Snowflake SQL extraction

Extend the standalone SQL extractor to cover **Snowflake dialect** (#69) and **dbt models** (#68). Object-level
lineage only. Derived from `docs/design/sql-dbt-snowflake.md`; baseline + primary-doc research in
`docs/research/sql-dbt-snowflake-coverage.md`. Parent spec: `docs/spec/sql-language-support.md` (the extractor,
its taxonomy, `scanBodyEdges`, the strip helpers). This spec adds constructs; it does not change parent behavior.

Code lives in `atomic/internal/codeintel/extraction/standalone/sql.go` (+ `sql_test.go`) and the taxonomy in
`atomic/internal/codeintel/types/` (`types.go` + `types_test.go`). No orchestrator or extension-registry change
(see Non-goals: `.sql.jinja` is v2; plain `.sql` dbt models are already ingested).

Two corrections from research, load-bearing:

- `modPat` (`sql.go:193`) already absorbs `OR REPLACE` / `OR ALTER` / `IF NOT EXISTS`. The Snowflake preamble gap
  is only the class/security modifiers below — do not re-add `OR REPLACE`.
- dbt `ref('a','b')` = `(package, model)` with package **first**; the version is a keyword (`version=`/`v=`), not a
  2nd positional. Treating a bare 2nd string literal as the model corrupts edges.

## Scope

In: Snowflake core (preamble modifiers, COPY INTO, CREATE TASK, CREATE STREAM, CREATE STAGE, CLONE) + dbt core
(Jinja gate, ref/source harvest, model node, placeholder residual) + O3 syntax-tolerance guards.

Out (v2, see Non-goals): `.sql.jinja` ingestion; FILE FORMAT node; VARIANT/OBJECT/ARRAY typing; FLATTEN-as-
reference; dbt macros and macro-body refs; versioned-ref distinct nodes; alias warehouse names; dbt project-path
awareness; standalone top-level `COPY INTO` (no owning definition).

## Taxonomy changes

Add four `types.NodeKind` constants (append to the const block in declared order after `NodeKindPolicy`, and to
`AllNodeKinds`). The exact identifiers and string values are contract:

- `NodeKindStage` = `"stage"` — Snowflake `CREATE STAGE`.
- `NodeKindStream` = `"stream"` — Snowflake `CREATE STREAM`.
- `NodeKindTask` = `"task"` — Snowflake `CREATE TASK`.
- `NodeKindModel` = `"model"` — a dbt model (one per templated model file).

Update `TestNodeKindCount` in `types_test.go`: `31` → `35`, and add the four identifiers above to the appendix-C
`want` set (membership is asserted by exact constant, not just count). **No new EdgeKind** — `AllEdgeKinds` stays
13; every relationship below reuses `references` / `writes` / `calls`.

## Part A — Snowflake (#69)

Two seams: **definitions** (`CREATE STAGE/STREAM/TASK`, and the preamble/CLONE work on `CREATE TABLE/VIEW`) are
top-level — a new package-level `*RE` + an extraction loop over the whole source, mirroring `sequenceRE` and its
loop. **Body edges** (`COPY INTO`) attach inside `scanBodyEdges`, owned by the enclosing routine/task. Implement
A1 first.

### A1 — Preamble class/security modifiers (prerequisite)

Widen the definition preamble so these parse and still produce the correct node:

- Table: `CREATE [OR REPLACE] {TRANSIENT|TEMPORARY|TEMP|VOLATILE|LOCAL|GLOBAL}* TABLE [IF NOT EXISTS] <name>`.
- View: `CREATE [OR REPLACE] {SECURE|RECURSIVE}* [TEMP|TEMPORARY]? VIEW [IF NOT EXISTS] <name>`.

Implementation: a `classPat` optional-keyword fragment spliced after `modPat` in `tableRE` / `viewRE`. Must not
regress the existing Postgres/MySQL/T-SQL/ANSI definition tests.

Success: `CREATE OR REPLACE TRANSIENT TABLE dbo.t (...)` → a `table` node `t`; `CREATE OR REPLACE SECURE VIEW v AS
SELECT ... FROM base` → a `view` node `v` + a `references` edge to `base`.

### A2 — COPY INTO (body edge, owned by the enclosing routine/task)

Add a `bodyCopyIntoRE` block to `scanBodyEdges` so a `COPY INTO` inside a routine or task body produces edges
owned by that routine/task:

- `COPY INTO <tbl> FROM @<stage>[/path]` → `writes` to `<tbl>` + `references` to the stage name.
- `COPY INTO @<stage> FROM <tbl|(query)>` → `writes` to the stage + `references` to the table(s). Direction is
  decided by whether the COPY target token starts with `@`.

Stage tokens (`@my_stage`, `@~/path`, `@%tbl`) are captured to the bare stage identifier (strip the leading `@`
and any `/path` suffix). A standalone top-level `COPY INTO` (not inside any definition) is **not** captured in v1
— consistent with how a bare top-level `INSERT` is not captured (no owning node). See Non-goals.

Success: a task/proc body `COPY INTO fact FROM @load_stage` → `writes` to `fact` + `references` to `load_stage`;
`COPY INTO @out_stage FROM fact` → `writes` to `out_stage` + `references` to `fact`.

### A3 — CREATE TASK

New `task` node (top-level definition loop) for `CREATE [OR REPLACE] TASK [IF NOT EXISTS] <name>`. Two edge
sources:

- `AFTER <t1>[, <t2> …]` → one `references` edge per predecessor task. **This requires a dedicated regex on the
  CREATE TASK statement text** — it must NOT be routed through `scanBodyEdges` (the word `AFTER` is in the
  ref-keyword denylist and `scanBodyEdges` only matches FROM/JOIN/INSERT/UPDATE/DELETE/MERGE/EXEC/CALL, so the
  AFTER predecessors would be silently dropped).
- Task body (`AS <sql>` / `AS CALL proc(...)`) → reuse `scanBodyEdges` with the task as the owning node:
  `references` for FROM/JOIN, `writes` for INSERT/…, `calls` for CALL/EXEC, and COPY per A2.

Success: `CREATE TASK load_t AFTER stg_t, dim_t AS INSERT INTO fact SELECT * FROM stg` → `task` node `load_t`;
`references` to `stg_t` and `dim_t`; `writes` to `fact`; `references` to `stg`.

### A4 — CREATE STREAM

New `stream` node (top-level definition loop) for `CREATE [OR REPLACE] STREAM [IF NOT EXISTS] <name> ON
{TABLE|VIEW|EXTERNAL TABLE|STAGE|DYNAMIC TABLE|EVENT TABLE} <source>` (alternation on the object keyword) → one
`references` edge to `<source>`.

Success: `CREATE STREAM s ON TABLE orders` → `stream` node `s` + `references` to `orders`; the same for the
`ON VIEW` / `ON EXTERNAL TABLE` / … variants.

### A5 — CREATE STAGE

New `stage` node (top-level definition loop) for `CREATE [OR REPLACE] [TEMPORARY|TEMP] STAGE [IF NOT EXISTS]
<name>`. No outbound edges (URL/credentials are option-level). Pairs with A2's `@stage` references.

Success: `CREATE STAGE my_stage URL='s3://...'` → `stage` node `my_stage`.

### A6 — CLONE

`CREATE [OR REPLACE] {TRANSIENT|…}* {TABLE|VIEW} <new> CLONE <src>` → a `references` edge `new → src` (the body
has no FROM; this is the only clone-lineage signal). Folds into A1's preamble work.

Success: `CREATE OR REPLACE TRANSIENT TABLE staging CLONE prod` → `table` node `staging` + `references` to `prod`.

## Part B — dbt (#68)

A new pre-pass that runs at the **top of `SQLExtractor.Extract`, operating on the raw `source` string before any
stripping**. Ordering is contract: `stripStrings` (`sql.go:382`) blanks the quoted argument inside
`{{ ref('x') }}`, so the ref/source harvest must read raw `source`, not the `stripped` value. The pre-pass
produces (a) the dbt edges and (b) a placeholder-substituted residual that the rest of `Extract` then processes
through its normal `stripComments`/`stripStrings` pipeline.

### B1 — Activation gate

Run the pre-pass only when the raw source contains `{{`, `{%`, or `{#`. Otherwise it is a no-op and the existing
extractor runs unchanged. Success: a plain `.sql` file with no Jinja produces byte-identical results to today.

### B2 — ref() harvest (whole raw source, after removing `{# … #}` comments)

Regex anchored on `ref(` + quoted literal(s), tolerating whitespace-control (`{{-`, `-}}`) and surrounding
spaces. Grammar disambiguation (contract):

- 1 string literal → model name = that literal.
- 2 string literals → `(package, model)`; model name = the **second** literal.
- A `version=`/`v=` keyword arg is ignored for the edge (versioned refs collapse to the model name).

Emit a `references` edge from this file's `model` node to the model name. Scan the **whole source** (refs live
inside `{% if %}`/`{% for %}`/`{% set %}` bodies), excluding `{# … #}`.

Success: `{{ ref('stg_orders') }}` → `references` to `stg_orders`; `{{ ref('pkg','stg_orders') }}` → `references`
to `stg_orders` (not `pkg`); `{{ ref('stg_orders', v=2) }}` → `references` to `stg_orders`; a `ref()` inside
`{% if is_incremental() %}` is still captured.

### B3 — source() harvest

Always-2-arg `{{ source('a','b') }}` → a `references` edge to the synthetic name `a.b`. No `source` node is
created (the definition lives in `sources.yml`, not parsed). Success: `{{ source('raw','orders') }}` →
`references` to `raw.orders`.

### B4 — model node

One `model` node per gated file (B1 true), named by the file **basename** without extension
(`models/staging/stg_orders.sql` → `stg_orders`; directory excluded; `alias` does not change it). `.sql` files use
the existing single-extension basename helper unchanged. This `model` node is the resolution target for other
models' `ref()`.

### B5 — placeholder substitution + residual extract

After harvesting B2/B3 from raw source: replace `{{ ref('x') }}` → `__dbt_ref_x` and `{{ source('a','b') }}` →
`__dbt_src_a__b`, then blank remaining `{{ … }}` / `{% … %}` length-preservingly (line numbers stay correct).
Feed that residual into the rest of `Extract` (its normal strip pipeline) for table/column lineage. Then **drop
any residual unresolved reference whose name begins `__dbt_ref_` or `__dbt_src_`** — the harvest already owns
those edges (prevents double counting and keeps `FROM {{ ref('x') }}` from leaving a dangling/garbage reference).
The `__dbt_ref_` / `__dbt_src_` prefixes are contract; choose them so no real table name collides.

Success: `SELECT * FROM {{ ref('stg') }} JOIN real_tbl ON ...` yields exactly one `references` to `stg` (B2) and
one `references` to `real_tbl` (residual SQL pass) — no `__dbt_ref_stg` reference survives.

## Part C — O3 syntax-tolerance guards

No new edges; regression tests proving Snowflake syntax does not break extraction or emit garbage:

- `QUALIFY ROW_NUMBER() OVER (...) = 1` does not truncate body-edge scanning or emit a `QUALIFY` reference.
- `col::VARIANT` / `x::NUMBER` casts do not emit a spurious reference and do not corrupt the preceding identifier.
- `SELECT t.$1, t.$2 FROM @stg t` does not emit a `$1`/`$2` reference.
- `FROM t, LATERAL FLATTEN(INPUT => t.col)` and `TABLE(FLATTEN(...))` emit a `references` edge to `t` only —
  never to `FLATTEN`/`LATERAL`/`TABLE` and not treated as a real table in the FROM list.

## Non-goals (v2 / out of scope)

Do not implement: `.sql.jinja` ingestion (needs orchestrator compound-extension routing + double-extension
basename — deferred; plain `.sql` dbt models are already ingested); standalone top-level `COPY INTO` with no
owning definition (needs a file-owner node concept); FILE FORMAT node (O1); VARIANT/OBJECT/ARRAY column typing
(O2); FLATTEN argument as a reference (O4); dbt macro nodes, `{{ macro() }}` call edges, and refs inside macro
bodies (O5/O6); versioned refs as distinct `m@vN` nodes (O7 — core collapses to `m`); dbt `alias` warehouse-name
capture (O8); dbt project-path / `dbt_project.yml` awareness (O9). Also out: `ref(var('x'))` / non-literal ref
args and cross-project resolution (not statically resolvable — emit nothing rather than a garbage edge);
`{{ this }}` self-edges (strip).

## Test plan

Follow `sql_test.go` conventions (`newSQL()`, `hasUnresolvedRef`, `countUnresolvedRefs`, `findSQLNode*`,
package-level `const <name>Fixture`). Cover, at minimum, one fixture per success criterion above, plus:

- A2 both COPY directions inside a task/proc body in one fixture.
- A3 a TASK with an `AFTER` list + a body write+reference, asserting the AFTER predecessors emit edges (guards the
  keyword-denylist trap).
- B2 the three ref grammars in one fixture, **including an explicit `ref('pkg','model')` case asserting the edge
  targets `model` not `pkg`** (highest-risk correctness point).
- B5 a `FROM {{ ref('x') }} JOIN real_tbl` fixture asserting exactly the two expected refs and zero `__dbt_*`.
- B1 a plain-SQL no-Jinja fixture asserting unchanged output.
- C the four O3 negative guards.
- Taxonomy: `TestNodeKindCount` 31→35 with the four named kinds present.

## Implementation sequencing

Land as ordered checkpoints, each green before the next: A1 (preamble — verify no regression) → A5 (stage) → A2
(COPY body edge) → A3 (task) → A4 (stream) → A6 (clone) → B taxonomy+model node+gate → B2/B3 (ref/source harvest)
→ B5 (placeholder residual) → C (guards). Snowflake and dbt are independent; either may go first, but A1 precedes
the other Snowflake items and A5 precedes A2 (so `@stage` references can resolve to a stage node).

## Change log

- 2026-06-24 — Revised after spec-mode review (pre-implementation, no prior build).
  **What changed:** `.sql.jinja` ingestion moved to Non-goals (v2) — `filepath.Ext` returns `.jinja`, so it needs
  orchestrator compound-extension routing, not a registry list-add; plain `.sql` dbt models are already ingested.
  COPY INTO (A2) specified as a body edge owned by the enclosing routine/task via `scanBodyEdges`; standalone
  top-level COPY moved to Non-goals. CREATE TASK `AFTER` (A3) specified as a dedicated regex, not `scanBodyEdges`
  (the `AFTER` keyword is denylisted there). dbt pre-pass seam (Part B) corrected to "raw `source` before
  `stripStrings`". NodeKind constant identifiers named explicitly (`NodeKindStage/Stream/Task/Model`). Definition
  vs body seams disambiguated in Part A.
  **Why:** the review found the `.sql.jinja` registry add and the COPY/AFTER/seam phrasings were mechanically
  wrong or ambiguous and would have failed silently in the build.
- 2026-06-24 — Created. Core scope for #68 + #69 + O3 guards; O1/O2/O4–O9 deferred to v2. dbt residual strategy =
  placeholder substitution. Corrects two ticket premises (Snowflake `OR REPLACE` already handled; dbt `ref` 2-arg
  = package-first, version is a keyword).
