---
description: Implement in the main agent, with a reviewer gate after every checkpoint. For work whose context is already in this conversation — files read, approach settled — where a fresh-context subagent would discard what you already know. Same checkpoint discipline and commit-per-green as /subagent-implementation, but you write the code. Finalizes with a signals refresh and one strong final gate you choose.
---

You implement this task yourself, in this context. That is the whole point of this verb: you have already read the files, settled the approach, and argued the tradeoffs with the user, and a fresh-context subagent would throw all of that away and pay to rebuild it.

What you do not get for free is a reader who is not you. The subagent loop gets that from its structure — a separate reviewer sees every iteration. Here it is a step you run deliberately, after every checkpoint, because the agent that wrote the code is the worst judge of it: it reviews its own reasoning rather than the diff, and every rationalization that produced a bug is still in its context.

`$ARGUMENTS`: `[<task description>] [auditor | strategist | reviewer]`. Both parts are optional. An empty task description is normal — the conversation already carries it. A trailing agent token picks the final gate and skips that prompt.

<workflow>

## Fit gate

Check whether this task belongs in the main agent before writing a line. HOLD means proceed; EXIT means hand off before any code is written.

| Signal | Verdict | Why |
|--------|---------|-----|
| The context for this work is already here — files read, cause known, approach settled in this conversation | HOLD | that context is the asset this verb exists to spend |
| A spec exists and you wrote or reviewed it in this session | HOLD | its checkpoints are already in context; re-reading them costs nothing |
| The context is not here — cold start, resumed session, or you would have to go read the surface area first | EXIT → `/subagent-implementation` | nothing to preserve, so the loop's fresh contexts are the cheaper way to buy it |
| The work is large enough that implementing it inline would crowd out the context that made this the right verb | EXIT → `/subagent-implementation` | main context is the resource being spent; spend it on judgment, not on file bodies |
| Root cause is unknown or unclear | EXIT → `/subagent-diagnose` (or the `atomic-debug` skill) | this verb assumes you already know what to build |
| Multiple viable approaches, or success criteria are fuzzy | EXIT → `/atomic-plan` | needs a decision, not execution |

On EXIT: name the signal in one line, print the handoff, and stop before writing code.

## Checkpoints

Declare the checkpoints before implementing. Without them, "review after each checkpoint" has no anchor and the reviewer fires once at the end, which is the failure mode this verb is built to avoid.

- **A spec exists at `docs/spec/<topic>.md`** → its checkpoint table is the list. Confirm the body reflects the current decision; if this conversation superseded any part of it, fix the spec first, then implement from the corrected version.
- **No spec** → declare the checkpoints yourself, from the task. Each one is a cohesive slice: one logical change, however many files it touches. Never split a coherent slice to make a diff smaller, and never merge two unrelated slices to save a review round.

State the list to the user before starting. Three to six checkpoints is the usual shape; more than that is a signal the fit gate missed, and it should go to `/subagent-implementation`.

## Worktree

Run the worktree gate only when the working tree is clean. When it is dirty, skip it and say so in one line: entering a worktree moves the session while uncommitted edits stay behind, and this verb is specifically for work already in flight.

{{ template "worktree-setup" . }}

## Code-intel index

```bash
test -f .claude/.atomic-index/atomic.db
```

- **Warm** → run `atomic code sync`. Skip silently if `atomic` is absent or errors.
- **Cold** → run `atomic code index` after a one-line "Building code index…" notice. Do not prompt; indexing is cheap and idempotent.

Lead with `atomic code explore "<query>"` when you need orientation on a surface you have not opened yet. A missing index never blocks the work.

## Scratchpad

Derive a short kebab-case `<topic>` slug from the task.

```bash
command -v atomic >/dev/null 2>&1 && atomic repo init >/dev/null
SCRATCH=$(atomic scratchpad new "<topic>" --purpose implement)
```

Write the same trio as `/subagent-implementation`. The difference is who reads it: there is no implementer subagent here, so `BRIEF.md` exists for the reviewer and the final gate, not for a handoff.

- **`$SCRATCH/BRIEF.md`** — seed via `atomic template brief`, fill every `<angle-bracket>` placeholder, delete the guidance comment. It carries the current checkpoint's scope and the success criteria the reviewer measures against, so refresh it each checkpoint — overwrite, don't append. Without a spec, the `**Spec:**` line is the literal string `no spec — inline brief in BRIEF.md`.
- **`$SCRATCH/STATE.md`** — seed via `atomic template state`. Record `git rev-parse HEAD` as the loop base SHA before the first entry; it is the from-sha for the range-scoped signals refresh and the final gate. Append one `## Checkpoint N` entry per cycle; never rewrite prior entries.
- **`$SCRATCH/FOLLOWUPS.md`** — seed via `atomic template followups`. Ledger of non-blocking reviewer findings (🟡 risk that did not drive the verdict, 🔵 nit, ❓ question). Readability 🟡 never lands here — it blocks. Append after every reviewer pass, including a `PASS`.

