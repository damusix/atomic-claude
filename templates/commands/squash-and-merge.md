---
description: Squash all branch commits via git merge --squash on base. One clean commit on base. Re-runs tests. Detects worktree, prompts to delete. Prefers `gh pr merge --squash` when a PR is open so GitHub closes the PR cleanly.
---

## Squash

{{ template "squash-flow" . }}

## Merge

{{ template "merge-flow" . }}

## Rules

- No AI bylines in commit messages, PR titles, or PR bodies.
- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- `-D` on branch delete is safe here because the squash commit on base contains the same tree.
- **Never force-push the base branch.** If a remote-path rollback is needed post-merge, use `git revert <SQUASH_SHA>` — the bad SHA stays in history, a new commit reverses it.
- **Remote path is preferred whenever a PR is open.** GitHub does not auto-close PRs when the squash commit lands locally and gets pushed — the new commit on base carries no PR reference, so the PR stays open as "Not merged" indefinitely. `gh pr merge --squash` is the only way to close the PR cleanly without manual `gh pr close`.
