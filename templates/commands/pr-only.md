---
description: Open a PR for the current branch via gh. Assumes commits exist. Delegates body tone to the atomic-review skill.
---

## Prereqs

- `command -v gh` — if missing, tell the user to install and authenticate. Stop.
- `gh auth status` — if unauthed, tell the user to run `gh auth login`. Stop.

## Pre-flight
{{ template "staleness-check" . }}

## Steps
{{ template "pr-flow" . }}

{{ template "git-safety" . }}
