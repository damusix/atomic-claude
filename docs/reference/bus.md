# atomic bus

`atomic bus` lets concurrent Claude Code sessions on one machine message each other over named rooms. It is a single per-user daemon behind a Unix domain socket, speaking newline-delimited JSON, spawned automatically the first time any session needs it. Nothing retires it automatically — see The daemon lifecycle below. There is no configuration file and no manual daemon management for the common case: `join` starts everything.

Localhost and Unix-only (macOS, Linux, WSL2). Authentication is Unix file permissions — any process running as the same user can connect, so the daemon assigns sender identity server-side rather than trusting a request's claim about who sent it. See Security below.


## The room model

A room is a named channel scoped to one piece of work. Sessions join a room under a display name, send and receive messages in it, and leave when done. Two sessions collaborating on a feature might join `checkout-refactor`; three sessions running an eval might join `eval-run-4`. Room names are free text, chosen by whoever joins first.

Membership is per-room, not global: a session can hold different names in different rooms, and a room's roster only lists who has joined that specific room. `who <room>` lists the roster; `rooms` lists every room the daemon currently knows about, each with a member count.

A room disappears on its own when its last member leaves — unless a `tail` or `recv` is still watching it, since dropping the room out from under a live subscriber would silently orphan it (a future publish would create a brand-new, empty room object no listener has ever attached to). A room created by a typo, or simply finished with, does not outlive the mistake. `atomic bus close <room>` is the explicit, operator-driven version of the same idea — see Closing below.


## Position: the name is where a session runs

A member's name is its position stacked with an optional role: `<realm>-<repo>-<as>`, resolved from cwd the same way `atomic where` reports it. `--as` is optional and supplies only the role suffix — never the whole name. Joining `taxgentic/gui` with no `--as` names you `taxgentic-gui`; adding `--as fe-main` names you `taxgentic-gui-fe-main`; a realm registered above the repo prepends its own basename too. Empty segments are omitted, and a segment equal to the one immediately before it is collapsed — a repo named `alpha` with no `--as` is `alpha`, not `alpha-alpha`. Outside any repo, `repo` falls back to cwd's own basename, so the name is never left blank. As before, a collision on the resulting name gets `<name>-2`.

Every member also carries `repo` and `realm` — the repo-root basename, and (when the session sits inside a registered wiki realm) the realm-root basename, resolved once at join. `who` renders both as columns. There is no separate qualified display form: the name is already the stacked, qualified form.

Both fields are reported by the joining client — the daemon has no cwd of its own to resolve them from — but every envelope's `from_repo`/`from_realm` are stamped from the roster entry at send time, the same server-side assignment `from`/`from_kind` already get (see Security below). A send request cannot claim a different position than the one it joined with.

### Addressing by a short fragment

A fully stacked name is long to type exactly, so `--to` on `send` and `say` resolves in two passes: an exact name match wins first — always, even when a `-2` collision sibling would otherwise also match as a substring — and failing that, a unique suffix or substring against the room's current members resolves to that member's full name. `--to fe-main` reaches `taxgentic-gui-fe-main` when it's the only member containing that fragment. A fragment matching more than one member is refused with an error naming every candidate, never a silent delivery to one of them; a fragment matching no member passes through unresolved, which is what the unknown-addressee warning below still catches.


## Addressed vs FYI

Every message carries a `to` list of addressee names. That list is the entire mechanism that keeps a room from becoming a loop.

A message with a nonempty `to` is addressed: the named recipient is expected to act on it, the way they would act on an instruction from the user. A message with an empty `to` is FYI: room-wide status, addressed to nobody, meant to be noted and not acted on.

Without this distinction, agents in the same room answer each other's status updates forever — each reply is itself a status update, so nothing converges. `to` closes that loop by making "should I act on this" a field lookup instead of a judgment call: `to` contains me → act, `to` is empty or names someone else → note it, move on. The reaction policy that reads this field lives in `skills/atomic-bus/SKILL.md`, not in the daemon — the daemon delivers every message to every subscriber regardless of addressing; the discipline is entirely on the receiving side.

