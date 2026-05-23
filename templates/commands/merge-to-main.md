---
description: Merge current branch into base. No squash, no push. Re-runs tests on merged tip. Detects worktree provenance and prompts to delete. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
---
{{ template "merge-flow" . }}
