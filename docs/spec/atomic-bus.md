# atomic bus — inter-session messaging over named rooms


## Goal


`atomic bus` lets concurrent Claude Code sessions on one machine message each other over named
rooms. A message addressed to a session reaches it through the `Monitor` tool as a prompt and is
acted on as an instruction. A human operator can watch any room, speak into it, and halt it.

Done means: two sessions join a room, one sends an addressed message, the other receives it on
stdout within a second without polling, and an operator running `atomic bus tail` sees both — and
can stop the exchange with `atomic bus halt`.


## Non-goals


- Remote or cross-machine messaging. Localhost only; Unix-only (macOS, Linux, WSL2).
- Authentication beyond Unix file permissions. Any process running as this user may connect.
- Replacing subagents or agent teams. This connects sessions that already exist.
- Replay that survives a daemon restart. Room logs are the durable record; `--since` replays from
  a bounded in-memory ring.
- Windows support.


## Success criteria


- [ ] `atomic bus join <room> --as <name>` returns immediately, does not block, and auto-spawns the
      daemon when absent.
- [ ] Two concurrent `join` calls racing from cold produce exactly one daemon.
- [ ] A second `join` with a taken name in the same room retries once with a numeric suffix; a
      third fails with exit 4.
- [ ] `atomic bus recv <room> --follow` emits one JSON envelope per line, flushed per line, and
      exits 0 on SIGTERM.
- [ ] A message sent by one session appears on another session's `recv --follow` stdout in under
      one second.
- [ ] A socket file whose listener is dead is unlinked, respawned, and connected to — once, not in
      a loop; a second failure exits 6.
- [ ] A client whose version differs from the running daemon's refuses with exit 6 and names both
      versions plus the remedy.
- [ ] `atomic bus tail <room>` shows messages addressed to other members, without joining and
      without appearing in `who`.
- [ ] `atomic bus halt <room>` causes agent `send` into that room to exit 7, while `atomic bus say`
      still succeeds; `resume` restores sends.
- [ ] Every room's traffic appends to `~/.atomic/rooms/<room>.log` whether or not anyone is watching.
- [ ] `who`, `rooms`, `recv`, and `status` all accept `--json`.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` clean; `make render` and `make bundle` leave no
      diff; `atomic validate` passes.


## Approach


A single per-user daemon behind a Unix domain socket, speaking newline-delimited JSON — see
`docs/design/atomic-bus.md`.


## Change tree


```
atomic/internal/bus/
├── protocol.go ............ A  (Request, Response, Envelope, Member, ops, ProtocolVersion)
├── protocol_test.go ....... A
├── paths.go ............... A  (SocketPath, LockPath, StatePath, RoomLogPath)
├── identity.go ............ A  (SessionID, State load/save)
├── identity_test.go ....... A
├── client.go .............. A  (Dial, Do, EnsureDaemon, Subscribe)
├── client_test.go ......... A
├── daemon.go .............. A  (Serve, connection loop, idle shutdown)
├── daemon_test.go ......... A
├── room.go ................ A  (Room, Hub, roster, ring, halt)
├── room_test.go ........... A
├── roomlog.go ............. A  (Append, ReadSince)
├── action.go .............. A  (BusAction verb dispatch)
├── action_test.go ......... A
├── render.go .............. A  (tail line, tables, colour, wrap)
├── render_test.go ......... A
├── chat.go ................ A  (interactive client)
└── chat_test.go ........... A
atomic/cmd/atomic/main.go .. M  (buildBusCmd, runBus)
atomic/cmd/atomic/main_test.go  M  (dispatch + verb-count assertions)
atomic/internal/cliusage/cliusage.go  M  (12 bus entries)
skills/atomic-bus/SKILL.md . A  (connect + reaction policy)
templates/commands/atomic-help.md  M  (topic row + tour stage)
CLAUDE.md .................. M  (one clause)
README.md .................. M  (feature-table row)
docs/reference/bus.md ...... A  (verb reference)
docs/reference/commands.md . M  (skill row)
docs/reference/skills.md ... M  (skill row)
docs/design/atomic-bus.md .. A
docs/spec/atomic-bus.md .... A
```


## Outline


```
atomic/internal/bus/protocol.go
  ProtocolVersion — constant gating the handshake
  Request  — op plus operand fields
  Response — ok, code, error, payload
  Envelope — id, room, from, from_kind, to, reply_to, ts, text, truncated, log
  Member   — name, kind, mode, session, joined
  ExitCode constants — Ok, Usage, Hard, NotJoined, NameTaken, NoRoom, Unreachable, Halted

atomic/internal/bus/paths.go
  SocketPath, LockPath, StatePath, RoomLogPath — resolve under ~/.atomic
  EnsureDirs — create ~/.atomic/rooms with 0700

