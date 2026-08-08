# `atomic repl` — persistent interpreter sessions for agents


## Goal


A named, persistent Python or Node interpreter session an agent drives across separate Bash
calls: `atomic repl start` spawns it, `atomic repl eval` runs code against it with state
surviving between calls, and `atomic repl stop`/idle timeout end it.


## Non-goals


- MCP exposure — the CLI over Bash is the agent surface; revisit only on demonstrated need.
- Eval history / transcript replay.
- Jupyter kernel protocol or rich (non-text) output.
- Languages beyond Python and Node in v1.
- Windows support.
- Cross-session or cross-repo variable sharing — a realm-root session is visible to every
  member repo by scope (see Success criteria), but two independently-scoped sessions never
  exchange state.
- TCP/remote transport — unix sockets only.
- Sandboxing — same trust level as the Bash tool.


## Success criteria


- [ ] `atomic repl start --name <s> --lang py|js [--env <file>] [--bin <path>]` spawns a
      detached harness and returns once its socket is live; a second `start` with the same
      `--name` while that session is still alive reports already-running instead of spawning a
      duplicate.
- [ ] `--lang` also accepts `python`/`js`/`node`/`javascript` as aliases for `py`/`js`,
      resolving to the same interpreter; `py`/`js` stay the canonical forms shown in `--help`.
      `start` with no usable interpreter — `exec.LookPath` fails and no `--bin` override is
      given, or an explicit `--bin` does not resolve — exits with the interpreter-unavailable
      code, naming the missing binary, before any spawn attempt.
- [ ] `atomic repl eval --name <s> <code>` evaluates a code argument; with no argument, code is
      read from stdin when stdin is not a tty; neither an argument nor piped stdin present exits
      with the usage-error code. A `--` separator before the code positional
      (`eval --name s -- '<code>'`) disambiguates code that itself starts with a dash (e.g. a
      valid JS `--i` prefix expression); code that starts with a dash and omits `--` is safest
      passed via stdin instead.
- [ ] Multiline code — embedded newlines in the argument, or a multiline stdin pipe — evaluates
      as a single unit.
- [ ] A variable defined by one `eval` call is readable by a later `eval` call against the same
      session name, run as a separate process (interpreter state persists across Bash calls,
      not just within one).
- [ ] `eval` returns structured `{stdout, stderr, value}` — `value` is the repr/inspect of the
      final expression (REPL semantics), empty for a bare statement — plus `--json` for
      machine-readable output on every verb.
- [ ] The wire response shape is pinned: every field is always present (never omitted); `value`
      and `error` are empty strings when not applicable, never null or absent. On an eval
      exception `error` carries the full traceback/stack trace including the failing line, and
      any stdout/stderr the code produced before the failure is still delivered, not discarded.
- [ ] Every wire message carries a protocol version. A harness spawned by an older binary that
      outlives an `atomic update` (version mismatch between the running harness and the current
      client) fails loud — the client reports the mismatch and names `repl stop` then `start` as
      the fix — and never attempts to parse a response against a version it doesn't recognize.
- [ ] stdout and stderr are each truncated at 64 KiB with `truncated: true` (`--json`) or an
      explicit `[truncated]` marker (human output); output at or under the cap is delivered
      whole and `truncated` is false/absent.
- [ ] `eval --timeout <duration>` (default `30s`) bounds the client-side wait; on expiry the
      client verifies the harness pid recorded in meta still belongs to this session (a
      `started_at` cross-check, so a recycled pid is never signaled), sends `SIGINT`, escalates
      to `SIGKILL` after a short grace if still unresponsive, and removes the session's socket +
      meta — later calls against that name exit session-not-found until `start` runs again.
- [ ] Exit codes are fixed, distinct, and documented — callers route on the code, never on
      parsed prose:

  | Code | Meaning |
  |---|---|
  | 0 | ok |
  | 1 | usage error |
  | 2 | session not found |
  | 3 | eval exception (the evaluated code raised/threw) |
  | 4 | timeout (SIGINT-then-SIGKILL escalation exhausted) |
  | 5 | session dead (socket unreachable) |
  | 6 | interpreter/environment unavailable (`--lang`'s interpreter not on PATH, or `--bin`
      does not resolve) |

  `list` is the one exception: it always exits 0, since it enumerates rather than targets a
  single session — a dead entry is reported inline as dead, not surfaced as a command failure.
