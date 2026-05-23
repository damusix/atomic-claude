---
description: Commit current changes, push, then open a PR via gh. Delegates message + body format to atomic-commit and atomic-review skills.
---

## Prereqs

- `command -v gh` — if missing: tell user to install (`brew install gh` / `winget install --id GitHub.cli` / https://cli.github.com/) then `gh auth login`. Stop.
- `gh auth status` — if unauthed: tell user `gh auth login`. Stop.

## Pipeline

### 1. Commit

Run `/commit-only` flow:

- Invoke `atomic-commit` skill.
- Stage explicit paths, HEREDOC commit, no `--no-verify`, no `--amend` on hook failure.

If nothing to commit AND branch has unpushed commits → skip to step 2.
If nothing to commit AND branch up to date → stop.

### 2. Push

- `git branch --show-current` — if on base branch, stop.
- No upstream → `git push -u origin <branch>`. Else → `git push`.

### 3. PR

Run `/pr-only` flow:

- Invoke `atomic-review` skill.
- Check existing PR first.
- `gh pr create` with `## Summary` / `## Why` / `## Test plan` sections via HEREDOC.
- Print PR URL.

## Rules

No AI bylines in commit, title, or body. No force-push. No `--draft` unless asked. One commit + one PR per invocation — if diff spans unrelated concerns, ask how to split.
