# SQL extractor: dbt + Snowflake coverage depth

Point-in-time research (2026-06-24) scoping issues **#68 (dbt Jinja+SQL)** and **#69 (Snowflake dialect)**
before designing. Read-only investigation: in-repo baseline (what the extractor does today + where new code
attaches) plus primary-doc verification of every Snowflake and dbt construct. Feeds the implementation plan;
not a maintained doc.

Target file for both: `atomic/internal/codeintel/extraction/standalone/sql.go` (object-level regex extractor;
definitions → nodes, FROM/JOIN → `references`, INSERT/UPDATE/DELETE/MERGE → `writes`, EXEC/CALL/APPLY → `calls`).

## Baseline (in-repo) — what exists, where to attach

- **One dialect-agnostic regex set, no branching.** Dialect differences are absorbed inside shared patterns,
  not switched on. `modPat` (`sql.go:193`) **already absorbs `OR REPLACE` / `OR ALTER` / `IF NOT EXISTS`** —
  so `CREATE OR REPLACE TABLE` already parses. (This corrects the Snowflake report, which assumed `OR REPLACE`
  was missing; only the *class/security* modifiers below are absent.)
- **Definitions today:** TABLE, VIEW, FUNCTION, PROC, TRIGGER, INDEX, SEQUENCE, SCHEMA, DATABASE, TYPE/ENUM,
  DOMAIN, SYNONYM, POLICY, plus ALTER ADD COLUMN/CONSTRAINT. `CREATE SEQUENCE` already works for Snowflake.
  VARIANT/OBJECT/ARRAY columns don't break (only the first identifier on a column line is read).
- **Body edges** live in `scanBodyEdges` (`sql.go:1446`); each kind is one `body*RE` + an `addRef(name, kind)`
  block (`:1478-1529`). New Snowflake body constructs attach here, mirroring the APPLY block just shipped.
- **Identifier pattern** `sqlQNameRaw` (`:327`) requires a leading `[A-Za-z_]`. Therefore `@stage` and `$1`
  do **not** match (no edge); `::cast` truncates harmlessly; `{{ ref('x') }}` produces **no edge** (silent
  miss, not a crash).
- **Indexer is strictly per-file by extension** (`orchestrator.go`): `.sql .ddl .pgsql .mysql`. A dbt
  `models/foo.sql` **is** ingested and silently degrades on Jinja. A `.sql.jinja` file is **not** ingested
  (extension unregistered). No `dbt_project.yml` awareness, no project pass, no Jinja preprocessing anywhere.
- **Strip order hazard (load-bearing for dbt):** `stripStrings` (`:171`) blanks the `'orders'` inside
  `{{ ref('orders') }}`. **A dbt ref must be harvested before `stripStrings` runs** — seam is the top of
  `SQLExtractor.Extract` (`:377`), ahead of `stripComments`/`stripStrings`.
- **Tests:** zero Snowflake/dbt coverage today. Conventions: `newSQL()`, `hasUnresolvedRef(refs,name,kind)`,
  `countUnresolvedRefs`, `findSQLNode*`, package-level `const <name>Fixture`.

## Snowflake (#69) — construct depth