- [ ] A session with no `eval` activity for longer than its idle window self-terminates —
      the harness removes its own socket and meta files and exits 0, with no daemon or reaper
      process involved. The window resolves repo-first: `[repl] idle_timeout` in
      `.claude/atomic.toml`, else `[repl] idle_timeout` in `~/.atomic/config.toml`, else `1h`.
      A reaped session's name and a name that was never `start`ed produce the identical
      not-found message and exit code — no marker distinguishes "used to exist" from
      "never existed", so an agent always gets the same "run `atomic repl start`" remedy.
- [ ] A session keys to the scope root where `start` ran — a repo root or, when invoked
      directly at one, a realm root. `eval`, `list`, `status`, `reset`, `stop` resolve the
      union of the calling repo's own sessions and its enclosing realm's, so a session started
      at a realm root is reachable from any member repo; a session started inside one member
      repo stays invisible from a sibling member. `reset` clears the interpreter namespace
      without ending the harness process; `stop` ends the session and removes its socket +
      meta.
- [ ] `list` and `status` accept `--all`: enumerate every session across every scope on the
      machine — name, origin root path, lang, pid, liveness — so a session can be found and its
      pid identified without knowing which repo or realm produced it. Without `--all`, both
      verbs are scoped to the current repo-plus-enclosing-realm union above.
- [ ] Sockets and meta files live under `~/.atomic/repl/<scope-key>/`, where `<scope-key>` is
      derived from the resolved scope root (repo or realm), keeping each scope's sessions on
      disk under a distinct, fixed-length directory name.
- [ ] `list` and `status` output never includes the values of any `--env`-loaded variable, in
      either human or `--json` form.
- [ ] A harness that dies without cleaning up after itself (crash, kill -9, host reboot) is
      detected by the next command that touches its socket (other than `list`, which reports
      per-entry liveness without failing) and reported dead (exit 5) — never silently treated
      as absent, and never silently restarted.
- [ ] Session directories are created `0o700`; socket and meta files are created `0o600` — a
      session socket is code execution into a process that may hold `--env` secrets, so it is
      never left at the house `0o755`/`0o644` default.
- [ ] A session `--name` is validated as a path component: empty, a leading dash, a path
      separator, or `.`/`..` are all rejected before any file is touched.
- [ ] The harness's working directory is the scope root it keys to, so relative paths in
      eval'd code resolve against it, not against wherever `atomic repl start` happened to be
      invoked from.
- [ ] Concurrent `eval` calls against one session serialize — a second call blocks until the
      harness's single-threaded accept loop reaches it or its own `--timeout` deadline expires;
      it never errors solely because another eval is in flight.


## Approach


Per-session self-serving harness — a detached `python3`/`node` child running an embedded
harness script serves its own unix socket; the Go CLI is a stateless spawner/client (design
option D) — see `docs/design/atomic-repl.md`.


## Change tree


