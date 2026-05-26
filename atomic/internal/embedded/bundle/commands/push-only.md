---
description: Push the current branch's commits to the remote. No commit, no PR, no merge.
---

## Pre-flight

<staleness-check>

Before continuing, check whether signals or documentation may be out of date. This is advisory — ask the user and accept their answer. **Why:** the next session benefits from a fresh project snapshot; stale signals cause hallucinated file references.

1. **Signals** — run `command -v atomic && atomic signals stale`. If stale (exit 1), ask: "Signals are stale — refresh before continuing?" Accept yes or no.
2. **Documentation** — run `git diff <base>..HEAD --name-only` to get changed files. Invoke `atomic-documentation` in dry-run mode. If it identifies surfaces that may need updating, summarize them and ask: "These docs may be outdated: <list>. Update before continuing?" Accept yes or no.

If the user declines, proceed without further prompting.

</staleness-check>

## Steps

<push-flow>

1. `git branch --show-current` — record the branch.
2. `git status --porcelain` — if dirty, stop and tell the user to commit first.
3. `git log @{u}..HEAD --oneline 2>/dev/null` — show what is about to ship. If the branch has no upstream, that is expected (set in step 4).
4. Push:
    - No upstream → `git push -u origin <branch>`.
    - Upstream exists and branch is ahead → `git push`.
    - Already up to date → stop.
5. Print the resulting `<old>..<new> <branch> -> <branch>` line.

If push is rejected (non-fast-forward), stop and tell the user. Let them decide how to resolve it.

</push-flow>

<git-safety>
- Stage explicitly by name (`git add <path>`), never `git add -A`. **Why:** `-A` can accidentally include secrets or untracked binaries.
- Use relative paths for `git add` based on the current working directory. **Why:** absolute paths and `git -C` can silently stage files outside the intended scope.
- Run each `git` command as a separate Bash call. **Why:** chaining with `&&` makes it impossible to inspect intermediate state and hides partial failures.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit — never `--amend`. **Why:** amending after a hook failure modifies the PREVIOUS commit, which may destroy unrelated work.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history. **Why:** force-pushing rewrites shared history, breaking every collaborator's checkout.
</git-safety>
