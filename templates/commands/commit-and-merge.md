---
description: Pipeline — commit pending changes (atomic-commit skill), then /merge-to-main flow. One change to base, in one shot. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
---

## Step 1 — Commit

{{ template "commit-flow" . }}

If nothing to commit AND branch has commits not on base → skip to step 2.
If nothing to commit AND branch up to date with base → stop.

## Step 2 — Merge

{{ template "merge-flow" . }}

## Report

`committed <sha>, merged <feature> into <base> as <MERGE_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

## Rules

- No AI bylines in commit messages.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- **Never force-push the base branch.** Remote-path rollback = `git revert`.
- **Remote path is preferred whenever a PR is open.** Step 1's new commit must be pushed BEFORE `gh pr merge` so the server-side merge includes it. The push goes to the PR's existing branch; no new PR is created.
