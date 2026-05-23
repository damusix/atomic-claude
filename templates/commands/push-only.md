---
description: Push the current branch's commits to the remote. No commit, no PR, no merge.
---

## Steps
{{ template "push-flow" . }}

## Rules

No commits. No PR creation — use `/pr-only` if you want a PR. No force-push. If you need to push a fix you forgot to commit, use `/commit-and-push` instead.
