---
name: atomic-tdd
description: Use test-first development for new behavior, known bug fixes, and refactors. Use atomic-debug first when the cause is unknown.
---

Write a test for observable behavior before changing production code. For a bug fix, use the original repro or an existing test that exercises it; add a regression test if neither does.

| Step | Action |
|---|---|
| Red | Run the targeted test against the old behavior. Confirm it fails for the expected reason. |
| Green | Make the smallest fix and rerun the test. |
| Refactor | Clean up without changing behavior, then rerun the affected checks. |

If the fix already exists, verify red against an isolated copy of the pre-fix code. Do not discard or overwrite existing work. For a behavior-preserving refactor, start with a passing test that protects the contract.

Use deterministic tests that exercise real behavior. Skip TDD for docs-only changes, non-behavioral config or comment edits, regenerated output, and explicitly disposable prototypes. State why. Use `atomic-verify` to match the final claim to the checks run.
