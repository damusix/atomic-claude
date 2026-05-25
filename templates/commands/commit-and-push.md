---
description: Commit current changes, then push. No PR, no merge. Delegates message format to the atomic-commit skill.
---

## 1. Commit

{{ template "commit-flow" . }}

If nothing to commit and branch has unpushed commits, skip to push.
If nothing to commit and branch is up to date, stop.

## 2. Push

{{ template "push-flow" . }}

{{ template "git-safety" . }}
