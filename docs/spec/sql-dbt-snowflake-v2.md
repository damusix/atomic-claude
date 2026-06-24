# dbt + Snowflake SQL extraction — v2

Implements the full **v2 deferred menu** from `docs/spec/sql-dbt-snowflake.md` (v1) Non-goals. Object-level lineage
only. Approach, rationale, and the per-item seam analysis live in `docs/design/sql-dbt-snowflake-v2.md` — read it for
the *why*; this spec is the *what* (the contract). Parent spec: `docs/spec/sql-dbt-snowflake.md`. Grandparent:
`docs/spec/sql-language-support.md` (the extractor, `scanBodyEdges`, the strip helpers, the taxonomy).

Code: `atomic/internal/codeintel/extraction/standalone/sql.go` (+ `sql_test.go`),
`atomic/internal/codeintel/types/` (`types.go` + `types_test.go`), and
`atomic/internal/codeintel/indexer/orchestrator.go` (+ `standalone/exts.go`) for `.sql.jinja` routing.

## Scope

In (every v1-deferred item, user-selected): `.sql.jinja` ingestion; dbt macros (O5) + macro-body ref attribution (O6)
+ project-path role detection (O9) + versioned-ref distinct targets (O7) + `config(alias=)` capture (O8); Snowflake
`CREATE FILE FORMAT` (O1) + VARIANT/OBJECT/ARRAY column typing (O2) + `LATERAL FLATTEN` argument reference (O4) +
standalone top-level `COPY INTO` with a lazy file-owner node.

Out (still deferred): `dbt_project.yml` custom `model-paths` parsing (needs orchestrator project state — O9 here is
path-convention only); resolving a bare unversioned `ref('m')` to a specific version via model YAML (`latest_version`);
`ref(var('x'))` / non-literal ref args; cross-project ref resolution beyond the package-first grammar already handled;
`{{ this }}` self-edges (strip).

## Taxonomy changes

Add three `types.NodeKind` constants (append to the const block in declared order after `NodeKindModel`, and to
`AllNodeKinds`). Identifiers and string values are contract:

- `NodeKindFileFormat` = `"file_format"` — Snowflake `CREATE FILE FORMAT`.
- `NodeKindMacro` = `"macro"` — a dbt `{% macro %}` definition.
- `NodeKindScript` = `"script"` — a lazily-created per-file owner for standalone top-level statements (currently only
  a top-level `COPY INTO`).

Update `TestNodeKindCount` in `types_test.go`: `35` → `38`, and add the three identifiers to the appendix-C `want` set
(membership asserted by exact constant). **No new EdgeKind** — `AllEdgeKinds` stays 13; macros use `calls`, COPY uses
`writes`/`references`, everything else `references`.

## Part D — `.sql.jinja` ingestion

### D1 — extension routing

`model.sql.jinja` must route to the standalone SQL extractor. Two coordinated changes:

- Add `".sql.jinja"` to `standalone.SQLExtensions` (`exts.go`). This makes `IsSQLExt` (suffix match) and the standalone
  registry recognise it through the existing single-canonical-list wiring.
- The orchestrator keys `extToLanguage` / `standaloneExts` by `filepath.Ext`, which returns `.jinja` for the compound
  name. Add a `compoundExt(path) string` helper (returns `.sql.jinja` when the path ends in `.sql.jinja`, else
  `filepath.Ext(path)`) and use it at the **two sites that call `filepath.Ext` directly** (the file-walk filter ~L318
  and the per-file language lookup ~L382). The standalone-dispatch guard (~L409) reads the already-computed `ext`
  variable, so fixing L382 covers it — do not add a third `compoundExt` call there. `init()` populates `.sql.jinja`
  into both maps from `SQLExtensions`.

Success: a repo containing `models/stg.sql.jinja` indexes that file; `atomic code search stg --kind model` finds it.

### D2 — double-extension model basename

The dbt model node basename (`sql.go`, the B4 block) strips one extension via `filepath.Ext`, leaving `stg.sql` for
`stg.sql.jinja`. When the path ends in `.sql.jinja`, strip the whole compound suffix → `stg`; otherwise keep the
single-ext behaviour. The model node name must equal what other models' `ref('stg')` resolves to.

Success: `models/stg.sql.jinja` → `model` node named `stg` (not `stg.sql`); a sibling model's `{{ ref('stg') }}`
produces a `references` edge to `stg` that resolves to it.

