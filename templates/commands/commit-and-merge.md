---
description: Commit pending changes, then merge into base branch. Prefers `gh pr merge --merge` when a PR is open so GitHub closes it cleanly.
---

## 1. Commit

{{ template "commit-flow" . }}

If nothing to commit and branch has commits ahead of base, skip to merge.
If nothing to commit and branch is up to date with base, stop.

## 2. Merge

{{ template "merge-flow" . }}

<constraints>
If there is an open PR, the new commit from step 1 must be pushed before `gh pr merge` so the server-side merge includes it. Push to the PR's existing branch — do not create a new PR.
</constraints>

{{ template "git-safety" . }}
