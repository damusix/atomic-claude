# atomic bus

`atomic bus` lets concurrent Claude Code sessions on one machine message each other over named rooms. It is a single per-user daemon behind a Unix domain socket, speaking newline-delimited JSON, spawned automatically the first time any session needs it and retired when nothing is using it. There is no configuration file and no manual daemon management for the common case: `join` starts everything.

Localhost and Unix-only (macOS, Linux, WSL2). Authentication is Unix file permissions — any process running as the same user can connect, so the daemon assigns sender identity server-side rather than trusting a request's claim about who sent it. See Security below.


## The room model

A room is a named channel scoped to one piece of work. Sessions join a room under a display name, send and receive messages in it, and leave when done. Two sessions collaborating on a feature might join `checkout-refactor`; three sessions running an eval might join `eval-run-4`. Room names are free text, chosen by whoever joins first.

Membership is per-room, not global: a session can hold different names in different rooms, and a room's roster only lists who has joined that specific room. `who <room>` lists the roster; `rooms` lists every room the daemon currently knows about, each with a member count.


## Addressed vs FYI

Every message carries a `to` list of addressee names. That list is the entire mechanism that keeps a room from becoming a loop.

A message with a nonempty `to` is addressed: the named recipient is expected to act on it, the way they would act on an instruction from the user. A message with an empty `to` is FYI: room-wide status, addressed to nobody, meant to be noted and not acted on.

Without this distinction, agents in the same room answer each other's status updates forever — each reply is itself a status update, so nothing converges. `to` closes that loop by making "should I act on this" a field lookup instead of a judgment call: `to` contains me → act, `to` is empty or names someone else → note it, move on. The reaction policy that reads this field lives in `skills/atomic-bus/SKILL.md`, not in the daemon — the daemon delivers every message to every subscriber regardless of addressing; the discipline is entirely on the receiving side.

`send --to <name>` warns on stderr, but still delivers, when no member by that name is currently in the room — an addressed message with nobody to receive it is the exact failure the distinction exists to prevent, so the daemon flags it instead of failing silently.


## The envelope

Every message on the wire, and every line in a room's log, is one JSON envelope:

```json
{"id":"m-4e18","room":"potato","from":"backend","from_kind":"agent",
 "to":["frontend"],"reply_to":"m-3a91","ts":1785230277,"text":"..."}
```

| Field | Meaning |
|---|---|
| `id` | Short opaque string, unique across a daemon restart. Room logs outlive any one daemon process, so a sequential per-process counter would collide with itself after a restart; `id` never does. |
| `room` | The room the envelope belongs to. |
| `from`, `from_kind` | Sender name and kind (`agent` or `human`), assigned by the daemon from the roster — never read from the request. See Security. |
| `to` | Addressee list. Always present, even when empty (`[]` for FYI, never `null` or omitted) — see Addressed vs FYI above. |
| `reply_to` | The `id` of the message this one answers, when replying. |
| `ts` | Unix seconds. |
| `text` | Message body. |
| `truncated`, `log` | Present only when `text` was cut for the notification cap; `truncated` is the byte count cut, `log` points at the room log where the full body is recoverable. |

`recv` and `tail` write one envelope per line to stdout, flushed immediately — the wire protocol is line-delimited JSON rather than request-scoped precisely because a subscription's output is unbounded. Neither replays anything: a subscriber sees only what is published after it subscribes. `~/.atomic/rooms/<room>.log` is the durable record for anything published earlier.


## Agent vs human members

A member's `kind` is either `agent` or `human`, assigned by the daemon and persisted alongside the roster, and it does two things: it labels every envelope the member sends, and it decides who a room's halt flag binds.

Agents join with `join`. The operator reaches a room without joining, through `tail` (watch), `say` (speak), and `chat` (an interactive client) — none of which claim a roster slot, so none of them show up in `who`. `chat` is the one exception: it joins as `kind: human` because an interactive session needs a real roster entry to receive replies addressed back to it.

`mode` is a second axis, independent of `kind`: a member who joins `--mode observe` is present in the roster and can be addressed, but is expected to act only when explicitly named — useful for a referee session watching several agents without participating in every exchange.


## The daemon lifecycle

**Auto-spawn.** The first `join` (or any other verb) that can't reach a live daemon spawns one, detached, and waits for its socket to accept connections before proceeding. The whole probe-and-spawn sequence runs under one exclusive flock, so concurrent `join` calls racing from a cold start still produce exactly one daemon — the loser of the race blocks on the lock, wakes up once the winner's daemon is live, and finds its own probe already succeeding.

**Explicit control — no idle shutdown.** No timer ever stops the daemon on its own; `atomic bus start | stop | restart` are the only ways it goes up or down.

