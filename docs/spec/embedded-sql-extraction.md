# Embedded SQL extraction

## Goal

Extend the code-intel indexer so that SQL embedded in host-language string literals (Go, Python, TypeScript, TSX) is extracted into real table/column/edge nodes attributed to the host file, making embedded-migration and query code visible to `atomic code impact` and related verbs.

## Non-goals

- Multi-fragment / concatenated queries (`"SELECT " + cols + " FROM t"`) — accepted false negative.
- dbt-Jinja and other templating dialects — separate follow-up.
- New SQL parsing capability — the existing regex extractor (`standalone/sql.go`) is reused verbatim; no new SQL parser.
- English-language heuristics in the gate — the gate recognizes SQL structure only; no stopword lists.
- Wiring variable/constant nodes into host extractors — ownership uses existing host nodes only; Python/Go emit no variable nodes.

## Success criteria

- [ ] A `.go` file containing `CREATE TABLE` in a raw or interpreted string literal is indexed with at least one `table` node attributed to that file and correct `StartLine`/`EndLine`.
- [ ] A `.py` file containing `CREATE TABLE` in a regular or triple-quoted string is indexed with at least one `table` node attributed to that file.
- [ ] A `.ts`/`.tsx` file containing `CREATE TABLE` in a string or template literal is indexed with at least one `table` node attributed to that file.
- [ ] DML (`SELECT`/`INSERT`/`UPDATE`/`DELETE`) embedded in a host-language literal produces `UnresolvedReference` entries owned by the enclosing host function node, or the file node when no containing function exists — matching decision 1 (`extractor.go:533-548` span containment).
- [ ] All edges and unresolved references produced by embedded extraction carry `Provenance: "embedded"` — new value, distinct from `""` (static) and `"heuristic"`. Queryable via `GetEdgesByProvenance("embedded")` (`crud.go:290`).
- [ ] A Python module-level, class-level, or function-level docstring that contains SQL-shaped text is **not** extracted (decision 4).
- [ ] An interpolated Python f-string or TypeScript template literal where the **table target** is an interpolation segment (e.g. `f"SELECT a FROM {table}"`) produces zero extracted nodes/edges for that literal (decision 8). An interpolated literal where the table name is a plain identifier and only a **value** is interpolated (e.g. `f"SELECT a FROM users WHERE id = {id}"`) produces a normal reference edge to `users` — the interpolation segment is treated as a parameter placeholder.
- [ ] Prose strings — `"choose an item from the dropdown"`, `"Copied from the original repo"` — do not pass the admission gate and produce zero nodes/edges.
- [ ] The corpus run (`scripts/code-eval/` harness extended) against at least one real repo returns zero resolved phantom edges: `GetEdgesByProvenance("embedded")` resolves only to nodes that actually exist in the index (decision 6).
- [ ] Line numbers on embedded nodes/edges are file-absolute (not relative to the literal's start), using the same `offsetResult` mechanism already used for Vue/Svelte (`standalone/standalone.go:123`).
- [ ] The existing standalone SQL extractor (`standaloneExts` at `orchestrator.go:150`) is unchanged — `.sql`/`.ddl`/`.pgsql`/`.mysql` files route identically to today.
- [ ] All existing tests pass unchanged after each checkpoint.

## Approaches

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Orchestrator post-pass: after host tree-sitter extraction, harvest string literals → gate → SQL machinery → merge into same `ExtractionResult` | Reuses `SQLExtractor` verbatim for DDL; single store transaction; ownership scan has host nodes in hand | One change in the hot indexing path; per-language literal node types to enumerate |
| B | Synthetic `CREATE FUNCTION` wrapper to reuse DML body-scan via `Extract` | No new exported entry point | Parse round-trip to fake what `scanBodyEdges` already accepts directly; offset math through wrapper; rejected in pressure-test |
| C | Second indexing pass over all files for SQL-in-strings (separate extractor registration) | No orchestrator change | Re-reads/re-parses every file; ownership scan loses host-node context; two store transactions per file |
| D | Wire a tree-sitter SQL grammar for validation + extraction | Real parser | No SQL grammar in `tsbinding/src/` (verified); large new surface; prose can be grammatically valid SQL (alias forms) |

## Recommendation

Approach A. The SQL side is proven reusable as-is: DDL via `Extract` verbatim, DML via direct `scanBodyEdges` call — both probed. The host side already has every primitive: literal spans from tree-sitter, line-offset mapping (`offsetResult`, `standalone/standalone.go:123`), and node spans for ownership containment. The genuinely new code is the per-language harvester, the gate, and orchestrator wiring. Provenance plumbing has a clean seam: embedded refs carry `Language: SQL` with a non-SQL `FilePath` extension, distinguishable at `createEdges` (`pipeline.go:753`).

## Gate contract

The gate is defined by the test corpus, not prose. Two implementers reading the corpus must build equivalent gates.

### Discriminator ranking

| Discriminator | Status | Description |
|---------------|--------|-------------|
| DDL keyword + identifier | **Required** | `CREATE TABLE|VIEW|INDEX|SEQUENCE|TRIGGER|FUNCTION|PROCEDURE|SCHEMA` followed by a valid SQL identifier |
| DML verb at trimmed start | **Required** | `SELECT`, `INSERT INTO`, `UPDATE`, `DELETE FROM`, or `MERGE INTO` as the first non-whitespace token |
| Structural corroboration | **Confidence** | At least one of: comma-separated list, comparison operator (`=`, `<`, `>`), quoted string literal inside the SQL, or positional/named placeholder (`$1`, `?`, `:name`, `%s`) |

A literal passes if it satisfies a Required discriminator; DML additionally requires at least one Confidence discriminator. Both must match on the trimmed literal after comment stripping (`stripComments` already applied in the SQL extractor).

### Canonical corpus cases

| Case | Input | Expected outcome |
|------|-------|-----------------|
| Real DDL | `"CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"` | Pass — extracts `table` node `users` with columns |
| Real DML | `"SELECT id, email FROM users WHERE active = $1"` | Pass — emits `UnresolvedReference` to `users` (reference edge) owned by enclosing function/file |
| UI prose | `"choose an item from the dropdown"` | Fail gate — zero nodes/edges |
| Code comment prose | `"Copied from the original repo"` | Fail gate — zero nodes/edges |
| Interpolated table target (f-string / template literal) | `f"SELECT a FROM {table} WHERE id = %s"` | Gate may pass (SELECT + FROM + placeholder), but the table target is an interpolation segment — zero nodes, zero refs from that literal (decision 8) |
| Interpolated value, literal table (f-string / template literal) | `f"SELECT a FROM users WHERE id = {id}"` | Pass — emits normal reference edge to `users`; the interpolation segment `{id}` is treated as a parameter placeholder |

## Checkpoints

| # | Checkpoint | Files / areas | Agent | Est. files | Verifies |
|---|-----------|---------------|-------|------------|---------|
| 1 | SQL-side embedded entry point | `standalone/sql.go`, `standalone/` tests | atomic-builder | 3-4 | Gate (DDL + DML discriminators); DDL path emitting nodes; DML path emitting `UnresolvedReference`; `Provenance: "embedded"` on directly-created edges; line-offset mapping via `offsetResult`. Synthetic gate tests covering all four canonical corpus cases plus offset correctness. |
| 2 | Go harvester + orchestrator post-pass | `extraction/languages/go.go`, `indexer/orchestrator.go`, `resolution/pipeline.go` (provenance seam) | atomic-builder | 5-6 | End-to-end: `.go` file with embedded DDL + DML → `table` nodes with file-absolute lines + embedded-provenance edges; ownership containment (enclosing function node or file fallback); existing tests unchanged |
| 3 | Python harvester | `extraction/languages/python.go`, harvester tests | atomic-builder | 3-4 | `.py` file with regular and triple-quoted string DDL/DML → nodes + edges; module/class/function docstrings excluded; (a) f-string with interpolated table target (`f"SELECT a FROM {table}"`) yields zero nodes, zero refs; (b) f-string with literal table and interpolated value (`f"SELECT a FROM users WHERE id = {id}"`) yields a normal reference edge to `users` |
| 4 | TypeScript + TSX harvester | `extraction/languages/typescript.go`, `extraction/languages/tsx.go`, harvester tests | atomic-builder | 3-4 | `.ts`/`.tsx` file with string and template-literal DDL/DML → nodes + edges; (a) template literal with interpolated table target (`` `SELECT a FROM ${table}` ``) yields zero nodes, zero refs; (b) template literal with literal table and interpolated value (`` `SELECT a FROM users WHERE id = ${id}` ``) yields a normal reference edge to `users` |
| 5 | Corpus harness gate run | `scripts/code-eval/`, `atomic/internal/codeintel/db/crud.go` | atomic-builder | 2-3 | Extend `run-eval.sh` (or new script) to harvest all string literals from at least one real repo, run gate, report admission count; assert `GetEdgesByProvenance("embedded")` returns zero resolved phantom edges |
| 6 | Docs + followup close | `docs/reference/code-intel.md`, `.claude/project/followups/embedded-sql-extraction.md` | atomic-builder | 1-2 | `docs/reference/code-intel.md` updated with embedded-SQL section; followup entry closed via `atomic followups close embedded-sql-extraction` |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Gate admits prose that resolves to a real table name | Low (strict gate) | Corpus run with zero-resolved-phantom bar before merge; `Provenance: "embedded"` makes any escapee queryable and deletable |
| Duplicate table nodes: same table defined in `.sql` and embedded migration | Medium | Both are real definitions — duplicates are legitimate facts; resolver's existing candidate machinery handles multi-candidate refs; revisit only if corpus run shows resolution misbehavior |
| Per-literal gate cost on string-heavy repos | Low | Cheap pre-filter (SQL keyword presence) before structural regex; gate runs only on literals, not source |
| Python docstring detection misses a form (module-level, class-level, nested) | Medium | All three docstring positions enumerated in synthetic tests (CP3) |
| Embedded `contains` edges (table→column) bypass resolution, so provenance must be stamped at creation time | Certain | New embedded entry point (CP1) stamps `Provenance` on directly-returned `Edge` values; resolution seam at `createEdges` (`pipeline.go:753`) covers unresolved refs |

## Change log

<!-- Populated on first post-approval amendment. -->

## Implementation log

### Shipped — 2026-06-10

Built across 6 checkpoints + 1 polish pass via `/subagent-implementation` (8 loop iterations; CP3 and CP4 each took one fix round). Branch `embedded-sql-extraction`. Commits (chronological):

- `d9b6dc1` — design + spec
- `261f63b` — CP1 SQL-side gate (`IsSQLLiteral`) + embedded entry point (`ExtractEmbeddedSQL`)
- `16b3808` — CP2 Go harvester + orchestrator post-pass + `Provenance: "embedded"` resolution seam
- `7afa3d6` — CP3 Python harvester (docstring exclusion, f-string interpolation) + ref-ID collision fix
- `7f284fc` — CP4 TypeScript + TSX harvester
- `47b41e0` — CP5 corpus gate harness (zero-phantom check)
- `c6578f8` — CP6 docs (`docs/reference/code-intel.md`) + closed the originating follow-up
- `388238f` — polish pass (dead var, var rename, comment fix)
- `be9e8d8` — deferred 7 follow-ups to `.claude/project/followups/`

**Out-of-scope work performed during this build:**

- `offsetResult` (`standalone/standalone.go`) now regenerates `UnresolvedReference.ID` after applying the line offset. Beyond CP3's stated scope, but a justified root-cause fix — see Unforeseens.

**Unforeseens — surprises that emerged during implementation:**

- Strengthening a vacuous CP3 test (decision-8b) exposed a real data-loss bug: two embedded refs on the same relative line hashed to identical IDs and `INSERT OR IGNORE` dropped one. Fixed in the `offsetResult` change above. The parallel `Node.ID` collision (DDL nodes) was left unfixed — see deferred items.
- CP2's Go literal harvester was implemented as a hand-written scanner rather than the briefed tree-sitter walk; reviewer-accepted after verifying escape/raw-string/rune/comment edge cases. Python (CP3) and TS/TSX (CP4) use tree-sitter (docstring position and template interpolation need structure a flat scanner can't provide).
- CP5 corpus run over the local `atomic/` tree: 22,847 literals scanned, 126 admitted, 0 dangling edges. Five `UPDATE`-verb prose false positives admitted by the gate but emit zero edges — zero resolved phantom edges, so the decision-6 bar holds.

