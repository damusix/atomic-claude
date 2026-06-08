# Code-intelligence engine — surfaces + validation (part 5/5)

Part 5 of the 5-part code-intelligence engine port. **Umbrella:**
`docs/spec/code-intel-engine.md` (goal, roadmap, dependency DAG, authoritative
**reference appendix A–O**). **Design:** `docs/design/code-intel-engine.md`
(read §atomic CLI integration — this part is where the engine meets `atomic`).

**Depends on:** parts 1–4 green — the facade wraps every prior operation.
**Blocks:** nothing — this is the last part.

Brand bindings (from the umbrella): commands `atomic code <verb>`; MCP prefix
`atomic_code_*`; data dir `<projectRoot>/.claude/.atomic-index/`, file
`atomic.db`. Never emit the reference implementation's product name; de-brand the
server-instructions text to `atomic` + the `atomic_code_*` tool names.

## Scope

The two adapters over the shared query core, plus the validation harness: the
engine facade (master CP20), the `atomic code` CLI subcommands (CP21), the MCP
server `atomic code mcp` (CP22), the optional daemon (CP23), and the validation
harness (CP24).

**Contracts (authoritative, in the umbrella appendix):** K (explore budget
constants — the tiers, the 25k ceiling, the `\n####` section-boundary cut), L
(MCP tool catalog — the 8 `atomic_code_*` tools, node-overload handling,
`buildFlowFromNamedSymbols`, server-instructions), M (the facade method set), N
(the `atomic code` subcommands, `status` JSON shape, DB path).

**Integration seams (design §Integration into the atomic CLI):** add
`case "code": runCode(args[1:], repoOverride)` to `atomic/cmd/atomic/main.go`;
`runCode` sub-dispatches with the existing `flag.FlagSet` style (no
cobra/commander); `atomic setup` adds `.claude/.atomic-index/` to `.gitignore`;
register `atomic code` in `CLAUDE.md` "Atomic binary subcommands", the
`/atomic-help` binary topic, and `docs/reference/`.

## Success criteria

- [x] The facade method set matches appendix M; both the MCP server and the
      `atomic code` CLI compile against it.
- [x] `atomic code <verb>` subcommands exist for index/sync/status/search/
      callers/callees/impact/node/files/affected/explore — each query verb
      with a `--json` mode — dispatched from `runCode`; `case "code"` wired into
      the top-level switch. `atomic code index` idempotently adds
      `.claude/.atomic-index/` to `.gitignore`. Registered in `CLAUDE.md` +
      `/atomic-help` + `docs/reference/commands.md`.
- [x] `atomic code mcp` subcommand exists (CP22 scope).
- [x] MCP `initialize` returns the de-branded server-instructions text; the
      `atomic_code_node` tool returns **all overloads in one call** on an
      ambiguous name; the explore tool respects the exact budget tiers, the 25k
      ceiling, and the section-boundary cut.
- [x] The MCP server is a per-project **singleton**: a tool call against a dead or
      absent `mcp.sock` auto-starts it (flock-guarded — concurrent starts don't
      double-spawn); a second client reuses the warm engine.
- [x] A connection idle >30 min (no request; `updated_at` stale) is force-closed
      by the reaper; the server exits and **removes the socket** after 0
      connections persist for 30 min, so the next call cleanly restarts it.
- [x] Every umbrella success criterion has an automated check in the harness.
      _(`validation.TestCoverageMap`, `validation.TestSchemaDrift`,
      `validation.TestSynthesizedEdgePrecision` —
      package `atomic/internal/codeintel/validation`.)_

## MCP server lifecycle (contract)

Atomic-specific (not a reference COPY — the reference daemon was optional). Full
rationale + sequence diagram in `docs/design/code-intel-engine.md` §MCP server
lifecycle. The contract a builder must implement:

