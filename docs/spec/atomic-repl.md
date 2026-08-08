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
- Cross-session or cross-repo variable sharing.
- TCP/remote transport — unix sockets only.
- Sandboxing — same trust level as the Bash tool.


## Success criteria


- [ ] `atomic repl start --name <s> --lang py|js [--env <file>] [--bin <path>]` spawns a
      detached harness and returns once its socket is live; a second `start` with the same
      `--name` while that session is still alive reports already-running instead of spawning a
      duplicate.
- [ ] `atomic repl eval --name <s> <code>` evaluates a code argument; with no argument, code is
      read from stdin when stdin is not a tty; neither an argument nor piped stdin present exits
      with the usage-error code.
- [ ] Multiline code — embedded newlines in the argument, or a multiline stdin pipe — evaluates
      as a single unit.
- [ ] A variable defined by one `eval` call is readable by a later `eval` call against the same
      session name, run as a separate process (interpreter state persists across Bash calls,
      not just within one).
- [ ] `eval` returns structured `{stdout, stderr, value}` — `value` is the repr/inspect of the
      final expression (REPL semantics), empty for a bare statement — plus `--json` for
      machine-readable output on every verb.
- [ ] stdout and stderr are each truncated at 64 KiB with `truncated: true` (`--json`) or an
      explicit `[truncated]` marker (human output); output at or under the cap is delivered
      whole and `truncated` is false/absent.
- [ ] `eval --timeout <duration>` (default `30s`) bounds the client-side wait; on expiry the
      client sends `SIGINT` to the harness process, escalates to `SIGKILL` after a short grace
      if still unresponsive, and removes the session's socket + meta — later calls against that
      name exit session-not-found until `start` runs again.
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

- [ ] A session with no `eval` activity for longer than its idle window self-terminates —
      the harness removes its own socket and meta files and exits 0, with no daemon or reaper
      process involved. The window resolves repo-first: `[repl] idle_timeout` in
      `.claude/atomic.toml`, else `[repl] idle_timeout` in `~/.atomic/config.toml`, else `1h`.
- [ ] `list`, `status`, `reset`, `stop` operate only on the current repo's sessions. `list` and
      `status` report a session whose socket is unreachable as dead rather than hanging or
      erroring. `reset` clears the interpreter namespace without ending the harness process.
      `stop` ends the session and removes its socket + meta.
- [ ] Sockets and meta files live under `~/.atomic/repl/<repo-key>/`, where `<repo-key>` is
      derived from the repo root, so sessions started in one repo are invisible to `list` and
      `status` run from another.
- [ ] `list` and `status` output never includes the values of any `--env`-loaded variable, in
      either human or `--json` form.
- [ ] A harness that dies without cleaning up after itself (crash, kill -9, host reboot) is
      detected by the next command that touches its socket and reported dead (exit 5) — never
      silently treated as absent, and never silently restarted.
- [ ] The harness's working directory is the repo root, so relative paths in eval'd code
      resolve against it, not against wherever `atomic repl start` happened to be invoked from.
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
│   │   ├── protocol.go                      A  wire request/response types, op + exit-code constants
│   │   ├── protocol_test.go                 A
│   │   ├── paths.go                         A  repo-key derivation, socket/meta paths under ~/.atomic/repl
│   │   ├── paths_test.go                    A
│   │   ├── meta.go                          A  session meta (pid, lang, bin, started_at) load/save
│   │   ├── meta_test.go                     A
│   │   ├── envfile.go                       A  minimal KEY=VALUE env-file parser
│   │   ├── envfile_test.go                  A
│   │   ├── spawn.go                         A  materialize + detached-spawn a harness; live/dead probe
│   │   ├── spawn_test.go                    A
│   │   ├── client.go                        A  dial, single round trip, timeout escalation
│   │   ├── client_test.go                   A
│   │   ├── action.go                        A  ReplAction verb dispatch (start/eval/list/status/reset/stop)
│   │   └── action_test.go                   A
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
    truncate at 64 KiB each; build the response
  handle_reset — clear the namespace, process stays alive
  idle watchdog — self-exit (remove socket + meta, exit 0) after the configured idle window

atomic/internal/repl/harness/node_harness.js
  main — parse --socket / --idle-timeout, bind the unix socket, run the accept loop
  handleEval — run against a persistent vm context; capture console/stdout/stderr; truncate at
    64 KiB each; build the response
  handleReset — clear the context, process stays alive
  idle watchdog — self-exit (remove socket + meta, exit 0) after the configured idle window

atomic/internal/repl/harness_embed.go
  HarnessScript — embedded script bytes for a given language

