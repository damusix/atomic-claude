---
description: Commit pending changes, then squash all branch commits into one. Does not merge or touch base.
---

## 1. Commit

{{ template "commit-flow" . }}

If nothing to commit, skip to squash.

## 2. Squash

{{ template "squash-flow" . }}

{{ template "git-safety" . }}