```
atomic/
├── internal/
│   ├── repl/                                A  new package
│   │   ├── harness/
│   │   │   ├── python_harness.py            A  embedded Python harness script
│   │   │   └── node_harness.js              A  embedded Node harness script
│   │   ├── harness_embed.go                 A  go:embed harness/*
│   │   ├── harness_embed_test.go            A
│   │   ├── harness_python_test.go           A  standalone: spawn python3 directly, drive the wire protocol
│   │   ├── harness_node_test.go             A  standalone: spawn node directly, drive the wire protocol
│   │   ├── protocol.go                      A  wire request/response types + version field, op + exit-code constants
│   │   ├── protocol_test.go                 A
│   │   ├── paths.go                         A  scope-key derivation, socket/meta paths, session-name validation
│   │   ├── paths_test.go                    A
│   │   ├── meta.go                          A  session meta (pid, lang, bin, started_at, root) load/save
│   │   ├── meta_test.go                     A
│   │   ├── envfile.go                       A  minimal KEY=VALUE env-file parser
│   │   ├── envfile_test.go                  A
│   │   ├── spawn.go                         A  materialize + detached-spawn a harness; live/dead probe
│   │   ├── spawn_test.go                    A
│   │   ├── client.go                        A  dial, single round trip, timeout escalation
│   │   ├── client_test.go                   A
│   │   ├── action.go                        A  ReplAction verb dispatch (start/eval/list/status/reset/stop)
│   │   ├── action_test.go                   A
│   │   └── binary_e2e_test.go               A  real-binary, separate-process, sandboxed-HOME e2e
│   ├── config/
│   │   ├── repo.go                          M  replSection + repoKnownSections/repoKnownLeaves entries
│   │   ├── repo_test.go                     M
│   │   ├── config.go                        M  user-level [repl] idle_timeout: Config field + knownKeys entry
│   │   └── config_test.go                   M
│   ├── doctor/
│   │   ├── checks_repo_config.go            M  idle_timeout validation folded into category 13
│   │   ├── checks_repo_config_test.go       M
│   │   ├── checks_config.go                 M  user-config idle_timeout validation
│   │   └── checks_config_test.go            M
│   └── cliusage/
│       └── cliusage.go                      M  6 {repl, <verb>} entries
├── cmd/atomic/
│   ├── main.go                              M  buildReplCmd + runRepl dispatch
│   └── main_test.go                         M  dispatch coverage; verb-count assertion bumped
└── internal/embedded/                       M  regenerated bundle (make bundle)
templates/commands/atomic-help.md            M  cli topic row
commands/atomic-help.md                      M  rendered (make render)
docs/reference/repl.md                       A  verb reference
docs/spec/atomic-doctor.md                   M  category-13 row + change log: idle_timeout validation
.vitepress/config.mts                        M  reference-sidebar entry (docs site; outside render/bundle)
README.md                                    M  feature-table row
CLAUDE.md                                    M  repl section (mirrors the bus section pattern)
CLAUDE.local.md                              M  Documentation surfaces row (repo-local, gitignored)
```


## Outline