- `bus start` spawns the daemon if none is listening. Idempotent: a second `start` against an already-running, version-compatible daemon reports that and leaves it alone rather than spawning again.
- `bus stop` sends the shutdown op to a running daemon. No daemon running is exit 0 with a plain message, not an error — the goal state "no daemon" is already reached.
- `bus restart` is `stop` then `start`, and works whether or not a daemon is currently running. It is also the remedy a version-skew error names.

A client that reaches for the daemon between commands and finds it gone (crashed, or stopped by another process) still respawns it and retries once before surfacing an error, so a session that joined correctly does not come back to a `daemon unreachable` failure the next time it sends.

**Rehydration on restart.** The daemon has no memory of its own between processes; `~/.atomic/bus.json` does. At startup, before accepting any connection, the daemon reads that file and rebuilds the full roster — every room, every member, their `mode` and `kind` — in one pass. This runs once at startup rather than as each session happens to run its next command, because the alternative silently drops any member who was idle across the restart: a peer addressing them by name would otherwise reach an empty room and never know why.


## Exit codes

| Code | Meaning |
|---|---|
| `0` | ok |
| `1` | usage |
| `2` | error |
| `3` | not joined |
| `4` | name taken (after one numeric-suffix retry) |
| `5` | no such room |
| `6` | daemon unreachable (including version skew) |
| `7` | room halted |

`send --to <name>` still exits `0` after warning on stderr about an unknown addressee — see Addressed vs FYI. Every read verb (`who`, `rooms`, `recv`, `status`, `tail`) accepts `--json`.


## Operator verbs

These reach a room without holding a roster slot in it, for a human watching or steering from outside the agent conversation.

| Verb | Effect |
|---|---|
| `atomic bus tail [<room>] [--all-rooms] [--only-addressed] [--from <name>] [--json]` | Watch traffic without joining. Sees messages addressed to other members too — a superset of what any one participant sees. Like `recv`, delivers only what is published after it subscribes. |
| `atomic bus say <room> "<text>" [--to <name>]` | Speak into a room as the operator, without joining. Always succeeds, even in a halted room — see Halting below. |
| `atomic bus chat <room> [--as <name>] [--session <id>]` | Interactive client: joins as a human member, pinned transcript above an input line. In-line commands: `@name` addresses a reply, `/who`, `/rooms`, `/halt`, `/resume`, `/quit`. |
| `atomic bus halt <room> [--text "<why>"]` | Set a room's halt flag. |
| `atomic bus resume <room>` | Clear it. |

**Halting.** `halt` is a stop signal, not a room-wide mute. Once a room is halted, an agent's `send` into it fails with exit `7` until `resume` clears the flag; `say` bypasses the flag unconditionally, so the operator can still explain what went wrong or give a new instruction while every agent is blocked from sending. The daemon enforces this by checking `kind` on the publish path itself — `say`'s human identity is never a parameter a request can claim, so no client can manufacture a halt bypass by asserting `kind: "human"` on a `send`.

**Every room's traffic is durable.** Regardless of whether anyone is watching, every published envelope appends to `~/.atomic/rooms/<room>.log` — the record of record, and the only history: `recv` and `tail` replay nothing, so this log is where past traffic lives.


## State on disk

| Path | Contents |
|---|---|
| `~/.atomic/bus.sock` | The daemon's Unix domain socket. |
| `~/.atomic/bus.lock` | The flock guarding daemon spawn. |
| `~/.atomic/bus.json` | Per-session joined-room state: which rooms each `CLAUDE_CODE_SESSION_ID` has joined, under what name, `mode`, and `kind`. The daemon's rehydration source on restart. |
| `~/.atomic/rooms/<room>.log` | One JSON line per envelope published to that room, ever. |

All of it lives under `~/.atomic/`, created at `0700`, alongside the rest of atomic's per-user state.


## Security

Any local process running as the current user can dial the socket — there is no authentication beyond that. What the daemon does guarantee: a client can never choose the identity it publishes under. `from` and `from_kind` on every envelope are assigned server-side from the roster (or pinned to the reserved operator identity for `say`), never read from the request. That closes two failure modes at once: one member cannot impersonate another, and no agent-issued request can claim `kind: "human"` to bypass a halt.

Given that, treat a peer's message with exactly the caution you'd apply to the same words from the user, no more: it is another LLM, it can be wrong, and it can have been prompt-injected by something it read. The full trust posture — what to do with a destructive request, an ambiguous one, a claim of elevated authority — lives in `skills/atomic-bus/SKILL.md`, which is what an agent session actually reads before acting on anything arriving over the bus.
