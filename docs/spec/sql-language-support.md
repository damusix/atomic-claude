# SQL language support

Add SQL as a first-class indexed language in the code-intelligence engine, via a **dialect-agnostic regex standalone extractor** (not tree-sitter) plus an extension of the node-kind/edge-kind taxonomy. Comprehensive symbol + relationship capture: tables, columns, views, functions, procedures, triggers, constraints, indexes, sequences, schemas, types, synonyms, RLS policies — and the **schema relationship graph**: foreign keys, view dependencies, trigger targets, RLS, and body-level reads (`references`), writes (`writes`), and calls (`calls`). Dialect coverage includes Postgres, MySQL, SQLite, and **T-SQL/MSSQL**.

Status: complete on branch `code-intel-engine` (7 checkpoints, see Implementation log). Ships with the engine.

## Why self-detection, not tree-sitter

Decided via `/gather-evidence` (2026-06-07) and a follow-up architecture review. Summary of the evidence (full trail in `.claude/project/followups/sql-language-support.md`):

- A tree-sitter SQL grammar (`@derekstride/tree-sitter-sql`, ABI 14) compiles clean into our `ts.wasm` and exposes typed `create_*` nodes — **but it is Postgres/MySQL/SQLite-only**. It does not parse T-SQL (`[bracket]` identifiers = 0, `GO` = 0). The one real T-SQL grammar (`Crary-Systems/tree-sitter-tsql`) folds `CREATE` into a hidden generic blob — useless for symbol extraction — and is immature.
- **The dialect split disappears with self-detection**: for symbol extraction we match the `CREATE <kind> <name>` preamble and the table body, not full expression grammar. One regex set covers Postgres, MySQL, SQLite, **and T-SQL** (`[dbo].[Users]`, `GO`, `CREATE OR ALTER`).
- The engine already has a proven non-tree-sitter path: `extraction/standalone/` (Liquid, Delphi DFM, MyBatis XML are pure-regex extractors). SQL fits this pattern exactly. No `ts.wasm` bloat (+0 MB vs +2.4 MB), no grammar/ABI maintenance.
- Tree-sitter would only win for full-AST needs (column type semantics, query-plan analysis) — out of scope for code-intel symbol/relationship search.

**Rejected approaches** (do not revisit without new evidence): vendoring DerekStride (no T-SQL); vendoring Crary (no clean definitions); adding any tree-sitter SQL grammar to `ts.wasm`.

## Taxonomy changes

### New NodeKinds (9)

Added to `types.NodeKind` const block and `AllNodeKinds` (in declared order, appended after the existing 22). These have no honest fit among existing kinds:

`table`, `view`, `column`, `procedure`, `trigger`, `constraint`, `index`, `sequence`, `policy`.

CP1 shipped the first 8 (appendix C now 30). `policy` is added in CP4 (appendix C → 31) alongside RLS extraction, since its value is the policy→table / policy→function edges.

### Reused existing NodeKinds (no new kind — honest fit)

- `CREATE FUNCTION` → `function`
- `CREATE SCHEMA` → `namespace` (a schema is a namespace)
- `CREATE TYPE … AS ENUM` → `enum`, with each label → `enum_member`
- `CREATE TYPE` (composite/scalar) / `CREATE DOMAIN` → `type_alias`
- `CREATE DATABASE` → `module`

### Edges

One new EdgeKind, the rest reused. Add `writes` to `types.EdgeKind` const block + `AllEdgeKinds` + the appendix-A edge list + `TestEdgeKindCount` (added in CP5 alongside write-edge extraction).

