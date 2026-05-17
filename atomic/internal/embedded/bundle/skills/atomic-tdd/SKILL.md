---
name: atomic-tdd
description: >
  Test-first discipline. Auto-triggers on "let's implement X", "add feature Y", "fix bug Z",
  "write a test for", "implement", "build out", and similar pre-code-change phrases.
  Iron rule: failing test exists before production code. Skip only for pure docs/config
  changes with an explicit "skipped because:" note. Explicit invocation: /atomic-tdd.
---

Test first. Watch it fail. Write minimal code. Watch it pass. Refactor green.

## When this fires

Auto-trigger on:

- "let's implement X", "add feature Y", "build out Z"
- "fix bug", "patch the regression"
- "write tests for", "write a test for"
- "make the code do X"
- Any pre-implementation language for behavior changes

Explicit: `/atomic-tdd`.

## The iron law

```
NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST
```

Wrote code before a failing test? Delete it. Start over. No keeping it as reference. No adapting it while writing tests. Delete means delete.

## The cycle

| Step | Action | Verify |
|------|--------|--------|
| RED | Write one minimal failing test | Run it. Must fail for the right reason — feature missing, not a typo. If it passes immediately, the test is wrong. Fix the test. |
| GREEN | Write the smallest code that makes the test pass | Run all tests. Must be green. No "while I'm here" extras. |
| REFACTOR | Clean up only after green | Re-run. Must stay green. No behavior changes. |

Repeat for the next behavior.

## Required for

- New features — any new behavior
- Bug fixes — test reproduces the bug first, then fix
- Refactoring — existing tests must stay green; if none exist, add one first
- Behavior changes

## Skip rules

TDD doesn't apply to these cases. Each skip requires an explicit `skipped because: <reason>` note in the response.

| Skip case | Example |
|-----------|---------|
| Pure docs / Markdown / comment-only edits | Updating README, adding a JSDoc |
| Pure config / `.env` / non-executable file edits | Changing a JSON config, updating an env var |
| Generated code | Regenerated from a schema or spec, not hand-written |
| Throwaway prototypes the user explicitly flagged as throwaway | "Just spike this, we'll throw it away" |

Skipping silently = violation.

## Red flags — STOP and start over

Refuse these rationalizations. They all mean: delete the code, start over with TDD.

- "I'll write tests after"
- "Too simple to need a test"
- "I already manually tested"
- "Deleting the code I wrote is wasteful" — sunk cost fallacy; the time is gone either way
- "Keep the code as reference while writing tests" — you'll adapt it; that's tests-after
- "Tests after achieve the same goals" — no: tests-first answer "what should this do?", tests-after answer "what does this do?"
- "TDD will slow me down" — TDD is faster than debugging-after
- "This is different because..."

## Good test characteristics

| Quality | Yes | No |
|---------|-----|----|
| Minimal | One behavior per test | "and" in the name → split it |
| Clear | Name describes the behavior | `test1`, `it works` |
| Real | Real inputs, real outputs | Asserts on mocks (tests the mock, not the code) |

## Boundaries

- **Bug fix:** the failing test must reproduce the bug as reported. If you can't write a test that fails on current code, you don't understand the bug yet — switch to `atomic-debug` first.
- **atomic-tdd + atomic-verify:** both fire independently. atomic-tdd ensures the test exists before production code. atomic-verify ensures the claim that tests pass is backed by a fresh run.
