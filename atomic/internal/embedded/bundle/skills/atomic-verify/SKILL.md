---
name: atomic-verify
description: >
  Evidence-before-claim gate. Auto-triggers when Claude is about to claim "done", "fixed",
  "passing", "complete", "ready to merge", "looks good", "should work", "should pass",
  "green", or any synonym. Iron rule: no completion claim without a fresh verification
  command run in this turn. Explicit invocation: /atomic-verify.
---

Verify before claim. No claim without fresh evidence in this turn.

<trigger>

- "done", "fixed", "passing", "complete", "ready to merge", "looks good"
- "should work", "should pass", "this works"
- "tests pass", "build green", "lint clean", "typecheck green"
- "bug fixed", "regression resolved"
- Any phrase implying success not preceded by verification output IN THIS TURN.

</trigger>

<workflow>

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
| spec/artifact changed | run `atomic validate spec` + `atomic validate config` + `atomic validate artifacts`, 0 FAIL (skip if `atomic` binary absent) |

## Verification discipline

Every completion claim needs a fresh command run in this turn. Watch for these moments:

- Before saying "done" or "fixed" — run the proof command first
- Before committing or pushing — verify all signals are green
- After a subagent reports success — check the artifacts yourself
- Partial checks (one test out of N) are not verification — run the full suite
- When the change touched `docs/spec/**`, `docs/design/**`, or bundled artifacts (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`), run `atomic validate spec` (when a spec changed), `atomic validate config`, and `atomic validate artifacts`. A FAIL is a gate failure with the same standing as a failing test. **Graceful degradation:** if the `atomic` binary is not on PATH or no matching files exist, skip silently — never fail the gate on absence. (This skill ships to user repos that may not have the binary installed.)

## When tempted to skip

| Temptation | What to do instead |
|-----------|-------------------|
| "Should work" | Run the command. Evidence over confidence. |
| "Linter passed" | Linter ≠ build ≠ tests. Run all applicable checks. |
| "Subagent said success" | Verify the diff yourself. |
| "Partial check is enough" | Run the full suite. Partial proves nothing about the rest. |

</workflow>

<constraints>

## Boundaries

- Fires only when Claude transitions from "doing" to "asserting done."
- Intent statements ("I'll fix it") are future — no gate needed yet.
- Progress reports ("ran tests, 2 failures") are evidence without a completion claim — no gate needed.

</constraints>
