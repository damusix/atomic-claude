---
description: Squash all commits on current branch into one. No merge. Synthesized commit message via atomic-commit skill. Does not touch base.
---

## Pre-flight
{{ template "squash-flow-preflight" . }}

## Steps
{{ template "squash-flow-steps" . }}

## Report

`squashed N commits into <new-sha>. branch still <branch>.`

## Rules

- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- This command does NOT merge into base and does NOT delete the branch.