atomic/internal/repl/protocol.go
  Request — Op, Code
  Response — OK, Stdout, Stderr, Value, Error, Truncated
  Op constants — Eval, Ping, Reset, Shutdown
  ExitCode constants — Ok, Usage, NotFound, EvalException, Timeout, Dead

atomic/internal/repl/paths.go
  RepoKey — short stable hash of an absolute repo root
  RootDir — ~/.atomic/repl
  SessionDir — RootDir/<repo-key>
  SocketPath / MetaPath — SessionDir/<name>.sock / <name>.meta.json

atomic/internal/repl/meta.go
  Meta — name, lang, bin, pid, socket, started_at
    Load, Save

atomic/internal/repl/envfile.go
  ParseEnvFile — KEY=VALUE lines; # comments and blank lines skipped; single/double-quoted
    values unquoted; no variable expansion

atomic/internal/repl/spawn.go
  materializeHarness — write the embedded script for a language to a stable on-disk path
  DefaultSpawn — detached exec (Setsid, nil stdio, merged env, cwd = repo root) of the
    interpreter against the materialized script, passing --socket and --idle-timeout
  IsLive — dial probe; connection-refused/absent means dead
  EnsureStarted — flock-guarded probe-and-spawn; already-live is a no-op

atomic/internal/repl/client.go
  Client — Dial, Do (single newline-delimited-JSON round trip)
    Eval — applies the --timeout deadline; on expiry sends SIGINT to the meta pid, escalates to
      SIGKILL after a grace period, and reports the session dead

atomic/internal/repl/action.go
  ReplAction — verb dispatch
    startAction, evalAction, listAction, statusAction, resetAction, stopAction
    readCode — positional argument wins; else stdin when not a tty; else usage error
    resolveIdleTimeout — repo [repl] idle_timeout, else user [repl] idle_timeout, else 1h

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
  6 Command entries — {repl,start}, {repl,eval}, {repl,list}, {repl,status}, {repl,reset}, {repl,stop}

atomic/cmd/atomic/main.go
  buildReplCmd — cobra parent + 6 children, DisableFlagParsing per child (mirrors buildBusCmd)
  runRepl — resolve repo root (repoctx.Resolve) and home, delegate to repl.ReplAction

templates/commands/atomic-help.md
  cli topic — new row naming atomic repl and its six verbs

commands/atomic-help.md
  cli topic — rendered copy of the same row (make render)

docs/spec/atomic-doctor.md
  Category 13 row — idle_timeout validation description
  Change log — new dated entry

docs/reference/repl.md
  Overview — session lifecycle, one-line mental model
  Verbs — start / eval / list / status / reset / stop, flags and examples
  Exit codes — the six-code table
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
2. CLI resolves the repo root and repo-key, derives the session's socket + meta paths
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
2. CLI reads the harness pid from meta and sends SIGINT
3. CLI waits a short grace period for a response or process exit
4. still unresponsive -> CLI sends SIGKILL, removes the session's socket + meta
5. CLI exits 4 (timeout); the session is gone — a later call against that name exits 2 until
   `start` runs again

Flow: idle reap
1. the harness's local timer tracks time since the last eval
2. the timer exceeds the configured idle window (passed as a spawn flag, sourced from
   [repl] idle_timeout)
3. the harness removes its own socket and meta files
4. the harness exits 0
5. a later CLI call against that name finds no socket -> list omits it; eval/status/reset/stop
   exit 2 (not found)

Flow: dead-session detection
1. agent runs `atomic repl status --name s` (or list/eval/reset/stop)
2. CLI dials the socket path recorded in meta
3. connection refused (harness crashed without cleanup, or the socket file is stale after a
   host reboot) -> CLI reports the session dead, exits 5
4. CLI never auto-restarts a dead session — the message names `atomic repl start` as the next
   step