- `contains` — schema→objects; table→{column, constraint, index}; enum→enum_member
- `references` — read relationships: FK targets, view→source tables, trigger→table, body `FROM`/`JOIN` reads, synonym→target, policy→table
- `writes` (new) — routine body mutation targets: `INSERT INTO` / `UPDATE` / `DELETE FROM` / `MERGE INTO` → table. Lets `code impact <table>` distinguish writers from readers — the core query for the user's mutation-through-procedures methodology.
- `calls` — procedure/function body invoking another routine (`EXEC`/`CALL`); PG trigger→function (`EXECUTE FUNCTION tg_*`); policy→function (`USING (fn(...))`)

## Construct → node/edge mapping (comprehensive)

| SQL construct | Node | Kind | Edges emitted |
|---|---|---|---|
| `CREATE TABLE [schema.]name (…)` | table | `table` | `contains` from owning schema (if any) |
| `CREATE FOREIGN/EXTERNAL TABLE` | table | `table` | metadata `{"foreign":true}` |
| ↳ column definition | column | `column` | `contains` table→column |
| ↳ GENERATED/computed column | column | `column` | `contains`; metadata `{"generated":true}` |
| ↳ inline FK `col … REFERENCES t(c)` | (no node) | — | `references` table→`t` |
| ↳ named constraint `CONSTRAINT n …` | constraint | `constraint` | `contains` table→constraint; if FK, `references` table→target |
| ↳ table-level `FOREIGN KEY (…) REFERENCES t` | constraint (anon ok) | `constraint` | `references` table→`t` |
| ↳ table-level `PRIMARY KEY`/`UNIQUE`/`CHECK` | constraint | `constraint` | `contains` table→constraint |
| `ALTER TABLE t ADD CONSTRAINT n FOREIGN KEY … REFERENCES u` | constraint | `constraint` | `contains` `t`→constraint; `references` `t`→`u` |
| `ALTER TABLE t ADD COLUMN c …` | column | `column` | `contains` `t`→column (column defined outside CREATE) |
| `CREATE [OR REPLACE] [MATERIALIZED] VIEW v AS SELECT … FROM a JOIN b` | view | `view` | `references` `v`→`a`, `v`→`b` (view dependency) |
| `CREATE [OR ALTER] FUNCTION f …` | function | `function` | body refs (see below) |
| `CREATE [OR ALTER] PROCEDURE p …` | procedure | `procedure` | body refs (see below) |
| `CREATE TRIGGER tr … ON t [EXECUTE FUNCTION fn]` | trigger | `trigger` | `references` `tr`→`t`; `calls` `tr`→`fn` (PG delegates logic to fn) |
| `CREATE [UNIQUE] INDEX i ON t (…)` | index | `index` | `contains` `t`→`i` (index belongs to its table) |
| `CREATE SEQUENCE s` | sequence | `sequence` | `contains` from owning schema (if any) |
| `CREATE SCHEMA s` | namespace | `namespace` | `contains` schema→objects declared within |
| `CREATE TYPE x AS ENUM ('a','b')` | enum (+members) | `enum`/`enum_member` | `contains` enum→member |
| `CREATE TYPE x AS TABLE (…)` (T-SQL table type / TVP) | type alias | `type_alias` | metadata `{"table_type":true}` |
| `CREATE TYPE x FROM <base>` (T-SQL alias type) | type alias | `type_alias` | — |
| `CREATE TYPE x` (composite) / `CREATE DOMAIN x` | type alias | `type_alias` | — |
| `CREATE SYNONYM s FOR target` (MSSQL) | synonym | `type_alias` | `references` `s`→target; metadata `{"synonym":true}` |
| `CREATE POLICY p ON t USING (fn(…))` (PG RLS) | policy | `policy` | `references` `p`→`t`; `calls` `p`→`fn` |
| `CREATE DATABASE d` | module | `module` | — |

### Body-level references (functions / procedures / triggers / views)

Within a routine or view body (the text after `AS`/`BEGIN … END`), capture:

