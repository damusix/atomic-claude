# atomic repl


## Problem


Agents have no way to keep interpreter state alive across Bash calls. Claude Code's Bash tool resets shell and interpreter state on every invocation (only the working directory persists), and NotebookEdit edits `.ipynb` JSON without a live kernel. Load-once-query-many work — exploring a large dataset, probing an API iteratively, building up a computation — forces either re-running the full setup on every step or hand-serializing state to disk between calls.

`atomic repl` gives an agent a named, persistent interpreter session: define a variable in one Bash call, use it in a later one.

Session lifecycle, from prepare to idle self-termination:

```mermaid
sequenceDiagram
    participant A as agent (Bash)
    participant CLI as atomic repl (Go, stateless)
    participant H as harness (python3/node child)

    A->>CLI: repl start --name s --lang py --env .env
    CLI->>H: spawn detached (Setsid, env merged, idle-timeout flag)
    H->>H: bind ~/.atomic/repl/<scope-key>/s.sock
    A->>CLI: repl eval --name s 'df = load(...)'
    CLI->>H: {op: eval, code} over socket
    H-->>CLI: {ok, stdout, stderr, value}
    A->>CLI: repl eval --name s 'df.head()'
    H-->>CLI: state still in memory
    Note over H: idle > timeout → remove socket+meta, exit 0
```


## Goals / Non-goals


- Goals:
  - Named sessions with prepared environment: `repl start --name <s> --lang py|js --env <file>` (`--bin` override for venv/alternate interpreters).
  - Multiline code via argument and via stdin pipe.
  - Interpreter state persists across `eval` calls until `stop`, `reset`, crash, or idle reap.
  - Idle reaping: a session terminates itself after a configurable idle window (default 1h; `[repl] idle_timeout` in `.claude/atomic.toml`).
  - Deterministic exit codes so agents route on codes, not prose.
  - Bounded, structured output: `{stdout, stderr, value}`, truncation flagged explicitly; `--json` for machine output.
  - Session introspection and control: `list`, `status`, `reset`, `stop`.
- Non-goals:
  - MCP exposure — the CLI over Bash is the agent surface; revisit only on demonstrated need.
  - Eval history / transcript replay.
  - Jupyter kernel protocol or rich (non-text) output.
  - Languages beyond Python and Node in v1.
  - Windows support.
  - Cross-session or cross-repo variable sharing.
  - TCP/remote transport — unix sockets only.
  - Sandboxing — same trust level as the Bash tool.


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Central per-user repl daemon (bus pattern): one Go daemon owns every session as a child interpreter | Single process to operate; mirrors `atomic bus` | Custom Go-side session registry, reaper loop, restart semantics; daemon crash kills all sessions; largest new surface |
| B | Embedded goja (pure-Go JS interpreter inside the binary) | Zero subprocess management; no runtime dependency | JS-only (Python is the primary ask); no npm ecosystem; interpreter fidelity gaps |
| C | Jupyter kernel protocol client (spawn ipykernel, speak ZMQ) | Interrupt + rich-output semantics already solved | Heavy deps (ZMQ), requires a Jupyter install, overkill for a text REPL |
| D | Per-session self-serving harness: `repl start` spawns a detached `python3`/`node` running an embedded harness script that serves its own unix socket; the Go CLI is a stateless spawner/client; the harness self-exits after the idle window | Fewest moving parts; per-session crash isolation; idle reaping is a harness-local timer — no daemon, no reaper loop; reuses the proven probe-and-spawn idiom | Harness logic written twice (~100 lines per language); interrupt is SIGINT-to-pid via a meta file rather than an in-band protocol |


## Recommendation


**D — per-session self-serving harness.** Evidence:

- Detached spawn + flock-guarded probe-and-spawn already exists and is battle-tested: `atomic/internal/codeintel/mcp/proxy.go:52-141` (`Setsid`, nil stdio, `Start` without `Wait`, stale-socket recovery, spawn race guarded by flock).
- Newline-delimited JSON over a unix socket is the established wire idiom (`docs/spec/atomic-bus.md:127`).
- `go:embed` for the two harness scripts follows `atomic/internal/doctemplate/doctemplate.go:17-30` and `atomic/internal/coldprompt/coldprompt.go:17-45`.
- A central daemon (A) buys nothing D lacks: the registry becomes a directory listing of meta files, the reaper becomes a timer inside each harness, and one session crashing cannot take the others down.
- Sockets live under `~/.atomic/repl/<scope-key>/` — macOS caps `sun_path` at ~104 chars, ruling out in-repo socket paths; `~/.atomic` is the established per-user state root (`atomic/internal/config/statemigrate.go`).

