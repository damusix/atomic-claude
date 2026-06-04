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

- [ ] The facade method set matches appendix M; both the MCP server and the
      `atomic code` CLI compile against it.
- [ ] `atomic code <verb>` subcommands exist for index/sync/status/search/
      callers/callees/impact/node/files/affected/explore/mcp — each query verb
      with a `--json` mode — dispatched from `runCode`; `case "code"` wired into
      the top-level switch.
- [ ] MCP `initialize` returns the de-branded server-instructions text; the
      `atomic_code_node` tool returns **all overloads in one call** on an
      ambiguous name; the explore tool respects the exact budget tiers, the 25k
      ceiling, and the section-boundary cut.
- [ ] `atomic setup` adds the `.gitignore` entry; the binary subcommand is
      registered in `CLAUDE.md` + `/atomic-help` + `docs/reference/`.
- [ ] The MCP server is a per-project **singleton**: a tool call against a dead or
      absent `mcp.sock` auto-starts it (flock-guarded — concurrent starts don't
      double-spawn); a second client reuses the warm engine.
- [ ] A connection idle >30 min (no request; `updated_at` stale) is force-closed
      by the reaper; the server exits and **removes the socket** after 0
      connections persist for 30 min, so the next call cleanly restarts it.
- [ ] Every umbrella success criterion has an automated check in the harness.

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
