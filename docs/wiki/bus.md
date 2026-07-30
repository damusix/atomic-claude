---
type: Domain
description: Inter-session messaging between concurrent Claude Code sessions over named rooms, via a per-user Unix-socket daemon.
---

# bus

## What it does

- `atomic bus` is a single per-user daemon listening on a Unix domain socket that lets concurrent Claude Code sessions on one machine message each other over named rooms, speaking newline-delimited JSON (`atomic/internal/bus/action.go:1-6`, package doc).
- Exposes 13 CLI verbs — `join leave send recv who rooms status serve tail say halt resume chat` — dispatched through `BusAction` (`atomic/internal/bus/action.go:28-66`).
- Localhost/Unix-only (macOS, Linux, WSL2); no configuration file; the daemon auto-spawns on first need (`Ensurer.EnsureDaemon`, [`atomic/internal/bus/client.go`](../../atomic/internal/bus/client.go)) and idle-shuts-down after `DefaultIdleWindow` (10 minutes, `atomic/internal/bus/daemon.go:18`) with zero open subscriptions.

## Artifacts

- [`skills/atomic-bus/SKILL.md`](../../skills/atomic-bus/SKILL.md) — auto-fires on connect/join/message-another-session language; documents the join-then-`Monitor(recv --follow)` connect flow, the envelope shape, the addressed-vs-FYI reaction policy, the trust posture for peer messages, and the operator-only verbs (`tail`, `say`, `chat`).

## CLI code

- [`atomic/internal/bus/protocol.go`](../../atomic/internal/bus/protocol.go) — wire types (`Request`, `Response`, `Envelope`, `Member`, `RoomInfo`), `ProtocolVersion = 1`, the 12 daemon op constants, `ExitCode` constants, and the wire-size limits (`MaxTextBytes`, `MaxIdentifierBytes`, `MaxAddressees`, `MaxAddresseesBytes`).
- [`atomic/internal/bus/paths.go`](../../atomic/internal/bus/paths.go) — `SocketPath`, `LockPath`, `StatePath`, `RoomLogPath`, `EnsureDirs`; every path is derived from `config.Dir(home)` (see Coupling below).
- [`atomic/internal/bus/identity.go`](../../atomic/internal/bus/identity.go) — `SessionID` (reads `CLAUDE_CODE_SESSION_ID`, or the `--session` override); `State` (the per-session joined-room map persisted at `bus.json`, with `Load`/`Save`/`Join`/`Leave`/`LastRoom`/`ResolveRoom`).
- [`atomic/internal/bus/client.go`](../../atomic/internal/bus/client.go) — `Client` (`Dial`, `Do`, `Subscribe`, `Close`); `Ensurer`/`EnsureDaemon` (flock-guarded probe-and-spawn, stale-socket recovery, version-skew refusal); `spawnServe` (launches `atomic bus serve` detached).
- [`atomic/internal/bus/daemon.go`](../../atomic/internal/bus/daemon.go) — `Serve`/`daemon`: accept loop, idle-shutdown timer (`armIdleTimer`, `subscriptionOpened`/`Closed`), and the per-op handlers (`handlePing`, `handleJoin`, `handleLeave`, `handleSend`, `handleSay`, `handleRecvOnce`, `handleWho`, `handleRooms`, `handleHalt`, `handleResume`, plus `subscribe` for `recv --follow`/`tail`).
- [`atomic/internal/bus/room.go`](../../atomic/internal/bus/room.go) — `Hub`/`Room`: roster with atomic name-claim (`Join`), the bounded replay ring, the halt flag, subscriber fan-out (`Publish`, `PublishAsOperator`, `Halt`, `Resume`, `Since`, `Subscribe`).
- [`atomic/internal/bus/roomlog.go`](../../atomic/internal/bus/roomlog.go) — `Append`/`ReadSince`: the durable, append-only per-room JSONL log at `RoomLogPath`.
- [`atomic/internal/bus/action.go`](../../atomic/internal/bus/action.go) — `BusAction` verb dispatch plus every `*Action` function (`joinAction`, `leaveAction`, `sendAction`, `recvAction`, `whoAction`, `roomsAction`, `statusAction`, `serveAction`, `haltAction`, `resumeAction`, `sayAction`, `tailAction`, `chatAction`) and the shared `parseFlags`/`dialDaemonRecovered` helpers.
- [`atomic/internal/bus/render.go`](../../atomic/internal/bus/render.go) — `TailLine` (timestamp/sender/addressee/text formatting with hanging-indent wrap and long-payload collapse), `MemberTable`, `RoomTable`, `colourFor` (stable per-sender ANSI colour, disabled on non-tty).
- [`atomic/internal/bus/chat.go`](../../atomic/internal/bus/chat.go) — `Chat`: the interactive client's core loop (pinned input line, `@name`/`/who`/`/rooms`/`/halt`/`/resume`/`/quit` in-chat syntax, backlog buffering while composing).
- [`atomic/cmd/atomic/main.go`](../../atomic/cmd/atomic/main.go) (`buildBusCmd`, `runBus`) — registers `bus` as a top-level Cobra command with 13 subcommands; `runBus` resolves `os.UserHomeDir()`/`os.Getwd()` and calls `bus.BusAction`.
- [`atomic/internal/cliusage/cliusage.go`](../../atomic/internal/cliusage/cliusage.go) — 13 `{Path: []string{"bus", "<verb>"}, ...}` entries (`join, leave, send, recv, who, rooms, status, serve, tail, say, halt, resume, chat`) mirroring the CLI surface, each with its `Args`/`Flags`/`Description`.

