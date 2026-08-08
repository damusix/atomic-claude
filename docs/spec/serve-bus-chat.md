# Spec: bus chat in `atomic serve`

Design: `docs/design/serve-bus-chat.md`. Parent contracts: `docs/spec/atomic-serve.md`
(read-only scope amended by CP3 of this spec), `docs/spec/atomic-bus.md` (the `read` verb's
own change is logged there, 2026-08-08).

## Goal

Operate bus rooms from the serve UI: watch any room's traffic live, open a channel (creating
it if absent), speak as the position-named human member with `@fragment` addressing, halt and
resume a room, and open the Claude Code session transcript behind any member — with the whole
chat surface refusing non-loopback requests so `--host 0.0.0.0` never exposes human-kind send
to the LAN.

## Non-goals

- Realm-scoped room filtering (design doc, Non-goals).
- Any change to the daemon protocol or its no-replay contract.
- Auth beyond the loopback gate.

## Success criteria

Serve API (`atomic/internal/serve/api_bus.go`, `api_bus_transcript.go`):

- [ ] `GET /api/bus/{status,rooms,who,log,tail}` are Dial-only: with no daemon they degrade
      (`running:false` / empty / 503 for tail) and never spawn one.
- [ ] `POST /api/bus/join` and the join-if-needed path inside `POST /api/bus/send` go through
      the EnsureDaemon seam (spawn-capable); send retries once through a fresh join when a
      cached membership went stale (`ExitNotJoined`).
- [ ] The web member joins `kind: human`, `mode: participate`, name from `bus.JoinIdentity`
      with role `web`, session id = `serve-web-<sha256(TargetDir)[:8]>` — stable across serve
      restarts, distinct across target dirs.
- [ ] `GET /api/bus/log` returns the last n (≤1000, default 200) parseable envelopes of the
      room log; missing log ⇒ empty list, not an error; malformed lines skipped.
- [ ] `GET /api/bus/tail` streams `data: <envelope-json>` SSE frames from a daemon OpTail
      subscription; the connection closes when the client or the subscription ends.
- [ ] `POST /api/bus/{say,halt,resume,leave}` proxy the matching ops; bus error codes map to
      HTTP (usage→400, no-room/not-joined→404, halted→409, unreachable→503).
- [ ] Every `/api/bus/*` request whose peer address is not loopback is refused with 403 and a
      body naming the loopback-only rule; loopback requests behave identically under
      `--host 127.0.0.1` and `--host 0.0.0.0`. (CP1)
- [ ] `GET /api/bus/sessions?room=` maps each roster member to transcript availability via a
      strict-validated glob of `<home>/.claude/projects/*/<session>.jsonl` (newest match wins).
- [ ] `GET /api/bus/transcript?session=&n=&offset=` renders the window `offset` entries back
      from the tail as HTML-in-JSON (goldmark): role headers with timestamps, thinking
      snippets, tool call/result fences, per-block truncation; memory bounded at `offset+n`
      entries; path-shaped session ids → 400; unknown → 404.

Frontend (`frontend/src/pages/Bus/`, `frontend/src/components/rail/Bus*`):

- [ ] `/bus` route + top-bar entry; the page is full-bleed in `#main-pane` (no reading
      padding), room list polls, transcript backfills from the log then follows the SSE tail,
      deduped by envelope id.
- [ ] Composer: `@` at start opens a member dropdown (click / arrows / Enter / Tab / Escape);
      picked or space-completed mentions become removable chips (backspace on empty pops);
      auto-growing textarea, Enter sends, Shift+Enter newline; Send button keeps its own box.
- [ ] Transcript follow: decided from the position *before* append; parked view gains a
      "↓ latest" button; composer growth re-pins via ResizeObserver while following.
- [ ] Message meta reads `<from> ⟨kind-pill⟩ to <names>` or `fyi` — no bare arrow.
- [ ] Rail on `/bus` lists members with kind/staleness and clickable session chips; the
      transcript modal pages older/newer with an absolute `entries X–Y of T` range.