Mechanism decisions that shape the spec:

| Concern | Decision |
|---------|----------|
| Session identity | Scope-aware: a session keys to the resolved scope root where `start` ran — a repo root (marker/git) or a realm root. Verbs resolve the union of the current repo's sessions and the enclosing realm's, so a realm-level session (e.g. one holding a cross-repo dataset) is usable from any member repo. Mirrors `atomic code`'s position-sensing. |
| Machine-wide visibility | Meta stores the origin root path and pid. `list --all` enumerates every session across all scopes — name, root, lang, pid, liveness — so the human can find and `kill` a frozen harness without knowing which repo produced it. |
| Wire protocol | Newline-delimited JSON request/response: `{v, op: eval\|ping\|reset\|shutdown, code}` → `{v, ok, stdout, stderr, value, error, truncated}`. `v` is the protocol version; on mismatch the client fails loud naming `repl stop` + `start` as the fix (a harness spawned by an older binary can outlive an `atomic update`). All response fields always present; `value`/`error` are empty strings when absent. Output is sanitized to valid UTF-8 before encoding. A contract test decodes each harness's live JSON against the canonical Go struct so the two hand-written emitters cannot drift. |
| Env preparation | Go parses the `--env` file (minimal KEY=VALUE: comments, blank lines, single/double quotes; no expansion, no new dep) and passes the merged environment at spawn via `exec.Cmd.Env`. `list`/`status` never echo env values. |
| Code input | Argument wins when present; otherwise stdin when it is not a tty; neither → usage error. |
| Eval semantics | Code executes in one persistent global namespace; `value` is the repr/inspect of the final expression (REPL semantics), empty for statements. |
| Timeout | Client-side deadline (`--timeout`, default 30s). On expiry the client sends SIGINT to the harness pid (from meta file); still hung after a short grace → SIGKILL, session reported dead. |
| Output bounds | Harness truncates stdout/stderr at 64KiB each, sets `truncated`; CLI prints an explicit `[truncated]` marker. |
| Concurrency | Harness is a single-threaded accept loop — evals serialize naturally; a concurrent caller blocks until its deadline. |
| Idle reap | Harness-local timer since last op, on a monotonic clock (wall-clock jumps from sleep/NTP must not fake idleness). On expiry: remove socket + meta, exit 0 — nothing left behind, and the next `eval` against that name gets the same "no live session — run `atomic repl start`" message as a never-started name (identical remedy, so no distinguishing marker). Timeout resolved by Go at `start` time — repo `.claude/atomic.toml` `[repl] idle_timeout`, else user `~/.atomic/config.toml` `[repl] idle_timeout`, else `1h` — and passed to the harness as a flag; the harness never reads config. |
| Interpreter resolution | `--lang py|python` and `--lang js|node|javascript` (aliases: the binary already teaches agents `python`/`javascript` via code-intel output) map to `python3`/`node` via PATH lookup; `--bin` overrides explicitly. No virtualenv auto-detection — guesses wrong in monorepos, and the flag is one token. Interpreter missing gets its own exit code — an agent must be able to tell "install/point `--bin`" apart from "I wrote the command wrong". |
| Crash semantics | Fail loud: a dead session is reported as dead with a distinct exit code and a hint to `repl start` again. Never silently restarted — silent restart would hide state loss. |
| Stuck harness | Accepted limitation: no independent reaper — a harness blocked in a hot loop or C-extension call can't service its own idle timer, and if no client returns to fire the timeout escalation it runs until killed by hand. `list --all` exposing the pid is the mitigation; a watchdog daemon was deliberately rejected with Approach A. |
| Reset | `reset` clears the interpreter namespace but keeps the process and its `--env`-loaded environment — which cannot be re-supplied without a fresh `start --env`, and skips interpreter startup. That is the entire justification; if `--env` re-supply ever becomes free, `reset` is a YAGNI cut candidate. |
| Secrets exposure | `--env` values live in the harness process environment and are visible to same-user OS inspection (`ps eww`, `/proc/<pid>/environ`). Accepted limitation, consistent with the same-trust-as-Bash non-goal; the CLI's own read verbs still never echo them. |
| Materialization + file modes | Session dirs `0o700`, sockets/meta `0o600` — the house `0o755`/`0o644` default is world-readable and a session socket is remote code execution into a process holding secrets. The harness script is rewritten from the embedded bytes before every spawn via temp-file-then-rename into a `0o700` dir — never trusted stale, never readable mid-write. |
| Working directory | Harness cwd = the scope root the session keys to. |


## Open questions


- None.
