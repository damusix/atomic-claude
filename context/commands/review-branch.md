---
description: Have atomic-reviewer review the branch diff before opening a PR or merging. No implementation loop or spec required.
---

Review the committed branch diff once. Do not modify code, commit, push, merge, or retry with fixes.

1. Run `git rev-parse --is-inside-work-tree`; stop if this is not a Git repository.
2. If the resolved preferences are missing, run `atomic workflow resolve`. Resolve the base using `atomic-git-discipline/references/shipping.md`. Stop if the current branch is the base or `git rev-list --count <base>..HEAD` is zero.
3. Capture the branch name, `git merge-base <base> HEAD`, `git rev-parse HEAD`, and repository path.
4. Dispatch `atomic-reviewer` once for `<base>..HEAD`, passing those values. No spec is provided: request `## Spec compliance` followed by `(no spec — branch review only)`, a thorough code-quality pass, and the usual TDD signals (typecheck, tests, build, lint). Request its standard output ending in `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
5. Return the reviewer's output verbatim. On PASS, append `suggested next step: Ask to open a PR or merge into <base>.` On CHANGES_REQUESTED, add no advice.