```
atomic/internal/repl/harness/python_harness.py
  main — parse --socket / --idle-timeout, bind the unix socket, run the accept loop
  handle_eval — exec/eval against a persistent module-level namespace; capture stdout/stderr;
    sanitize to valid UTF-8 (invalid bytes replaced); truncate at 64 KiB each; build the response
  handle_reset — clear the namespace, process stays alive
  idle watchdog — monotonic-clock timer; self-exit (remove socket + meta, exit 0) after the
    configured idle window

atomic/internal/repl/harness/node_harness.js
  main — parse --socket / --idle-timeout, bind the unix socket, run the accept loop
  handleEval — run against a persistent vm context; capture console/stdout/stderr; sanitize to
    valid UTF-8 (invalid bytes replaced); truncate at 64 KiB each; build the response
  handleReset — clear the context, process stays alive
  idle watchdog — monotonic-clock timer; self-exit (remove socket + meta, exit 0) after the
    configured idle window

atomic/internal/repl/harness_embed.go
  HarnessScript — embedded script bytes for a given language

atomic/internal/repl/protocol.go
  Request — V, Op, Code
  Response — V, OK, Stdout, Stderr, Value, Error, Truncated — all fields always present;
    Value/Error empty strings when not applicable
  ProtocolVersion — constant gating the handshake; a mismatch fails loud rather than misparsing
  Op constants — Eval, Ping, Reset, Shutdown
  ExitCode constants — Ok, Usage, NotFound, EvalException, Timeout, Dead, InterpreterUnavailable

atomic/internal/repl/paths.go
  ScopeKey — short stable hash of an absolute scope root (repo or realm)
  RootDir — ~/.atomic/repl — takes home as an explicit parameter, never os.UserHomeDir internally
  SessionDir — RootDir/<scope-key>
  SocketPath / MetaPath — SessionDir/<name>.sock / <name>.meta.json
  AllSessionDirs — every <scope-key> subdirectory under RootDir, for --all enumeration
  ValidateName — reject empty, a leading dash, a path separator, or "."/".." (mirrors
    atomic/internal/wiki/bucket.go's validateBucketName — a session name is a path component)

atomic/internal/repl/meta.go
  Meta — name, lang, bin, pid, socket, started_at, root (origin scope root, for --all)
    Load, Save

atomic/internal/repl/envfile.go
  ParseEnvFile — KEY=VALUE lines; # comments and blank lines skipped; single/double-quoted
    values unquoted; no variable expansion

atomic/internal/repl/spawn.go
  materializeHarness — write the embedded script for a language into a 0o700 dir via
    temp-file-then-atomic-rename; always rewritten from the embedded bytes before every spawn,
    never trusted stale
  DefaultSpawn — detached exec (Setsid, nil stdio, merged env, cwd = scope root) of the
    interpreter against the materialized script, passing --socket and --idle-timeout
  SpawnFunc — injectable spawn seam (mirrors codeintel/mcp/proxy.go's SpawnFunc), so tests
    substitute an in-process stub instead of a real interpreter
  IsLive — dial probe; connection-refused/absent means dead
  EnsureStarted — flock-guarded probe-and-spawn against an injected SpawnFunc; already-live is
    a no-op

atomic/internal/repl/client.go
  Client — Dial, Do (single newline-delimited-JSON round trip)
    Eval — applies the --timeout deadline; on expiry cross-checks the meta pid's started_at
      against the running process before signaling (never signals a recycled pid), sends
      SIGINT, escalates to SIGKILL after an injectable grace period, and reports the session
      dead

atomic/internal/repl/action.go
  ReplAction — verb dispatch
    startAction, evalAction, listAction, statusAction, resetAction, stopAction
    readCode — `--` separator, then positional argument wins; else stdin when not a tty; else
      usage error
    resolveScopeRoots — the calling repo's own scope root plus the enclosing realm's, when the
      repo is a realm member
    resolveIdleTimeout — repo [repl] idle_timeout, else user [repl] idle_timeout, else 1h

atomic/internal/repl/binary_e2e_test.go
  end-to-end — builds the real atomic binary, runs it under a sandboxed temp HOME, drives
    start -> eval -> eval -> stop as separate OS-process invocations (not in-process calls),
    asserting cross-process state persistence, exit codes, and --json shape

atomic/internal/config/repo.go
  replSection — idle_timeout leaf
  RepoConfig.Repl — new field
  repoKnownSections / repoKnownLeaves — "repl" / "repl.idle_timeout" entries

atomic/internal/config/config.go
  Config.Repl — user-level [repl] idle_timeout default
  knownKeys — "repl.idle_timeout" entry

atomic/internal/doctor/checks_repo_config.go
  RunCheckRepoConfigWith — idle_timeout parse validation folded into the existing warn/pass path

atomic/internal/doctor/checks_config.go
  config check — user-level repl.idle_timeout duration validation

atomic/internal/cliusage/cliusage.go
  6 Command entries — {repl,start}, {repl,eval}, {repl,list}, {repl,status}, {repl,reset}, {repl,stop};
    list/status carry --all, eval documents the -- separator, start documents --lang aliases

atomic/cmd/atomic/main.go
  buildReplCmd — cobra parent + 6 children, DisableFlagParsing per child (mirrors buildBusCmd)
  runRepl — resolve repo root (repoctx.Resolve) and enclosing realm root (where.Resolve) plus
    home, delegate to repl.ReplAction

templates/commands/atomic-help.md
  cli topic — new row naming atomic repl and its six verbs

commands/atomic-help.md
  cli topic — rendered copy of the same row (make render)

docs/spec/atomic-doctor.md
  Category 13 row — idle_timeout validation description
  Change log — new dated entry

docs/reference/repl.md
  Overview — session lifecycle, one-line mental model
  Scope — repo-vs-realm session identity, the current-scope union, --all
  Verbs — start / eval / list / status / reset / stop, flags and examples
  Exit codes — the seven-code table
  Config — [repl] idle_timeout

.vitepress/config.mts
  Reference sidebar — { text, link } entry for /reference/repl

README.md
  Feature table — one row

CLAUDE.md
  Repl section — what it is, the six verbs, the idle_timeout config key (mirrors the
    Inter-session messaging section's altitude)

CLAUDE.local.md
  Documentation surfaces table — docs/reference/repl.md row, atomic-writing voice
```


## Flows


