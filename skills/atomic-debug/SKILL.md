---
name: atomic-debug
description: >
  Hypothesis-driven debugging skill. Use when a bug, test failure, crash, or unexpected behavior
  is reported. Auto-trigger on error pastes or "broken/doesn't work/failing" language. Explicit
  invocation: /atomic-debug. Output: symptom statement → hypothesis table → cheapest test first →
  root cause. No symptom-patching.
---

Debug by hypothesis, not by guessing. Cheapest test first. Root cause, not symptom.

## When to invoke

Auto-trigger on:

- Error message or stack trace pasted
- "broken", "doesn't work", "failing", "crash", "regression", "flaky"
- Test failure output

Explicit: `/atomic-debug` or "let's debug X".

## Five steps

### 1. State the symptom exactly

One sentence. Quote the exact error. Note what changed recently (commit, dep, env) if known.

```
Symptom: `POST /users` returns 500 since commit abc1234. Stack: `TypeError: Cannot read 'id' of undefined at users.ts:42`.
```

### 2. Hypothesis table

List 3-5 candidates ranked by likelihood × cheapness to test.

```
| # | Hypothesis | Likelihood | Test cost | Test |
|---|-----------|-----------|-----------|------|
| 1 | req.body missing `user` after middleware reorder | high | 1m | log req.body at line 40 |
| 2 | DB returns null for new user before commit | med | 5m | run failing test in isolation |
| 3 | Type widening hides null in TS 5.5 upgrade | low | 10m | revert tsconfig strict change |
```

Order: highest (likelihood / cost) first. Don't list more than 5 — if you have more, you're guessing.

### 3. Run the cheapest test

Execute the test for hypothesis #1. Report the observation, exact and unedited.

If hypothesis confirmed → step 4.
If refuted → cross it off, move to #2. Update the table.
If observation surprises you → STOP, re-state the symptom. The mental model is wrong.

### 4. Root cause statement

Once a hypothesis is confirmed, write the root cause as a causal chain:

```
Root cause: middleware order change (commit abc1234) moved `parseBody` after `requireAuth`,
so `requireAuth` accesses `req.body.user` before it's populated → `undefined` →
`.id` throws TypeError.
```

ASCII flow diagram if the chain has ≥3 hops:

```
parseBody (moved after) → requireAuth runs → req.body = {} → req.body.user undefined → .id throws
```

### 5. Fix the cause, not the symptom

Propose the minimal fix at the root, not a guard at the symptom. State what regression test will lock the fix in.

Bad fix: `if (req.body?.user)` guard at line 42.
Good fix: restore middleware order so `parseBody` runs before `requireAuth`. Add ordering test in middleware-pipeline.test.ts.

If the user wants the symptom guard anyway (e.g., defense-in-depth), add it ON TOP of the root fix, not instead of.

## Anti-patterns to refuse

- "Try this and see if it works" without a hypothesis — that's guessing
- Adding try/catch to silence the error
- Reverting commits without identifying which line caused the regression
- Saying "it should work now" without a verification step
- Stack-trace-to-fix without reading the surrounding code

## Rules

- One symptom per debug session. If the user reports two unrelated bugs, debug separately.
- If hypothesis #1 is confirmed in step 3, do NOT also explore #2-5 "for completeness". Stop, fix, verify.
- If 5 hypotheses are refuted, you don't understand the system. Stop and ask for more context (logs, env, recent changes) — don't fish.
- Report cheap signals first: `git log --oneline -20`, `git diff HEAD~5`, exact env, exact reproduction command.
