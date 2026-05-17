---
description: Squash all branch commits via git merge --squash on base. One clean commit on base. Re-runs tests. Detects worktree, prompts to delete.
---

## Pre-flight

1. Invoke `atomic-verify` skill — gate: no merge claim without fresh evidence.
2. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
3. `git branch --show-current`. If on base: `refused: already on <base>. nothing to squash-merge.`
4. `git status --porcelain`. If dirty: `refused: working tree dirty. commit or stash first.`

## Steps

1. Record feature branch name.
2. Gather subjects (oldest-first) for the synthesized message: `git log <base>..<feature> --format='%s' --reverse`.
3. `git checkout <base>`.
4. `git pull`.
5. `git merge --squash <feature>` — stages all changes, no commit yet.
6. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from gathered subjects. Present for user review/edit. Commit via HEREDOC once confirmed.
7. Re-run tests. If fail: report failures. Ask user: roll back with `git reset --hard ORIG_HEAD`?
8. `git branch -D <feature>` (force required — squash leaves merge-base check unresolved).
9. Worktree check: `git worktree list`. If feature branch lived in `.worktrees/<feature>/`, ask via AskUserQuestion:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find root via `git rev-parse --show-toplevel` on main checkout. `git worktree remove <path>`. `git worktree prune`.

## Report

`squash-merged <feature> into <base> as <sha>. branch deleted. worktree: <kept|removed>.`

## Rules

- No AI bylines in commit messages.
- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- `-D` on branch delete is safe here because the squash commit on base contains the same tree.
