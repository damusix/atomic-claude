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
- [ ] The migration runner records the baseline in `schema_versions` and applies
      a synthetic pending migration idempotently (machinery for future schema
      changes — not a replay of the reference's historical v1–v4; see CP4 + its
      change-log entry).
- [ ] FTS search returns results in the **same rank order** as the reference on a
      known corpus (BM25 weights `(0,20,5,1,2)` from appendix J); cascade delete
      removes edges; chunked `IN (...)` works past 500 params.

## Checkpoints

Phased, one cohesive builder slice each, ending green. CP1 (master CP0) is a hard
gate — the rest of the port is contingent on it.

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **(master CP0) Build-strategy gate — viability PROVEN (spike round 2).** Remaining is mechanical: (1) produce the full **19-grammar `ts.wasm`** (vendor the missing 5–6: typescript, tsx, dart, luau, objc, pascal, at the reference's grammar versions) via the forked `ts-fork` harness + `zig cc` (build-time only; runtime stays CGO-free); (2) adopt the proven runtime shape — **instance-per-goroutine pool + recycle every ~500 parses + low-round-trip traversal** (R-WALK). | `internal/codeintel/tsbinding/` (binding fork of `malivvan/tree-sitter`, self-embeds `lib/ts.wasm` via `//go:embed`, wired via `replace` in `atomic/go.mod`); `internal/codeintel/grammars/README.md` (grammar manifest + version pins); ref `src/extraction/grammars.ts`, `parse-worker.ts` (SKIP threads; KEEP memory-reclaim intent); spike `tmp/code-intel-engine-go/.../spikes/grammar-bundle` | 19-grammar `ts.wasm` builds + each vocab-matches; pool race-clean; RSS bounded at recycle@500; traversal avoids per-node WASM crossings |
| 2 | **(master CP1) Types contract + Go conventions**: kind/language consts + core structs; decide JSON-column handling (`json.RawMessage` vs typed), integer-bool scan, stable-sort-on-serialize for Subgraph. A test asserts the verbatim lists. | `internal/codeintel/types`; ref `src/types.ts` (COPY); appendix C | Test asserts 22 NodeKind / 12 EdgeKind / 29 Language; conventions documented in package doc |
| 3 | **(master CP2) DB connection + schema**: embed schema via `//go:embed`; open a **single connection** with the pragma sequence (busy_timeout first; FK on); run schema; version row. | `internal/codeintel/db`; ref `src/db/index.ts`, `schema.sql` (COPY); appendix A, O | Fresh DB dumps schema byte-identical to reference; WAL + FK on; single connection |
| 4 | **(master CP3) Migration machinery**: a `schema_versions` ledger (applied version + timestamp per row) + a forward-only runner that applies an ordered list of Go-defined migrations above the recorded version, idempotently. CP3 already builds the current schema directly (appendix A = the post-v4 reference state), so the runner seeds that schema as the baseline (already-applied) and is a no-op on a fresh CP3 DB; it exists to carry FUTURE schema changes. NOT a replay of the reference's historical v1–v4 (those deltas are unrecoverable — the reference `migrations.ts` is absent locally). | `internal/codeintel/db`; ref `src/db/migrations.ts` (intent only — file absent) | Runner records the baseline in `schema_versions`; a synthetic pending migration applies once, is recorded, and re-running is a no-op (idempotent) |
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

### 2026-06-05 — CP0a built; ts.wasm embed location corrected

**Correction:** CP0a (the grammar half of checkpoint 1) shipped the 19-grammar
`ts.wasm` self-embedded by the binding fork at
`internal/codeintel/tsbinding/lib/ts.wasm` (`//go:embed lib/ts.wasm` in
`treesitter.go`), not at `internal/codeintel/grammars/ts.wasm` as the body
originally implied. How we know: `go:embed` cannot reference paths outside the
embedding package's module, and the binding fork is its own module — so the wasm
must live inside it. `grammars/` now holds only the grammar manifest README.
CP0b decides whether to introduce a dedicated `grammars` package that owns the
embed and injects bytes into the binding (the original design separation), or
keep the binding self-embedding. Body updated to the as-built location.

**What changed:** Built across the CP0a iterations: 19 grammars vendored + a
forked wazero binding at `internal/codeintel/tsbinding/`, `replace`'d in
`atomic/go.mod`; probe confirms 20/20 grammars (incl. tsx) load at ABI 14 with
zero ERROR nodes; `CGO_ENABLED=0` build clean.

### 2026-06-05 — CP4 migrations: machinery, not historical replay

**What changed:** Rewrote checkpoint 4 from "reproduce v2–v4" to "build the
migration machinery (a `schema_versions` ledger + forward-only runner) seeded at
the current baseline." CP3 builds the final schema (appendix A) directly, so the
runner is a no-op on a fresh DB and exists to carry future schema changes; it is
verified with a synthetic pending migration rather than the reference's history.

**Why:** The reference `src/db/migrations.ts` is absent from this repo (learning
anchor only), so the historical v1→v4 deltas are unrecoverable; and this is a new
engine with zero existing databases, so replaying historical migrations has no
consumer. The durable contract is the runner + ledger, not the old version numbers.

**Superseded:** prior CP4 contract — "`schema_versions`, forward-only runner,
v2–v4; old-schema DB migrates to v4" (a replay of the reference's migration
history).

