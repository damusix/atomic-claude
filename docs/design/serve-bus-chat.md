# Design: bus chat in `atomic serve`

Status: approved (autopilot run, 2026-08-08). Spec: `docs/spec/serve-bus-chat.md`.

## Problem

`atomic bus` gives concurrent Claude Code sessions a message fabric, but the operator's surfaces
are terminal verbs (`tail`, `say`, `chat`) — one room at a time, no history on connect, no
context about who the members are. `atomic serve` already renders the realm the agents work in.
Operating the agents belongs on the same surface: watch rooms, open one, speak as the human,
inspect the session behind a member.

## Shape

Four pieces, one feature:

1. **`/bus` page** in the serve SPA: room list (member counts, halted chips), transcript
   (durable-log backfill + live SSE tail), composer (mention dropdown, addressee chips,
   wrapping textarea), halt/resume controls.
2. **`/api/bus/*`** — a thin Go facade over the in-process `internal/bus` client: reads are
   Dial-only (a browser tab never spawns a daemon), operator-intent writes (`join`, `send`) go
   through `EnsureDaemon` so opening a channel works from cold.
3. **Session rail** — the right rail on `/bus` lists each member's Claude Code session, located
   by globbing `~/.claude/projects/*/<session>.jsonl`; clicking renders the transcript as
   markdown (server-side goldmark, same pipeline as pages) in a paginated modal.
4. **`atomic bus read <room> <msg-id>`** — full-text recovery of one envelope from the room
   log; the deterministic answer to notification layers that truncate long messages
   (spec change: `docs/spec/atomic-bus.md`, 2026-08-08 entry).

## Decisions

**Read-only contract.** `docs/spec/atomic-serve.md` declares serve a read-only presentation
layer. The chat adds serve's first POST endpoints. Resolution: the contract is narrowed, not
broken — serve remains read-only *with respect to realm and repo content* (no file it renders
is ever written). Bus messaging is ephemeral coordination state owned by the bus daemon, a
different state domain. Alternative rejected: a tail-only v1 (watch, no send) keeps the letter
of the contract but defeats the purpose — the operator's steering verbs (`say`, halt/resume)
are the point.

**LAN exposure → loopback gate, not a flag.** `atomic serve --host 0.0.0.0` deliberately
exposes the read-only viewer to the LAN. The chat must not ride along: `send`/`say` publish as
the *human* operator, and `say` bypasses halt — a capability escalation over read-only
browsing. Resolution: every `/api/bus/*` request is refused unless its peer address is
loopback, regardless of bind host. Zero configuration; LAN viewers get a clear
"bus chat is loopback-only" error and the page states it. Alternative rejected: an opt-in
`--bus` flag — a flag is configuration for a safety property that code can decide
deterministically from the connection itself.

**Web member identity.** One member per serve instance, session id derived from a hash of the
served directory — stable across restarts so the daemon's rehydrated roster re-attaches the
same member instead of minting `gui-web-2, -3, …` (the pid-based first cut did exactly that).
Position-derived name via `bus.JoinIdentity` (`<realm>-<repo>-web`), `kind: human` — halt
blocks agents, never the operator.

**Composer: textarea, not contenteditable.** Auto-growing textarea (Enter sends, Shift+Enter
newline) plus committed addressee chips gets wrapping and an input-group without
contenteditable's caret/paste/controlled-state edge cases. Chips-in-text inline flow is the
contenteditable v2 if ever wanted.

**Transcripts are Claude Code's internal format.** `.jsonl` lines are unversioned; the parser
tolerates unknown line types, malformed lines, and over-long lines (skips, never fails), and
truncates per-block on render. A future format change degrades rendering quality, never
availability. Session ids are strictly validated before touching the filesystem.

## Non-goals

- **Realm room filtering.** Rooms are free-text names on a single per-user daemon; filtering
  the room list by member positions is a heuristic that hides rooms wrongly. The sessions rail
  already shows each member's position. Revisit if multi-realm usage makes the flat list noisy.
- Multi-user auth on the chat (the bus's own trust model is same-Unix-user; the loopback gate
  preserves exactly that).
- Replaying room history through the daemon (the log backfill reads the file directly; the
  daemon's no-replay contract is untouched).
