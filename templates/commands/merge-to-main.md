---
description: Merge current branch into base. No squash, no push. Re-runs tests on merged tip. Detects worktree provenance and prompts to delete. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
---

## Pre-flight
{{ template "merge-flow-preflight" . }}

## Steps
{{ template "merge-flow-steps" . }}

## Report

`merged <feature> into <base> as <MERGE_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

## Rules

- No `--no-verify`. No `--amend` on hook failure — fix root cause and recommit.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- **Never force-push the base branch.** If a remote-path rollback is needed post-merge, use `git revert` — the bad SHA stays in history, a new commit reverses it.
- **Remote path is preferred whenever a PR is open.** GitHub does not auto-close PRs on local merge + push of the base branch; the PR stays open as "Not merged" indefinitely. `gh pr merge` is the only way to close it cleanly without manual `gh pr close`.