## Per checkpoint — implement → verify → review → commit

Repeat for each declared checkpoint.

### 1. Implement

Write the code yourself. The `atomic-tdd` skill governs how: failing test first, then the implementation that passes it. Stay inside the checkpoint's scope — work that belongs to a later checkpoint waits for its own review round.

### 2. Verify

Run the project's own signals before dispatching anyone: typecheck, tests, lint, build. Record the exact commands and results in `STATE.md`. A checkpoint that does not pass its own suite is not ready for review, and sending it anyway spends a dispatch to be told what you already knew.

### 3. Review

Dispatch `atomic-reviewer` (`subagent_type: "atomic-reviewer"`) in code mode. This is not optional and it is not batched to the end — one dispatch per checkpoint.

Build the prompt by running `atomic prompt reviewer` and substituting:

| Placeholder | Value |
|-------------|-------|
| `{SCRATCH_PATH}` | absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | absolute path to `docs/spec/<topic>.md`, or `"no spec — inline brief in BRIEF.md"` |
| `{BASE_SHA}` | HEAD before this checkpoint |
| `{HEAD_SHA}` | HEAD after your work on it |

Tell the reviewer the code came from the main agent rather than an implementer subagent, so it reads the diff against the brief instead of hunting for an implementer report that does not exist.

### 4. Triage

- Parse the `VERDICT:` line. Update `STATE.md` with the checkpoint number, what you built, the reviewer's findings, and the focus for the next round.
- Harvest non-blocking findings into `FOLLOWUPS.md` as `F-N` entries — `path:line`, severity emoji, problem, suggested fix, origin checkpoint. Numbering runs sequentially across severities.
- **`VERDICT: CHANGES_REQUESTED`** → fix every 🔴 and every readability 🟡 (comment noise, over-engineering, repetition) yourself, then re-dispatch the reviewer on the updated diff. Do not commit around a red finding, and do not defer a readability finding to the ledger.
- **`VERDICT: PASS`** → commit, then move to the next checkpoint.

**Stuck check.** When two consecutive `CHANGES_REQUESTED` rounds carry the same underlying blocking signal — the same root failure, however the reviewer phrases it — stop and surface the choice via `AskUserQuestion`: continue the loop, run `/pressure-test` on the spec, or dispatch `atomic-strategist` for read-only root-cause analysis. Never auto-dispatch the strategist; it is expensive and the user opts in. Record the choice in `STATE.md`. The check resets when the blocking signal changes.

### 5. Commit the green checkpoint

1. Invoke the `atomic-git-discipline` skill for the message.
2. Stage only the files this checkpoint touched — explicit paths, no `-A`.
3. Commit via HEREDOC. Conventional Commits. No AI bylines.
4. Record the SHA in `STATE.md` under the checkpoint's `Commit:` line.
5. If `.claude/.atomic-index/atomic.db` exists, run `atomic code sync` so the next checkpoint's reviewer queries current state. Skip silently on absence or error.

Each checkpoint is bisectable, and the next review diffs against the prior commit rather than the merge base.

## Escape hatch

Fires mid-loop the moment any of these appears:

- Your remaining context is thin enough that you are working from memory of files rather than their contents.
- The checkpoint list has grown past what you declared, and the new work is not a variation on it.
- A second distinct root cause surfaces, or the one you were building against turns out wrong.
- An architectural or contract choice appears that the fit gate did not see — new public API, schema migration, cross-service contract.

On fire: stop, name the signal in one line, and print the handoff.

```
Escape hatch: <signal that fired>.

/implement assumes the context is here and the work fits inside it — that
just stopped being true. Handoff:

/subagent-implementation <task>   same checkpoint discipline, fresh contexts per
                                  iteration, no dependence on this conversation
/atomic-plan <task>               if the approach itself is now in question

Scratchpad retained at <SCRATCH> — STATE.md carries the checkpoints and SHAs forward.
```

Retain `$SCRATCH`. The next verb reads it.

## Finalize

Once the last checkpoint is green:

1. **Verify.** Invoke the `atomic-verify` skill and run the full suite yourself. When the work touched `docs/spec/**`, `docs/design/**`, or bundled artifacts, also run `atomic validate spec` and `atomic validate config` (skip silently if `atomic` is not on PATH).

