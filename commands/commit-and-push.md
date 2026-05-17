---
description: Commit current changes, then push. No PR, no merge. Delegates message format to the atomic-commit skill.
---

## Pipeline

### 1. Commit

Run `/commit-only` flow:

- Invoke `atomic-commit` skill.
- Stage explicit paths, HEREDOC commit, no `--no-verify`, no `--amend` on hook failure.

If nothing to commit AND branch has unpushed commits → skip to step 2.
If nothing to commit AND branch up to date → stop.

### 2. Push

- `git branch --show-current` — record the branch (pushing to base, e.g. `main`, is allowed here; this verb is the trunk-based counterpart to `/commit-and-pr`).
- No upstream → `git push -u origin <branch>`. Else → `git push`.
- Never `--force` or `--force-with-lease`. If push is rejected (non-fast-forward), stop and tell the user; do not rewrite history.
- Print the resulting `<old>..<new> <branch> -> <branch>` line so the user sees what shipped.

## Rules

No AI bylines in the commit message. No force-push. No PR creation — use `/commit-and-pr` if you want a PR. One commit per invocation; if the diff spans unrelated concerns, ask how to split.
