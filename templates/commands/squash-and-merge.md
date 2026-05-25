---
description: Squash all branch commits via git merge --squash on base. One clean commit on base. Re-runs tests. Detects worktree, prompts to delete. Prefers `gh pr merge --squash` when a PR is open so GitHub closes the PR cleanly.
---

## 1. Squash

{{ template "squash-flow" . }}

## 2. Merge

{{ template "merge-flow" . }}

{{ template "git-safety" . }}
