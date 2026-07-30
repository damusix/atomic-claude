---
id: bus-truncated-field-never-set
title: Envelope.truncated is declared but never set
created: "2026-07-30"
origin: |
    found while documenting the large-payload convention
kind: finding
severity: nit
review_by: "2026-09-28"
status: open
file: atomic/internal/bus/protocol.go:186
---

`Envelope.Truncated` is declared in `protocol.go` with a doc comment describing a
notification-cap truncation, but nothing in production code ever sets it — the only
assignment in the repo is the golden wire fixture in `protocol_test.go`. `Log` is
likewise set only by `dropMarkerEnvelope` (subscriber buffer overflow), never for
truncated text.

There is no truncation at all: `Hub.Publish` rejects anything over `MaxTextBytes`
(1 MiB) outright, and delivers everything under it whole.

`docs/reference/bus.md` claimed both fields described a truncation path; that row is
corrected. The dead field remains. Either implement a notification cap that sets it,
or remove it — removal changes the wire shape and so needs a `ProtocolVersion` bump
and a golden-test update.
