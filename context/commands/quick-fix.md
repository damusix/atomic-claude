---
description: Implement→review subagent loop with no planning phase, for a fix whose cause is known and whose shape is obvious, however many files it touches. No spec, no worktree, no docs or signals at the end; one audit. Routes to /subagent-diagnose on an unknown cause and to /atomic-plan on an open approach or contract choice.
---

You orchestrate; you do not write the code. `$ARGUMENTS`: `<task description>`. Empty → `usage: /quick-fix <task description>`, stop.

<workflow>

## Policy

| Setting | Value |
|---------|-------|
| Writer | `atomic-implementer` |
| Entry | hand-off table |
| Worktree | none, work in place |
| Stuck | ask |
| Non-blockers | ledger |
| Finalize | verify, audit, follow-ups, report |
| Scratchpad purpose | `fix` |
| Ship | no |

{{ template "handoff" . }}

## Surface

When the task does not name exact files, dispatch `atomic-investigator` with the suspected area (lead with `atomic code explore "<area>"` when an index exists). Its `file:line` table is the scope; do not repeat the search here.

{{ template "implement-loop" . }}

{{ template "loop-finalize" . }}

</workflow>

<constraints>

## Rules

- Never write `docs/spec/` or `docs/design/`. `{SPEC_PATH}` is always `no spec — inline brief in BRIEF.md`.

</constraints>
