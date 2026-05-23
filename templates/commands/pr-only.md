---
description: Open a PR for the current branch via gh. Assumes commits exist. Delegates body tone to the atomic-review skill.
---

## Prereqs

- `command -v gh` — if missing: tell user to install (`brew install gh` / `winget install --id GitHub.cli` / https://cli.github.com/) then `gh auth login`. Stop.
- `gh auth status` — if unauthed: tell user `gh auth login`. Stop.

## Steps
{{ template "pr-flow" . }}
