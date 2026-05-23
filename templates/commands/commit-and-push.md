---
description: Commit current changes, then push. No PR, no merge. Delegates message format to the atomic-commit skill.
---

## Pipeline

### 1. Commit

{{ template "commit-flow" . }}

If nothing to commit AND branch has unpushed commits → skip to step 2.
If nothing to commit AND branch up to date → stop.

### 2. Push

{{ template "push-flow" . }}

## Rules

No AI bylines in the commit message. No force-push. No PR creation — use `/commit-and-pr` if you want a PR. One commit per invocation; if the diff spans unrelated concerns, ask how to split.