```
Flow: start (spawn + probe)
1. agent runs `atomic repl start --name s --lang py --env .env [--bin path]`
2. CLI resolves the scope root — the repo root, or the realm root itself when invoked directly
   at one — and derives the scope-key and the session's socket + meta paths
3. CLI dials the socket; live -> print already-running, exit 0
4. dead or absent -> acquire the session's flock, re-probe (a race loser also reports
   already-running), remove any stale socket file
5. CLI parses --env (if present) and merges it into the spawn environment
6. CLI materializes the embedded harness script for --lang and spawns it detached (Setsid, nil
   stdio) with --socket and --idle-timeout (resolved repo config -> user config -> 1h default)
7. CLI polls the socket with bounded backoff until it accepts, writes meta (pid, lang, bin,
   started_at), exits 0

Flow: eval happy path
1. agent runs `atomic repl eval --name s <code>` (or pipes code on stdin)
2. CLI resolves the socket path and dials it; absent/refused -> exit 2 (session not found)
3. CLI sends {op: eval, code} bounded by the --timeout deadline
4. harness executes code against its persistent namespace, capturing stdout/stderr (truncated
   at 64 KiB each) and the final expression's value
5. harness replies {ok: true, stdout, stderr, value, truncated} and resets its idle timer
6. CLI prints the structured result (human or --json); exit 0 on {ok: true}, exit 3 on
   {ok: false, error} (eval exception)

Flow: eval timeout escalation
1. the --timeout deadline (default 30s) elapses with no response
2. CLI reads the harness pid and started_at from meta; the recorded pid no longer matches that
   started_at (a recycled pid) -> treat as dead without signaling, skip to step 5
3. pid still valid -> CLI sends SIGINT, then waits a short grace period for a response or
   process exit
4. still unresponsive -> CLI sends SIGKILL, removes the session's socket + meta
5. CLI exits 4 (timeout); the session is gone — a later call against that name exits 2 with the
   same not-found message a never-started name gets, until `start` runs again

Flow: idle reap
1. the harness's local, monotonic-clock timer tracks time since the last eval
2. the timer exceeds the configured idle window (passed as a spawn flag, sourced from
   [repl] idle_timeout)
3. the harness removes its own socket and meta files
4. the harness exits 0
5. a later CLI call against that name finds no socket -> `list` simply omits it (still exits 0);
   `eval`/`status`/`reset`/`stop` exit 2 (not found) with the identical message a never-started
   name would get

Flow: dead-session detection
1. agent runs `atomic repl status --name s` (or eval/reset/stop) — `list` is excluded: it
   reports a dead entry inline and still exits 0, since it enumerates rather than targets one
   session
2. CLI dials the socket path recorded in meta
3. connection refused (harness crashed without cleanup, or the socket file is stale after a
   host reboot) -> CLI reports the session dead, exits 5
4. CLI never auto-restarts a dead session — the message names `atomic repl start` as the next
   step
```


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Embedded harness scripts (Python + Node) + wire framing. Standalone scripts that bind a unix socket, serve newline-delimited-JSON eval/ping/reset/shutdown, truncate output at 64 KiB, sanitize to UTF-8, and self-exit on a monotonic idle timer. | `atomic/internal/repl/harness/`, `atomic/internal/repl/harness_embed.go`, `atomic/internal/repl/protocol.go` + tests | atomic-implementer (mode: feature) | ~7 | `go test ./internal/repl/...`: each harness test spawns its interpreter directly (skipped via `exec.LookPath` when absent), dials the socket, drives eval (single-line, multiline, exception), ping, reset, shutdown, asserts 64 KiB truncation, asserts self-exit on a short test-only idle window, and asserts two evals sent over two separate connections against the one listening socket serialize (second completes only after the first's response, never errors). A cross-language contract test decodes each harness's live JSON into the canonical Go `Response` struct with unknown-field rejection, so the two hand-written emitters cannot drift. `protocol_test.go` carries one table-driven test locking all seven `ExitCode` literal values in one place. |
| 2 | `internal/repl` Go package: paths, meta, env-file parser, detached spawn, liveness probe, client. No CLI wiring yet. | `atomic/internal/repl/{paths,meta,envfile,spawn,client}.go` + tests | atomic-implementer (mode: feature) | ~10 | `go test ./internal/repl/...`: scope-key stable for a given root and distinct across roots; path resolution takes home as an explicit parameter, exercised against a temp home; env-file parsing (comments, blank lines, quoted values, no expansion); session-name validation rejects empty/leading-dash/path-separator/`.`/`..`; session dirs created `0o700` and sockets+meta `0o600`, asserted directly; `EnsureStarted` takes an injectable `SpawnFunc` and the concurrency test drives it with an in-process stub (no real interpreter), asserting a concurrent race produces exactly one live session; dead/stale socket reported dead, not error; client `Eval` escalates SIGINT-then-SIGKILL past an injectable grace period (no real wall-clock wait) against a stub harness that ignores SIGINT, and skips signaling entirely when the meta pid's started_at no longer matches (recycled-pid guard) |
| 3 | CLI verbs: `start eval list status reset stop` through `ReplAction`; `buildReplCmd` and `runRepl`; `cliusage` entries. | `atomic/internal/repl/action.go`, `atomic/cmd/atomic/main.go`, `atomic/internal/cliusage/cliusage.go` + tests | atomic-implementer (mode: feature) | ~6 | `go test`: every verb dispatches; `--json` on all six; the seven exit codes each covered by a scenario (including 6 for a missing interpreter/invalid `--bin`); `list` exits 0 even with a dead entry in its output; `eval` argument-vs-stdin precedence, the `--` separator disambiguating dash-leading code, and the usage-error case with neither; `--lang` alias resolution (`python`/`js`/`node`/`javascript`); a session started at a realm root is visible to `list`/`eval` run from a member repo, while a session started inside one member stays invisible from a sibling; `list --all` enumerates across scopes with root/lang/pid/liveness; `list`/`status` output contains no `--env` value |
| 4 | `[repl] idle_timeout` config key at both scopes: `replSection` in the repo schema + user-level `[repl]` in `~/.atomic/config.toml`, repo-first resolution, doctor validation in category 13 (repo) and the config check (user). | `atomic/internal/config/repo.go`, `atomic/internal/config/config.go`, `atomic/internal/doctor/checks_repo_config.go`, `atomic/internal/doctor/checks_config.go`, `docs/spec/atomic-doctor.md` + tests | atomic-implementer (mode: feature) | ~9 | `go test`: valid duration string parses at each scope; repo value wins over user value; user value applies when repo key absent; both absent defaults to 1h with no warning; invalid duration WARNs (repo, category-13 ceiling) / is flagged (user, config check) naming the value; `repl.idle_timeout` excluded from unknown-key detection at both scopes; `docs/spec/atomic-doctor.md`'s category-13 row + change log amended to name the `idle_timeout` validation |
| 5 | Discoverability + public docs: `templates/commands/atomic-help.md` cli-topic row, `docs/reference/repl.md`, VitePress sidebar entry, README feature-table row, CLAUDE.md repl section, Documentation-surfaces row, render + bundle parity. | `templates/commands/atomic-help.md`, `commands/atomic-help.md`, `docs/reference/repl.md`, `.vitepress/config.mts`, `README.md`, `CLAUDE.md`, `CLAUDE.local.md`, `atomic/internal/embedded/` | atomic-implementer (mode: feature) | ~8 | `make render && make -C atomic bundle` leave no diff; `npm run docs:build` green with `/reference/repl` in the sidebar; grep confirms `atomic repl` is named in `templates/commands/atomic-help.md`, `README.md`, and `CLAUDE.md`; `CLAUDE.local.md`'s Documentation surfaces table carries a `docs/reference/repl.md` row; `atomic validate` clean |
| 6 | Real-binary end-to-end. Builds the actual `atomic` binary and drives it as separate OS-process invocations under a sandboxed temp `HOME`, closing the gap an in-process suite can't see (this repo's own recorded lesson: a green in-process suite has hidden real CLI bugs before). | `atomic/internal/repl/binary_e2e_test.go` | atomic-implementer (mode: surgical) | 1 | `go test ./internal/repl/...`: `start` → `eval` → `eval` → `stop` run as four separate `exec.Command` invocations of the built binary; a variable set in the first `eval` is readable in the second, proving cross-process persistence; exit codes 0 (ok), 2 (not found, post-`stop`), and 3 (eval exception) are each hit by a real invocation; `--json` output on `eval` and `list` matches the documented shape; skipped via `exec.LookPath("python3")` when the interpreter is absent |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `sun_path` length limit (~104 chars on macOS) | med | scope-key is a short fixed-length hash, not the scope-root path, so the socket path is always `~/.atomic/repl/<12-hex>/<name>.sock`; `start` fails loud with a clear error before spawning if the computed path would still exceed the platform limit, rather than truncating silently |
| Harness crashes without cleaning up, leaving an orphaned socket + meta | med | every read path (`list`, `status`, `eval`) probes by dialing rather than trusting the meta file's existence; an unreachable socket is reported dead (exit 5), never silently treated as absent or alive |
| Interpreter absent from PATH | med | `start` resolves the default bin via `exec.LookPath` before spawning; absent with no `--bin` override (or an explicit `--bin` that doesn't resolve) exits with the dedicated interpreter-unavailable code before any spawn attempt, naming the missing binary — distinct from a usage mistake, so an agent can tell "install/point `--bin`" apart from "I wrote the command wrong" |
| macOS SIGINT delivery to a detached (Setsid) process | low | the signal is sent directly by pid (`os.Process.Signal`), not through terminal job control, so detachment doesn't affect delivery; Node's synchronous single-threaded eval may still not yield to a signal handler mid-loop — the SIGKILL escalation after a grace period exists specifically to bound that case |
| Concurrent `start` race (two calls, same `--name`) | med | flock-guarded probe-and-spawn (mirrors the `atomic code mcp` daemon idiom): acquire the session's lock, re-probe, spawn only if still dead |
| Stale socket file surviving a host reboot | med | every probe is a live dial, not a file-existence check; connection-refused is treated as dead — recovered by `start` (remove-then-respawn) or reported dead by the read verbs |
| A harness stuck in a blocking eval (hot loop, C-extension call) never returns to service its own idle timer or a timeout escalation | low | accepted limitation — no independent reaper; a watchdog daemon was deliberately rejected (design Approach A). Mitigation is `list --all` exposing the pid so a human can `kill` it by hand |
| `~/.atomic/repl/` accumulates scope-key directories for repos or worktrees that no longer exist | low | no auto-prune in v1; `list --all` shows each entry's origin root path, so debris is identifiable at a glance; removal is manual (`rm -rf`) |
| `--env` secrets are visible to any same-user process via `ps eww`/`/proc/<pid>/environ` for the harness's lifetime | low | accepted limitation, consistent with the same-trust-as-Bash non-goal; the CLI's own `list`/`status` output still never echoes the values |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->

### 2026-08-08 — user-level idle_timeout fallback

**What changed:** The idle window now resolves repo-first with a user-level fallback: `[repl] idle_timeout` in `.claude/atomic.toml`, else `[repl] idle_timeout` in `~/.atomic/config.toml`, else `1h`. CP4 widened to cover the user-config schema (`config.go` knownKeys + Config field), the doctor config check, and repo-over-user precedence tests; its mode moved surgical → feature (~9 files).

**Why:** Owner decision resolving the design's open question — a per-user default avoids repeating the key in every repo's `atomic.toml`.

**Superseded:** Idle window sourced from the repo `.claude/atomic.toml` key only, defaulting to `1h`.

### 2026-08-08 — public documentation surfaces added to CP5

**What changed:** CP5 now also covers the VitePress reference-sidebar entry (`.vitepress/config.mts` — outside the render/bundle pipeline, verified via `npm run docs:build`), a `docs/reference/repl.md` row in `CLAUDE.local.md`'s Documentation surfaces table so `/documentation` maintenance tracks the page, and a full CLAUDE.md repl section in place of a one-line callout (mirrors the Inter-session messaging section's altitude).

