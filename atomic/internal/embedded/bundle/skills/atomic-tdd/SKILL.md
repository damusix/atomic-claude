---
name: atomic-tdd
description: >
  Test-first discipline. Auto-triggers on "let's implement X", "add feature Y", "fix bug Z",
  "write a test for", "implement", "build out", and similar pre-code-change phrases.
  Iron rule: failing test exists before production code. Skip only for pure docs/config
  changes with an explicit "skipped because:" note. Explicit invocation: /atomic-tdd.
---

Test first. Watch it fail. Write minimal code. Watch it pass. Refactor green.

<trigger>

Auto-trigger on:

- "let's implement X", "add feature Y", "build out Z"
- "fix bug", "patch the regression"
- "write tests for", "write a test for"
- "make the code do X"
- Any pre-implementation language for behavior changes

Explicit: `/atomic-tdd`.

</trigger>

## The iron law

Write the failing test before production code. **Why:** tests written after implementation mirror what the code does, not what it should do. Tests-first are specifications; tests-after are tautologies.

Wrote code before a failing test? Delete it. Start over. **Why:** keeping existing code as reference biases the test toward the implementation rather than the intent.

<workflow>

## The cycle

| Step | Action | Verify |
|------|--------|--------|
| RED | Write one minimal failing test | Run it. Must fail for the right reason — feature missing, not a typo. If it passes immediately, the test is wrong. Fix the test. |
| GREEN | Write the smallest code that makes the test pass | Run all tests. Must be green. No "while I'm here" extras. |
| REFACTOR | Clean up only after green | Re-run. Must stay green. No behavior changes. |

Repeat for the next behavior.

</workflow>

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

## Discipline reminders

The test always comes first. These situations tempt shortcuts — resist them:

- Code already written without a test → delete it, write the test first, then reimplement. Keeping existing code as reference biases the test toward the implementation rather than the intent.
- "Too simple to need a test" → simple code deserves a simple test. Write it.
- Tests-after vs tests-first are fundamentally different: tests-first answer "what should this do?", tests-after answer "what does this do?"

## Good test characteristics

| Quality | Yes | No |
|---------|-----|----|
| Minimal | One behavior per test | "and" in the name → split it |
| Clear | Name describes the behavior | `test1`, `it works` |
| Real | Real inputs, real outputs | Asserts on mocks (tests the mock, not the code) |

## Purpose of tests

Tests prove the feature works for real users. They are not the goal — they are evidence.

- Passing tests are not success. Correct behavior is success. If the feature works wrong but tests pass, the tests are wrong.
- A test that cannot fail when business logic changes is dead weight. Every test encodes an intention: "if this breaks, something the user cares about is wrong."
- Do not write tests to satisfy coverage metrics or make CI green. Write tests that would catch a regression a user would notice.
- Tests-first answer "what should this do?" — they encode intent before implementation exists. This is why TDD works: the test is a specification, not a verification of existing code.

<constraints>

## Boundaries

- **Bug fix:** the failing test must reproduce the bug as reported. If you can't write a test that fails on current code, you don't understand the bug yet — switch to `atomic-debug` first. **Why:** a test that doesn't match the reported symptom may pass after the fix while the real bug persists.
- **atomic-tdd + atomic-verify:** both fire independently. atomic-tdd ensures the test exists before production code. atomic-verify ensures the claim that tests pass is backed by a fresh run.

</constraints>
