---
description: Push the current branch's commits to the remote. No commit, no PR, no merge.
---

## Steps

1. `git branch --show-current`. Record the branch (pushing to base, e.g. `main`, is allowed here — this is the trunk-based counterpart to `/pr-only`).
2. `git status --porcelain`. If working tree is dirty, stop and tell the user to run `/commit-only` or `/commit-and-push` first.
3. `git log @{u}..HEAD --oneline 2>/dev/null` to read what's about to ship. If the branch has no upstream, the command errors — that is expected; the upstream is set in step 4.
4. Push:
    - No upstream → `git push -u origin <branch>`.
    - Upstream exists and branch is ahead → `git push`.
    - Branch up to date with upstream → stop, print `already up to date`.
5. Never `--force` or `--force-with-lease`. If push is rejected (non-fast-forward), stop and tell the user; do not rewrite history.
6. Print the resulting `<old>..<new> <branch> -> <branch>` line.

## Rules

No commits. No PR creation — use `/pr-only` if you want a PR. No force-push. If you need to push a fix you forgot to commit, use `/commit-and-push` instead.