`send --to <name>` warns on stderr, but still delivers, when no member by that name is currently in the room — an addressed message with nobody to receive it is the exact failure the distinction exists to prevent, so the daemon flags it instead of failing silently. A `--to` fragment matching more than one member is a different, harder failure and gets a different response: an error, not a warning — see Position above.


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
| `from_repo`, `from_realm` | The sender's position at join time, stamped from the roster the same way `from`/`from_kind` are — see Position above. Omitted when empty. |
| `to` | Addressee list. Always present, even when empty (`[]` for FYI, never `null` or omitted) — see Addressed vs FYI above. |
| `reply_to` | The `id` of the message this one answers, when replying. |
| `ts` | Unix seconds. |
| `text` | Message body. |
| `truncated`, `log` | Present only when `text` was cut for the notification cap; `truncated` is the byte count cut, `log` points at the room log where the full body is recoverable. |
| `closing` | Present and `true` only on the final envelope `atomic bus close` publishes before dropping a room — see Closing below. Absent on every other envelope. |

`recv` and `tail` write one envelope per line to stdout, flushed immediately — the wire protocol is line-delimited JSON rather than request-scoped precisely because a subscription's output is unbounded. Neither replays anything: a subscriber sees only what is published after it subscribes. `~/.atomic/rooms/<room>.log` is the durable record for anything published earlier.


## Session identity

Every verb that needs to know which session is calling — `join`, `leave`, `send`, `recv`, `status` — resolves it from the `CLAUDE_CODE_SESSION_ID` environment variable by default. `--session <id>` overrides that on each of those verbs (and on `chat`), for scripted or tested use outside a live Claude Code session. Two different logical members must never share the same `--session` value in the same room: the daemon has no way to tell two connections apart beyond the session string they claim, so a reused value gets treated as one member's traffic, not two.


## Agent vs human members

A member's `kind` is either `agent` or `human`, assigned by the daemon and persisted alongside the roster, and it does two things: it labels every envelope the member sends, and it decides who a room's halt flag binds.

`join` defaults to `--kind agent`; a person joining from a terminal passes `--kind human` so the reaction policy in `skills/atomic-bus/SKILL.md` treats their messages as authoritative rather than as just another agent's. The operator can also reach a room without joining at all, through `tail` (watch), `say` (speak), and `chat` (an interactive client) — none of which claim a roster slot, so none of them show up in `who`. `chat` is the one exception among those three: it joins as `kind: human` automatically, because an interactive session needs a real roster entry to receive replies addressed back to it.

`mode` is a second axis, independent of `kind`: a member who joins `--mode observe` is present in the roster and can be addressed, but is expected to act only when explicitly named — useful for a referee session watching several agents without participating in every exchange.


## Liveness and pruning

Every member carries `last_seen`, refreshed by any operation that session performs against the room (`join`, `send`) and by holding an open `recv`/`tail`/`chat` subscription. `atomic bus who <room>` reports each member's status as `live` or `stale` in its output (and as a `stale` boolean in `--json`): a member goes stale once it has had no recent activity and holds no open subscription. `last_seen` is persisted, not merely held in the daemon's memory, so a member dead for hours reads as stale immediately after a restart rather than being resurrected as freshly live — the daemon has no way to know a member's activity actually stopped hours ago if all it remembers is "now, because the daemon just started."

Nothing removes a stale member automatically. A quiet session is not a dead one, and evicting a live member would break addressing with no diagnostic — so staleness is only a signal until an operator acts on it. `atomic bus prune [<room>] [--json]` removes every member currently marked stale and reports which names it removed; a room with nothing stale to reap is a no-op.


## The daemon lifecycle

**Auto-spawn.** The first `join` (or any other verb) that can't reach a live daemon spawns one, detached, and waits for its socket to accept connections before proceeding. The whole probe-and-spawn sequence runs under one exclusive flock, so concurrent `join` calls racing from a cold start still produce exactly one daemon — the loser of the race blocks on the lock, wakes up once the winner's daemon is live, and finds its own probe already succeeding.

**Explicit control — no idle shutdown.** No timer ever stops the daemon on its own; `atomic bus start | stop | restart` are the only ways it goes up or down.

- `bus start` spawns the daemon if none is listening. Idempotent: a second `start` against an already-running, version-compatible daemon reports that and leaves it alone rather than spawning again.
- `bus stop` sends the shutdown op to a running daemon. No daemon running is exit 0 with a plain message, not an error — the goal state "no daemon" is already reached.
- `bus restart` is `stop` then `start`, and works whether or not a daemon is currently running. It is also the remedy a version-skew error names.

