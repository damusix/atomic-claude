# atomic bus over a network — hosted rooms with per-machine keys


## Goal


Let Claude Code sessions on different machines share `atomic bus` rooms, over a transport an attacker
on the network path cannot read, alter, replay, reorder, or inject into. A dropped or delayed frame is
detected, not prevented: it ends the stream, the client reconnects, and envelopes published during the
gap are lost rather than forged — `read` recovers them from the room log.

A gateway process runs beside the bus daemon, starts it, opens each AEAD-sealed frame, rewrites the
caller's identity, and speaks the existing protocol to the daemon over its Unix socket. Each machine
holds one 32-byte key issued at enrollment. Holding a key puts a machine in the same position as a
process on the host with socket access, minus `shutdown`. There are no roles.

The daemon is hardened rather than untouched: it gains a room-name guard, a session length cap, its
own roster file, a `Host` field on memberships, and `OpRead`. Adding an op changes the pinned wire
shape, so `ProtocolVersion` goes to 4.

`atomic serve` reaches remote rooms through the same client, so the browser lists, joins, reads and
closes remote rooms alongside local ones.


## Non-goals


- No roles, and no per-op authorization beyond refusing `shutdown`.
- No end-to-end encryption. The gateway decrypts to route, so room logs on the host stay plaintext.
- No protection of state at rest. `keys.json`, `bus.json` and room logs are readable by anyone with
  shell on the host, which is accepted: that host is infrastructure the deployer controls.
- No multi-tenancy, no federation between gateways.
- No key rotation window. Rotating means enrolling again and revoking the old key.
- No OS keyring. The client key is a file under `~/.atomic/`.
- No HTTP/3 and no QUIC. No h2 either: it is disabled explicitly on both sides.
- No required TLS, no `autocert`, no new `golang.org/x/crypto` dependency.
- No rate limiting, no log rotation. Both are operational and deferred.
- No change to local bus behavior. A machine with no remote configured behaves as it does today.


## Success criteria


1. Clients on two machines, enrolled with different keys against one gateway, join the same room and
   exchange addressed messages.
2. A frame with a valid `key_id` and any single byte altered fails to open, and the gateway writes no
   bytes in response.
3. A frame replayed inside the timestamp window is rejected on the nonce; outside it, on the
   timestamp. The window is 120 seconds either side of gateway time. The gateway writes no bytes for
   either.
4. A frame whose `key_id` is unknown or revoked causes the gateway to write no bytes.
5. **Stream integrity holds server to client.** A frame from one stream does not open on another; a
   whole stream captured earlier does not open against a new connection; a gap, a repeat, a reorder,
   and a first frame whose counter is not zero are each detected and end the stream. Heartbeats and
   error frames are sealed and advance the sequence.
6. Under one key, no two streams ever produce the same `(subkey, nonce)` pair, established by
   deriving the `s2c` subkey per stream from the request nonce.
7. The gateway rewrites `Session` **including when the client sends an empty one**: two keys both
   sending with `Session: ""` resolve to two different members, not one.
8. Two Claude sessions on one machine, sharing a key, appear as two members in `who`.
9. Every op except `shutdown` succeeds over the wire for any valid key, `halt`, `say`, `tail`,
   `close` and `end` included. `shutdown` is refused with a sealed error.
10. Revoking a key ends its live stream within one frame, with no gateway restart.
11. Restarting the host daemon preserves the roster and per-room halt state, and a reconnecting
    `recv` resumes as a named member rather than anonymously. No process but the daemon writes its
    state file.