## Part E — dbt macros and project awareness

### E1 — O9 path-convention role detection (prerequisite for E2/E3)

A `dbtFileRole(filePath) → role` helper classifying by path segment:

- path contains a `/macros/` segment → role **macro**: harvest `{% macro %}` definitions; do **not** create a model
  node for the file.
- path contains a `/analyses/`, `/tests/`, `/seeds/`, or `/snapshots/` segment → role **other**: do not create a model
  node; `{% macro %}` definitions are still harvested if present.
- otherwise (including `/models/` and any unrecognised location) → role **model**: create the model node exactly as
  v1. This default preserves v1 behaviour for the common case — **no regression** for files not under a recognised
  non-model directory.

Only `filePath` is consulted (the extractor is stateless per file). `dbt_project.yml` is not read (see Out).

Success: `macros/util.sql` with `{% macro u() %}…{% endmacro %}` → a `macro` node `u`, NO `model` node;
`models/stg.sql` with Jinja → a `model` node as before.

### E2 — O5 macro nodes + call edges

- **Definition.** `{% macro <name>(<args>) %} … {% endmacro %}` (tolerate whitespace-control `{%-`/`-%}` and spacing)
  → one `macro` node named `<name>`, harvested in any gated file regardless of role.
- **Call edge.** A `{{ <name>(...) }}` invocation → a `calls` edge from the enclosing owner (model node by default;
  macro node when the call is inside a macro span — see E3 for span ownership) to `<name>`. Apply both guards:
  - **Bare-name denylist** (NOT macro calls): `ref`, `source`, `config`, `var`, `env_var`, `is_incremental`,
    `should_full_refresh`, `this`, `target`, `builtins`, `adapter`, `exceptions`, `modules`, `api`, `log`, `print`,
    `run_query`, `run_started_at`, `statement`, `return`, `set`, `dbt_version`, `invocation_id`, `flags`, `model`,
    `graph`, `fromjson`, `tojson`, `fromyaml`, `toyaml`, `zip`, `range`. (`zip`/`range` are Jinja2 builtins, not dbt —
    note that in a code comment so the list's purpose is clear to a future reader.)
  - **Package-qualified skip**: any `a.b(...)` (e.g. `dbt_utils.star(...)`, `dbt.foo(...)`) emits no edge — external
    package macros are not defined in this repo, and edges to unresolvable names are noise. Consequence (verified
    against the dbt-utils package's *own* source): a package's self-referential calls (`dbt_utils.x()` inside the
    dbt-utils repo itself) are also skipped — the stateless extractor cannot tell a self-reference from an external
    dependency without the project's package name (`dbt_project.yml` `name:`, out of scope per O9). This is the correct
    trade-off for the common case: a project that *consumes* dbt_utils as a dependency, where `dbt_utils.x` is external.

Success: `{{ my_macro() }}` → `calls` edge to `my_macro`; `{{ dbt_utils.star(...) }}`, `{{ ref('x') }}`,
`{{ config(...) }}` → no `calls` edge.

### E3 — O6 refs inside macro bodies attributed to the macro

The v1 ref/source harvest credits every `ref()`/`source()` to the model node. v2 first computes the byte spans of all
`{% macro … %} … {% endmacro %}` blocks (on the same comment-stripped raw source the harvest scans). For each harvested
`ref()`/`source()`:

- match offset **inside** a macro span → `references` edge owned by **that macro node**.
- match offset **outside** all macro spans → owned by the **model node** (v1 behaviour).

The same applies to `calls` edges from E2. The B5 placeholder-substitution residual is unchanged (every `{{ ref }}` is
still substituted so residual SQL stays grammatical); only edge *ownership* changes.

Success: a model with top-level `{{ ref('a') }}` and `{% macro m() %}{{ ref('b') }}{% endmacro %}` → `references` to
`a` owned by the model node, `references` to `b` owned by macro `m`. No model→`b` edge; no macro→`a` edge.

### E4 — O7 versioned refs → distinct reference target

`dbtRefRE` *matches* the `v=`/`version=` clause but in a **non-capturing** group (`(?:…v(?:ersion)?\s*=\s*[^)]+)?`) —
the integer `N` is **not currently captured anywhere**. v2 must **add a capture group for `N`** to both `dbtRefRE` and
`dbtRefSubstRE`. Group indices are contract: keep group 1 (first literal) and group 2 (second literal / model) exactly
as they are, and add the version value as a **new trailing group** (so existing group-1/group-2 reads in B2 and B5 are
unchanged). When an explicit version `N` is captured, the reference target name is `<model>_v<N>` (dbt's default
compiled identifier); otherwise `<model>` (unchanged). Both the harvest edge (E3) and the B5 substitution placeholder
must compute and use the same versioned name so they agree (today B5 emits `__dbt_ref_<model>` with no version — it
must become `__dbt_ref_<model>_v<N>` for a versioned ref). The model name in `<model>_v<N>` is the resolved model
literal (group 2 if a package was given, else group 1).

Success: `{{ ref('orders', v=2) }}` → `references` to `orders_v2`; `{{ ref('orders') }}` → `references` to `orders`;
`{{ ref('pkg','orders', version=3) }}` → `references` to `orders_v3`.

### E5 — O8 config(alias=…) capture

A `{{ config(... alias='<name>' ...) }}` call anywhere in a model file → set the model node's `Metadata` to a JSON
object containing `"alias":"<name>"` (tolerate other config kwargs and double-quoted values). Annotation only: no node
renamed, no edge. If multiple `config()` blocks set alias, the first wins (deterministic).

Success: `{{ config(materialized='table', alias='daily_orders') }}` → the model node's `Metadata` JSON has
`alias` = `daily_orders`.

## Part F — Snowflake additions

### F1 — O1 CREATE FILE FORMAT

New top-level definition loop (mirror `stageRE` and its loop): `CREATE [OR REPLACE] [TEMPORARY|TEMP] FILE FORMAT
[IF NOT EXISTS] <name>` → a `file_format` node named `<name>` (schema-qualified name parsed via the existing
`parseQName`). No outbound edges.

Success: `CREATE OR REPLACE FILE FORMAT my_csv TYPE = CSV` → a `file_format` node `my_csv`.

### F2 — O2 VARIANT/OBJECT/ARRAY column typing

`extractColumns` records the column's declared type token into the column node's `Metadata` as a JSON object
`{"type":"<TYPE>"}` for **every** column unconditionally (not only VARIANT/OBJECT/ARRAY — capturing all is simpler and
more useful). This is an intentional wire-format change: column nodes that previously had nil `Metadata` now carry a
type object (the `generated:true` case must still be preserved — merge, do not overwrite). `<TYPE>` is the base type
keyword upper-cased (`VARIANT`, `OBJECT`, `ARRAY`, `NUMBER`, `VARCHAR`, …); parameterised (`NUMBER(38,0)`) and
structured (`OBJECT(a INT)`) forms record the base token (`NUMBER`, `OBJECT`). Must not perturb existing column-name
assertions (they assert names, not `Metadata`).

Success: a table with `c VARIANT, o OBJECT, a ARRAY` → three column nodes whose `Metadata.type` is `VARIANT`,
`OBJECT`, `ARRAY` respectively.

### F3 — O4 LATERAL FLATTEN argument reference (guarded)

Parse `FLATTEN ( [INPUT =>] <expr> )` (including inside `TABLE(FLATTEN(...))`). Emit a `references` edge from the
enclosing owner to `<expr>` **only** when `<expr>` is a single **unqualified** identifier (`other_tbl`, no dot). A
**dotted** expression (`raw.payload`) is skipped — `schema.table` and `alias.column` are syntactically identical, and
in FLATTEN's real usage the input is overwhelmingly a VARIANT *column* (`t.payload`), so treating every dotted form as
a column expression (already covered by its FROM table) avoids the noisy false edges the v1 design warned about. Never
reference `FLATTEN` / `LATERAL` / `TABLE` / the `INPUT` keyword.

Success: `FROM raw, LATERAL FLATTEN(INPUT => raw.payload)` → `references` to `raw` only (none to `payload`/`FLATTEN`;
the dotted FLATTEN input is skipped); `… , LATERAL FLATTEN(INPUT => other_tbl)` → `references` to `other_tbl` (the
unqualified input is treated as a relation).

### F4 — standalone top-level COPY INTO (lazy `script` owner)

v1 captures `COPY INTO` only inside a routine/task body. v2 adds a **lazily-created `script` node** named by the file
basename that owns top-level `COPY INTO` statements with no enclosing definition. The node is created **only** when at
least one such top-level COPY exists — never for ordinary SQL files. Reuse v1's COPY direction logic (`@`-prefix on the
target ⇒ `writes` to stage + `references` to source table; otherwise `writes` to table + `references` to stage). A dbt
model file already owns its top-level statements via the model node (B5), so no `script` node is created there.

Success: a `.sql` file whose only top-level statement is `COPY INTO fact FROM @load_stage` → a `script` node (basename)
owning a `writes` to `fact` + a `references` to `load_stage`; a `.sql` file with only `CREATE TABLE`/`SELECT` and no
top-level COPY → NO `script` node.

## Checkpoints

Land as ordered, each green before the next. The dbt track (E) and Snowflake track (F) are independent after taxonomy;
either may go first, but E1 precedes E2/E3.

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Taxonomy: 3 new node kinds | `types/types.go`, `types/types_test.go` | `TestNodeKindCount` = 38; file_format/macro/script present |
| 2 | D1/D2 `.sql.jinja` ingestion | `standalone/exts.go`, `indexer/orchestrator.go`, `standalone/sql.go` | `.sql.jinja` routes + indexes; model basename `stg` |
| 3 | E1 path-role detection | `standalone/sql.go` | `macros/` → macro node, no model node; `models/` unchanged |
| 4 | E2 macro nodes + call edges | `standalone/sql.go` | `{% macro %}` → node; `{{ m() }}` → calls; denylist + pkg skip |
| 5 | E3 macro-body ref ownership | `standalone/sql.go` | in-span ref → macro owner; out-of-span → model owner |
| 6 | E4 versioned refs | `standalone/sql.go` | `ref('m', v=N)` → `m_v<N>`; B5 placeholder agrees |
| 7 | E5 config(alias=) | `standalone/sql.go` | model node Metadata `alias` set; annotation only |
| 8 | F1 CREATE FILE FORMAT | `standalone/sql.go` | `file_format` node; no edges |
| 9 | F2 column typing | `standalone/sql.go` | column Metadata `type` VARIANT/OBJECT/ARRAY; generated merged |
| 10 | F3 FLATTEN argument | `standalone/sql.go` | unqualified input → reference; dotted skipped |
| 11 | F4 standalone COPY | `standalone/sql.go` | lazy `script` owner; in-body COPY not double-owned |

## Test plan

`sql_test.go` conventions (`newSQL`, `findSQLNode`/`findSQLNodeExact`, `hasUnresolvedRef`, `countUnresolvedRefs`,
package-level `const <name>Fixture`). At minimum one fixture per success criterion above, plus the high-risk guards:

- **D1/D2**: a `.sql.jinja` fixture indexes with model node basename `stg` and a ref DAG identical to its `.sql` twin.
- **E2 false-edge guard**: bare local macro call emits a `calls` edge; `dbt_utils.star()` (package) and `ref()` /
  `config()` (builtins) emit none — in one fixture.
- **E3 span boundary**: a ref immediately before `{% macro %}` (→ model) and a ref inside it (→ macro), one fixture.
- **E4**: the three ref grammars (`v=`, `version=`, none) in one fixture asserting `orders_v2`/`orders_v3`/`orders`.
- **F2**: VARIANT/OBJECT/ARRAY columns asserting `Metadata.type`.
- **F3**: the column-expr (no edge) and bare-relation (edge) FLATTEN cases in one fixture.
- **F4**: top-level COPY → `script` owner; no-top-level-COPY file → no `script` node.
- Taxonomy: `TestNodeKindCount` = 38 with the three new kinds present.

## Implementation log

- 2026-06-24 — Implemented on branch `feat/sql-dbt-snowflake` (autopilot hands-off, full v1-deferred menu), six
  review-gated checkpoint groups: `46e00c6` taxonomy (file_format/macro/script, AllNodeKinds 35→38); `6441dcc` D1/D2
  `.sql.jinja` ingestion (compound-ext routing + double-ext basename); `78a6070` E1/E2/E3 dbt macros (path-role, macro
  nodes + call edges with builtin/package denylist, macro-body ref ownership via byte-span on comment-stripped source);
  `0017bfb` E4/E5 versioned refs (`m_v<N>`) + config(alias) capture; `5a89450` F1/F2 CREATE FILE FORMAT + column typing
  (VARIANT/OBJECT/ARRAY into column Metadata, merged with generated flag); `1778e56` F3/F4 LATERAL FLATTEN argument
  (unqualified-only) + standalone top-level COPY with a lazy `script` owner (body-span dedup against routine/task COPY).
  Each group passed `atomic-reviewer` (code-mode); findings fixed in-iteration — notably a real UTF-8 span/offset
  misalignment in E3 (macro spans were computed on `source` but harvested from the comment-stripped string; a unicode
  Jinja comment shifted the coordinate systems and mis-attributed macro-body refs), the alias Metadata moved from raw
  string-concat to `json.Marshal`, a column type-guard denylist to stop attribute keywords reading as types, and a
  fragile slice-backing pointer replaced with a captured ID. Verified: standalone + types suites green; module-wide
  `go vet` / `gofmt` / `go build` / `atomic validate` (exit 0) clean. End-to-end `atomic code index` on a sample dbt +
  Snowflake corpus confirms all three new node kinds, column type Metadata, alias Metadata, the versioned-ref target
  (`dim_customer_v2`), the cross-file macro `calls` edge, the FLATTEN unqualified/dotted split, and the COPY body-vs-
  top-level ownership split all extract, persist, and query correctly. Pre-existing `internal/hooks` env-dependent test
  failures are unrelated (`hooks-tests-read-real-home`, untouched by this branch).

## Change log

- 2026-06-24 — Real-repo validation pass (jaffle-shop-classic, dbt-utils, Snowflake snowpark guide; Haiku batch
  cross-check against source). Surfaced and fixed a v1-era bug — Jinja `{# #}` comment prose leaked as false `from`/
  `join` references because the B5 residual was built from raw `source`, not the comment-stripped `rawForHarvest`
  (fix: build residual from `rawForHarvest`). Documented the package-qualified self-call behaviour in E2 (a package's
  own `pkg.x()` calls are skipped when indexing that package's source — correct for the common consumer case).
- 2026-06-24 — `## Implementation sequencing` reshaped to the `## Checkpoints` table form required by `atomic validate`
  rule S5 (columns `# | Checkpoint | Files/areas | Verifies`). Content unchanged — same 11 ordered checkpoints.
- 2026-06-24 — F3 disambiguated during implementation (pre-Group-F dispatch).
  **What changed:** F3 now emits a FLATTEN-argument reference only for a single **unqualified** identifier; any dotted
  expression is skipped. The original wording ("`tbl` or `schema.tbl`") was self-contradictory — `schema.tbl` and the
  `t.col` it said to skip are syntactically identical, so no implementation could honour both. The success criteria
  (1-part emits, 2-part skips) already pinned this behaviour; the body now matches.
  **Why:** an un-resolvable ambiguity in the body would have produced either arbitrary or noisy behaviour; the
  unqualified-only rule is honest, testable, and matches FLATTEN's real usage (input is almost always a VARIANT column).
- 2026-06-24 — Revised after spec-mode review (pre-implementation, no prior build).
  **What changed:** E4 corrected — `dbtRefRE`/`dbtRefSubstRE` *match* but do **not capture** the version integer `N`
  (it lives in a non-capturing group); v2 must add a new trailing capture group, preserving group-1/group-2 indices,
  and B5 must emit `__dbt_ref_<model>_v<N>`. D1 narrowed to the two real `filepath.Ext` call sites (the L409 guard
  inherits the computed `ext` — no third call). F2 clarified as an intentional all-columns wire-format change that must
  merge with (not overwrite) the existing `generated:true` Metadata. E2 denylist gained a code-comment note that
  `zip`/`range` are Jinja2 builtins.
  **Why:** the review found E4 implied the version value was already available (it is not — would have produced a
  silent no-op or a missing capture-group read), and the D1/F2 phrasings were ambiguous enough to mis-guide a
  fresh-context implementer.
- 2026-06-24 — Created. Full v1-deferred menu (`.sql.jinja`, O1/O2/O4/O5/O6/O7/O8/O9, standalone COPY) as a child of
  `docs/spec/sql-dbt-snowflake.md`. Three new node kinds (`file_format`, `macro`, `script`), `TestNodeKindCount`
  35→38, no new EdgeKind. O7 reference naming fixed to dbt's real `<model>_v<N>` compiled form (primary-doc verified),
  superseding the v1 design's `m@vN` sketch. O9 scoped to path-convention role detection (the stateless extractor has
  no project state); `dbt_project.yml` custom `model-paths` parsing explicitly deferred. O8 alias is annotation-only
  (ref uses the resource name, never the alias). Standalone COPY owner is a lazily-created `script` node to avoid a
  per-file node explosion.