```


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Embedded harness scripts (Python + Node) + wire framing. Standalone scripts that bind a unix socket, serve newline-delimited-JSON eval/ping/reset/shutdown, truncate output at 64 KiB, and self-exit on idle. | `atomic/internal/repl/harness/`, `atomic/internal/repl/harness_embed.go`, `atomic/internal/repl/protocol.go` + tests | atomic-implementer (mode: feature) | ~7 | `go test ./internal/repl/...`: each harness test spawns its interpreter directly (skipped via `exec.LookPath` when absent), dials the socket, drives eval (single-line, multiline, exception), ping, reset, shutdown, asserts 64 KiB truncation, asserts self-exit on a short test-only idle window, and asserts two concurrent evals against one socket connection serialize (second completes only after the first's response, never errors) |
| 2 | `internal/repl` Go package: paths, meta, env-file parser, detached spawn, liveness probe, client. No CLI wiring yet. | `atomic/internal/repl/{paths,meta,envfile,spawn,client}.go` + tests | atomic-implementer (mode: feature) | ~10 | `go test ./internal/repl/...`: repo-key stable for a given root and distinct across roots; env-file parsing (comments, blank lines, quoted values, no expansion); flock-guarded concurrent `EnsureStarted` calls produce exactly one live harness; dead/stale socket reported dead, not error; client `Eval` escalates SIGINT-then-SIGKILL past the deadline against a stub harness that ignores SIGINT |
| 3 | CLI verbs: `start eval list status reset stop` through `ReplAction`; `buildReplCmd` and `runRepl`; `cliusage` entries. | `atomic/internal/repl/action.go`, `atomic/cmd/atomic/main.go`, `atomic/internal/cliusage/cliusage.go` + tests | atomic-implementer (mode: feature) | ~6 | `go test`: every verb dispatches; `--json` on all six; the six exit codes each covered by a scenario; `eval` argument-vs-stdin precedence and the usage-error case with neither; `list`/`status` output contains no `--env` value |
| 4 | `[repl] idle_timeout` config key at both scopes: `replSection` in the repo schema + user-level `[repl]` in `~/.atomic/config.toml`, repo-first resolution, doctor validation in category 13 (repo) and the config check (user). | `atomic/internal/config/repo.go`, `atomic/internal/config/config.go`, `atomic/internal/doctor/checks_repo_config.go`, `atomic/internal/doctor/checks_config.go`, `docs/spec/atomic-doctor.md` + tests | atomic-implementer (mode: feature) | ~9 | `go test`: valid duration string parses at each scope; repo value wins over user value; user value applies when repo key absent; both absent defaults to 1h with no warning; invalid duration WARNs (repo, category-13 ceiling) / is flagged (user, config check) naming the value; `repl.idle_timeout` excluded from unknown-key detection at both scopes; `docs/spec/atomic-doctor.md`'s category-13 row + change log amended to name the `idle_timeout` validation |
| 5 | Discoverability + public docs: `templates/commands/atomic-help.md` cli-topic row, `docs/reference/repl.md`, VitePress sidebar entry, README feature-table row, CLAUDE.md repl section, Documentation-surfaces row, render + bundle parity. | `templates/commands/atomic-help.md`, `commands/atomic-help.md`, `docs/reference/repl.md`, `.vitepress/config.mts`, `README.md`, `CLAUDE.md`, `CLAUDE.local.md`, `atomic/internal/embedded/` | atomic-implementer (mode: feature) | ~8 | `make render && make -C atomic bundle` leave no diff; `npm run docs:build` green with `/reference/repl` in the sidebar; grep confirms `atomic repl` is named in `templates/commands/atomic-help.md`, `README.md`, and `CLAUDE.md`; `CLAUDE.local.md`'s Documentation surfaces table carries a `docs/reference/repl.md` row; `atomic validate` clean |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `sun_path` length limit (~104 chars on macOS) | med | repo-key is a short fixed-length hash, not the repo path, so the socket path is always `~/.atomic/repl/<12-hex>/<name>.sock`; `start` fails loud with a clear error before spawning if the computed path would still exceed the platform limit, rather than truncating silently |
| Harness crashes without cleaning up, leaving an orphaned socket + meta | med | every read path (`list`, `status`, `eval`) probes by dialing rather than trusting the meta file's existence; an unreachable socket is reported dead (exit 5), never silently treated as absent or alive |
| Interpreter absent from PATH | med | `start` resolves the default bin via `exec.LookPath` before spawning; absent with no `--bin` override exits with the usage-error code before any spawn attempt, naming the missing interpreter |
| macOS SIGINT delivery to a detached (Setsid) process | low | the signal is sent directly by pid (`os.Process.Signal`), not through terminal job control, so detachment doesn't affect delivery; Node's synchronous single-threaded eval may still not yield to a signal handler mid-loop — the SIGKILL escalation after a grace period exists specifically to bound that case |
| Concurrent `start` race (two calls, same `--name`) | med | flock-guarded probe-and-spawn (mirrors the `atomic code mcp` daemon idiom): acquire the session's lock, re-probe, spawn only if still dead |
| Stale socket file surviving a host reboot | med | every probe is a live dial, not a file-existence check; connection-refused is treated as dead — recovered by `start` (remove-then-respawn) or reported dead by the read verbs |


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
