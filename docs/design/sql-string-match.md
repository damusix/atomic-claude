# SQL string-match extraction

Status: approved (autopilot run 2026-07-18). Spec: `docs/spec/sql-string-match.md`.


## Problem

Host-language code that references SQL objects through string literals — Kysely's `selectFrom('worker_document_crawl_v')`, knex's `.table('users')`, a proc name passed to a runner — produces no graph edge to the `.sql`-defined object. Text search finds the identifier in both files; the graph shows no link. Impact analysis on a view misses every query-builder call site: a false negative, the expensive kind of error for lineage.

The embedded-SQL gate (`IsSQLLiteral`) is statement-shaped by design: it admits SQL *text*, not identifier strings in API position. Query-builder usage never reaches it.


## Approach: index-anchored string matching

Don't detect query builders per library. Anchor on the index instead:

1. **Harvest** identifier-shaped string literals from host code (the embedded-SQL literal harvest already visits every string literal in 19+ languages) and emit them as *speculative* unresolved references, owner-attributed via the existing `findOwnerNode` postpass span matching.
2. **Match at resolution time** against already-indexed SQL object names (tables, views, procedures, SQL functions). A match becomes a `references` edge with provenance `string-match`; a non-match is deleted — speculative refs never linger as unresolved noise. Nodes are never minted from strings: no index entry, no edge.
3. **Boost confidence** when the literal sits in argument position of a known query-builder method (`selectFrom`, `insertInto`, `from`, `table`, join variants…) — one shared cross-language vocabulary of bare callee names, not per-library adapters. A false vocabulary hit is harmless because the string still has to match an indexed object name.

Columns are riskier (`status`, `name`, `id` are everywhere), so they only match **anchored**: a qualified `alias.col` / `table.col` form self-disambiguates; a bare column name matches only when its owner scope (the enclosing function) already matched a table or view, and then only against *that* object's column nodes.


## Confidence ladder

```
querybuilder table arg       high    (API position guarantees semantics)
bare string = object name    medium  (index-anchored, no position info)
qualified table.col          medium  (self-disambiguating)
bare col + table anchor      low     (scope co-occurrence heuristic)
  └ vocabulary callee         medium (declaration-DSL position, e.g. Column("name"))
fragment tokens              one notch below the equivalent whole-string tier
bare col, no anchor          never emitted
```


## Fragment tier (wild-usage follow-through)

Ecosystem research (2026-07-19, two doc sweeps across 20+ libraries) showed the largest
idiomatic surface we initially missed is **SQL fragments in builder args**: ActiveRecord
`where("title LIKE ?")` / `order("created_at DESC")` / `pluck("orders.created_at, customers.email")`,
GORM `Where("name = ?")`, Laravel `whereRaw`, sqflite `columns:` comma lists. These strings are
neither identifier-shaped (spaces/operators) nor statement-shaped (no leading DML/DDL verb), so
they fell through both mechanisms.

The fragment tier tokenizes them: a literal failing both gates but passing a cheap
fragment-shape check (identifier token + a fragment discriminator: comparison operator,
placeholder, comma, ASC/DESC, or SQL connective) has its identifier tokens (bare and
one-dot qualified) extracted, SQL keywords stoplisted, and each token emitted as a
speculative ref — same index-anchor + owner-anchor discipline, one confidence notch below
the equivalent whole-string match because tokenization adds prose-collision risk
("error = timeout" tokenizes; it only edges if an object named `error`/`timeout` exists).

The same research drove the vocabulary expansion: declaration-DSL callees
(`column`, `field`, `tableName`, `withTableName`, `toTable`, `hasColumnName`, `entityName`, …)
are string-arg surfaces in Slick / SQLite.swift / Exposed / GRDB / EF Core / jOOQ escape
hatches — annotation and declaration strings were already matched, but at flat medium;
vocabulary position now upgrades them.

Confidence rides in `Edge.Metadata` (JSON) — no schema change; `Edge.Provenance` (`string-match`) is the coarse filter and already has a DB index.


## Why resolution-phase, not extraction-phase

Extraction runs per file; the SQL objects a literal names may not be indexed yet (or live in another file entirely). Resolution runs after all files are stored and already special-cases SQL refs (`byQualifiedName` dot-routing). Only there is the "does this string equal a real object?" question answerable.


## Rejected alternatives

- **Per-library builder adapters** (kysely, knex, drizzle, sqlalchemy…): unbounded maintenance across N languages × M libraries. The index anchor makes discovery library-agnostic; the vocabulary is only a confidence booster.
- **Extraction-time matching against a name snapshot**: ordering-dependent, wrong across incremental syncs.
- **Persisting unmatched speculative refs**: every identifier-ish string in the codebase would become permanent unresolved-ref noise. Delete on no-match.
- **Unanchored column matching**: false-positive volume too high; user-confirmed scope-anchor requirement.


## Out of scope (future work)

**View columns.** The SQL extractor mints column nodes for tables only, never for views (verified empirically during implementation). A view anchor therefore enables no bare-column matches; qualified `view.col` forms also find no column node. Lifting this requires view column extraction in the SQL extractor — a separate feature.

Symbol-based builders (drizzle schema objects, SQLAlchemy ORM models, jOOQ codegen, Ecto schemas) pass symbols, not strings — nothing to string-match. The missing piece there is a schema-definition-site extractor linking the *symbol* to the SQL object; genuinely per-library, deferred.