12. A room name containing `/`, `\`, `..`, or a control character is refused by the daemon on `join`,
    on `tail`, and on rehydrate from disk, whether or not a gateway is in front.
13. A session that joined `potato` on a remote is refused a local `potato` join, and the reverse.
14. `read --host <name>` returns a remote room's transcript, so a truncated message is recoverable
    from another machine.
15. `atomic serve` lists local and remote rooms in one list tagged by host and streams remote
    envelopes to the browser over the existing `text/event-stream` route. A remote room's backlog is
    empty by design — there is no bulk-history wire op (`OpTail` is live-only, `OpRead` answers one
    id at a time) — and the live SSE tail fills the transcript from the point of connection.
16. A machine with no `[bus.remotes]` runs every bus verb against the local socket with no key and no
    flag. The existing suite passes with only the golden wire-shape and verb-count updates named in
    the change tree.
17. `docs/guides/bus-hosting.md` walks a reader from nothing to two machines sharing a room, for a
    host inside a VPN and a host behind a proxy.


## Approach


A gateway beside the daemon, one AES-256-GCM key per machine carrying admission, identity and
confidentiality, and no authorization beyond refusing `shutdown`. See
`docs/design/atomic-bus-network.md`.


## Change tree


```
atomic/internal/bus/
├── protocol.go ............ M  (OpRead, ProtocolVersion 4)
├── protocol_test.go ....... M  (wire-shape golden gains OpRead and the version)
├── room.go ................ M  (validRoomName in getOrCreateRoom, session length cap)
├── room_test.go ........... M  (traversal on join, on tail, on rehydrate; cap case)
├── roomlog.go ............. M  (Append refuses a path-shaped room, second guard)
├── daemon.go .............. M  (persist to the daemon-owned file, handleRead)
├── daemon_test.go ......... M  (restart preserves roster and halt; read op)
├── identity.go ............ M  (RosterPath, roster load and save, Host on roomMembership)
├── identity_test.go ....... M  (same-name-other-host refused; migration read)
├── action.go .............. M  (--host resolution, endAction, read over the wire)
├── action_test.go ......... M  (routing precedence, end verb, no spawn on remote failure)
└── remote/
    ├── frame.go ........... A  (binary header, hkdf subkeys, Seal, Open, sequence)
    ├── frame_test.go ...... A  (tamper, replay, wrong key, cross-stream, gap, nonzero first)
    ├── client.go .......... A  (Dial, Do, Stream, sequence checks, backoff, remotes config)
    └── client_test.go ..... A
atomic/internal/gateway/
├── gateway.go ............. A  (listener, optional TLS, /v1/op, h2 off, start the daemon)
├── gateway_test.go ........ A
├── admission.go ........... A  (ladder, silent drops, body cap, read deadline, session rewrite)
├── admission_test.go ...... A  (one case per drop path, asserting no bytes written)
├── keys.go ................ A  (keys.json, enroll, revoke, lookup with an mtime re-read)
├── keys_test.go ........... A
└── nonce.go ............... A  (seen-nonce window, low edge max of window and start)
atomic/internal/serve/
├── api_bus.go ............. M  (per-room routing in do, handleTail, handleLog, handleRooms)
└── api_bus_test.go ........ M  (a remote room routes remotely on all four paths)
atomic/internal/serve/frontend/src/pages/Bus/
└── Bus.tsx ................ M  (host on the room model, query param, EventSource URL)
atomic/cmd/atomic/main.go .. M  (bus gateway, enroll, revoke, end)
atomic/cmd/atomic/main_test.go  M  (verb-count assertions)
atomic/internal/cliusage/cliusage.go  M  (new bus entries)
context/skills/atomic-bus/SKILL.md  M  (joining a remote, --host, key setup)
context/commands/atomic-help.md  M  (bus topic row, tour stage 4)
context/CLAUDE.md .......... M  (one clause on the bus binary)
README.md .................. M  (feature-table row)
docs/reference/bus.md ...... M  (gateway, enroll, revoke, end, read; remote exit codes)
docs/reference/serve.md .... M  (remote rooms in the bus page)
docs/spec/atomic-bus.md .... M  (ProtocolVersion 4, roster file, room-name guard)
docs/spec/serve-bus-chat.md  M  (tail and log are no longer Dial-only)
docs/wiki/bus.md ........... M  (domain page)
docs/guides/bus-hosting.md . A  (deployment walkthrough, both topologies)
docs/design/atomic-bus-network.md  A
docs/spec/atomic-bus-network.md .. A
```


## Outline


```
atomic/internal/bus/remote/frame.go
  Header     — ver, key_id, timestamp, nonce; the exact AAD bytes
  Marshal    — fixed binary layout, no reserialization on the verify side
  Seal       — direction subkey, random nonce on c2s, sequence as nonce on s2c
  Open       — parse, derive subkey, open against the header bytes
  subkeys    — crypto/hkdf; c2s per key, s2c per stream from the request nonce
  ErrOpen    — one opaque failure value; callers cannot branch on why

atomic/internal/bus/remote/client.go
  Remotes    — the [bus.remotes] table: host, key, optional ca
  Resolve    — --host, else this session's membership host, else local
  Client     — one remote target
  Dial       — resolve and connect; never spawn a daemon
  Do         — seal, POST, open the response under the stream subkey
  Stream     — POST, then yield opened frames, rejecting gap, repeat, nonzero first
  backoff    — reconnect schedule; a stream that ends is a fault

