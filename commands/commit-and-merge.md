---
description: Pipeline — commit pending changes (atomic-commit skill), then /merge-to-main flow. One change to base, in one shot.
---

## Step 1 — Commit

Invoke `atomic-commit` skill. Follow it for message format.

Run `/commit-only` flow:

- `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
- Stage relevant files explicitly by path. No `git add -A` / `.`. Skip secrets, build artifacts, large binaries.
- Commit via HEREDOC. No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- `git status` to confirm.

If nothing to commit AND branch has commits not on base → skip to step 2.
If nothing to commit AND branch up to date with base → stop.

## Step 2 — Merge

Invoke `atomic-verify` skill — gate: no merge claim without fresh evidence.

Determine base:
```
gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
  || git config init.defaultBranch \
  || echo main
```

If on base: `refused: already on <base>. nothing to merge.`

1. `git checkout <base>`.
2. `git pull`.
3. `git merge <feature>`.
4. Re-run tests. If fail: report failures. Ask user: roll back with `git reset --hard ORIG_HEAD`?
5. **Update implementation logs.** After tests pass, find spec files whose content changed across the merge:

    ```bash
    git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```

    For each match, append at end-of-file:

    ```
    **Merged into `<base>` as `<merge-or-tip-sha>` — <YYYY-MM-DD>.** Iteration commits above remain reachable in history.
    ```

    `<merge-or-tip-sha>` = the merge commit if non-FF, otherwise the new HEAD (which equals the feature tip). Stage by explicit path. Commit as a follow-up: `docs(spec): record merge SHA <sha>`. Never amend. If no specs match: skip silently.
6. `git branch -d <feature>`.
7. Worktree check: `git worktree list`. If feature branch lived in `.worktrees/<feature>/`, ask via AskUserQuestion:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find root via `git rev-parse --show-toplevel` on main checkout. `git worktree remove <path>`. `git worktree prune`.

## Report

`committed <sha>, merged <feature> into <base>. branch deleted. worktree: <kept|removed>.`

## Rules

- No AI bylines in commit messages.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
