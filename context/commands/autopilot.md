---
description: Autonomous delivery: plan, run the implement→review loop, ship. Takes a task or a GitHub issue number and an optional merge verb. Asks one question, how to merge, and only when the verb was not given.
---

You run the whole lifecycle without input, except how to merge. `$ARGUMENTS`: `<task | issue#> [commit | commit push | commit pr | commit merge | commit squash | commit squash merge]`.

<workflow>

## Policy

| Setting | Value |
|---------|-------|
| Writer | `atomic-implementer` |
| Entry | none; you plan it |
| Worktree | auto |
| Stuck | auto |
| Non-blockers | fix-all: every 🟡 and 🔵 goes into the next dispatch; the ledger ends empty |
| Finalize | verify, docs, audit, log, signals, report |
| Scratchpad purpose | `implement` |
| Ship | the merge verb from `$ARGUMENTS`, else ask once |

The ship gate is the only `AskUserQuestion` in the run. Anything else that would prompt becomes a judgment call recorded in `STATE.md`; a true blocker halts and surfaces.

## Scratch hygiene

`rm` and chained commands (`&&`, `;`) trigger permission prompts that stall an unattended run. `mkdir -p tmp/trash` once; move scratch there instead of deleting; one command per Bash call. Every implementer brief includes: `Discard scratch by moving it to tmp/trash/; never rm; do not chain shell commands.` Deletion happens once, in Ship.

## Resolve

Bare `N` or `#N` → `gh issue view N --json title,body,labels` is the task. Derive the topic slug. Note the merge verb.

## Plan

Follow `/atomic-plan`'s discipline with no approval gate: trivial → inline spec; otherwise `docs/design/<topic>.md` and `docs/spec/<topic>.md`. Verify assumptions against primary sources now; you cannot ask later. The spec body stays current before every dispatch: revise it and log the change; leave no superseded content. A subagent that writes the spec or design is briefed to follow `rules/specs/spec-currency.md`.

{{ template "worktree-setup" . }}

{{ template "implement-loop" . }}

{{ template "loop-finalize" . }}

## Ship

Run the merge verb from `$ARGUMENTS`. Without one, ask:

    <topic> is built, reviewed, and green. How should it ship?
    /commit | /commit push | /commit squash merge | /commit merge | /commit pr

The ship verb handles message format, worktree cleanup (auto-confirm on `merge` and `squash merge`), and its own signals gate, which sees a fresh signals file and does nothing.

Extend the finalize report with strategist dispatches and their findings, judgment calls from `STATE.md`, and the merge result. Then `rm -rf tmp/trash`, the one expected permission prompt; if permission is not granted, leave it (gitignored). `$SCRATCH` stays for `/git-cleanup`.

</workflow>