atomic/internal/bus/identity.go
  SessionID — read CLAUDE_CODE_SESSION_ID, error when absent
  State     — per-session joined rooms, persisted at ~/.atomic/bus.json
    Load, Save, Join, Leave, LastRoom
  ResolveRoom — explicit arg, else LastRoom, else not-joined error

atomic/internal/bus/client.go
  Client — one connection to the daemon
    Do        — single round trip
    Subscribe — round trip then stream frames until close
    Close
  EnsureDaemon — flock-guarded probe-and-spawn; stale-socket recovery; one retry
  checkVersion — refuse on skew with an actionable message

atomic/internal/bus/daemon.go
  Serve — listen, accept, idle-shutdown timer, signal handling
  conn  — per-connection state and op dispatch
    handleJoin, handleLeave, handleSend, handleRecv, handleTail,
    handleWho, handleRooms, handleHalt, handleResume, handlePing, handleShutdown
  idleTimer — fire when subscriber count has been zero for the configured window

atomic/internal/bus/room.go
  Hub — all rooms, guarded by one mutex
    Join    — atomic name claim with numeric-suffix retry
    Leave, Who, Rooms
    Publish — assign id, append to log and ring, fan out to subscribers
    Halt, Resume, IsHalted
  Room — roster, ring buffer, halt flag, subscribers
    Since — replay from ring

atomic/internal/bus/roomlog.go
  Append   — one JSON line per envelope, 0600
  ReadSince — recover envelopes after a daemon restart

atomic/internal/bus/action.go
  BusAction — exported entry; verb switch
    joinAction, leaveAction, sendAction, recvAction, whoAction, roomsAction,
    serveAction, tailAction, chatAction, sayAction, haltAction, resumeAction, statusAction
  readText — positional text, or stdin when "-"
  parseTo  — comma-separated names to []string

atomic/internal/bus/render.go
  TailLine    — timestamp, sender, arrow, addressee, text
  wrapHanging — wrap to terminal width, indent to the message column
  MemberTable, RoomTable
  colourFor   — stable per-sender colour; disabled when not a tty
  collapse    — long payloads to a marker plus a log pointer

atomic/internal/bus/chat.go
  Chat — interactive loop
    render  — transcript above, input line pinned
    onInput — bare line, @name, /who, /rooms, /halt, /resume, /quit
    backlog — buffer and count while scrolled up

atomic/cmd/atomic/main.go
  buildBusCmd — cobra parent plus 12 children
  runBus      — resolve home and cwd, delegate to bus.BusAction

skills/atomic-bus/SKILL.md
  Connecting — join then Monitor(recv --follow)
  Reaction policy — addressed vs FYI, human vs agent, observe mode
  Replying — send with --to and --reply-to
  Halt — what a halted room means for the agent
```


## Flows


```
Flow: join
1. client reads CLAUDE_CODE_SESSION_ID; absent and no --session -> exit 2
2. client takes an exclusive flock on ~/.atomic/bus.lock
3. client dials the socket; on success it pings and compares versions
4. no socket, or connection refused -> unlink any stale socket, spawn `atomic bus serve`
   detached, wait for the socket to accept (bounded), retry once, else exit 6
5. client releases the lock, sends {"op":"join", room, name, mode, kind, session}
6. hub claims the name; taken -> retry once as <name>-2; taken again -> exit 4
7. client records room and assigned name in ~/.atomic/bus.json, prints, exits 0

Flow: send and receive
1. sender runs `atomic bus send <room> <text> --to backend`
2. daemon assigns a short id, stamps ts and from_kind, appends to the room log,
   pushes onto the room ring
3. daemon writes the envelope to every open subscriber on that room
4. the receiving session's `recv --follow` writes one JSON line and flushes
5. Monitor turns that line into a notification; the skill's reaction policy decides
   act (addressed) or note (FYI)

Flow: halt
1. operator runs `atomic bus halt <room> --text "stop, wrong approach"`
2. hub sets the halt flag and publishes a control envelope from kind "human"
3. every agent `send` into that room now returns code 7; the client exits 7
4. `atomic bus say` bypasses the flag and always publishes
5. `resume` clears the flag and publishes the clearing envelope

Flow: version skew
1. client pings; daemon replies with its ProtocolVersion
2. mismatch -> client prints running vs client version and
   "run `atomic bus serve --stop` to retire the old daemon", exits 6
3. no drain, no auto-restart: another session may hold a live subscription