| Construct | Lineage value | Node / edge | Verdict |
|---|---|---|---|
| Preamble class/security modifiers: `TRANSIENT`/`TEMPORARY`/`TEMP`/`VOLATILE` (table), `SECURE`/`RECURSIVE` (view) | **prereq** | widens definition regexes | **Do first** — idiomatic Snowflake; without it new node kinds below are missed on the common spelling |
| `COPY INTO <tbl> FROM @stage` / `COPY INTO @stage FROM <tbl\|query>` | **H** | body edges | **Do** — writes + references; direction by `@`-prefix |
| `CREATE TASK … AFTER <t> … AS <sql>/CALL` | **H** | new node + task→task `references` + body edges | **Do** |
| `CREATE STREAM … ON {TABLE\|VIEW\|EXTERNAL TABLE\|STAGE\|…} <name>` | **H** | new node + `references` to source | **Do** |
| `CREATE STAGE` + `FROM @stage` | H def / M ref | new node + `references` | **Do** (anchors COPY's stage end; needs `@`-token tolerance) |
| `… CLONE <src>` on table/view | **M** | `references` (new → src) | **Do** — cheap add-on to the preamble work |
| `CREATE FILE FORMAT` | M | node only, no edges | **Optional** — node completeness only |
| `VARIANT`/`OBJECT`/`ARRAY` types, `QUALIFY`, `::` cast, `$1` positional, `FLATTEN`-as-call | **L** | none | **Skip** — column/syntax-level; only guard against false positives |

Primary sources: docs.snowflake.com create-table / create-view / create-task / create-stream / create-stage /
copy-into-table / qualify / flatten (cited in full in the research dispatch).

## dbt (#68) — construct depth

The DAG lives in Jinja, not the SQL — extract `ref`/`source` directly; that is cheaper and more reliable than
parsing the templated SQL. Two ticket-premise corrections from primary docs:

- **`ref()` grammar:** 1 string literal = model; **2 literals = `(package, model)` — package FIRST**; version is
  a **keyword** `version=`/`v=`, *not* a bare 2nd positional. The ticket's "2nd arg = model" was wrong; a regex
  that treats `ref('a','b')` as `(model a, edge b)` would corrupt edges. (docs.getdbt.com/reference/dbt-jinja-functions/ref, …/builtins)
- **Refs are not confined to FROM.** They appear inside `{% if %}`, `{% for %}`, `{% set %}`, and macro bodies
  (verified: incremental models put `ref()` inside `{% if is_incremental() %}`). The ref/source scan must cover
  the **whole file** (minus `{# #}` comments) — which is simpler than clause-aware SQL scanning.

| Element | Verdict |
|---|---|
| Gate: file contains `{{` / `{%` / `{#` → run Jinja pass; else no-op (plain SQL unchanged) | **Do** — self-activating, harmless to non-dbt SQL |
| `ref('model')` / `ref('pkg','model')` / `ref('model', v=N)` → model→model edge (string-literal args only) | **Do** — correct 1/2-arg+version disambiguation |
| `source('src','tbl')` (always 2-arg) → model→source edge | **Do** |
| Model node = file **basename** (dir excluded; alias does not change the `ref` name) | **Do** — resolvable per-file, no catalog |
| Strip Jinja → run existing extractor on residual for table lineage. **Placeholder-substitute** `{{ ref('x') }}`→`__dbt_ref_x` (not delete) to avoid dangling `FROM`; dedup placeholder edges | **Do** — substitution is the clean default |
| `ref(var('x'))` / non-literal args; cross-project resolution; `{{ this }}` self-edge; `alias` table names | **Skip** — not statically resolvable per-file |
| Macro nodes + `{{ macro() }}` call edges; refs inside macro bodies | **Defer v1** — needs a built-in/package denylist to avoid false edges |
| `.sql.jinja` extension support | **Decide** — register the extension, or only handle `.sql` dbt models (already ingested) |

Primary sources: docs.getdbt.com ref / source / builtins / jinja-macros / custom-aliases / using-jinja /
dont-nest-your-curlies (cited in full in the research dispatch).

## Open decisions (shape the implementation plan)

1. **Breadth.** Full both-tickets in one pass, or stage it — Snowflake (preamble+COPY+TASK+STREAM+STAGE+CLONE)
   and dbt (gate+ref/source+placeholder), deferring the optional/low-value tails?
2. **dbt residual strategy.** Placeholder-substitution (recommended) vs two-pass edge-dedup.
3. **dbt macros.** Defer macro nodes/call-edges and macro-body refs to v2 (recommended), or include now?
4. **`.sql.jinja`.** Register the extension, or rely on `.sql` dbt models (already ingested)?
5. **Versioned refs.** Collapse `ref('m', v=N)` to a single `m` node (recommended) vs distinct `m@vN` nodes.
6. **PR shape.** One combined PR (one branch already), or land Snowflake and dbt as separate commits/PRs.

## Status

Research complete. Implementation plan (design + spec) pending the decisions above. No code written.
