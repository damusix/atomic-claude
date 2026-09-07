{{- define "implement-loop" -}}
<implement-loop>

## Index

`test -f .claude/.atomic-index/atomic.db` → present: `atomic code sync`; absent: `atomic code index` after a one-line notice. Skip silently when `atomic` is missing or errors. The loop never blocks on the index.

## Scratchpad

Derive a kebab-case `<topic>` from the spec filename or the task.

    command -v atomic >/dev/null 2>&1 && atomic repo init >/dev/null
    SCRATCH=$(atomic scratchpad new "<topic>" --purpose <implement|fix>)

Paths come from `atomic scratchpad` / `atomic where --json`; if what is on disk does not match, run `atomic migrate --show-log`. Seed three files from `atomic template brief|state|followups`; fill every `<placeholder>`, delete the guidance comment.

| File | Rule |
|------|------|
| `BRIEF.md` | This iteration's scope, success criteria, and reviewer feedback. Overwrite each iteration. Without a spec the `**Spec:**` line reads `no spec — inline brief in BRIEF.md`. |
| `STATE.md` | `Loop base SHA: $(git rev-parse HEAD)` before the first entry. One `## Iteration N` per cycle; never rewrite a prior entry. |
| `FOLLOWUPS.md` | Non-blocking findings (🟡 that did not drive the verdict, 🔵, ❓) as `F-N`, numbered across severities. Append after every reviewer pass, PASS included. Readability 🟡 is never recorded here; it blocks the iteration. |

If `atomic prompt` or `atomic template` fails, stop and report the error. Never inline a prompt or improvise a skeleton.

## Iterate

One iteration per checkpoint. Checkpoints come from the spec's table when one exists, else from the brief. Repeat until the reviewer passes the last one.

### Implement

Writer `atomic-implementer` → dispatch it (`subagent_type: "atomic-implementer"`), fresh context. Mode `surgical` when the iteration touches at most 2 non-test files and is mechanically obvious, else `feature`. Prompt from `atomic prompt implementer`, substituting `{SCRATCH_PATH}`, `{SPEC_PATH}`, `{MODE}`, `{ITERATION_SCOPE}`, `{REVIEWER_FEEDBACK}` (`N/A — first iteration` on the first), `{BASE_SHA}` = HEAD now.

Writer `main agent` → write the checkpoint yourself under the `atomic-tdd` skill, run the project's signals, record the commands and results in `STATE.md`. Stay inside the checkpoint.

### Review

Dispatch `atomic-reviewer` (`subagent_type: "atomic-reviewer"`), fresh context, code mode. Prompt from `atomic prompt reviewer`, substituting `{SCRATCH_PATH}`, `{SPEC_PATH}`, `{BASE_SHA}`. The iteration is uncommitted at this point; the reviewer diffs the working tree against `{BASE_SHA}`. Attach the implementer's report, or say `main agent wrote this`.

### Triage

1. Read the `VERDICT:` line. Append the iteration to `STATE.md`: built, findings, next focus.
2. Non-blockers `ledger` → harvest non-blocking findings into `FOLLOWUPS.md`. Non-blockers `fix-all` → nothing goes to the ledger: on `CHANGES_REQUESTED` every finding is the next focus; on `PASS` with open 🟡 or 🔵 findings, run one more implement→review round on them before committing.
3. `PASS` → Commit. `CHANGES_REQUESTED` → stuck check, then loop with every 🔴 and every readability 🟡 as the next focus. Readability findings are fixed, never deferred.

**Stuck check.** Two consecutive `CHANGES_REQUESTED` on the same underlying blocking signal (same root failure, however the reviewer phrases it):

- Stuck `ask` → `AskUserQuestion`: continue / `/pressure-test @docs/spec/<topic>.md` (`/pressure-test <task>` without a spec) / dispatch `atomic-strategist` (read-only RCA). Record the choice in `STATE.md`. A strategist run feeds the next `BRIEF.md` and is not an iteration.
- Stuck `auto` → dispatch `atomic-strategist` without asking; fold its findings into the next `BRIEF.md`.

The check resets when the blocking signal changes.

### Commit

1. Message: the implementer's `## Commit` proposal (the reviewer already checked it), else the `atomic-git-discipline` skill.
2. Stage the touched files by explicit path. No `-A`.
3. Commit via HEREDOC. Record the SHA in `STATE.md`.
4. `atomic code sync` when the index exists; skip silently on error.

Skip only when the iteration produced no diff, and say so in `STATE.md`.

</implement-loop>
{{- end -}}