atomic/internal/gateway/keys.go
  Store      — keys.json, re-read when its mtime moves
  Enroll     — 32 bytes from crypto/rand, id = truncated sha256, print a TOML block once
  Revoke     — delete by name
  Lookup     — key id to key and name; the seal path re-checks before every frame

atomic/internal/gateway/nonce.go
  Window     — seen nonces, low edge max of now minus window and gateway start

atomic/internal/gateway/admission.go
  Admit      — the ladder in order, returning a caller or a silent drop
  rewrite    — Session becomes key_id and the client session, always, empty included
  dropReason — logged, never sent

atomic/internal/gateway/gateway.go
  Run        — start the daemon, listen, optional TLS, h2 disabled
  handleOp   — admit, refuse shutdown, dial the socket, copy
  copyStream — seal each daemon line with the next sequence; heartbeat under 30s

atomic/internal/bus/identity.go
  RosterPath — the daemon-owned state file, written by nothing else
  Host       — on roomMembership; a same-name join on another host is refused

atomic/internal/serve/api_bus.go
  do          — per-room routing for the one-shot routes
  handleTail  — the remote Stream instead of its own Dial
  handleLog   — empty backlog for a remote room; no bulk-history wire op exists
  handleRooms — fan out across configured remotes

docs/guides/bus-hosting.md
  What this gives you     — two machines, one room, no third party
  Before you start        — a host, an address, the binary, clocks within two minutes
  Run the gateway         — one command, one volume, one port
  Enroll a machine        — enroll on the host, paste the block on the client
  Join from two machines  — the worked transcript
  Inside a VPN            — bind to the interface, no TLS needed
  Behind a proxy          — the proxy terminates TLS, idle timeouts and the heartbeat
  What is protected       — transport only, and what that excludes
  Revoke a machine        — one command
