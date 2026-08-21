---
description: Run the implement→review subagent loop without the planning phase — for straightforward fixes with a known cause and one obvious approach, regardless of how many files the change spreads across. Skips the spec gate, worktree gate, and finalize ceremony that /subagent-implementation carries. Not for unknown root causes (/subagent-diagnose territory) or genuine uncertainty (/atomic-plan territory) — the fit gate and mid-loop escape hatch route there on uncertainty signals, never on file count.
---

You are the **orchestrator**. The user has handed you a task whose cause is known and whose fix has one obvious shape. You will NOT implement it yourself — you drive the same fresh-context implement→review loop as `/subagent-implementation`, minus the spec gate, the worktree gate, and the finalize ceremony. Speed is the point: no spec authored, no worktree created, no implementation log, no docs/signals refresh at the end.

`$ARGUMENTS`: `<task description>`. If empty, refuse: `usage: /quick-fix <task description>`. Stop.

<workflow>

## Fit gate

Before dispatching anything, check whether this task actually fits `/quick-fix`. HOLD means proceed; EXIT means stop and hand off before any subagent runs.

| Signal | Verdict | Why |
|--------|---------|-----|
| Root cause is known; one fix shape is obvious | HOLD | exactly what `/quick-fix` is for |
| Change spans many files but is one cohesive, mechanically obvious slice (e.g. one param threaded through DTO → validator → controller → repo → client) | HOLD | cohesion-bounded, not file-count-bounded — axiom 1 |
| Root cause is unknown or unclear | EXIT → `/subagent-diagnose` (or the `atomic-debug` skill) | `/quick-fix` assumes the cause is already known; diagnosing an unknown cause is different work |
| Multiple viable approaches to the fix | EXIT → `/atomic-plan` | needs a decision, not just execution |
| Success criteria are fuzzy or debatable | EXIT → `/atomic-plan` | needs criteria fixed before dispatch |
| An architectural or contract choice is implied (new public API, schema/migration, cross-service contract change) | EXIT → `/atomic-plan` | needs design, not just a fix |

On EXIT: state the signal that fired in one line, print the handoff text from the Escape hatch section below, and stop before any dispatch. No numeric file threshold appears anywhere in this gate.

## Phase 0 — Optional surface mapping

Skip when the task names exact files and there's nothing to locate (e.g. "fix the null check in src/auth/token.ts:42"). Otherwise dispatch `atomic-investigator` — Haiku-backed and read-only, so it's cheap — with a focused brief naming the suspected area. When a code-intel index is warm, tell it to lead with `atomic code explore "<surface-area query>"` before targeted verbs or `sg`/`grep`. Use its `file:line — what` table as scoping evidence; do not duplicate the search in the main context.

## Code-intel index

Before writing the scratchpad, make sure the index is current:

```bash
test -f .claude/.atomic-index/atomic.db
```

- **Warm (DB exists):** run `atomic code sync`. Skip silently if `atomic` is absent or errors.
- **Cold (no DB):** proceed degraded — do **not** build the index here. Speed is the point of this command; a full `atomic code index` run belongs to `/subagent-implementation` or `/refresh-wiki`, not a quick fix. Subagents fall back to `sg`/`grep` automatically.

## Scratchpad

Derive a short kebab-case `<topic>` slug from the task.

```bash
command -v atomic >/dev/null 2>&1 && atomic repo init >/dev/null
SCRATCH=$(atomic scratchpad new "<topic>" --purpose fix)
```

Same trio as `/subagent-implementation`, and this command reuses the exact same `atomic prompt implementer` / `atomic prompt reviewer` briefs — the two command files are co-consumers of one scratchpad contract; a future shape change to either should update both. Paths come from `atomic scratchpad` / `atomic where --json`; if what you find on disk does not match, run `atomic migrate --show-log` for the change history.

### `$SCRATCH/BRIEF.md`