Flow: idle shutdown
1. daemon tracks open subscriptions
2. count reaches zero -> arm a timer for the configured window (default 10m, 0 disables)
3. new subscription before it fires -> disarm
4. timer fires -> close the listener, unlink the socket, release the lock, exit 0
```


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Protocol, paths, identity. Wire types, exit-code constants, `~/.atomic` path helpers, session-id resolution, `bus.json` state with `ResolveRoom`. No daemon, no CLI. | `internal/bus/{protocol,paths,identity}.go` + tests | atomic-implementer (mode: feature) | ~6 | `go test ./internal/bus/...`; state round-trips; missing session id errors |
| 2 | Hub and daemon. Rooms, roster with atomic name claim, ring buffer, halt flag, room log, subscriber fan-out, `serve` with idle shutdown and `--stop`. | `internal/bus/{room,roomlog,daemon}.go` + tests | atomic-implementer (mode: feature) | ~7 | duplicate name rejected; halt blocks agent publish; idle timer arms and disarms; log appended |
| 3 | Client and daemon lifecycle. Dial, round trip, subscribe, flock-guarded `EnsureDaemon`, stale-socket recovery, version refusal. | `internal/bus/client.go` + tests | atomic-implementer (mode: feature) | ~3 | **concurrent join spawns exactly one daemon**; stale socket recovered once then exit 6; skew exits 6 |
| 4 | Agent verbs. `join leave send recv who rooms serve status` through `BusAction`; `buildBusCmd` and `runBus`; `cliusage` entries. | `internal/bus/action.go`, `cmd/atomic/main.go`, `cliusage.go` + tests | atomic-implementer (mode: feature) | ~6 | `--json` on every read verb; exit codes 3/4/5/6; `recv --follow` delivery under 1s; stdin on `-`; **its own `t.Setenv("HOME", …)` real-filesystem test at the dispatch layer** — checkpoint 1's disk test injects `home` directly and so cannot catch a wrong `os.UserHomeDir()` hand-off here |
| 5 | Human participation. `kind` on roster and envelope, `tail say halt resume`, `render.go` formatting with colour, wrap, collapse, and the `--only-addressed` / `--from` filters. | `internal/bus/{render,action,room}.go` + tests | atomic-implementer (mode: feature) | ~6 | tail sees others' mail; halt blocks agent, permits human; no colour when not a tty |
| 6 | `chat`. Interactive client: pinned input, `@name` and slash commands, scroll backpressure. | `internal/bus/chat.go` + tests | atomic-implementer (mode: feature) | ~3 | input line survives concurrent arrivals; `/halt` and `/quit` work |
| 7 | Artifacts and docs. `skills/atomic-bus/SKILL.md` with the reaction policy; `atomic-help` topic row and tour stage; `CLAUDE.md`, `README.md`, `docs/reference/bus.md`, commands and skills tables; `make render` and `make bundle`. | `skills/`, `templates/commands/atomic-help.md`, docs | atomic-implementer (mode: feature) | ~9 | help MISSING-scan clean; render and bundle parity; `atomic validate artifacts` passes |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Daemon spawn race yields two daemons | med | The entire probe-and-spawn path runs under one exclusive `flock`; the daemon holds the lock for its lifetime, so acquiring it is itself proof the previous daemon is gone. CP3 test spawns N concurrent joins from cold and asserts one pid. |
| Test suite leaves stray daemons or sockets behind | high | Every test overrides `HOME` to `t.TempDir()`, so socket, lock, and state land in the temp dir; `t.Cleanup` shuts the daemon down. No test touches the real `~/.atomic`. |
| `recv --follow` tests hang forever on a missed message | med | Every subscription assertion is bounded by a `select` on a timeout channel and fails with a clear message rather than blocking the suite. |
| Notification cap silently truncates a long message | med | Envelopes over the threshold carry `truncated` and `log`; the full body is always in the room log, and the skill documents how to fetch it. |
| `chat` TUI scope creeps into a terminal-emulator rewrite | med | CP6 is last and deliberately minimal — line-oriented redraw, not a full-screen widget tree. Slipping it does not block CP1-5 or the skill. |
| Session id absent outside Claude Code breaks scripted use | low | `--session` override on `join`, exercised by tests. |
| Unix-only build breaks the Windows release target | low | `flock` and `net.Listen("unix", ...)` are POSIX; the package builds on darwin and linux. goreleaser still emits a Windows binary, and `bus` returns a clear unsupported error there via a build-tagged stub. |


## Change log

### 2026-07-28 — checkpoint 4 owes its own real-filesystem test

Checkpoint 4's `Verifies` column now requires a `t.Setenv("HOME", …)` test at the CLI dispatch
layer. Raised by the checkpoint 1 review: that checkpoint's real-disk test passes `home` in
directly, because nothing in `internal/bus` reads `$HOME`. The env-var-to-`home` hand-off first
exists in `runBus`, so the scope-root class of bug (`.claude/skills/atomic-cli-contrib/SKILL.md`
§3) is only reachable — and only catchable — at checkpoint 4.
