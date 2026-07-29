---
id: bus-member-naming-convention
title: 'Bus member naming: position-derived names once scope markers land'
created: "2026-07-29"
origin: |
    live two-session testing, 2026-07-29
kind: plan
review_by: "2026-09-27"
status: open
file: atomic/internal/bus/action.go
---

Blocked on #172 (`.claude/atomic.toml` scope marker).

`atomic bus join --as <name>` takes a model-invented name today. Two gaps:

- A peer running `who` sees `builder`, `fulanito`, `peer1` and cannot tell which session
  is the backend it wants to delegate to. Addressing is exact-match on an arbitrary
  self-description.
- `BusAction(args, home, cwd, out)` already receives `cwd` and explicitly does not use it
  ("accepted for signature parity with WikiAction; nothing in bus needs it today").

Planned once #172 lands:

- `--as` optional, defaulting to the repo basename via `repoctx.Resolve("")`.
- `Member` gains `realm` and `repo`, populated from `where` at join; `who` renders them, so
  peers identify each other by position rather than by guessing a name.
- Envelope gains `from_realm` / `from_repo` so the room log stays unambiguous while names
  stay short enough to type in `--to`.

Open design question, unresolved: owner proposed a single compound name
(`taxgentic-server-admin-ui-agent`); the counter-proposal is short names plus structured
fields. Compound reads better in a bare log line; fields keep `--to` short, let peers filter,
and avoid a name that mutates when an unrelated realm registration appears above the session.
Settle this before implementing.
