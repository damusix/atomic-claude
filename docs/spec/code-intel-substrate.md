# Code-intelligence engine — substrate (part 1/5)

Part 1 of the 5-part code-intelligence engine port. **Umbrella:**
`docs/spec/code-intel-engine.md` (goal, roadmap, dependency DAG, and the
authoritative **reference appendix A–O** — the contracts). **Design:**
`docs/design/code-intel-engine.md` (build-strategy decision, wazero-memory
correction, concurrency model, §atomic CLI integration).

**Depends on:** nothing — this is the foundation. **Blocks:** every other part.

Brand bindings (from the umbrella): commands `atomic code <verb>`; data dir
`<projectRoot>/.claude/.atomic-index/`; SQLite file `atomic.db`; MCP prefix
`atomic_code_*`. Never emit the reference implementation's product name.

## Scope

The viability gate plus the SQLite substrate every later part sits on: the
19-grammar wasm + proven runtime shape (master CP0), the types contract (CP1),
the single-connection DB + schema + pragmas (CP2), migrations (CP3), and the
prepared-statement query layer + FTS parity (CP4).

**Contracts (authoritative, in the umbrella appendix):** A (schema DDL), C (kind
& language strings), D (extension→language map), J (BM25 column weights +
`ORDER BY score, nodes.id` tiebreaker — the FTS-parity portion only; the full
search contract is part 4), O (connection pragmas).

## Success criteria

- [ ] `CGO_ENABLED=0 go build` of `atomic` (engine compiled in) produces a single
      static binary; cross-compiles to darwin/linux × amd64/arm64 with no C
      toolchain (windows best-effort, not a gate).
- [ ] All 19 grammars load under wazero with **node-type vocabulary matching**
      the reference's grammars; parser pool is race-clean; a recycle returns RSS
      to within K% of baseline on a 10k-file repo (master CP0 gate).
- [ ] `NodeKind`/`EdgeKind`/`Language` consts equal the appendix C lists (count +
      spelling), enforced by a test.
- [ ] A fresh DB dumps a schema **byte-identical** to the reference schema
      (appendix A), with WAL + `foreign_keys=ON` on the one connection.
- [ ] An old-schema DB migrates forward to v4 idempotently.
- [ ] FTS search returns results in the **same rank order** as the reference on a
      known corpus (BM25 weights `(0,20,5,1,2)` from appendix J); cascade delete
      removes edges; chunked `IN (...)` works past 500 params.

## Checkpoints

Phased, one cohesive builder slice each, ending green. CP1 (master CP0) is a hard
gate — the rest of the port is contingent on it.

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **(master CP0) Build-strategy gate — viability PROVEN (spike round 2).** Remaining is mechanical: (1) produce the full **19-grammar `ts.wasm`** (vendor the missing 5–6: typescript, tsx, dart, luau, objc, pascal, at the reference's grammar versions) via the forked `ts-fork` harness + `zig cc` (build-time only; runtime stays CGO-free); (2) adopt the proven runtime shape — **instance-per-goroutine pool + recycle every ~500 parses + low-round-trip traversal** (R-WALK). | `internal/codeintel/grammars` (embedded `ts.wasm`); binding under `internal/codeintel/extraction` (iface only here); ref `src/extraction/grammars.ts`, `parse-worker.ts` (SKIP threads; KEEP memory-reclaim intent); spike `tmp/code-intel-engine-go/.../spikes/grammar-bundle` | 19-grammar `ts.wasm` builds + each vocab-matches; pool race-clean; RSS bounded at recycle@500; traversal avoids per-node WASM crossings |
| 2 | **(master CP1) Types contract + Go conventions**: kind/language consts + core structs; decide JSON-column handling (`json.RawMessage` vs typed), integer-bool scan, stable-sort-on-serialize for Subgraph. A test asserts the verbatim lists. | `internal/codeintel/types`; ref `src/types.ts` (COPY); appendix C | Test asserts 22 NodeKind / 12 EdgeKind / 29 Language; conventions documented in package doc |
| 3 | **(master CP2) DB connection + schema**: embed schema via `//go:embed`; open a **single connection** with the pragma sequence (busy_timeout first; FK on); run schema; version row. | `internal/codeintel/db`; ref `src/db/index.ts`, `schema.sql` (COPY); appendix A, O | Fresh DB dumps schema byte-identical to reference; WAL + FK on; single connection |
| 4 | **(master CP3) Migrations**: `schema_versions`, forward-only runner, v2–v4. | `internal/codeintel/db`; ref `src/db/migrations.ts` (COPY) | Old-schema DB migrates to v4; idempotent re-run |
| 5 | **(master CP4) Query layer + FTS parity**: prepared statements for node/edge/file CRUD, FTS search, `INSERT OR REPLACE`/`OR IGNORE`, 500-param chunking, batch `getNodesByIds`. FTS test asserts **rank order** on a known corpus; pin + record both SQLite versions. | `internal/codeintel/db`; ref `src/db/queries.ts` (COPY shapes); appendix J (BM25 weights + tiebreaker) | CRUD round-trips; cascade delete removes edges; chunked IN >500; FTS rank order matches reference |

## Risks

Inherited from the umbrella (full table there): **R-A** parallel parse safety,
**R-B** grammar ABI/vocab mismatch, **R1** wazero grow-only memory, **R-WALK**
per-node traversal cost, **R-WASM** blob size, **FK trap** per-connection
`foreign_keys`, **R-F** modernc vs node:sqlite FTS ranking drift. CP1 (master
CP0) is the gate that retires R-A/R-B/R1/R-WALK before any extractor work.

## Change log

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP0–CP4 (grammar/runtime gate,
types, db+schema+pragmas, migrations, query layer + FTS) from
`docs/spec/code-intel-engine.md` into this focused part-spec. Contracts remain
authoritative in the umbrella appendix (referenced by letter here).

**Why:** The 25-checkpoint monolith was too large for a single
`/subagent-implementation` run; split into five dependency-ordered parts.