**Why:** Owner review — every existing reference page (code-intel, serve, bus) has a sidebar entry and a surfaces-table row; without them the new page is invisible on the docs site and untracked by doc maintenance.

**Superseded:** CP5 scoped public docs to `docs/reference/repl.md` + README row + a one-line CLAUDE.md callout.

### 2026-08-08 — challenge-swarm fold + scope decisions

**What changed:** Folding in challenge-swarm findings and owner decisions across seven areas:

- **Scope identity (owner decision).** Sessions now key to the resolved scope root — a repo
  root or a realm root — not a bare repo root. `eval`/`list`/`status`/`reset`/`stop` resolve
  the union of the calling repo's own sessions and its enclosing realm's, so a realm-root
  session is reachable from any member repo. `<repo-key>` is renamed `<scope-key>` throughout.
  `list`/`status` gain `--all` for machine-wide enumeration (name, origin root, lang, pid,
  liveness); meta gains a `root` field. "Harness cwd = repo root" becomes "cwd = scope root".
- **Exit codes.** A seventh code, 6 (interpreter/environment unavailable), replaces the
  usage-error routing for a missing interpreter or unresolvable `--bin`. `list` always exits 0
  — per-entry liveness in its output, never a command failure — and is dropped from the
  dead-session-detection flow. A reaped session and a never-started name now produce the
  identical not-found message and exit code, by design (no distinguishing marker). CP1 gains a
  table-driven test pinning all seven literal values in one place.
