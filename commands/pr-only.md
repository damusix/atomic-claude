---
description: Open a PR for the current branch via gh. Assumes commits exist. Delegates body tone to the atomic-review skill.
---

## Prereqs

- `command -v gh` — if missing, tell the user to install and authenticate. Stop.
- `gh auth status` — if unauthed, tell the user to run `gh auth login`. Stop.

## Pre-flight

<staleness-check>

Before continuing, check whether signals or documentation may be out of date. This is advisory — ask the user and accept their answer.

1. **Signals** — run `command -v atomic && atomic signals stale`. If stale (exit 1), ask: "Signals are stale — refresh before continuing?" Accept yes or no.
2. **Documentation** — run `git diff <base>..HEAD --name-only` to get changed files. Invoke `atomic-documentation` in dry-run mode. If it identifies surfaces that may need updating, summarize them and ask: "These docs may be outdated: <list>. Update before continuing?" Accept yes or no.

If the user declines, proceed without further prompting.

</staleness-check>

## Steps

<pr-flow>

Invoke the `atomic-review` skill for PR title and body tone.

1. `git branch --show-current` — if on base branch, stop.
2. Determine base: `gh repo view --json defaultBranchRef -q .defaultBranchRef.name`.
3. Read what is shipping: `git log <base>..HEAD --oneline` + `git diff <base>...HEAD --stat` (parallel).
4. Check for existing PR: `gh pr view --json url 2>/dev/null` — if one exists, print its URL and stop.
5. Push if needed: `git push -u origin <branch>` (no upstream) or `git push` (behind).
6. Create the PR:
    ```
    gh pr create --title "<imperative, ≤70 chars>" --body <HEREDOC>
    ```
    Body sections: `## Summary` (1-3 bullets), `## Why` (skip if obvious), `## Test plan` (checklist).
7. Print the PR URL.

If the working tree is dirty, stop and tell the user to commit first.

</pr-flow>

<git-safety>
- Use relative paths for `git add` based on the current working directory.
- Run each `git` command as a separate Bash call.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit. The hook exists for a reason.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history.
</git-safety>
