---
description: Merge current branch into base. No squash, no push. Re-runs tests on merged tip. Detects worktree provenance and prompts to delete. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
---

## Pre-flight
{{ template "staleness-check" . }}

{{ template "merge-flow-preflight" . }}

## Steps
{{ template "merge-flow-steps" . }}

## Report

`merged <feature> into <base> as <MERGE_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

{{ template "git-safety" . }}