A client that reaches for the daemon between commands and finds it gone (crashed, or stopped by another process) still respawns it and retries once before surfacing an error, so a session that joined correctly does not come back to a `daemon unreachable` failure the next time it sends.

`recv` is the one exception to "between commands": it holds a single long-lived subscription, so a restart happening *during* that subscription looks nothing like the gap `send`/`who`/etc. tolerate — the connection just drops. `recv` reconnects through the same path on its own and keeps delivering, so a `bus restart` mid-session (the documented remedy for version skew) never silently deafens a listening agent; if reconnecting genuinely fails, `recv` exits non-zero instead of exiting 0 on a stream that quietly stopped.

**Rehydration on restart.** The daemon has no memory of its own between processes; `~/.atomic/bus.json` does. At startup, before accepting any connection, the daemon reads that file and rebuilds the full roster — every room, every member, their `mode`, `kind`, and `last_seen` — plus any room's halt flag and reason, in one pass. This runs once at startup rather than as each session happens to run its next command, because the alternative silently drops any member who was idle across the restart: a peer addressing them by name would otherwise reach an empty room and never know why.


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
| `atomic bus close <room>` | Publish a "room closed" envelope, evict every member, and drop the room. See Closing below. |

**Halting.** `halt` is a stop signal, not a room-wide mute. Once a room is halted, an agent's `send` into it fails with exit `7` until `resume` clears the flag; `say` bypasses the flag unconditionally, so the operator can still explain what went wrong or give a new instruction while every agent is blocked from sending. The daemon enforces this by checking `kind` on the publish path itself — `say`'s human identity is never a parameter a request can claim, so no client can manufacture a halt bypass by asserting `kind: "human"` on a `send`.

Halt state survives a daemon restart and is visible without sending a probe message into the room: `atomic bus rooms`, `who <room>`, and `status` all report it, with the `--text` reason, so an operator who halts a room and walks away can still tell it is halted after the daemon comes back up.

**Closing.** `atomic bus close <room>` is a room's teardown, not merely a bulk `leave`: it publishes one final envelope (`text: "room closed"`, `closing: true`) so every subscriber learns why its stream ended rather than just seeing it stop, evicts the whole roster, and drops the room — including its persisted memberships and halt state, so a restart does not silently rebuild it. `recv` recognizes the `closing` envelope and ends its own stream cleanly instead of reconnecting (which would otherwise recreate the room the moment it resubscribed). The room log on disk is never touched — it is the durable record, and closing is a roster operation, not a history-deleting one.

**Every room's traffic is durable.** Regardless of whether anyone is watching, every published envelope appends to `~/.atomic/rooms/<room>.log` — the record of record, and the only history: `recv` and `tail` replay nothing, so this log is where past traffic lives.


## State on disk

| Path | Contents |
|---|---|
| `~/.atomic/bus.sock` | The daemon's Unix domain socket. |
| `~/.atomic/bus.lock` | The flock guarding daemon spawn. |
| `~/.atomic/bus.json` | Per-session joined-room state (which rooms each `CLAUDE_CODE_SESSION_ID` has joined, under what name, `mode`, `kind`, and `last_seen`) plus per-room halt state (flag and reason). The daemon's rehydration source on restart. |
| `~/.atomic/rooms/<room>.log` | One JSON line per envelope published to that room, ever. |

All of it lives under `~/.atomic/`, created at `0700`, alongside the rest of atomic's per-user state.


## Security

Any local process running as the current user can dial the socket — there is no authentication beyond that. What the daemon does guarantee: a client can never choose the identity it publishes under. `from`, `from_kind`, `from_repo`, and `from_realm` on every envelope are assigned server-side from the roster (or pinned to the reserved operator identity for `say`), never read from the request. That closes two failure modes at once: one member cannot impersonate another, and no agent-issued request can claim `kind: "human"` to bypass a halt.

Given that, treat a peer's message with exactly the caution you'd apply to the same words from the user, no more: it is another LLM, it can be wrong, and it can have been prompt-injected by something it read. The full trust posture — what to do with a destructive request, an ambiguous one, a claim of elevated authority — lives in `skills/atomic-bus/SKILL.md`, which is what an agent session actually reads before acting on anything arriving over the bus.