**Deferred items still open** (tracked in `.claude/project/followups/`):

- `embedded-sql-node-id-collision` (risk) — `offsetResult` doesn't regenerate `Node.ID`; latent DDL-node collision, needs edge-endpoint remap.
- `embedded-sql-dml-gate-tightening` (risk) — tighten DML gate to cut `UPDATE`-verb prose FPs.
- `embedded-sql-ext-list-dup` (risk) — SQL ext list duplicated across pipeline + orchestrator.
- `embedded-sql-harness-empty-index` (risk) — corpus harness vacuous-PASS on empty index.
- `embedded-sql-multiline-offset-test`, `embedded-sql-lineoffset-test-nodeid`, `embedded-sql-eval-tool-nits` (nits).

**Follow-on scope (not part of this spec):** extending embedded extraction to the remaining engine languages (Ruby, Java, Rust, C#, etc.) — to be planned separately. The harvester registry and `StringLiteralSpan` seam already generalize for it.

**Squashed to b439d18 — 2026-06-10.** Per-iteration SHAs above are historical (unreachable from any branch). All seven deferred items listed above were subsequently fixed in the followup-hardening batch (see `docs/spec/followup-hardening.md`); only the separate Vue/Svelte node-ID staleness (`followup-hardening-f-4`) remains open.
