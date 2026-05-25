---
description: Commit current changes, push, then open a PR via gh. Delegates message + body format to atomic-commit and atomic-review skills.
---

## 1. Commit

{{ template "commit-flow" . }}

If nothing to commit and branch has unpushed commits, skip to push.
If nothing to commit and branch is up to date, stop.

## 2. Push

{{ template "push-flow" . }}

## 3. PR

{{ template "pr-flow" . }}

{{ template "git-safety" . }}
