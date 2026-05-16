---
description: Merge current branch into base. No squash, no push. Re-runs tests on merged tip. Detects worktree provenance and prompts to delete.
---

## Pre-flight

1. Invoke `atomic-verify` skill — gate: no merge claim without fresh evidence.
2. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
3. `git branch --show-current`. If on base: `refused: already on <base>. nothing to merge.`
4. `git status --porcelain`. If dirty: `refused: working tree dirty. /commit-only first, then /merge-to-main.`

## Steps

1. `git checkout <base>`.
2. `git pull`.
3. `git merge <feature>` (default strategy; no `--no-ff` forced, no `--squash`).
4. Re-run tests. If fail: report failures. Ask user: roll back with `git reset --hard ORIG_HEAD`?
5. `git branch -d <feature>`.
6. Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via AskUserQuestion:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.

## Report

`merged <feature> into <base>. branch deleted. worktree: <kept|removed>.`

## Rules

- No `--no-verify`. No `--amend` on hook failure — fix root cause and recommit.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