- **Socket = liveness.** Per-project unix domain socket at
  `<projectRoot>/.claude/.atomic-index/mcp.sock`. Connectability is truth; a
  stale socket file whose server is dead (connect → `ECONNREFUSED`) is
  "not running". No PID file.
- **Auto-start (flock-guarded).** Client connect path: try connect → on
  failure/absence, `flock(<…>/.atomic-index/mcp.lock)`, re-check socket, remove
  stale socket file, spawn server **detached**, retry connect with bounded
  backoff. Lock prevents two simultaneous tool calls double-spawning.
- **Connection registry.** Server holds `connID → {created_at, updated_at}`.
  `updated_at` is set to *now* on **every request** from that connection (last
  activity, not connection age).
- **Reaper (in-process goroutine).** Ticks every `reapTick` (60 s); force-closes
  + removes any connection with `now − updated_at > connIdleTTL` (30 min).
- **Auto-shutdown.** When the registry is empty, start a `serverIdleTTL` (30 min)
  timer; if no connection arrives before it fires, close the listener, remove the
  socket file, exit. (0 connections sustained 30 min → exit.)
- **Constants** (named, centralized — axiom 2 / R6): `connIdleTTL = 30m`,
  `serverIdleTTL = 30m`, `reapTick = 60s`. A test asserts them.
- **Relationship to CP3.** CP3 builds the MCP server + tool handlers; CP4 wraps
  that server in this socket-listener + registry + reaper lifecycle. The go-sdk
  server runs over the unix-socket transport rather than stdio.

## Index profiling (`--profile`)

`atomic code index --profile` emits per-phase wall-time to stderr so an indexing
perf cliff is attributable to a phase even when the run is killed by a timeout.
`ATOMIC_CODE_PROFILE=1` enables the same output without the flag (eval scripts
toggle it via env). Default (no flag, env unset): no profiling output; normal
index output unchanged.

Phases are emitted **incrementally** — each line is written as soon as that phase
completes, before the next phase starts — so a process killed mid-resolve still
shows the completed `extract` (and any completed resolve sub-phase) with its
duration. Lines (prefix `[profile] `):

- `extract: <dur> (<n> files)` — `Engine.IndexAll` (scan + tree-sitter extraction + store).
- `frameworks: <dur> (<n> routes)` — `Engine.ExtractFrameworkNodes` (framework route-node extraction; runs after generic extract, before resolve).
- `resolve.warm: <dur> (<n> nodes)` — `warmCaches` (known-files + known-names load).
- `resolve.match: <dur> (<n> refs)` — the `resolveOne` batch loop.
- `resolve.synth: <dur>` — `SynthesizeCallbackEdges`.

