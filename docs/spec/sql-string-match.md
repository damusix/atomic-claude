# Spec: SQL string-match extraction

Design: `docs/design/sql-string-match.md` (approach + rejected alternatives — read it first).

Match identifier-shaped string literals in host-language code against already-indexed SQL object names, producing `references` edges with provenance `string-match` and a per-edge confidence in `Edge.Metadata`. Never mint nodes from strings. Columns match only anchored.


## Contracts

### C1 — Speculative reference harvest (extraction/indexer)

- A new constant `ReferenceKindSQLString types.EdgeKind = "sql_string"` in `types/types.go`. It is a *discriminator only*, never an edge kind: the `unresolved_refs.reference_kind` column is unconstrained TEXT so storage is safe, but `sql_string` refs must be **excluded from the standard resolution input set** — the pipeline caller filters them out before `resolveOne` ever sees one (they must never reach `promoteEdgeKind`). Passes A/B (C2/C3) consume them instead and always create edges with `Kind: references` explicitly.
- The embedded-SQL literal postpass (`indexer/embedded_sql_postpass.go`) already visits every harvested string literal per host language. For each literal that **fails** the `IsSQLLiteral` admission gate, emit an `UnresolvedReference{ReferenceKind: sql_string}` when ALL hold:
  - Literal content matches the identifier shape: `^[A-Za-z_][A-Za-z0-9_]{2,}(\.[A-Za-z_][A-Za-z0-9_]+)?$` (min 3 chars before any dot, at most one dot).
  - Host file language is not SQL.
  - The (owner, literal) pair was not already emitted for this file (dedupe).
- `FromNodeID` = owner from the existing `findOwnerNode` span matching (function node, falling back to file node). `ReferenceName` = literal content verbatim. `Line`/`Column` = literal position. `Language` = host language.
- **Callee capture**: the TypeScript/TSX, Python, and Go harvesters additionally record the bare callee name of the nearest enclosing call expression whose argument list contains the literal, carried in `CalleeExpr`. A literal not inside any call expression gets empty `CalleeExpr` — uniform across all three harvesters. The 16 generic-language harvesters (`HarvestEmbeddedLiterals`) always emit empty `CalleeExpr`. Empty `CalleeExpr` simply caps the ref at medium confidence in C2. No new struct fields on the span types beyond what callee capture needs.
- Literals that PASS the SQL gate keep today's behavior untouched (full embedded-SQL extraction, provenance `embedded`).

### C2 — Resolution pass A: object names

- A new resolution step for `sql_string` refs, running after standard per-ref resolution completes. Exclusion mechanism per C1: the standard pipeline's input set filters out `sql_string` refs entirely; pass A is a separate batch step.
- Candidate set: nodes with `Language == SQL` and kind ∈ {`table`, `view`, `procedure`, `function`}. Retrieval: **one bulk fetch per kind** (`GetNodesByKind` × 4, filtered to `Language == SQL`), loaded once into an in-memory lowercase-name map reused for the whole pass — not a per-ref DB round trip. Match = case-insensitive equality of `ReferenceName` (dotless refs only in this pass) against `Node.Name`.
- On match: create edge `{Source: FromNodeID, Target: node.ID, Kind: references, Provenance: "string-match", Metadata: {"confidence": <tier>}, Line/Column from ref}`.
  - Tier `high` when the ref's `CalleeExpr` bare name is in the query-builder vocabulary (C4), else `medium`.
- Ambiguity cap: >3 exact-name candidates → no edges (delete the ref). 2–3 candidates → edge to each (same metadata).
- Never create nodes. Never resolve against non-SQL nodes.

### C3 — Resolution pass B: columns (anchored only)

Runs after pass A, operating on `sql_string` refs pass A did not consume. Pass A must return (in memory, same batch invocation) the owner→anchor map — `FromNodeID` → set of matched table/view nodes — that pass B's bare-form matching consumes; pass B does not re-derive it from the edge table.

