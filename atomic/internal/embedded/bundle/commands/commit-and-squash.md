---
description: Pipeline — commit pending changes, then /squash-only flow. Tidies history in one shot.
---

## Step 1 — Commit

Invoke `atomic-commit` skill. Follow it for message format.

Run `/commit-only` flow:

- `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
- Stage relevant files explicitly by path. No `git add -A` / `.`. Skip secrets, build artifacts, large binaries.
- Commit via HEREDOC. No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- `git status` to confirm.

If nothing to commit → skip to step 2.

## Step 2 — Squash

Determine base:
```
gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
  || git config init.defaultBranch \
  || echo main
```

If on base: `refused: already on <base>. nothing to squash.`

Count commits: `git rev-list --count <base>..HEAD`. If 1 (only the just-landed commit): `refused: only one commit on branch after commit. nothing to squash.`

1. Gather subjects (oldest-first): `git log <base>..HEAD --format='%s' --reverse`.
2. `git reset --soft $(git merge-base HEAD <base>)`.
3. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from gathered subjects. Present for user review/edit. Commit via HEREDOC once confirmed.
4. `git status` to confirm.

## Report

`committed pending change <sha-old>, squashed N commits into <sha-new>.`

## Rules

- No AI bylines in commit messages.
- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- Does NOT merge into base and does NOT delete the branch.