2. **Disposition `FOLLOWUPS.md`.** List every open `F-N` entry and ask the user per entry. Four dispositions: `fix-now` (another checkpoint round), `defer` (promote to `.claude/project/followups/<id>.md` via `atomic followups add` — see `/subagent-implementation`'s Phase 3 defer mechanics for the args and the commit step), `issue` (`/report-issue`), `drop` (state the reason). Don't auto-decide.

3. **Write the implementation log** when a spec exists. Append an `## Implementation log` section to `docs/spec/<topic>.md` using `atomic template implementation-log` as the structural contract. Pull SHAs from `STATE.md`, deferrals from `FOLLOWUPS.md`, and the user's dispositions from step 2. Without a spec, skip this — there is nowhere durable to put it.

4. **Update documentation** by invoking `/documentation`.

5. **Final gate.** One strong reader over the whole delivered range, in a fresh context that never saw your reasoning. Pick from the `$ARGUMENTS` token if one was given; otherwise ask via `AskUserQuestion`:

    | Choice | Agent | What it catches | Returns |
    |--------|-------|-----------------|---------|
    | `auditor` (recommended) | `atomic-auditor` | cumulative spec compliance, cross-checkpoint coherence, commit-message soundness, doc adherence — the four things per-checkpoint review structurally cannot see | a verdict |
    | `reviewer` | `atomic-reviewer` | line-level correctness across the full range in one pass | a verdict |
    | `strategist` | `atomic-strategist` | whether the approach was right at all, and what it assumed | a recommendation, no verdict |

    Dispatch it once, with everything it needs: `spec: docs/spec/<topic>.md` (or `brief: $SCRATCH/BRIEF.md` when there is none), `range: <loop-base>..HEAD`, `state: $SCRATCH/STATE.md`, `scratch: $SCRATCH`, and the `## Documentation surfaces` table if the project has one.

    **Acting on the result depends on which agent ran.** The auditor and the reviewer end on a verdict line: `VERDICT: PASS` → continue; `VERDICT: CHANGES_REQUESTED` → fix the findings yourself, commit, then continue. The strategist gates nothing and emits no verdict — it is read-only and refuses PASS/CHANGES_REQUESTED by its own scope boundary, so never parse its output for one. Read its risks and recommendation, act on what you agree with, and record in `STATE.md` what you declined and why.

    **Dispatch the final gate exactly once**, whichever it is. Re-gating after the fix makes finalize unbounded.

6. **Refresh signals for the range.** This runs once, here — the implementation phase owns the refresh, not the ship verb.

    1. `command -v atomic` returns nothing → skip.
    2. Run `atomic signals stale`. Exit 0 → skip. Exit 2 → report the error and skip.
    3. Exit 1 → dispatch `atomic-wiki-inferrer` with `mode: silent`, `first_run: false`, and `changed_range: <loop-base>..HEAD`. Run `atomic wiki mark-dirty` best-effort afterward.
    4. Stage `docs/wiki/*.md`, then the per-domain pointer cards guarded on the directory existing: `[ ! -d .claude/rules/wiki ] || git add -A .claude/rules/wiki/`. Check for an ignore-file edit mechanically with `git status --short -- .gitignore .claude/.gitignore` and stage whichever path reports modified. Commit `chore(signals): refresh after <topic>` and record the SHA in `STATE.md`.

    The refresh runs after the final gate so the one fix round is already inside the range it scans.

7. `$SCRATCH` stays. A bundle is retired by `atomic scratchpad archive <slug>`, driven by `/git-cleanup` reaping its worktree or branch, never by this command.

8. **Report**: what shipped, checkpoints with their commit SHAs, what was verified and with which commands, the final gate's verdict, how each `FOLLOWUPS.md` entry was dispositioned, and what is left.

Do not push, merge, or open a PR. The user picks how to ship.

</workflow>

<constraints>

## Rules

- You write the code. That is the difference between this verb and `/subagent-implementation`, and the reason the fit gate exists — if the context is not already here, the premise is false and the handoff is the right move.
- The reviewer dispatch after each checkpoint is not optional and never batched to the end. **Why:** it is the only independent read the work gets before it lands, and its value comes from firing while a checkpoint is small enough to fix.
- Never review your own checkpoint in place of the dispatch. Your own suite run is evidence, not review.
- Readability findings are fixed, never deferred. 🔴 blocks the commit. **Why:** the ledger is for deliberate decisions, not for findings you would rather not act on.
- Checkpoints are declared before implementing, not discovered while writing. **Why:** an undeclared checkpoint boundary means the reviewer fires once at the end, which is the gate this command exists to move earlier.
- The final gate runs exactly once, whichever agent the user picks, and only the auditor and the reviewer are read for a verdict. **Why:** `atomic-strategist` is read-only by contract and refuses to gate a diff with PASS/CHANGES_REQUESTED; parsing its output for one stalls finalize on a refusal.
- This command and `/subagent-implementation` are co-consumers of the same `atomic prompt reviewer` brief and the same scratchpad trio contract — a shape change to either should update both command files.
- Document skeletons and the reviewer prompt come from the binary (`atomic prompt reviewer`; `atomic template brief|state|followups|implementation-log`). If a verb fails, surface the error rather than improvising the structure.
- Do not push, merge, or open a PR. Ship is `/commit`.

</constraints>