- `FROM <name>` / `JOIN <name>` → `references` routine→table (read).
- `INSERT INTO <name>` / `UPDATE <name>` / `DELETE FROM <name>` / `MERGE INTO <name>` → `writes` routine→table (mutation target). This is the high-value relationship: the user's methodology routes all writes through procedures, so `code impact <table>` must answer "what writes this."
- `EXEC[UTE] <name>` / `CALL <name>(…)` → `calls` routine→routine.

These are heuristic (regex over the body, comment-stripped). False positives are acceptable at low severity; the resolver drops references whose target name doesn't resolve to an indexed node, so unresolved noise is naturally filtered. Guard against CTE shadowing: a name bound by `WITH <name> AS (…)` must NOT emit a dangling `references`/`writes` to a non-existent table (the resolver drops it, but a CP5 test asserts no false edge).

## Extractor contract

New file: `atomic/internal/codeintel/extraction/standalone/sql.go`. Mirror the MyBatis/DFM exemplars in `standalone.go`.

- Implements `Extractor`: `Extract(filePath, source string) (types.ExtractionResult, error)`.
- Registered in `standalone.NewRegistry` `entries` map under `.sql` (and `.ddl`, `.pgsql`, `.mysql` as aliases).
- Produces `types.Node` via `extraction.GenerateNodeID(filePath, string(kind), qualifiedName, line)`; `contains` edges via the existing `containsEdge(parentID, childID)` helper; relationships via `types.UnresolvedReference{FromNodeID, ReferenceName, ReferenceKind, Line, FilePath, Language: types.LanguageSQL}`.
- Line numbers computed as `strings.Count(source[:matchByteOffset], "\n") + 1` (the established pattern).
- `Node.Language = types.LanguageSQL` on every node.
- `IsExported = true` for all top-level objects (SQL has no visibility concept; everything is reachable).
- Store dialect-specific hints in `Metadata` JSON where useful (e.g. `{"constraint_type":"foreign_key"}`, `{"materialized":true}`).

### Dialect-agnostic matching (mandatory)

A single regex set must handle all of:

- Quoting: bare `name`, `"name"` (ANSI), `` `name` `` (MySQL), `[name]` (T-SQL).
- Schema qualification: `schema.name`, `[db].[schema].[name]`.
- `CREATE OR REPLACE` (Postgres), `CREATE OR ALTER` (T-SQL), `IF NOT EXISTS`.
- Statement terminators: `;` and T-SQL `GO` batch separator.

**Comment stripping first** (prevents `CREATE` matches inside comments/strings): strip `-- line comments`, `/* block comments */` before matching, preserving line counts (replace stripped spans with newlines/spaces so line numbers stay accurate). String-literal stripping is best-effort.

### Identifier normalization

