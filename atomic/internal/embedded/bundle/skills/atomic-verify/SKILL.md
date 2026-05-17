---
name: atomic-verify
description: >
  Evidence-before-claim gate. Auto-triggers when Claude is about to claim "done", "fixed",
  "passing", "complete", "ready to merge", "looks good", "should work", "should pass",
  "green", or any synonym. Iron rule: no completion claim without a fresh verification
  command run in this turn. Explicit invocation: /atomic-verify.
---

Verify before claim. No claim without fresh evidence in this turn.

## When this fires

- "done", "fixed", "passing", "complete", "ready to merge", "looks good"
- "should work", "should pass", "this works"
- "tests pass", "build green", "lint clean", "typecheck green"
- "bug fixed", "regression resolved"
- Any phrase implying success not preceded by verification output IN THIS TURN.

## The gate

1. **IDENTIFY** — what command proves this claim?
2. **RUN** — execute it fresh in this turn.
3. **READ** — full output, check exit code.
4. **VERIFY** — output matches the claim?
5. **ONLY THEN** — state the claim, WITH evidence.

Skip any step = lying, not verifying.

## Claim → required check

| Claim | Required verification |
|-------|----------------------|
| tests pass | run test command, see 0 failures |
| build green | run build, exit 0 |
| lint clean | run lint, 0 errors |
| typecheck green | run typecheck (tsc/mypy/etc), 0 errors |
| bug fixed | run repro that failed before, see it pass now |
| agent task complete | check VCS diff for actual changes |
| regression test works | red → fix → green sequence verified |

## Red flags — STOP

- "should", "probably", "seems to"
- "Great!", "Perfect!", "Done!" without a fresh command run
- About to commit/push/PR without verification
- Trusting subagent success reports without checking artifacts
- "Linter passed" → linter ≠ compiler
- Partial verification ("ran one test out of N")

## Rationalization prevention

| Excuse | Reality |
|--------|---------|
| "Should work" | Run the command. |
| "I'm confident" | Confidence ≠ evidence. |
| "Just this once" | No exceptions. |
| "Linter passed" | Linter ≠ build. |
| "Subagent said success" | Verify diff yourself. |
| "Partial check is enough" | Partial proves nothing. |

## Boundaries

- Does NOT fire on intent statements ("I'll fix it" — future, not claim).
- Does NOT fire on progress reports ("ran tests, 2 failures" — evidence, no claim).
- Fires the moment Claude transitions from "doing" to "asserting".