## Docs

- [`docs/reference/bus.md`](../reference/bus.md) — verb and concept reference: room model, addressed-vs-FYI, envelope field table, agent-vs-human `kind`, daemon lifecycle (auto-spawn, idle shutdown, rehydration, retiring), exit-code table, operator verbs, state-on-disk table, security model.
- [`docs/spec/atomic-bus.md`](../spec/atomic-bus.md) — implementation contract: goal, non-goals, 19-item success-criteria checklist, 7 checkpoints, risks table, and a change log with 5 dated amendments (sender-identity assignment moved server-side; daemon-side roster rehydration; idle-shutdown/envelope-shape fixes; a correction removing an unneeded Windows build-tag stub; checkpoint 4 gains its own real-filesystem `t.Setenv("HOME", …)` test requirement).
- [`docs/design/atomic-bus.md`](../design/atomic-bus.md) — design doc: problem statement, a sequence diagram of one message's path, 3 considered approaches (Unix-socket daemon / append-only inbox files / localhost WebSocket) with the daemon approach (A) recommended, the wire-protocol op table, and 5 resolved open decisions (ring-buffer durability, version-skew refusal, client-side `observe` enforcement, server-enforced halt, `chat`/`tail` identity).

## Coupling

- **config domain.** Every piece of bus's per-user state — `bus.sock`, `bus.lock`, `bus.json`, `rooms/*.log` — lives under `~/.atomic/`, resolved via `config.Dir(home)` called directly from [`atomic/internal/bus/paths.go`](../../atomic/internal/bus/paths.go) (not re-derived or duplicated there). A change to `config.Dir`'s root path moves bus's state location too.
- **config domain (verb count).** `buildBusCmd` in [`atomic/cmd/atomic/main.go`](../../atomic/cmd/atomic/main.go) registers `bus` as a new top-level Cobra command, bringing the total from 20 to 21; [`atomic/cmd/atomic/main_test.go`](../../atomic/cmd/atomic/main_test.go)'s `TestRootCmdExact21Verbs` gates the exact top-level verb count (`bus, claude, code, config, docker, docs, doctor, followups, hooks, migrate, profile, prompt, reminder, repo, serve, signals, template, update, validate, where, wiki`) and must be updated whenever a verb is added, removed, or renamed anywhere in the binary.
- **doctor domain.** [`atomic/internal/cliusage/cliusage.go`](../../atomic/internal/cliusage/cliusage.go)'s 13 `{"bus", ...}` `Path` entries feed the A1 artifact-citation lint (`atomic validate artifacts`) surface table — adding, removing, or renaming a bus verb or flag without updating `cliusage.go` causes A1 to either false-positive on a valid citation or miss an invalid one.
- **bundle domain.** [`skills/atomic-bus/SKILL.md`](../../skills/atomic-bus/SKILL.md) is a new bundle input under [`skills/`](../../skills) — it must be present in the regenerated [`atomic/internal/embedded/bundle/`](../../atomic/internal/embedded/bundle) output (`make bundle`) and wired into discovery surfaces ([`CLAUDE.md`](../../CLAUDE.md), [`templates/commands/atomic-help.md`](../../templates/commands/atomic-help.md)) per the mandatory artifact checklist.

## Conventions worth knowing

- The daemon's 12 supported ops ([`atomic/internal/bus/protocol.go`](../../atomic/internal/bus/protocol.go)): `ping, join, leave, send, say, recv, tail, who, rooms, halt, resume, shutdown`. `recv` (with `Follow: true`) and `tail` are the two ops that open a subscription rather than a single round trip.
- A room's in-memory replay ring holds `ringCapacity = 256` envelopes (`atomic/internal/bus/room.go:16`) — the bounded backing for `--since` catch-up while the daemon is up; the per-room log file (`roomlog.go`) is the durable record that survives a daemon restart.
- Sender identity (`from`, `from_kind` on every `Envelope`) is always assigned server-side: from the caller's roster membership in `Hub.Publish`, or pinned to the fixed operator identity in `Hub.PublishAsOperator` — never read from the wire request (`room.go`'s `PublishAsOperator` doc, `daemon.go`'s `handleSay` doc). This is what makes a halted room's block on agent `send` unbypassable and makes member impersonation impossible.
- Two names are reserved and cannot be claimed by `Join`: `"system"` (`systemName`, used by daemon control envelopes — halt/resume announcements, drop markers) and `"human"` (`operatorName`, used by every `say`/`halt`/`resume` envelope) — both enforced via one `reservedNames` map (`room.go:136-148`).
- `subscriberBuffer = 32` (`room.go:20`) bounds each live subscriber's delivery channel; a full channel drops the envelope rather than blocking the publisher, and the next envelope that does fit is preceded by a synthetic drop-marker envelope (from `"system"`) naming how many were dropped and the room-log path where they remain durably recorded.
