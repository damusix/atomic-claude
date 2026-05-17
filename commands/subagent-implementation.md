---
description: Orchestrate implement→review subagent loop until task complete. Reads the approved spec, writes a thin brief to .claude/.scratchpad/, dispatches fresh-context subagents, loops until reviewer signs off, commits per green iteration, then updates repo docs.
---

You are the **orchestrator**. The user has given you a task. You will NOT implement it yourself. You drive a loop of fresh-context subagents until the task is done, then update documentation.

## Phase 0 — Understand

1. Read the user's task. If anything is genuinely ambiguous and would block work, ask consolidated questions. Otherwise proceed.
2. **Dispatch `atomic-investigator` first** to locate the relevant surface area — files, call sites, existing tests, conventions. Haiku-backed and read-only, so it's cheap. Pass a focused brief (e.g. `Map src/auth/. Find: token generation, validation, refresh. Report file:line table.`). Use its `file:line — what` table as your scoping evidence — do NOT duplicate the search yourself in the main context. The investigator's job is to spend Haiku tokens so Sonnet builder/reviewer dispatches start with a precise target.
3. Read only the files the investigator flagged that you need to make scoping decisions (spec gate, agent choice, iteration breakdown). Do NOT start implementing.

Skip the investigator only when the task names exact files and there's nothing to locate (e.g. "fix the typo in README.md line 42"). When in doubt, dispatch it — its cost is trivial compared to a misaimed builder.

## Phase 0.5 — Spec gate

Derive the topic slug from the task: short kebab-case (e.g. `oauth-refresh`, `user-search-perf`).

Check for `docs/spec/<topic>.md`:

- **Spec exists** → use it as the canonical brief source. Skip to Phase 0.6.
- **No spec, task is <30 min of obvious work** → proceed inline. State the assumption: `no spec; proceeding inline because task is small/obvious.` Skip to Phase 0.6.
- **No spec, task is non-trivial** → refuse. Tell user: `Run /atomic-plan first. I need an approved spec at docs/spec/<topic>.md before launching the implementation loop.` Stop.

Bar for "non-trivial": touches ≥3 files, introduces new architectural patterns, or has any ambiguity about success criteria. When in doubt, require the spec.

## Phase 0.6 — Worktree gate

Detect existing isolation:

```bash
GIT_DIR=$(cd "$(git rev-parse --git-dir)" 2>/dev/null && pwd -P)
GIT_COMMON=$(cd "$(git rev-parse --git-common-dir)" 2>/dev/null && pwd -P)
```

- If `$GIT_DIR != $GIT_COMMON` (and not a submodule) → already in a worktree. Skip the question.
- Else, prompt via `AskUserQuestion`:

    ```
    Significant work ahead. Use an isolated worktree?
    - Yes, new branch → /worktree-start <derived-name>
    - No, work in place
    ```

- On `Yes`: instruct user to run `/worktree-start <branch>` and resume `/subagent-implementation` after. Stop.
- On `No`: proceed in place.

For tasks classified as obviously small in Phase 0.5, skip the worktree question.

## Phase 1 — Write brief to `$SCRATCH`

Pick the working dir: `.claude/.scratchpad/<YYYY-MM-DD>-<topic>/`. Use today's date.

```bash
SCRATCH=".claude/.scratchpad/$(date +%Y-%m-%d)-<topic>"
mkdir -p "$SCRATCH"
```

`.claude/.scratchpad/` must be gitignored — verify, add if missing.

Write two files inside `$SCRATCH`:

### `$SCRATCH/BRIEF.md`

Thin orchestrator-curated brief. Contents:

```markdown
# Brief: <topic>

**Spec:** `docs/spec/<topic>.md` (canonical source — read this first)

**Iteration scope (this turn):** <which checkpoint from the spec>

**Reviewer feedback to address:** <findings from prior iteration, or "N/A — first iteration">

**Success criteria for this iteration:**
- <criterion>
- <criterion>

**Base SHA for diff:** <git rev-parse HEAD>
```

Refreshed each iteration — overwrite, don't append.

### `$SCRATCH/STATE.md`

Append-only iteration log:

```markdown
# State: <topic>

## Iteration 1 — <date>
- Implementer: <one-line summary>
- Reviewer: <verdict + key findings>
- Decisions: <anything load-bearing>
- Commit: <sha or "deferred">

## Iteration 2 — <date>
...
```

### `$SCRATCH/FOLLOWUPS.md`

