<!--
Template for $SCRATCH/STATE.md — emitted by `atomic template state`. The append-only iteration log kept by the loop
orchestrator (/subagent-implementation, /atomic-plan spec loop, /subagent-diagnose).
Copy this body, fill every <angle-bracket> placeholder, delete this comment.
Append one `## Iteration N` entry per implement→review cycle; never rewrite prior entries.
Capture `git rev-parse HEAD` as the loop base SHA before the first entry — it is the
from-sha for the range-scoped signals refresh at finalize.
/subagent-diagnose variant: precede Iteration 1 with `## Iteration 0 — baseline`
carrying `top_level_error: <value>` + `normalized_hash: <first 12 chars of sha256>`.
-->
# State: <topic>

Loop base SHA: <git rev-parse HEAD>

## Iteration 1 — <date>
- Implementer: <one-line summary>
- Reviewer: <verdict + key findings>
- Decisions: <anything load-bearing>
- Commit: <sha or "deferred">