- **Qualified form** (`x.y`): split on the dot; if `x` case-insensitively names a SQL table/view, and a column node exists whose `QualifiedName` equals `<thatTableQName>.y` (case-insensitive), create the edge to the column node, confidence `medium`. Ambiguity cap as C2.
- **Bare form + anchor**: for each owner (`FromNodeID`) that gained ≥1 pass-A table/view edge, match its remaining bare `sql_string` refs case-insensitively against the column nodes of those anchor objects only (via `QualifiedName` prefix `<anchorQName>.`). Match → edge to the column node, confidence `low` — upgraded to `medium` when the ref's `CalleeExpr` is in the C4 vocabulary (declaration-DSL position, e.g. `Column("name")`).
- Bare column refs with no anchor in their owner scope: never emitted as edges.

### C4 — Query-builder vocabulary

- One shared, exported set of bare callee names in the standalone extraction package (single flat list, case-insensitive compare): `selectFrom, insertInto, updateTable, deleteFrom, replaceInto, mergeInto, from, into, table, join, innerJoin, leftJoin, rightJoin, fullJoin, crossJoin, joinRaw, call, callproc, from_, table_, column, field, tableName, withTableName, toTable, hasColumnName, hasTableName, entityName` (28 names; the last 8 are declaration-DSL callees from the 2026-07-19 ecosystem research — Slick/SQLite.swift/Exposed/GRDB/EF Core/jOOQ escape hatches).
- Vocabulary membership only upgrades confidence: object matches medium → high (C2); anchored bare-column matches low → medium (C3). It never causes an edge on its own — the index match is always required.

### C5 — Cleanup: speculative refs never persist

After passes A and B, every remaining `sql_string` unresolved reference is deleted. Post-resolution, zero `sql_string` rows exist in `unresolved_refs` regardless of match outcome (matched refs are consumed; unmatched refs are dropped). Re-index/sync is idempotent: same input → same edges, no accumulation.

### C8 — Fragment tier: tokenized SQL fragments

Covers builder-arg SQL fragments (ActiveRecord `where("title LIKE ?")`, GORM `Where("name = ?")`, `order("created_at DESC")`, comma lists like `select("isbn, out_of_print")`) that fail both the identifier shape (C1) and the `IsSQLLiteral` gate.