Ledger of non-blocking reviewer findings (🟡 risk / 🔵 nit / ❓ question) — anything that didn't block the iteration's PASS but is worth a deliberate decision before final ship. Append after every reviewer pass that returns findings; do NOT discard them just because the verdict was PASS.

Initialize on first iteration with this structure:

```markdown
# Follow-ups: <topic>

Non-blocking findings carried across iterations. At finalization (Phase 3): review with the user, decide what to fix in a polish pass, what to defer to a tracked issue, what to drop.

---

## 🟡 risks

### F-1 — <one-line title>

`<path:line>`

<problem + suggested fix in 1-3 sentences>

Origin: iteration <N> reviewer.

## 🔵 nits

### F-N — <title>
...
```

Numbering is sequential across all severities (F-1, F-2, F-3...). When a follow-up gets closed in a later iteration, mark `*(closed iter N — <commit-sha>)*` next to its title and keep the entry for traceability — don't delete it.

That's it. No GOAL.md, no CONTEXT.md, no PLAN.md — the spec at `docs/spec/<topic>.md` IS those. The scratchpad is a thin handoff plus a deliberate-decision ledger, not a duplicate.

## Phase 2 — Implement → Review → Commit loop

Repeat until reviewer signs off or hard stop (default: 6 iterations — ask user before exceeding).

### Step A — Dispatch implementer (fresh context)

Pick the agent based on iteration scope:

- **`atomic-surgeon`** when scope touches ≤2 files and is mechanically obvious (typo, single-fn rewrite, rename, single-callsite fix).
- **`atomic-builder`** for feature checkpoints — one cohesive slice, however many files.
- **`general-purpose`** as fallback if neither fits.

Build the implementer prompt by reading `commands/_templates/implementer-prompt.md` and substituting:

| Placeholder | Value |
|-------------|-------|
| `{SCRATCH_PATH}` | absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | absolute path to `docs/spec/<topic>.md` (or `"no spec — inline brief in BRIEF.md"`) |
| `{ITERATION_SCOPE}` | this iteration's scope from BRIEF |
| `{REVIEWER_FEEDBACK}` | findings from STATE.md (or `"N/A — first iteration"`) |
| `{BASE_SHA}` | current HEAD SHA before this iteration |

Dispatch via `Agent` tool with the chosen `subagent_type`.

### Step B — Dispatch reviewer (fresh context)

Use `subagent_type: "atomic-reviewer"`.

Build the reviewer prompt from `commands/_templates/reviewer-prompt.md`, substituting:

| Placeholder | Value |
|-------------|-------|
| `{SCRATCH_PATH}` | absolute path to `$SCRATCH` |
| `{SPEC_PATH}` | absolute path to `docs/spec/<topic>.md` |
| `{BASE_SHA}` | HEAD before this iteration |
| `{HEAD_SHA}` | current HEAD after implementer's work |

### Step C — Orchestrator triages