```


## Flows


**Flow: enroll a machine**

1. Operator runs `atomic bus gateway enroll web-api` on the host.
2. Gateway generates 32 bytes from `crypto/rand`, derives the id as a truncated `sha256` of the key,
   stores id, key and name in `~/.atomic/gateway/keys.json`, and prints a `[bus.remotes]` TOML block
   once.
3. Operator pastes the block into `~/.atomic/config.toml` on the client.

**Flow: send from a remote client**

1. Client resolves the target: `--host`, else this session's membership host, else local.
2. Client seals the request under the `c2s` subkey with a random nonce, keeping that nonce.
3. Client POSTs to `/v1/op`.
4. Gateway reads with a deadline and a body cap, parses the header, checks the timestamp window and
   the key id, opens the frame, checks the nonce is unseen. Any failure writes no bytes and logs one
   line.
5. Gateway refuses `shutdown` with a sealed error; otherwise it rewrites `Session` to the key id and
   the client's session, always, and writes the plaintext to a fresh Unix connection.
6. Daemon resolves the sender from its roster, appends to the room log, and fans out.
7. Gateway seals the response under `hkdf(key, "s2c" ‖ request nonce)` with sequence 0.
8. Client opens it under the subkey derived from the nonce it sent.

**Flow: receive on a remote client**

1. Client seals `{"op":"recv", room}` and POSTs it; the response body stays open.
2. Gateway holds one Unix connection per subscriber and seals each envelope with the next sequence,
   flushing per line, re-checking the key store before each seal.
3. A quiet stream gets a sealed heartbeat that advances the sequence, at most 30 seconds apart.
4. Client rejects a gap, a repeat, a nonzero first counter, a frame that does not open, or a stale
   timestamp, and ends the stream.
5. An ended stream is a fault, so the client reconnects with backoff under a new nonce, and never
   spawns a local daemon.

**Flow: the human surface**

1. Human opens `atomic serve` in a browser on their own machine.
2. `serve` lists rooms from `bus.json` plus a fan-out across configured remotes, in one list tagged
   by host.
3. One-shot routes go through `do`; `handleTail` uses the remote stream and `handleLog` uses `read`.
4. Live envelopes reach the browser over the existing `text/event-stream` route, unchanged.

**Flow: revoke a machine**

1. Operator runs `atomic bus gateway revoke web-api` on the host.
2. The record is deleted; the gateway re-reads `keys.json` on its next lookup.
3. A live stream fails its pre-seal key check and ends within one frame.
4. The next frame from that machine does not open, and the gateway writes no bytes.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Frame format. Binary header as exact AAD, `ver`, hkdf subkeys with `s2c` derived per stream, sequence as the `s2c` nonce, one opaque open error. No network. | `internal/bus/remote/frame.go` + tests | atomic-implementer (mode: feature) | ~2 | round trip; any altered byte fails; wrong key fails; **a frame from stream A does not open on stream B; a whole stream replayed at a new nonce does not open; a gap, a repeat, a reorder and a nonzero first counter are each detected**; no two streams under one key share a `(subkey, nonce)` pair; open failure is indistinguishable across causes |
| 2 | Daemon hardening. Room-name and control-character guard in `getOrCreateRoom` and `Append`, session cap, roster and halt in a daemon-owned file, `Host` on memberships with same-name refusal, `OpRead`, `ProtocolVersion` 4. | `internal/bus/{room,roomlog,daemon,identity,protocol}.go` + tests | atomic-implementer (mode: feature) | ~10 | `../escape` refused on join, **on tail, and on rehydrate**; restart preserves roster and halt; a reconnecting recv resumes named; a same-name join on another host is refused; no process but the daemon writes the roster file |
| 3 | Key store and admission. `keys.json` with an mtime re-read, enroll printing a TOML block, revoke, the ladder in order, seen-nonce window with a start-time low edge, unconditional session rewrite, `shutdown` refusal, the drop log. No listener. | `internal/gateway/{keys,admission,nonce}.go` + tests | atomic-implementer (mode: feature) | ~7 | one test per drop path asserting **no bytes written**; replay inside and outside the 120s window; **a frame captured before a restart and replayed after, inside the window, is dropped**; **two keys sending with an empty session resolve to two members**; `shutdown` refused, every other op forwarded |
| 4 | Gateway server. Listener, optional TLS, h2 disabled, `/v1/op`, starting the daemon, copy with flush, sealed heartbeat under 30s, pre-seal key re-check, body cap and read deadline. | `internal/gateway/gateway.go` + tests | atomic-implementer (mode: feature) | ~3 | one-shot and stream take one path; **an admission drop on one request does not end a concurrent stream**; revoke ends a live stream within one frame with no restart; a slow reader does not stall the room |
| 5 | Remote client and routing. `Do`, `Stream` with sequence checks, backoff treating an ended stream as a fault, remotes config with optional `ca`, resolution precedence, `end` and `read` verbs, `main.go` wiring. | `internal/bus/remote/client.go`, `action.go`, `cmd/atomic/main.go`, `cliusage.go` + tests | atomic-implementer (mode: feature) | ~8 | precedence table; local-only machine unchanged; **no daemon spawned on a failed remote dial**; `end` stops a listener; `read --host` returns a remote transcript |
| 6 | `atomic serve`. Per-room routing in `do`, `handleTail` on the remote stream, `handleLog` on `read`, `handleRooms` fanning out, host on the room model and the `EventSource` URL. Severable: nothing else depends on it. | `internal/serve/api_bus.go`, `frontend/src/pages/Bus/Bus.tsx` + tests | atomic-implementer (mode: feature) | ~6 | a remote room routes remotely on all four paths; two rooms named `potato` stay distinct in the UI; SSE to the browser unchanged; loopback guard intact |
| 7 | Artifacts and docs. `docs/guides/bus-hosting.md` for both topologies, reference and wiki pages, the two sibling specs brought current, skill, help router, README, CLAUDE.md, then `make bundle`. | `docs/`, `context/`, `README.md` | atomic-implementer (mode: feature) | ~11 | help MISSING-scan clean; `atomic validate artifacts` passes; sibling spec bodies match the new contract; the guide walks nothing to two machines sharing a room |

Checkpoints 1 and 2 are independent of each other and of the gateway, so the daemon hardening lands
whether or not the network work does. Checkpoint 6 is severable.


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| An `s2c` nonce repeats under one key, recovering the GHASH key and letting the attacker forge frames | **would have shipped** | A per-key `s2c` subkey with a 32-bit stream id in the nonce collides at 39% after 65,536 streams, and an attacker forcing reconnects drives toward it. The subkey is derived per stream from the full 96-bit request nonce instead, so uniqueness is structural. Criterion 6 and CP1 pin it. |
| The response stream is sealed per frame with nothing binding frames together, so a network-path attacker drops, reorders, replays or splices lines without a key | **high** | Per-frame AEAD does not cover this. The per-stream subkey plus a monotonic sequence closes it, and the client rejects any break. CP1 tests drop, reorder, repeat, cross-stream and whole-stream replay. |
| The session rewrite skips an empty client session, so one key publishes as another's member | **high** | `Hub` records an empty session under `""`, so two keys would collide there. The rewrite is unconditional. Criterion 7 and CP3 pin it. |
| Daemon persistence collides with the CLI and `serve` writing `bus.json` unlocked | high | The daemon writes its own file that nothing else touches, and `Rehydrate` reads it first. CP2 asserts no other writer. |
| A silent-drop path writes a body or a distinguishable error and becomes an oracle | high | CP3 asserts zero bytes and an identical outcome per cause. `ErrOpen` is one value, and an AES-GCM open costs the same on a wrong key as a right one. |
| The room-name guard lands in `Join` only, leaving `tail` and rehydrate able to escape | **known live bug** | `Subscribe` and `Rehydrate` both reach `getOrCreateRoom` without `Join`, which is why nothing catches it today. CP2 tests all three paths and `Append` keeps a second guard. |
| h2 is negotiated by default, so a silent close tears down an unrelated live stream | med | `TLSNextProto` cleared server-side, `ForceAttemptHTTP2` false client-side. CP4 asserts a drop on one request leaves a concurrent stream alive. |
| A remote dial failure spawns a local daemon and silently splits the bus | high | `EnsureDaemon` is bypassed for a remote target. CP5 points a remote at a dead host and asserts no daemon appears. |
| Dropping TLS from the floor is read as dropping it from the product | med | The gateway takes `--tls-cert` and `--tls-key`, the client takes `ca`, and there is no insecure flag. The guide says TLS hides metadata and that the frame carries the guarantee. |
| A reconnect loses envelopes published while the stream was down | med | Inherent: a subscriber gets no backlog. A heartbeat under 30 seconds keeps a dead stream from going unnoticed, and `read` recovers from the room log. |
| Removing roles is read as removing security | med | The design states it directly: a key holder sits where a local process sits, and any enrolled machine can `say`. The guide repeats it where keys are issued. |
| The React work expands into a redesign of the bus page | med | CP6 is additive and severable: a host discriminator, the query parameter, the `EventSource` URL. No new page. |
| A reader follows the guide and believes transcripts are encrypted | med | The guide has a "what is protected" section naming transport only, matching the design's accepted-risk table. Checked in CP7. |


## Change log


### 2026-09-08 — final review remediation

**What changed:** the Goal's "cannot ... drop" claim is corrected: a dropped or delayed frame is
detected, not prevented — it ends the stream, the client reconnects, and envelopes published during
the gap are lost rather than forged. `atomic bus gateway enroll` now takes `--tls-cert` and prints the
matching scheme on `host`, since a bare host defaults to `https` while the gateway defaults to plain
HTTP. `atomic bus gateway` now recovers a stale socket left by a crashed process before listening.
`remote.Client`'s reconnect backoff resets after any connection that opened at least one frame.
`identity.State.ClearRoom` takes a `host` argument and only drops memberships on that host.
`serve`'s `handleRooms` fans remotes out concurrently instead of sequentially. `gateway.MaxFrameBytes`
carries the same headroom over `bus.MaxTextBytes` that `api_bus.go`'s `maxLogLineBytes` already does.

**Why:** final whole-diff review (docs/guides/bus-hosting.md's walkthrough failed as written; the
other five are cross-machine correctness and availability bugs found in the same pass).

**Correction:** the Goal previously read "...cannot read, alter, replay, reorder, drop, or inject
into" with no caveat on drop.

### 2026-09-08 — criterion 15 corrected; audit remediation

**What changed:** criterion 9's seven remaining verbs (`say`, `tail`, `halt`, `resume`, `prune`,
`close`, `end`) now carry `--host` and route through `doOnHost`, matching what the criterion always
described. Criterion 15's "serves a remote transcript" claim is corrected: there is no bulk-history
wire op, so a remote room's backlog is empty by design and `handleLog`'s unreachable id-qualified
branch was removed rather than kept dead.

**Why:** a whole-loop audit found the CLI-side `--host` gap was a silent wrong-target bug (`Hub.setHalted`
and `Hub.PublishAsOperator` resolve by bare room name, so a same-named local room could be acted on
instead) and that criterion 15 overstated what the wire protocol supports.

**Correction:** criterion 15 previously read "...and serves a remote transcript" with no caveat.

### 2026-09-08 — initial spec

**What changed:** First version, derived from `docs/design/atomic-bus-network.md`.

**Why:** The design settled the trust model, the frame format, and the human surface. This carries it
into a checkpointed contract.