Durations are monotonic-clock wall-time. The instrument is read-only: it changes
no indexing behavior, only adds timing + output. Resolve sub-phase timings are
surfaced from the pipeline (a `ResolveProfile` value returned from the batched
resolve path); the CLI prints them when profiling is enabled.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **(master CP20) Engine facade**: the struct exposing the shared query API (lifecycle, index/sync, all get*/search/traversal/context methods). Watch methods stubbed. | `internal/codeintel/engine`; ref `src/index.ts` (COPY surface); appendix M | Facade method set matches appendix M; MCP + CLI both compile against it |
| 2 | **(master CP21) `atomic code` CLI subcommands**: `runCode` dispatch in `atomic/cmd/atomic/main.go` (existing `flag.FlagSet` style) for index/sync/status/search/callers/callees/impact/node/files/affected/explore, each query verb with `--json`, over the engine facade. Wire `case "code"` into the top-level switch; `atomic setup` adds the `.gitignore` entry. | `atomic/cmd/atomic/main.go`, `internal/codeintel/engine`, `internal/claudeinstall` (setup gitignore); ref `src/bin/` (ADAPT to atomic dispatch; SKIP commander); appendix N | `atomic code <verb>` returns correct data + `--json` on a fixture repo; `case "code"` dispatches; setup writes ignore entry |
| 3 | **(master CP22) MCP server** (`atomic code mcp`): go-sdk server, the 8 tool handlers (`atomic_code_*`), the explore algorithm (`buildFlowFromNamedSymbols`, file scoring, adaptive sizing), exact budget tiers + 25k ceiling + section-boundary cut, node-tool overload handling, server-instructions verbatim (de-branded to `atomic` + `atomic_code_*`), tiny-repo tool gating. | `internal/codeintel/mcp`; ref `src/mcp/{tools,server-instructions,session}.ts` (COPY logic; ADAPT transport to go-sdk); appendix K, L | `initialize` returns instructions; explore respects budgets + cut; node returns all overloads |
| 4 | **(master CP23) MCP singleton daemon lifecycle**: per-project unix-socket auto-managed server (see §MCP server lifecycle below for the full contract). Socket at `<projectRoot>/.claude/.atomic-index/mcp.sock`; flock-guarded auto-start on tool call; connection registry with per-request `updated_at`; in-process reaper drops connections idle >30 min; auto-shutdown + socket removal when 0 connections for 30 min. Named-constant thresholds. | `internal/codeintel/mcp`; ref `src/mcp/{daemon,proxy,engine}.ts` (ADAPT — simpler in Go) | socket liveness check; dead-socket auto-start; second client reuses warm index; idle conn reaped at 30 min; server exits + removes socket after 30 min idle; concurrent starts don't double-spawn |
| 5 | **(master CP24) Validation harness**: schema-diff test, reference-exported node-id vectors, per-language extraction fixtures, synthesized-edge precision spot-check, FTS rank-order test, MCP budget tests. | `internal/codeintel/...` test files; ref `__tests__/`, `scripts/agent-eval/` (ADAPT) | Every success criterion has an automated check |

## Risks

Inherited from the umbrella: **R6** (calibrated constants drift — a test asserts
the explore budget tiers + BM25 weights literally, per appendix K/J). The
artifact-discovery checklist (`CLAUDE.local.md`) is a hard obligation for CP2 —
a new `atomic code` verb that is not registered in `CLAUDE.md` + `/atomic-help` +
`docs/reference/` is an invisible feature.

## Change log

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP20–CP24 (engine facade,
`atomic code` CLI, MCP server, daemon, validation harness) from
`docs/spec/code-intel-engine.md`. The atomic CLI integration seams (dispatch,
setup gitignore, artifact registration) are concentrated here.

**Why:** Split the 25-checkpoint monolith into five dependency-ordered parts.

### 2026-06-06 — CP20 engine facade implemented

**What changed:** `internal/codeintel/engine` package created. One `Engine`
struct wraps db, extraction.Pool, indexer.Orchestrator, resolution.Pipeline
(with framework + synthesis seams), graph.Manager, search.Searcher, and
codectx.Builder into the appendix-M method set. `db/stats.go` adds GetStats,
GetAllFiles, and Clear helpers. Watch/StopWatch stubbed with
ErrWatchNotImplemented. All 10 engine tests pass; full suite green (39 packages).

**Why:** CP20 checkpoint — the facade is the surface both CP21 (CLI) and CP22
(MCP) compile against.

### 2026-06-04 — MCP server is a singleton auto-managed daemon (CP23 rewrite)

