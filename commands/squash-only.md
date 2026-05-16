---
description: Squash all commits on current branch into one. No merge. Synthesized commit message via atomic-commit skill. Does not touch base.
---

## Pre-flight

1. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
2. `git branch --show-current`. If on base: `refused: already on <base>. nothing to squash.`
3. `git status --porcelain`. If dirty: `refused: working tree dirty. commit or stash first.`
4. Count commits: `git rev-list --count <base>..HEAD`. If 1: `refused: only one commit on branch. nothing to squash.`

## Steps

1. Gather subjects (oldest-first): `SUBJECTS=$(git log <base>..HEAD --format='%s' --reverse)`.
2. `git reset --soft $(git merge-base HEAD <base>)` — collapses all branch commits into the index.
3. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from `SUBJECTS`. Present it for user review/edit. Commit via HEREDOC once confirmed.
4. `git status` to confirm.

## Report

`squashed N commits into <new-sha>. branch still <branch>.`

## Rules

- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- This command does NOT merge into base and does NOT delete the branch.