- **Wire protocol.** Request/Response gain a version field (`V`) and a `ProtocolVersion`
  constant; a mismatch (harness spawned by an older binary, outliving an `atomic update`) fails
  loud naming `repl stop` + `start`, never silently misparses. Every response field is always
  present; `value`/`error` are empty strings when absent; an eval exception's `error` carries
  the full traceback including the failing line, with any pre-failure stdout/stderr still
  delivered. Harness output is sanitized to valid UTF-8 before encoding; the idle watchdog runs
  on a monotonic clock. CP1 gains a cross-language contract test (each harness's live JSON
  decoded into the canonical Go `Response` with unknown-field rejection) and its concurrency
  test now explicitly dials two separate connections against one socket.
- **Spawn, files, security.** `materializeHarness` always rewrites the harness script from
  embedded bytes via temp-file-then-atomic-rename before every spawn — never trusts a stale
  copy — into a `0o700` parent dir; session dirs are `0o700` and sockets/meta `0o600`, asserted
  by a CP2 test. Session `--name` is validated as a path component (empty, leading dash, path
  separators, `.`/`..` all rejected). The timeout escalation cross-checks the meta pid's
  `started_at` before signaling, so a recycled pid is never signaled. `EnsureStarted` takes an
  injectable spawn function (mirrors `codeintel/mcp/proxy.go`'s `SpawnFunc`) so CP2's
  concurrency test uses an in-process stub, not a real interpreter; path resolution takes home
  as an explicit parameter (statemigrate.go convention); the SIGINT→SIGKILL grace is injectable
  so no test waits on real wall-clock time.
- **CLI ergonomics.** `--lang` accepts `python`/`js`/`node`/`javascript` as aliases for the
  canonical `py`/`js`. `eval` supports a `--` separator before the code positional so
  dash-leading code (e.g. valid JS `--i`) is unambiguous, with stdin recommended as the safer
  default for such code.
- **New CP6.** A real-binary end-to-end test (`atomic/internal/repl/binary_e2e_test.go`) builds
  the actual binary and drives `start → eval → eval → stop` as separate OS-process invocations
  under a sandboxed temp `HOME`, asserting cross-process state persistence, exit codes, and
  `--json` shape — closing the gap an in-process suite structurally cannot see.
- **Risks.** Two rows added: a harness stuck in a blocking eval has no independent reaper
  (accepted limitation, `list --all` is the manual-kill mitigation); `~/.atomic/repl/`
  accumulates scope-key directories for repos/worktrees that no longer exist (no auto-prune in
  v1, `list --all` makes debris identifiable).

**Why:** Challenge-swarm review plus a critical tester finding (in-process-only coverage has
hidden real CLI bugs in this repo before — see `verify-cli-against-real-binary` in agent
memory) surfaced gaps between the design's mechanism decisions and what the spec actually
locked down: cross-repo session reuse, protocol-version safety, file-permission and pid-reuse
hardening, and end-to-end proof against the real binary.

**Superseded:** Sessions were repo-scoped only (`<repo-key>`, invisible across repos, no
`--all`). Six exit codes, with a missing interpreter routed to the usage-error code and no
distinction between a reaped and a never-started session name. No protocol version field, no
pinned empty-string/traceback encoding contract, no UTF-8 sanitization or monotonic-clock
mention. No file-mode or session-name-validation guarantees, no pid-recycle guard, no
injectable spawn/grace/home test seams called out. `--lang` had no aliases; `eval` had no `--`
separator. No CP6; no stuck-harness or state-directory-debris risk rows.
