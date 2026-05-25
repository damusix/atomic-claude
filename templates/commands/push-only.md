---
description: Push the current branch's commits to the remote. No commit, no PR, no merge.
---

## Pre-flight
{{ template "staleness-check" . }}

## Steps
{{ template "push-flow" . }}

{{ template "git-safety" . }}