## Implementation log

### v1 substrate (master CP0–4) — 2026-06-05

Built across 8 iterations of `/subagent-implementation` (worktree
`code-intel-engine`). Commits (chronological):

- `bbcf00a` — CP1/master-CP0a: 19 tree-sitter grammars vendored + wazero binding fork (`internal/codeintel/tsbinding`)
- `e5757b4` — CP1/master-CP0b: parser pool + recycle@500 + cursor traversal (`internal/codeintel/extraction`)
- `f7dad20` — CP2/master-CP1: types contract (22 NodeKind / 12 EdgeKind / 29 Language + structs)
- `ce75596` — CP3/master-CP2: single-connection modernc sqlite + embedded schema + pragmas
- `f87e056` — CP4/master-CP3: schema_versions ledger + forward-only migration runner
- `8faa3ba` — CP5/master-CP4: db CRUD + FTS search (bm25 0,20,5,1,2 + tiebreaker), rank parity vs spike golden

**Out-of-scope work performed during this build:**
- Dropped a redundant 35 MB `grammars/ts.wasm` duplicate before the CP0a commit (the binding self-embeds `tsbinding/lib/ts.wasm`; `go:embed` can't cross module dirs) — corrected the spec's embed location + change-logged.
- Rewrote CP4 from "replay reference v2–v4" to "migration machinery" (reference `migrations.ts` absent; fresh schema already current) — spec body + change log updated.

**Unforeseens — surprises during implementation:**
- Reviewer caught 4 real 🔴 bugs that clean compiles + passing probes hid: a `ParseString` nil-free-defer panic, `NamedChildCount` calling the wrong wasm fn, a `Close()` instance leak, and a `Tree` interface leaking the concrete `sitter.Node` type. All fixed with regression tests.
- The vendored binding exposes no tree cursor (only a per-node iterator), so `WalkNamed` crosses the WASM boundary per node — the true low-round-trip bulk-serialize is deferred (F-3 / design open-Q#4); not a viability blocker.
- Grammar C sources + `ts.wasm` were trimmed from the spike bundle as regenerable; vendored 19 grammars fresh from upstream at pinned versions (recorded in `grammars/README.md`).

**Deferred items still open (scratchpad FOLLOWUPS F-1..F-10, engine-wide):**
- Binding mem-leaks (F-1 strlenPtr, F-2 ctx); true bulk-walk traversal (F-3); async recycle (F-4); recycle-panic reconsider (F-5); close-error visibility (F-6); single-conn FK-on-reconnect hardening (F-7); migration MAX-NULL guard + stronger rollback test (F-8); CP5 test hardening (F-9); `UpsertNode` updated_at=0 never round-trips — CP10 sync needs it (F-10).
- These target later checkpoints (CP10/CP18/CP20/CP22) and are carried in the shared engine scratchpad ledger, dispositioned with the user when the full engine completes.