**What changed:** Rewrote master CP23 from an *optional* daemon ("only if
cold-open exceeds a threshold") into the **primary serving model**: a per-project
unix-socket singleton that auto-starts on a tool call (flock-guarded), tracks
per-connection `updated_at`, reaps connections idle >30 min via an in-process
ticker, and auto-shuts-down (removing the socket) after 0 connections for 30 min.
Added the `## MCP server lifecycle (contract)` section, two success criteria, and
expanded the CP23 verification column. Constants `connIdleTTL`/`serverIdleTTL`
(30 min) + `reapTick` (60 s) are named + asserted. Design counterpart:
`docs/design/code-intel-engine.md` §MCP server lifecycle.

**Why:** Project-owner directive — the server must be invisible to the user: one
instance, started on demand via socket presence (not a PID file), never on
forever, never orphaned.

**Superseded:** prior CP23 contract — "(Optional) Daemon … only if cold-open
exceeds a set threshold (e.g. >Xms on a 5k-file DB)"; serving was otherwise a
plain long-lived/stdio process.

### 2026-06-06 — CP21 `atomic code` CLI subcommands implemented

**What changed:** `atomic/internal/codeintel/cli` package added with
`RunCode(args, projectRoot, stdout, stderr)` dispatcher and 11 verb handlers:
`runIndex`, `runSync`, `runStatus`, `runSearch`, `runCallers`, `runCallees`,
`runImpact`, `runNode`, `runFiles`, `runAffected`, `runExplore`. All query verbs
support `--json`. `StatusJSON` struct matches appendix N (initialized, version,
indexPath, lastIndexed, fileCount, nodeCount, edgeCount, backend, journalMode,
nodesByKind, pendingChanges). `EnsureGitignore` idempotently adds
`.claude/.atomic-index/` to `.gitignore` (creates file if absent). F-56 fix:
`orchestrator.IndexPaths` added; `engine.IndexFiles` now delegates to it instead
of calling `IndexAll`. `case "code"` wired into `atomic/cmd/atomic/main.go`
top-level switch. Registered in `CLAUDE.md` + `templates/commands/atomic-help.md`
(binary topic + Stage 4) + `docs/reference/commands.md`. Bundle regenerated. 14
CLI tests pass; full suite green.

**Why:** CP21 checkpoint — the `atomic code` binary interface is the primary user
surface for the code-intel engine; CLI handlers are extracted into a testable
`io.Writer`-injected package so verb behavior can be verified without
`os.Exit`.

### 2026-06-06 — CP22 MCP server implemented

**What changed:** `atomic/internal/codeintel/mcp` package added with `NewServer`,
`RunStdio`, and 8 `atomic_code_*` tool handlers (search, callers, callees, impact,
node, explore, status, files). `GetExploreBudget` / `GetExploreOutputBudget`
(`ExploreOutputBudget` struct with exported fields) / `ApplyCeiling` exported for
test assertions. Appendix-K budget tiers encoded exactly and verified by
table-driven tests. `atomic_code_explore` respects the 25 k hard ceiling and
section-boundary cut (`\n####`); `buildFlowFromNamedSymbols` BFS with ≤1 bridge
hop. Tiny-repo gating: `<500` files → 3 tools only; ≥500 → all 8. Server
instructions de-branded (no reference product name). `jsonschema` tags use
plain-description format (SDK v1.6.1 rejects `WORD=value` prefix). `go-sdk
v1.6.1` added to `go.mod`; `CGO_ENABLED=0 go build ./...` and full 43-package
test suite green. `case "mcp"` wired into `cli/code.go` `runCode` dispatch and
`printCodeUsage`. 20 in-process tests via `sdk.NewInMemoryTransports()` pass.

**Why:** CP22 checkpoint — transport-agnostic MCP server is the primary interface
for agents navigating the indexed codebase; CP23 (daemon) connects to it over a
unix socket using the same `NewServer` + `srv.Connect` surface.

### 2026-06-06 — CP23 MCP singleton daemon lifecycle implemented

**What changed:** Implemented master CP23: `mcp/daemon.go` (per-project unix-socket
daemon with `Daemon`, clock-injectable `registry`, `touchingConn`, `IsLive`,
`RunDaemon`, `NewTestDaemon`, `RunAcceptLoop`; exported constants `ConnIdleTTL`,
`ServerIdleTTL`, `ReapTick`; in-process reaper goroutine; auto-shutdown with socket
removal; fixed defer ordering bug that deadlocked reaper and shutdown).
`mcp/proxy.go` (`SpawnFunc`, `DefaultSpawn`, `EnsureRunning`, flock-guarded
connect-or-start, `RunProxy` bidirectional pipe). `cli/code.go` wired: `mcp` verb
uses `RunProxy`; internal `__serve` verb invokes `RunDaemon`. 13 CP23 tests all
pass (constants, IsLive, registry, auto-start, warm-reuse, auto-shutdown,
reaper, e2e real-socket MCP session). Success-criteria checkboxes ticked.

### 2026-06-06 — CP24 validation harness implemented (part 5 complete; engine feature-complete CP0–24)

**What changed:** New package `atomic/internal/codeintel/validation` with three
tests. `TestCoverageMap` — auditable Go table mapping all 11 umbrella criteria to
covering automated tests; fails if any non-CI criterion is unmapped.
`TestSchemaDrift` — opens a fresh migrated DB, dumps `sqlite_master` normalised +
sorted, compares against an embedded 15-entry canonical snapshot; fails on schema
drift. `TestSynthesizedEdgePrecision` — multi-synthesizer fixture asserting exact
heuristic edge set, zero edges on non-qualifying nodes, and every heuristic edge
has `kind=calls`, `provenance="heuristic"`, non-empty `synthesizedBy`. All 16
`codeintel/*` packages pass `go test ./...`. Last success criterion ticked `[x]`.

**Why:** CP24 — the final checkpoint. All five part-specs (CP0–24) are now
implemented and green. Engine feature-complete.

### 2026-06-06 — `atomic code index --profile` phase timing

**What changed:** Added a `--profile` flag (and `ATOMIC_CODE_PROFILE=1` env) to
`atomic code index` that emits incremental per-phase wall-time to stderr:
`extract`, `resolve.warm`, `resolve.match`, `resolve.synth`. Each line is flushed
as its phase completes, so a timeout still attributes time to the last completed
phase. Resolve sub-phase durations are surfaced from the pipeline via a
`ResolveProfile` value returned from the batched resolve path; default output is
unchanged when profiling is off. New body section "Index profiling (`--profile`)".

**Why:** real-repo eval found indexing times out on rw-django (44 files) and zod
(176 files). Phase timing localizes the cliff (extraction vs resolve.warm vs
resolve.match vs synth) with data before committing to a fix — the prior
"fuzzy is the cliff" diagnosis was unmeasured inference, and a pre-filter gate in
`resolveOne` (skip unless the exact lowercase name is in the warmed cache) may
make fuzzy largely inert, so measurement must precede the fix.

### 2026-06-06 — framework route extraction wired into the index pipeline

**What changed:** `Engine.ExtractFrameworkNodes(ctx)` added and called by the
index pipeline (`code index` / `code sync`) **after** generic extraction and
**before** `ResolveReferences`. It scans project files and invokes
`frameworks.Registry.ExtractAndPersist`, which runs each detected framework
resolver's `Extract` over each file — persisting `route` nodes and their
unresolved handler references. The engine now retains the `*frameworks.Registry`
(previously only its resolution view `FrameworkRegistry()` was kept, so the
extraction seam had no caller). A `[profile] frameworks: <dur> (<n> routes)` line
is emitted under `--profile`.

**Why:** real-repo eval showed `routes = 0` on every framework app (rw-express,
rw-gin, rw-django) despite all 23 framework resolvers implementing working
`Extract` methods and `MakeRouteNode`. Root cause: `Registry.ExtractAndPersist`
was a defined seam documented as "the engine facade (CP20) will call it" — but
the call site was never added, so route-node extraction was dead code. Wiring the
call lights up route nodes; resolution then links route→handler edges via the
resolvers' existing `Resolve`/`ClaimsReference`.

**Correction:** CP20/CP21 shipped the engine + CLI without invoking the
framework-extraction seam — the index pipeline ran extract → resolve, skipping
framework route extraction. The pipeline is now extract → framework-extract →
resolve.
