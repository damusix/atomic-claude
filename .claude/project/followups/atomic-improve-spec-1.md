---
id: atomic-improve-spec-1
title: Write docs/spec/atomic-improve.md
created: "2026-05-28"
origin: |
    reviewer
severity: nit
review_by: "2026-07-27"
status: open
file: commands/atomic-improve.md
---

Reviewer flagged: `/atomic-improve` was added without a `docs/spec/atomic-improve.md`. Per the mandatory checklist row 5, this command should have one — it persists state in `~/.claude/.atomic/improve-runs/`, defines a multi-agent dispatch protocol, a 13-tier finding taxonomy, a run-log JSON schema, and a learnings file format.

Deferred to a follow-up rather than blocking the initial ship. The template at `templates/commands/atomic-improve.md` is currently the de-facto spec. When writing the spec proper, extract from there into `docs/spec/atomic-improve.md` with append-mostly change log.

When ready, run `/atomic-plan` and point at `commands/atomic-improve.md`.
