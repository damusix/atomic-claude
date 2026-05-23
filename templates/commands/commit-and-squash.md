---
description: Pipeline — commit pending changes, then /squash-only flow. Tidies history in one shot.
---

## Step 1 — Commit

{{ template "commit-flow" . }}

If nothing to commit → skip to step 2.

## Step 2 — Squash

{{ template "squash-flow" . }}

## Report

`committed pending change <sha-old>, squashed N commits into <sha-new>.`

## Rules

- No AI bylines in commit messages.
- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- Does NOT merge into base and does NOT delete the branch.
