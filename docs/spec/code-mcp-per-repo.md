# Per-repo code-intel MCP: serve by explicit path, not cwd

## Goal

`atomic --repo <path> code mcp` starts and serves the correct symbol graph for `<path>` regardless of the launching cwd (including a realm root or any non-git directory), so multiple per-repo MCP servers run concurrently from one `.mcp.json`. A realm member serves its realm db; a standalone repo serves its local index. Nothing is written into a member's source tree. The daemon keeps its index fresh by syncing periodically.

## Non-goals

- Realm-mode / fan-out MCP, grouped results, a `member` tool argument. Each MCP is single-repo.
- A standalone `atomic code watch` verb or a wiki watcher. Periodic sync lives only in the MCP daemon.
- Search-tier prompt changes and sync-on-commit (separate effort).
- Windows correctness.

## Success criteria

- [ ] A test reproduces the pre-fix failure: a daemon/proxy launched with cwd at a non-git directory (realm root) fails to bind its socket. The test is RED on current code.
- [ ] After the fix, `atomic --repo <path> code mcp` launched from a non-git cwd binds its socket and completes the MCP `initialize` handshake (stdout carries a valid JSON-RPC `initialize` result; no "daemon did not start within 10s").
- [ ] The daemon resolves its db from the explicit path, not cwd: a realm member → `<realm>/.atomic/<key>.db`; a standalone repo → `<repo>/.claude/.atomic-index/atomic.db`. Asserted by a test.
- [ ] Serving a realm member via `--repo` creates no `.claude/.atomic-index/` (db, socket, or lock) inside the member's source tree. Asserted by a test.
- [ ] Two or more MCP daemons for different member repos run simultaneously with no socket/lock collision; each serves its own graph. Asserted by a test and verified live on taxgentic (server + gui + apps) launched from the realm root.
- [ ] Repo-mode MCP (cwd inside a standalone git repo, no `--repo`) still serves the local index; its socket/lock live in the local index dir (`<repo>/.claude/.atomic-index/`). The socket may be renamed (it is internal to the proxy/daemon pair) as long as both agree and the daemon connects.
- [ ] The daemon self-syncs its index on an interval (named const default 10s), bound to the daemon ctx so it stops on shutdown; `--watch-interval` overrides and `--no-watch` disables. Default asserted by a test.
- [ ] `go test ./internal/codeintel/...` green; `go vet` clean; binary builds.
- [ ] If a user-facing `code mcp` flag is added, `atomic/internal/cliusage/`, `templates/commands/atomic-help.md`, and `docs/reference/code-intel.md` reflect it; `make render` + `make -C atomic bundle` drift-clean.

## Approach

Resolve the db from the explicit path and spawn a cwd-independent daemon that serves it via `NewWithDBPath`; socket keyed to the db directory; in-daemon periodic sync — see `docs/design/code-mcp-per-repo.md`.

## Checkpoints

| # | Checkpoint | Files / areas | Agent | Est. files | Verifies |
|---|-----------|--------------|-------|-----------|---------|
| 1 | Reproduce + fix: cwd-independent daemon serving the explicit db, socket next to the db, no member pollution | `atomic/internal/codeintel/cli/code.go` (`runMCP`, `runServe`), `atomic/internal/codeintel/cli/realm.go` (`--repo`/`__serve` path resolution; the explicit-path single target must not hit the mcp/`__serve` realm reject), `atomic/internal/codeintel/mcp/proxy.go` (spawn explicit source+db, cwd-independent), `atomic/internal/codeintel/mcp/daemon.go` (`RunDaemon` takes source+db, `NewWithDBPath`; `SocketPath`/`LockPath` derive from the db dir), `atomic/cmd/atomic/main.go` (`--repo` resolves member→realm db); new test in `mcp/` or `cli/` | atomic-implementer (feature) | 5–9 | new regression test is RED before the fix, GREEN after: daemon launched with cwd at a non-git dir binds its socket and answers `initialize`; member path → realm db, standalone → local index; member source tree stays clean; repo-mode socket path unchanged |
| 2 | In-daemon periodic self-sync (10s) | `atomic/internal/codeintel/mcp/daemon.go` (poller goroutine on daemon ctx), small poller helper if needed; `cli/code.go` (`--watch-interval`/`--no-watch` flags) | atomic-implementer (feature) | 3–5 | sync fires on a clock-injected interval and stops on ctx cancel (test); named const default 10s asserted by test |
| 3 | Concurrency + surface | test for 2+ concurrent daemons different repos; `atomic/internal/cliusage/`, `templates/commands/atomic-help.md`, `docs/reference/code-intel.md` for any new mcp flag; `make render` + `make -C atomic bundle` | atomic-implementer (feature) | 4–6 | 2+ daemons different repos, no collision (test); cliusage/help/docs reflect any new flag; render+bundle drift-clean |

> Checkpoint 3 touches `templates/` + docs → requires `make render` + `make -C atomic bundle`. Checkpoints 1–2 are Go-only.
> Orchestrator runs the live taxgentic verification (server/gui/apps from the realm root) in Phase 4 — not a checkpoint.

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Socket-path change breaks existing repo-mode MCP | Medium | Repo-mode (local index) keeps its current socket path; only members move the socket to the db dir under `<realm>/.atomic/`. Test both paths. |
| Un-rejecting mcp/`__serve` re-introduces ambiguity at a realm root | Low | Only the explicit-path single-target (`--repo`, or the spawned `__serve` with explicit source+db) serves; `code mcp` from a realm-root cwd with no `--repo` has no single target and stays rejected. |
| Self-sync boots a parser pool on every change | Low | Lazy pool (already landed) only boots on a real change; gate the sync on a cheap pending-change check; a no-op tick stays sub-second. |
| Two daemons race writing the same realm db (member MCP + a future external sync) | Low | One daemon per db; SQLite WAL + busy-timeout already configured. Out of scope to coordinate external writers. |

## Change log

(empty — spec created 2026-06-22)