Seed via `atomic template brief > "$SCRATCH/BRIEF.md"`, fill every `<angle-bracket>` placeholder, delete the guidance comment. The `**Spec:**` line becomes the literal string `no spec — inline brief in BRIEF.md` — this command never writes `docs/spec/` or `docs/design/`. State success criteria explicitly from the task; don't leave them implicit. Refreshed each iteration — overwrite, don't append.

### `$SCRATCH/STATE.md`

Seed via `atomic template state`. Before the first entry, capture `git rev-parse HEAD` and record it as the loop base SHA. Append one `## Iteration N` entry per cycle; never rewrite prior entries.

### `$SCRATCH/FOLLOWUPS.md`

Seed via `atomic template followups` on first iteration. Ledger of non-blocking reviewer findings (🟡 risk / 🔵 nit / ❓ question) — append after every reviewer pass, even on `PASS`.

## Loop — implement → review → commit

Cap: 3 iterations. Repeat until the reviewer signs off, the escape hatch fires, or the cap is hit.

### Dispatch implementer

- **`atomic-implementer (mode: surgical)`** when this iteration's scope touches ≤2 files and is mechanically obvious (typo, single-fn rewrite, rename, single-callsite fix).
- **`atomic-implementer (mode: feature)`** for a cohesive multi-file slice — however many files.
- **`general-purpose`** as fallback if neither fits.

This mirrors `/subagent-implementation` Step A's heuristic verbatim — it is agent selection for cohesion-fit, not a scope cap on `/quick-fix` itself.

Build the prompt by running `atomic prompt implementer` and substituting:

| Placeholder | Value |
|------------|-------|
| `{SCRATCH_PATH}` | absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | `"no spec — inline brief in BRIEF.md"` |
| `{ITERATION_SCOPE}` | this iteration's scope from `BRIEF.md` |
| `{REVIEWER_FEEDBACK}` | findings from `STATE.md`, or `"N/A — first iteration"` |
| `{BASE_SHA}` | current HEAD SHA before this iteration |

### Dispatch reviewer

`subagent_type: "atomic-reviewer"`. Build the prompt by running `atomic prompt reviewer` and substituting:

| Placeholder | Value |
|------------|-------|
| `{SCRATCH_PATH}` | absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | `"no spec — inline brief in BRIEF.md"` |
| `{BASE_SHA}` | HEAD before this iteration |
| `{HEAD_SHA}` | HEAD after the implementer's work |

### Triage

- Parse the `VERDICT:` line.
- Update `STATE.md`: iteration number, implementer summary, reviewer findings, next-iteration focus.
- Harvest non-blocking findings (🟡 / 🔵 / ❓ that didn't block `PASS`, or `CHANGES_REQUESTED` items the next iteration won't address) into `FOLLOWUPS.md` as `F-N` entries.
- Implementer reports `BLOCKED` or `NEEDS_CONTEXT` → escape hatch fires unconditionally, regardless of iteration count.
- A reviewer finding surfaces an escape-hatch signal (approach fork, criteria dispute, contract choice, shifted root cause) → escape hatch fires.
- `PASS` → commit (below), then check the brief for more scope; if none remains, go to Finalize.
- `CHANGES_REQUESTED` with no escape signal → loop back to the implementer with the blocking findings as focus.

### Commit per green iteration

1. Invoke the `atomic-git-discipline` skill for message format.
2. Stage only the files named in the implementer's `## Did` section — explicit paths, no `-A`.
3. Commit via HEREDOC. Conventional Commits format. No AI bylines.
4. Record the commit SHA in `STATE.md`.
5. If `.claude/.atomic-index/atomic.db` exists, run `atomic code sync`. Skip silently if absent or it errors.

## Escape hatch

Fires the moment any of these signals appears — at the fit gate or mid-loop:

- Multiple viable approaches surface (the fix could be built two different ways and nothing in the brief decides between them).
- Success criteria turn fuzzy or become actively debated.
- An architectural or contract choice appears (new public API, schema/migration, cross-service contract change) that wasn't visible at the fit gate.
- The root cause shifts — what looked like the cause turns out wrong, or a second distinct cause surfaces.
- The implementer reports `BLOCKED` or `NEEDS_CONTEXT` — this forces the stop unconditionally; no judgment call.

On fire:

1. Stop the loop immediately. Do not dispatch another iteration.
2. Name the signal that fired, in one line.
3. Print the handoff:

    ```
    Escape hatch: <signal that fired>.

    /quick-fix assumes a known cause and one obvious approach — that assumption
    just broke. Handoff options:

    /subagent-implementation <task>   same loop, with a spec gate; its own inline
                                       path may still apply if this turns out small
    /atomic-plan <task>               only if this is genuinely non-trivial (new
                                       pattern, real ambiguity, worth a design doc)

    Scratchpad retained at <SCRATCH> — BRIEF.md and STATE.md carry the context forward.
    ```

4. Retain `$SCRATCH` — do not delete it.

## At iteration cap

3 iterations without a `PASS` verdict: stop, don't silently run a 4th. Ask via `AskUserQuestion`:

- `continue` — run another iteration anyway
- `escalate to /subagent-implementation` — hand off with the scratchpad intact
- `stop` — halt, retain the scratchpad, report where it left off

## Finalize

Once the reviewer says `PASS` and there's no more scope in the brief:

1. Invoke the `atomic-verify` skill — the orchestrator re-runs the full signal suite itself; do not trust subagent claims at the finish line.
2. Surface `FOLLOWUPS.md` to the user, per open `F-N` entry. Four dispositions, same as `/subagent-implementation`:
    - **`fix-now`** — run another iteration to address it.
    - **`defer`** — promote to `.claude/project/followups/<id>.md` via `atomic followups add` (see `/subagent-implementation`'s Phase 3 defer mechanics for the exact args and commit step).
    - **`issue`** — file as a tracked GitHub issue via `/report-issue`.
    - **`drop`** — discard; state the reason.
3. Delete `$SCRATCH` — only after the user has dispositioned every `FOLLOWUPS.md` entry.
4. Report to the user: what shipped, iteration + commit SHAs, what was verified, what `FOLLOWUPS.md` entries were dispositioned.

No implementation log, no `/documentation` dispatch, no signals refresh here — those belong to the finalize ceremony this command deliberately skips. Ship verbs' `signals-gate` covers ad-hoc commits; the user runs `/documentation` and `/commit` when ready.

</workflow>

<constraints>

## Rules

- Orchestrator does not implement. Only scratchpad writes, state updates, commits per `PASS`, final verify.
- Every subagent dispatch is fresh context. The brief in `BRIEF.md` is the only handoff.
- Reviewer and implementer are always separate agents. Never combine roles.
- No spec or design is ever written by this command. `{SPEC_PATH}` is always the literal string `"no spec — inline brief in BRIEF.md"`.
- No worktree gate — this command assumes work happens in place.
- No numeric file-count threshold gates scope or triggers the escape hatch anywhere in this command. The surgical-vs-feature agent choice in the Loop section is cohesion-fit selection, not a scope cap.
- If the implementer reports `BLOCKED`/`NEEDS_CONTEXT`, the escape hatch fires unconditionally — never loop past it silently.
- Subagent prompts and document skeletons both come from the binary (`atomic prompt implementer|reviewer`; `atomic template brief|state|followups`). If either verb fails, stop: `implementer/reviewer prompt unavailable (atomic prompt <name> failed) — install/update the atomic binary. cannot proceed.` — rather than inlining prompts or improvising structure.
- This command and `/subagent-implementation` are co-consumers of the same `atomic prompt implementer` / `atomic prompt reviewer` briefs and the same scratchpad trio contract — a shape change to either should update both command files.
- Do not push, merge, or open a PR. Ship is `/commit`.

</constraints>
