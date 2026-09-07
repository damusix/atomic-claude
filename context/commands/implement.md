---
description: Implement in the main agent, with atomic-reviewer gating every checkpoint. For work whose context is already in this conversation, where a fresh subagent would have to re-read what you have already read. Same checkpoints, commit-per-green, and finalize as /subagent-implementation; you write the code.
---

You write the code here. The reviewer dispatch after each checkpoint is the only independent review the work gets; it is never skipped, batched to the end, or replaced by your own suite run.

`$ARGUMENTS`: `[<task>]`. Empty is normal; the task is already in the conversation.

<workflow>

## Policy

| Setting | Value |
|---------|-------|
| Writer | main agent |
| Entry | hand-off table, plus: the context is not already here (cold start, resumed session, unread surface), or the work would crowd it out → `/subagent-implementation` |
| Worktree | ask, only when the tree is clean; otherwise say `dirty tree, staying in place` |
| Stuck | ask |
| Non-blockers | ledger |
| Finalize | verify, docs, audit, follow-ups, log, signals, report |
| Scratchpad purpose | `implement` |
| Ship | no |

{{ template "handoff" . }}

## Checkpoints

Declare them before writing code and state the list to the user. A spec's checkpoint table is the list (fix its body first when this conversation superseded it). Without a spec, split the task into checkpoints, one logical change each, however many files. More than six means the work belongs in `/subagent-implementation`. Mid-loop, little remaining context or a checkpoint list that has grown past the declaration → hand off to `/subagent-implementation` (`STATE.md` records the checkpoints and SHAs).

{{ template "worktree-setup" . }}

{{ template "implement-loop" . }}

{{ template "loop-finalize" . }}

</workflow>
