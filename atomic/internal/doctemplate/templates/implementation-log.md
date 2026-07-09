<!--
Template for the `## Implementation log` section appended to the END of
docs/spec/<topic>.md — emitted by `atomic template implementation-log` — at loop finalize (/subagent-implementation Phase 3; /autopilot
inherits). Copy this body, fill every <angle-bracket> placeholder, delete this comment.
Append a new `###` entry per build; never rewrite prior entries. Pull commit SHAs from
STATE.md; out-of-scope and unforeseens from STATE.md decision lines; deferred items from
FOLLOWUPS.md dispositions. One line each — the log is a navigation aid, not a narrative.
If the spec is dead, still write the entry with status `abandoned — <date>` and one line
on why.
-->
## Implementation log

### <version-or-status> — <date>

Built across <N> iterations of /subagent-implementation. Commits (chronological):

- `<sha>` — CP-1 <one-line>
- `<sha>` — CP-2 <one-line>

**Out-of-scope work performed during this build:**

- <what + why it ended up in scope> (or "none")

**Unforeseens — surprises that emerged during implementation:**

- <surprise + how it was handled> (or "none")

**Deferred items still open:**

- <link to FOLLOWUPS triage decisions, tracked issues, or "none">