- Parse reviewer's verdict line: `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- Update `STATE.md` with iteration number, implementer summary, reviewer findings, next-iteration focus.
- **Harvest non-blocking findings** (🟡 / 🔵 / ❓ that the reviewer let through to PASS, or anything in CHANGES_REQUESTED's set that the next iteration is NOT going to address) into `FOLLOWUPS.md` as new `F-N` entries. Cite `path:line`, severity emoji, problem, suggested fix, origin iteration. Don't drop them; they exist for a reason and the user reviews the ledger before ship.
- If `CHANGES_REQUESTED` → loop back to Step A with the blocking findings (🔴, plus any 🟡 the orchestrator chooses to address now) as the implementer's focus. Anything not addressed in the next iteration stays in `FOLLOWUPS.md`.
- If implementer reported `BLOCKED` or `NEEDS_CONTEXT` → stop loop and surface to user.
- If `PASS` → continue to Step D.

### Step D — Commit the green iteration

After each PASS, commit before the next iteration:

1. Invoke `atomic-commit` skill for message format.
2. Stage only the files the implementer touched (explicit paths from the implementer's `## Did` section). No `-A`.
3. Commit via HEREDOC. Conventional Commits format. No AI bylines.
4. Record the commit SHA in STATE.md under the iteration's `Commit:` line.

Skip Step D only if the iteration produced zero behavior change (pure investigation, no diff). State that explicitly in STATE.md.

This makes each iteration bisectable. The next iteration's reviewer diffs against the prior commit, not the merge base — cleaner reviews, easier rollback if something goes wrong later.

## Phase 3 — Finalize

Once reviewer says `PASS` and there are no more checkpoints in the spec to ship:

1. Run the full test/typecheck/lint/build suite yourself (orchestrator) to confirm green. Do NOT trust subagent claims at the finish line — invoke the `atomic-verify` skill here, which is exactly this gate.
2. **Surface `FOLLOWUPS.md` to the user.** Read it, list every open `F-N` entry, and ask the user what to do with each. Four dispositions:

    - **`fix-now`** — run another iteration to address it.
    - **`defer`** — promote to project-level `.claude/project/followups.md` (committed, durable, auto-loaded into future sessions). The entry survives scratchpad deletion. Optionally chain to `/remind-me <duration> <text>` so the user gets surfaced via `/follow-up` later.
    - **`issue`** — file as a tracked GitHub issue via `/report-issue`.
    - **`drop`** — discard. State the reason in the implementation log so the audit trail explains why it wasn't worth keeping.

    Don't auto-decide; this is the deliberate-decision gate the ledger exists for.

    **`defer` mechanics.** When promoting an F-N entry to project-level:

    1. Create `.claude/project/followups.md` if absent.
    2. Append the entry under its severity section (🔴 / 🟡 / 🔵 / ❓), keeping the same F-id (rewrite to a project-wide unique id if collision — prefix with topic slug, e.g. `oauth-refresh-F-3`).
    3. Each entry must carry an `Origin:` line citing the source spec + iteration (e.g. `Origin: docs/spec/oauth-refresh.md, iter 3 reviewer`).
    4. Verify `.claude/project/followups.md` is `@-ref`'d from `claude.md` / `CLAUDE.md` / `claude.local.md` / `CLAUDE.local.md` (same search order as signals). If not present in any, append the `@-ref` block to whichever signals are wired in.
    5. Stage and commit alongside the implementation log: `docs(followups): promote F-N <title> from <topic>`.
3. **Write an implementation log to the spec.** Append (or create) an `## Implementation log` section at the END of `docs/spec/<topic>.md`. This is the durable record someone reads in 6 months when they ask "what did we ship?", "where did this come from?", or "what's still open?". Format:

    ```markdown
    ## Implementation log

    ### <version-or-status> — <date>

    Built across N iterations of /subagent-implementation. Commits (chronological):

    - `<sha>` — CP-1 <one-line>
    - `<sha>` — CP-2 <one-line>
    - ...

    **Out-of-scope work performed during this build:**
    - <what + why it ended up in scope> (or "none")

    **Unforeseens — surprises that emerged during implementation:**
    - <surprise + how it was handled> (or "none")

    **Deferred items still open:**
    - <link to FOLLOWUPS triage decisions, tracked issues, or "none"> 
    ```

    Pull commit SHAs from `STATE.md`. Pull out-of-scope and unforeseens from `STATE.md` decision lines and from any iteration where the implementer's report flagged scope drift or surprise. Pull deferred items from `FOLLOWUPS.md`'s Queued section and the user's disposition answers from step 2. Keep entries tight — one line each. The log is a navigation aid, not a narrative.

    If the spec is dead (e.g. user decided not to ship the feature), still write the log with the status as `abandoned — <date>` and one line on why.

4. Update repo documentation by invoking `/documentation` — it handles `README.md`, `claude.md`, `docs/spec/`, `docs/design/`.
5. Delete `$SCRATCH` (the task's dated dir) — only after the user has signed off on the FOLLOWUPS triage AND the implementation log is written. Other dated dirs from prior runs are not your concern.
6. Report to the user: what shipped, which iterations + commit SHAs, what was verified, what FOLLOWUPS were dispositioned, what's left (if anything). Mirror what you just wrote to the spec — they should match.

Do NOT push, merge, or open a PR. The user picks the ship verb (`/pr-only`, `/merge-to-main`, `/squash-and-merge`, etc.) when ready.

## Rules

- Parent orchestrator does NOT write implementation code. Only goal docs, state updates, commits per PASS, final docs, final verification.
- Every subagent invocation is fresh context. The scratchpad brief is the only handoff. If the brief is bad, the loop is bad — invest in it.
- Reviewer and implementer are separate agents. Never the same one. Never combine roles.
- If the same finding repeats across two iterations, stop and re-examine the brief/spec — the implementer is stuck or the spec is wrong.
- Subagent output is the tool result. Summarize it to the user in 1-3 lines per iteration; don't dump full transcripts.
- Templates live in `commands/_templates/`. If they're missing, the loop can't start — surface that error rather than inlining prompts.