- [ ] When the loopback gate refuses (non-loopback viewer), the page shows the loopback-only
      notice instead of a broken chat. (CP1)

CLI (`atomic/internal/bus/`):

- [ ] `atomic bus read <room> <msg-id> [--json]` prints one envelope from the room log with
      no daemon round trip; human form never collapses; path-shaped room names rejected;
      exit 5 when the room has no log, exit 2 for an unknown id.

Contract:

- [ ] `docs/spec/atomic-serve.md` states the narrowed read-only scope (read-only w.r.t. realm
      and repo content; bus chat POSTs target the bus daemon's own state domain, loopback-only)
      with a change-log entry. (CP3)
- [ ] Docs wired: `docs/reference/serve.md` chat section, `docs/reference/bus.md` serve-chat
      pointer, README feature row, `/atomic-help` serve topic names `/bus`, CLAUDE.md serve
      clause. (CP3)

## Approach

See the design doc. The experiment code (7 commits, `02eb01e..0335f91`) is the implementation
of everything unmarked; CP1–CP3 below are the productionization delta, and CP2 is the
adversarial review of the whole feature diff against this spec.

## Checkpoints

| # | Scope | Files | Agent | Status |
|---|-------|-------|-------|--------|
| CP1 | Loopback gate on `/api/bus/*` + frontend notice + tests | `serve/api_bus.go`, `serve/api_bus_test.go`, `frontend/src/pages/Bus/Bus.tsx` | atomic-implementer (feature) | done |
| CP2 | Full-diff review of `origin/next..HEAD` against this spec; every finding fixed | (findings-driven) | atomic-reviewer + atomic-implementer (surgical) | done |
| CP3 | atomic-serve.md amendment + reference docs + README + atomic-help + CLAUDE.md | docs, templates | atomic-implementer (feature) | done |

## Risks

| Risk | Mitigation |
|------|------------|
| Transcript `.jsonl` format drift (unversioned internal format) | Tolerant parser: unknown types ignored, malformed/over-long lines skipped, per-block truncation; failure mode is degraded rendering, never an error page. |
| Loopback detection fooled by proxies | Gate reads the TCP peer (`RemoteAddr`), not headers; a local reverse proxy that forwards LAN traffic is an explicit user choice outside serve's threat model — documented in the reference. |
| Room log growth makes backfill slow | Backfill reads the whole log but caps parsed output at n; acceptable for local logs. Revisit with tail-seek if real logs reach tens of MB. |

## Change log

### 2026-08-08 — Implementation log (autopilot run)

CP1 (loopback gate, commit after review-fix round: `feat(serve): loopback-gate the bus chat
API`) — one CHANGES_REQUESTED round: reviewer required a frontend notice test and a poll stop
on block; both fixed in-iteration. CP2 (full-diff review of `origin/next..HEAD`) — findings:
path-traversal via the HTTP log endpoint's room param (fixed: shared requireRoom guard on all
ten room-taking routes), backfill/tail ordering race (fixed: tail opens after backfill
settles), range math duplicated client-side (fixed: firstEntry/lastEntry single-sourced),
unused join `as` plumbing (removed), missing rail tests (added); re-review PASS. Pre-existing
daemon-side room-name validation gap filed as follow-up `bus-daemon-room-name-validation`.
CP3 (contract + docs) — one round: stale embedded bundle flagged, regenerated in-commit by
the pre-commit hook. Verify: full Go suite green, 153 frontend tests + tsc green, render +
bundle parity clean, `atomic validate` exit 0, /atomic-help MISSING-scan clean. All success
criteria boxes above hold as of this entry.

### 2026-08-08 — Initial spec (productionization of the experiment branch)

Captures the experiment's shipped behavior as success criteria, adds the loopback gate (CP1),
the full-diff review (CP2), and the contract/docs wiring (CP3).
