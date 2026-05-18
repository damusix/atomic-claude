---
description: Pipeline — commit pending changes (atomic-commit skill), then /merge-to-main flow. One change to base, in one shot. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
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
gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null
  || git config init.defaultBranch
  || echo main
```

If on base: `refused: already on <base>. nothing to merge.`

**Detect open PR for this branch.** Decides remote vs local path:

```
gh pr view --json number,state -q '.state' 2>/dev/null
```

- Output `OPEN` → capture PR number; use **remote path** (preferred — closes the PR cleanly).
- Otherwise → **local path**. A local merge with an open PR leaves the PR open as "Not merged"; prefer remote whenever a PR exists.
- If `gh` is missing or unauthed: fall through to local path with a one-line note.

**Execute — pick path:**

- **Remote path** (PR open):
    1. `git push` — push the new commit from step 1 to the PR's branch so `gh pr merge` includes it in the merge.
    2. `gh pr merge <PR#> --merge --delete-branch`. Server-side merge. Auto-closes the PR. `--delete-branch` removes the remote branch.
    3. `git checkout <base>`.
    4. `git pull` to fast-forward local base to the new tip.
    5. Record SHA: `MERGE_SHA=$(git rev-parse HEAD)`.

- **Local path** (no PR):
    1. `git checkout <base>`.
    2. `git pull`.
    3. `git merge <feature>`.
    4. Record SHA: `MERGE_SHA=$(git rev-parse HEAD)`. Merge commit if non-FF, otherwise the feature tip.

Re-run tests. If fail:

- **Local path**: ask user about rolling back with `git reset --hard ORIG_HEAD`.
- **Remote path**: the merge SHA is already published on `origin/<base>`. `git reset --hard` is wrong (would diverge from origin). Offer `git revert -m 1 <MERGE_SHA>` (merge commit) or `git revert <MERGE_SHA>` (FF) instead. Never force-push the base branch.

**Update implementation logs.** After tests pass, find spec files whose content changed across the merge:

```bash
git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
  grep -q '^## Implementation log' "$f" && echo "$f"
done
```

For each match, append at end-of-file:

```
**Merged into `<base>` as `<MERGE_SHA>` — <YYYY-MM-DD>.** Iteration commits above remain reachable in history.
```

Stage by explicit path. Commit as a follow-up: `docs(spec): record merge SHA <MERGE_SHA>`. Push after commit on remote path (so the impl-log SHA ends up on origin too). Never amend. If no specs match: skip silently.

**Delete local feature branch**: `git branch -d <feature>`.

- **Remote path**: `gh pr merge --delete-branch` already removed the remote branch.
- **Local path**: no remote branch to clean up.

Worktree check: `git worktree list`. If feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:

> Branch was checked out in worktree at `<path>`. Delete it?
> - Yes, remove worktree
> - No, keep it

On Yes: find root via `git rev-parse --show-toplevel` on main checkout. `git worktree remove <path>`. `git worktree prune`.

## Report

`committed <sha>, merged <feature> into <base> as <MERGE_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

## Rules

- No AI bylines in commit messages.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- **Never force-push the base branch.** Remote-path rollback = `git revert`.
- **Remote path is preferred whenever a PR is open.** Step 1's new commit must be pushed BEFORE `gh pr merge` so the server-side merge includes it. The push goes to the PR's existing branch; no new PR is created.
