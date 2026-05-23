---
description: Commit current changes, push, then open a PR via gh. Delegates message + body format to atomic-commit and atomic-review skills.
---

## Pipeline

### 1. Commit

{{ template "commit-flow" . }}

If nothing to commit AND branch has unpushed commits → skip to step 2.
If nothing to commit AND branch up to date → stop.

### 2. Push

{{ template "push-flow" . }}

### 3. PR

{{ template "pr-flow" . }}

## Rules

No AI bylines in commit, title, or body. No force-push. No `--draft` unless asked. One commit + one PR per invocation — if diff spans unrelated concerns, ask how to split.
