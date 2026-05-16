---
description: Orchestrate implement→review subagent loop until task complete. Parent writes goal docs to .claude/.scratchpad/, dispatches fresh-context subagents, loops until reviewer signs off, then updates repo docs.
---

You are the **orchestrator**. The user has given you a task. You will NOT implement it yourself. You will drive a loop of fresh-context subagents until the task is done, then update documentation.

## Phase 0 — Understand & plan

1. Read the user's task. If anything is genuinely ambiguous and would block work, ask consolidated questions. Otherwise proceed.
2. Inspect the repo enough to scope the work: relevant files, conventions, existing tests, build/test commands. Do NOT start implementing.
3. Define explicit success criteria. These are the bar the reviewer will hold the implementer to.

## Phase 1 — Write goal docs to `.claude/.scratchpad/<date>-<description>/`

Pick a working dir for this task: `.claude/.scratchpad/<YYYY-MM-DD>-<short-kebab-description>/` (e.g. `.claude/.scratchpad/2026-05-15-add-oauth-refresh/`). Use today's date. Description = 2-5 kebab-case words summarizing the task.

Create it: `mkdir -p .claude/.scratchpad/<date>-<description>`. Ensure `.claude/.scratchpad/` is gitignored (add to `.gitignore` if not already covered).

Refer to this dir as `$SCRATCH` below. Every path passed to subagents must be the full path including `$SCRATCH`, not a bare filename.

Write these files inside `$SCRATCH`:

- `$SCRATCH/GOAL.md` — the north star. What the user actually wants, why, and the success criteria. Stable across iterations.
- `$SCRATCH/CONTEXT.md` — repo facts the subagents need: file paths, conventions, build/test commands, anything load-bearing they'd otherwise have to rediscover. Stable across iterations.
- `$SCRATCH/PLAN.md` — the current implementation plan, broken into checkpoints. May evolve between iterations.
- `$SCRATCH/STATE.md` — iteration counter, what last implementer did, what last reviewer said, what's still open. Updated by orchestrator each loop.

Keep these tight. Subagents will read them — bloat costs tokens.

## Phase 2 — Implement → Review loop

Repeat until reviewer signs off or hard stop (default: 6 iterations — ask user before exceeding).

### Step A — Dispatch implementer (fresh context)

Use the Agent tool with `subagent_type: "atomic-builder"` for 1-2 file surgical edits, else `general-purpose`.

Implementer prompt MUST include:

- "Read `$SCRATCH/GOAL.md`, `$SCRATCH/CONTEXT.md`, `$SCRATCH/PLAN.md`, `$SCRATCH/STATE.md` first (use the actual full path). They are your brief."
- The specific subset of the plan to execute this iteration.
- Any reviewer feedback from `STATE.md` to address.
- "TDD: for any new behavior, write a failing test first, confirm it fails for the right reason, then implement, then confirm green. Skip TDD only for pure docs/config changes — state when you skip and why."
- "Run typecheck, tests, build, lint per CONTEXT.md. Report a quality signal block at the end:
   ```
   typecheck: ✓ / ✗ (errors)
   tests:     ✓ / ✗ (N passed, M failed, K added)
   build:     ✓ / ✗ / n/a
   lint:      ✓ / ✗ / n/a
   ```
   Never silently omit a signal. If `n/a`, say why."
- "Do NOT mark anything complete you did not verify. If you couldn't run a signal, mark it `✗ (could not run: <reason>)`."
- "Do not edit files in `.claude/.scratchpad/`."
- "Respond atomic: findings/results only, no preamble."

### Step B — Dispatch reviewer (fresh context)

Use Agent with `subagent_type: "atomic-reviewer"`, else `general-purpose`.

Reviewer prompt MUST include:

- "Read `$SCRATCH/GOAL.md` and `$SCRATCH/CONTEXT.md` (use the actual full path). These define the bar."
- "Review the diff against `main` (or the appropriate base). Report findings as `path:line: <emoji> severity: problem. fix.`"
- "Verify success criteria from GOAL.md are met."
- "Verify TDD signals: did implementer write a failing test before implementing? Run typecheck/tests yourself to confirm the implementer's report. If signals were not actually run, that's a `🔴 bug` finding."
- "End your response with exactly one of: `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`."
- "Do NOT fix the code. Report only. Respond atomic."

### Step C — Orchestrator triages

- Parse reviewer's verdict.
- Update `STATE.md` with iteration number, implementer summary, reviewer findings, next-iteration focus.
- If `PASS` → exit loop.
- If `CHANGES_REQUESTED` → loop back to Step A with the findings as the implementer's focus.
- If implementer reported an unrecoverable blocker (env broken, ambiguous requirement) → stop loop and surface to user.

## Phase 3 — Finalize

Once reviewer says `PASS`:

1. Run the full test/typecheck/lint/build suite yourself (orchestrator) to confirm green. Do NOT trust subagent claims at the finish line.
2. Update repo documentation by invoking `/documentation` — it handles `README.md`, `claude.md`, `docs/spec/`, `docs/design/`.
3. Delete `$SCRATCH` (the task's dated dir). Other dated dirs from prior runs are not your concern.
4. Report to the user: what shipped, what was verified, what's left (if anything).

Do NOT commit unless the user asked you to.

## Rules

- Parent orchestrator does NOT write implementation code. Only goal docs, state updates, final docs, and final verification.
- Every subagent invocation is fresh context. The scratchpad docs are the only handoff. If the docs are bad, the loop is bad — invest in them.
- Reviewer and implementer are separate agents. Never the same one. Never combine roles.
- If the same finding repeats across two iterations, stop and re-examine the plan — the implementer is stuck or the spec is wrong.
- Subagent output is the tool result. Summarize it to the user in 1-3 lines per iteration; don't dump full transcripts.