- A second discriminator constant `ReferenceKindSQLFragment types.EdgeKind = "sql_fragment"` in `types/types.go` — same rules as `sql_string`: unconstrained-TEXT storage, excluded from the standard resolution input, never an edge kind.
- **Fragment gate** (harvest side, checked only after C1's identifier shape and `IsSQLLiteral` both fail): literal length ≤ 160 chars, AND contains ≥1 identifier token, AND contains ≥1 fragment discriminator: a comparison operator (`=`, `<`, `>`, `<=`, `>=`, `<>`, `!=`), a placeholder (`?`, `$N`, `:name`), a comma, or a word-boundary case-insensitive SQL connective/order token (`ASC`, `DESC`, `LIKE`, `IN`, `IS`, `AND`, `OR`, `NOT`, `NULL`, `BETWEEN`).
- **Tokenization**: extract bare identifiers and one-dot qualified pairs (same shapes as C1's regex, applied per token). Drop tokens on a case-insensitive SQL-keyword stoplist (at minimum the discriminator words above plus `SELECT, FROM, WHERE, ORDER, GROUP, BY, HAVING, LIMIT, OFFSET, JOIN, ON, AS, DISTINCT, CASE, WHEN, THEN, ELSE, END`). Each surviving token → one `UnresolvedReference{ReferenceKind: sql_fragment}` with the literal's owner, line/column, and the enclosing `CalleeExpr` (where captured). Dedupe per (owner, token) alongside C1's dedupe.
- **Resolution**: passes A and B (C2/C3) process `sql_fragment` refs identically to `sql_string` refs, then demote the computed confidence **one notch** (high → medium, medium → low, low stays low) — tokenization adds prose-collision risk, e.g. `"error = timeout"` tokenizes and only stays silent because no such objects exist. All other rules unchanged: ambiguity cap, anchor requirement for bare columns, no minting, C5 cleanup deletes leftovers of both kinds (zero `sql_string` AND zero `sql_fragment` rows post-resolution).

### C6 — Query surfaces

- `string-match` edges are ordinary edges: included by default in `callers`/`callees`/`impact`/graph output. `Provenance` and `Metadata` already serialize in JSON output paths — verify, don't rebuild.
- The MCP server annotates/filters `string-match` provenance wherever it does so today for `"heuristic"` provenance (same code path, one more recognized value).

### C7 — Fixtures and validation

- New eval fixture under `scripts/code-eval/fixtures/sql-string-match/`: a `.sql` file defining a table (with columns), a view, and a procedure; a TypeScript file using Kysely-style calls (`selectFrom('<view>')`, `innerJoin`, a qualified `t.col` string, a bare column string in anchored scope, and negative cases: a prose string, a bare column with no anchor, a string matching nothing).
- An engine-level integration test (`engine` package, real SQLite via `t.TempDir()`) indexes the fixture and asserts: expected edges with provenance `string-match` and correct confidence tiers; zero `sql_string` refs remaining; no minted nodes.


## Checkpoints

| # | Deliverable | Done when |
|---|-------------|-----------|
| 1 | C1: `sql_string` speculative harvest + callee capture (TS/TSX, Python, Go) + identifier filter + dedupe | Unit tests cover positive/negative literal shapes, owner attribution, callee capture per language, gate-passing literals untouched; `go test ./internal/codeintel/...` green |
| 2 | C2 + C4 + C5: pass A object matching, vocabulary, provenance/metadata stamping, ambiguity cap, unmatched deletion | Unit tests cover high/medium tiers, case-insensitivity, ambiguity cap, non-SQL-node exclusion, ref cleanup; green |
| 3 | C3: pass B qualified + anchored column matching | Unit tests cover qualified medium, anchored low, no-anchor never-emit, anchor scoping (wrong owner's anchor does not leak); green |
| 4 | C6 + C7: MCP provenance parity, fixture corpus, end-to-end integration test | Integration test asserts the C7 edge set; full `go test ./...` green |
| 5 | C8 fragment harvest: `sql_fragment` discriminator, fragment gate, tokenizer, stoplist, resolution-input exclusion | Unit tests cover gate boundaries (length cap, discriminator presence, prose rejection), tokenization (bare + qualified, keyword stoplist, dedupe), exclusion; green |
| 6 | C8 resolution + C4 vocabulary expansion + C3 vocab column upgrade + fixture extension | Unit tests cover one-notch demotion at every tier, vocab column upgrade, both-kind C5 cleanup; e2e fixture gains fragment cases (where-fragment, order-DESC, comma pluck, prose negative) and asserts their edges; full `go test ./...` green |


## Implementation log

- CP1 `4ef7167` — sql_string harvest, callee capture (Go/TS/TSX/Python; generic langs empty), resolution-input exclusion.
- CP2 `7b88006` — pass A object matching, 20-name vocabulary, confidence metadata, C5 cleanup.
- CP3 `63931d8` — pass B qualified + anchored column matching.
- CP5 `a0b37a5` — sql_fragment discriminator, fragment gate, tokenizer + stoplist, exclusion, both-kind cleanup sweep.
- CP6 — fragment resolution (compute-then-demote), vocabulary 20→28, C3 vocab column upgrade, fixture fragment cases + prose-collision decoy assertion.
- CP4 — MCP provenance parity (`isSynthesizedProvenance`), fixture corpus, engine e2e test. Empirical finding recorded in the design doc: views get no column nodes, so view-column anchoring is out of scope.


## Change log

- 2026-07-18 — Initial spec (autopilot run; design approved in-conversation).
- 2026-07-19 — Wild-usage follow-through (ecosystem research, two doc sweeps): added C8 fragment tier (`sql_fragment` kind, fragment gate, tokenizer, one-notch demotion); expanded C4 vocabulary 20 → 28 with declaration-DSL callees; C3 anchored-column vocab upgrade (low → medium); C5 cleanup now covers both kinds; checkpoints 5–6 added.
- 2026-07-18 — Spec-review amendments: `sql_string` is a discriminator excluded from the standard resolution input (never reaches `promoteEdgeKind`; edges always `Kind: references`); C2 retrieval pinned to bulk per-kind fetch + in-memory name map; pass A returns the owner→anchor map pass B consumes; empty-`CalleeExpr` behavior made uniform; integration test pinned to engine level.