Strip surrounding quote characters (`"`, `` ` ``, `[` `]`) from captured names so `[dbo].[Users]`, `` `users` ``, `"users"`, and `users` all yield `ReferenceName`/`Name` = `users` (schema-qualified retained in `QualifiedName`). FK targets must normalize identically so `references` edges resolve across quoting styles.

## Routing & wiring surfaces

Exact file:line targets (mapped by investigator, verify before editing):

| Surface | Location | Change |
|---|---|---|
| NodeKind consts | `types/types.go` 42–65 | add 9 new kinds (8 in CP1, `policy` in CP4) |
| `AllNodeKinds` | `types/types.go` 69–92 | append (same order) |
| EdgeKind consts + `AllEdgeKinds` | `types/types.go` (EdgeKind block ~95–135) | add `writes` (CP5); update `TestEdgeKindCount` + appendix A |
| `Language` consts | `types/types.go` 144–174 | add `LanguageSQL = "sql"` |
| `AllLanguages` | `types/types.go` 178–208 | append `LanguageSQL` |
| taxonomy test | `types/types_test.go` `TestNodeKindCount` 19–72, `TestLanguageCount` | update expected counts/lists |
| appendix C | `docs/spec/code-intel-engine.md` (grep "appendix C" / NodeKind list) | add the 8 new kinds to the verbatim list |
| search validation | `search/search.go` `validKinds` 107–113 | flows from `AllNodeKinds` — confirm no hardcoded list to edit |
| search scoring | `search/search.go` `KindBonus` 186–209 | add bonuses for new kinds (table/view/procedure ≈ class/function tier; column/constraint/index ≈ field tier) |
| extractor | `extraction/standalone/sql.go` (new) | the extractor |
| extractor registry | `extraction/standalone/standalone.go` `NewRegistry` 52–61 | register `.sql` (+ aliases) |
| ext→Language | `indexer/orchestrator.go` `extToLanguage` 50–132 | `.sql`/`.ddl`/`.pgsql`/`.mysql` → `LanguageSQL` |
| standalone routing | `indexer/orchestrator.go` `standaloneExts` 142–152 | add the SQL extensions |
| status JSON | `cli/code.go` 279, 333–336 | generic `NodesByKind` — confirm new kinds flow through with no code change |
| eval corpus | `scripts/code-eval/corpus.tsv` lang section | add SQL lang row(s) + a real schema repo |

## Checkpoints

Each is one cohesive `/subagent-implementation` iteration with its own green gate. Build in order — later checkpoints depend on earlier.

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Taxonomy foundation — 8 NodeKinds + `LanguageSQL` | `types.go`, `code-intel-engine.md` appendix C, search `KindBonus` | `go test ./internal/codeintel/types/... ./internal/codeintel/search/...`; `TestNodeKindCount`/`TestLanguageCount` updated |
| 2 | Definition extraction (tables, columns, views, functions, procedures, triggers, indexes, sequences, schemas, types, synonyms, databases) | `extraction/standalone/sql.go`; orchestrator routing, `extToLanguage`, `standaloneExts` | multi-dialect (PG/MySQL/T-SQL) extractor unit tests assert kinds/names/lines; comment/string false-positive test; real `.sql` indexed end-to-end |
| 3 | Constraints → `constraint` nodes (inline, table-level, ALTER) | `sql.go` | fixtures: inline PK/UNIQUE, table-level CONSTRAINT, ALTER-based, across dialects |
| 4 | Relationship edges (FK/view/trigger/synonym `references`, trigger→fn `calls`) + RLS `policy` NodeKind | `sql.go`, `types.go` | FK/view/trigger/synonym references resolve; policy→table+fn resolves; `code impact`/`callers <table>` surface referrers |
| 5 | Body-level edges + `writes` EdgeKind (INSERT/UPDATE/DELETE/MERGE→writes, FROM/JOIN→references, EXEC/CALL→calls) | `sql.go`, `types.go` | proc fixture: writes/references/calls resolve distinctly; `code impact` distinguishes writers from readers |
| 6 | Eval + real-repo validation | `scripts/code-eval/corpus.tsv` | run `scripts/code-eval/run-eval.sh`; non-zero tables/columns/constraints + resolved edges, no timeout |

Per-checkpoint detail:

- **CP1 — Taxonomy foundation.** Add 8 NodeKinds + `LanguageSQL` to `types.go` (consts + `AllNodeKinds` + `AllLanguages`). Update `TestNodeKindCount`/`TestLanguageCount`. Update appendix C in `code-intel-engine.md`. Add `KindBonus` entries. Confirm `validKinds` derives from `AllNodeKinds` (no hardcoded edit). Green: `go test ./internal/codeintel/types/... ./internal/codeintel/search/...`.
- **CP2 — Definition extraction.** `sql.go` extractor capturing the **definition nodes**: table (incl. FOREIGN/EXTERNAL → metadata), column (incl. GENERATED/computed → metadata, and `ALTER TABLE ADD COLUMN`), view, function, procedure, trigger, index, sequence, schema(→namespace), type (`AS ENUM`→enum/member; `AS TABLE`→type_alias table_type; `FROM <base>` T-SQL alias→type_alias; composite/DOMAIN→type_alias), synonym(→type_alias + metadata), database(→module). Comment stripping + dialect-agnostic identifier matching (incl. T-SQL `[..]`, MySQL backticks, `CREATE OR ALTER`, `GO`) + normalization. Register `.sql` (+ `.ddl`/`.pgsql`/`.mysql`) in standalone registry; wire orchestrator routing + `extToLanguage` + `standaloneExts` + `LanguageSQL`. `contains` edges for the obvious hierarchy (table→column, enum→member, schema→objects, table→index). Green: extractor unit tests on synthetic multi-dialect DDL (Postgres + MySQL + T-SQL fixtures) assert every node kind + correct names/lines, incl. the T-SQL `CREATE TYPE … FROM` alias arm; comment/string false-positive test; `go build ./...`; index a real `.sql` file end-to-end and confirm nodes land in the DB.
- **CP3 — Constraints.** Named + table-level + inline constraints → `constraint` nodes with `contains` to table; including `ALTER TABLE … ADD CONSTRAINT`. Metadata records constraint type. Green: fixtures covering inline PK/UNIQUE, table-level CONSTRAINT, ALTER-based constraints across dialects.
- **CP4 — Relationship edges + RLS policy.** FK → `references` (inline, table-level, ALTER); view → source-table `references` (FROM/JOIN in view body); trigger → table `references` (ON) **and** trigger → function `calls` (PG `EXECUTE FUNCTION`); synonym → target `references`. **Add `policy` NodeKind** (types.go consts + AllNodeKinds + appendix C → 31 + KindBonus + TestNodeKindCount) and extract `CREATE POLICY p ON t USING (fn)` → `policy` node + `references` p→t + `calls` p→fn. References emitted as `UnresolvedReference`; verify they resolve via the existing pipeline. Green: a fixture schema with FKs across tables produces resolved `references` edges; a PG fixture with a trigger→fn and a policy→table+fn resolves; `code impact <table>` / `code callers <table>` surface referencing tables.
- **CP5 — Body-level edges + `writes` EdgeKind.** **Add `writes` to EdgeKind** (types.go consts + AllEdgeKinds + appendix A + TestEdgeKindCount). Routine/view body: `FROM`/`JOIN` → `references`; `INSERT INTO`/`UPDATE`/`DELETE FROM`/`MERGE INTO` → `writes`; `EXEC`/`CALL` → `calls`. Heuristic, comment-stripped, unresolved-name noise dropped by resolver; CTE-shadow guard (no false edge to a `WITH`-bound name). Green: fixture with a proc that INSERTs/UPDATEs tables, SELECTs from others, and EXECs another proc → `writes` + `references` + `calls` edges resolve distinctly; `code impact <table>` distinguishes writers from readers.
- **CP6 — Eval + real-repo validation.** Add SQL lang row(s) to `corpus.tsv` (a real schema/migration repo; include a T-SQL repo if one is readily cloneable — try a dbt project, a Postgres schema, and an MSSQL sample DB like AdventureWorks DDL). Run `scripts/code-eval/run-eval.sh` on it; confirm non-zero tables/columns/constraints and resolved `references`/`writes`/`calls` edges, no timeout. Record counts.

## Testing requirements

- Multi-dialect fixtures are mandatory: every extractor test must include a Postgres form, a MySQL form (backticks), and a T-SQL form (`[brackets]`, `GO`, `CREATE OR ALTER`). A test that only covers ANSI/Postgres is insufficient — dialect-agnosticism is the core value.
- Tests assert observable outcomes: node kind + name + line, edge kind + endpoints — not internal regex structure.
- Comment/string false-positive test: `CREATE TABLE` inside a `--` comment and inside a string literal must NOT produce a node.
- Keep existing `standalone_test.go` and `types_test.go` green.

## Out of scope (v1)

- Column type semantics / data-type modeling (we capture column *names*, not parsed types).
- Query-level analysis beyond FROM/JOIN/EXEC/CALL name capture (no WHERE/CTE/subquery graph).
- Stored-procedure control-flow.
- Cross-file `USE <db>` context switching.
- Postgres `CREATE ROLE`/`EXTENSION`, `CREATE RULE`/`AGGREGATE`/`OPERATOR`, grants — low code-intel value or legacy; defer. (RLS `CREATE POLICY` is now IN — see mapping table.)
- Partition functions/schemes, filegroups/tablespaces, temporal/ledger table variants — physical/storage, not navigable symbols (a metadata flag at most).
- A dedicated `code search kind:column` ranking beyond the `KindBonus` tier assignment.

## Success criteria

- `.sql` files index across Postgres, MySQL, SQLite, and T-SQL dialects with no error nodes on dialect-specific syntax (incl. T-SQL `[brackets]`, `GO`, `CREATE OR ALTER`, `CREATE TYPE … FROM`).
- All 9 new NodeKinds are produced on appropriate constructs and searchable via `code search kind:<k>`.
- FK / view / trigger / synonym / policy relationships resolve to `references` edges; routine write-targets resolve to `writes` edges; `EXEC`/`CALL` + trigger→fn + policy→fn resolve to `calls`. The schema relationship graph is navigable via `code impact` / `code callers` / `code explore` on a table, with `code impact <table>` distinguishing writers (`writes`) from readers (`references`).
- `code-intel` test suite green (all packages); eval run on a real SQL repo reports non-zero tables/columns/constraints and resolved `references`/`writes`/`calls` edges.

## Change log

### 2026-06-07 — Initial spec

**What changed:** New spec. SQL support via dialect-agnostic regex standalone extractor + 8 new NodeKinds + reuse of `contains`/`references`/`calls` edges. Comprehensive scope: definitions, columns, constraints, and the FK/view/trigger/body relationship graph.

**Why:** User wants SQL (incl. in-house MSSQL) shipped with the code-intel engine. `/gather-evidence` showed no tree-sitter grammar covers both T-SQL dialect and clean definition nodes; self-detection sidesteps the dialect split and reuses the engine's existing standalone-extractor pattern. User directive: capture everything, including constraints and the schema relationship graph.

### 2026-06-07 — Scope expansion from skills audit

**What changed:** `atomic-strategist` audited scope against the user's installed MSSQL/Postgres skills. Added: (1) `writes` EdgeKind for routine→table mutation targets (`INSERT`/`UPDATE`/`DELETE`/`MERGE`), so `code impact` distinguishes writers from readers; (2) `policy` NodeKind + RLS `CREATE POLICY` extraction (policy→table `references`, policy→function `calls`); (3) T-SQL `CREATE TYPE … FROM` alias arm, T-SQL table types (`AS TABLE`), `CREATE SYNONYM`, `CREATE FOREIGN/EXTERNAL TABLE`, GENERATED/computed columns, `ALTER TABLE ADD COLUMN`; (4) PG trigger→function `calls` edge. NodeKinds 8→9, EdgeKinds +1 (`writes`). CP4 now also adds `policy`; CP5 now also adds `writes`. RLS removed from out-of-scope.

**Why:** The user's own writing-guidelines route every mutation through procedures, making the write-edge the single highest-value relationship for these schemas — it was the biggest omission. The T-SQL alias-type arm was intended but unspecified in the dialect contract (the most-used MSSQL construct). Trigger→function and RLS policies are core to the Postgres methodology. User directive: "capture everything useful, don't leave anything behind." Decisions confirmed via AskUserQuestion (new `writes` EdgeKind over metadata; include RLS now over deferring).

**Superseded:** prior contract had 8 NodeKinds (no `policy`), no new EdgeKind (writes folded into `references`), no T-SQL alias-type/synonym/foreign-table/generated-column/ALTER-column capture, no trigger→function edge, and RLS in out-of-scope.

### 2026-06-08 — Checkpoints rendered as a table

**What changed:** Added the canonical `## Checkpoints` table (required columns `# | Checkpoint | Files/areas | Verifies`) above the existing per-checkpoint bullets. No checkpoint content changed — the bullets are retained verbatim as per-checkpoint detail.

**Why:** `atomic validate` rule S5 requires a `## Checkpoints` table; this spec carried only bullets and so failed validation (and the CI Validate gate). Reconciled to the convention. The S5 check itself was also fixed the same day to accept the required columns as an ordered subsequence, so the 6-column `/atomic-plan` header passes too (follow-up `validate-checkpoint-header-drift`).

## Implementation log

### v1 — 2026-06-07

Built across 7 checkpoints (+2 prep commits) of /subagent-implementation. Commits (chronological):

- `84f6dcb` — prep: spec scope expansion from skills audit (writes edge, RLS, T-SQL types)
- `7fb7d24` — prep: close the old tree-sitter followup plan (superseded by self-detect)
- `b30b793` — CP1 taxonomy: 8 NodeKinds + LanguageSQL
- `a1353af` — CP2 dialect-agnostic definition extractor (standalone regex, comment/string guards)
- `7376d06` — CP3 constraint nodes (named / table-level / ALTER)
- `d7894d4` — CP4 relationship graph (FK/view/trigger/synonym refs) + policy NodeKind + RLS
- `34f3a9b` — CP5 writes EdgeKind + routine-body reads/writes/calls + CTE-shadow guard
- `e1abd78` — CP6 fix: resolve FKs in pg_dump `ALTER TABLE ONLY` form (real-repo eval)
- `eb4eda4` — CP7 polish: address all 13 loop followups (behavior-preserving)

**Approach decided pre-build:** self-detection (regex standalone extractor) over tree-sitter, via `/gather-evidence` + a skills audit. No grammar vendored; dialect-agnostic so T-SQL/MSSQL works (the tree-sitter path couldn't cover it). See the "Why self-detection" section + change log.

**Out-of-scope work performed during this build:**
- Skills-audit-driven scope expansion (writes edge, policy/RLS, T-SQL alias types, synonyms, foreign tables, generated/ALTER columns, trigger→fn) — folded into the spec before CP2 via `atomic-strategist` audit against the user's MSSQL/Postgres skills.
- CP6 fixed a real `ALTER TABLE ONLY` (pg_dump) extraction bug surfaced only by real-repo eval — synthetic fixtures missed it.

**Unforeseens — surprises during implementation:**
- The "FK references don't resolve on real repos" symptom (CP6) was initially misdiagnosed as a resolution-layer bug. Root cause was the `ALTER TABLE ONLY` regex capturing `ONLY` as the table name → the FK reference was never emitted. CP4's synthetic e2e passed because it used the inline-FK path (`inlineRefRE`), a different code path from pg_dump's ALTER-based FKs. Lesson: synthetic fixtures must mirror real dump formats; the eval harness earned its keep.
- A builder iteration (CP7) mis-reported its own work as "already done in prior iterations"; the changes were actually made that iteration. Caught by diffing against the committed SHA rather than trusting the report — reviewer confirmed the code state directly.

**Eval results (real repos, CP6):** Chinook 121 tables; pgsamples 140 tables / 1127 columns / 387 constraints / 96 views / triggers / functions; Northwind resolves 12 table→table FK `references` edges. Both PG and T-SQL dialect scripts index, no timeouts.

**Deferred items still open:** none. All 13 review followups (F-1..F-15; F-6/F-7 closed in CP5) were fixed in CP7 per user disposition ("fix all 13 now"). F-2 and F-15 were documented as non-issues in-code (harmless / invalid-DDL).

**Squashed to 84aeb5d — 2026-06-08.** Per-iteration SHAs above are historical (unreachable from any branch).
