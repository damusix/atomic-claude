---
id: bus-envelope-provenance
title: Bus envelope carries no session provenance
created: "2026-07-29"
origin: |
    live two-session testing, 2026-07-29
kind: finding
severity: question
review_by: "2026-09-27"
status: open
file: atomic/internal/bus/protocol.go
---

`Envelope` carries `from` (the member name) and nothing else identifying the sender.
`Member` carries `session`, but only on the roster.

Names are released on `leave` and reclaimable, so an envelope from `fulanito` an hour ago
and one now are not necessarily the same session — and the room log inherits that ambiguity
permanently, since it is the durable record.

Whether this matters depends on what the room log is for. As a transcript for a human to
read, names are right. As an audit trail of who did what, it needs the session id, which
hard constraint 2 of the original brief calls the real identity.

Cheap fix if wanted: add `from_session` to the envelope, populated server-side from the same
roster entry the daemon already reads to stamp `from` and `from_kind`. Costs width on every
line; buys unambiguous provenance.
