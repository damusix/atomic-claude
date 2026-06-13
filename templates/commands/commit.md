---
description: Stage, commit, and optionally ship further. Pass an escalation token (push, pr, merge, squash, squash merge) to skip the prompt. With no token, commits then asks how far to ship. Delegates message format to the atomic-commit skill.
---

## Parse arguments

Read `$ARGUMENTS`. Scan for escalation tokens: `push`, `pr`, `merge`, `squash`, `squash merge`.

Token → escalation mapping:

| Token(s) in args | Path |
|---|---|
| _(none)_ | commit only, then ask |
| `push` | commit + push |
| `pr` | commit + push + PR |
| `merge` | commit + merge to base |
| `squash` (without `merge`) | commit + squash branch |
| `squash merge` (both tokens) | commit + squash + merge to base |

If the args contain none of the tokens above, run the commit step, then prompt (see "Interactive path" below).

## Commit

{{ template "commit-flow" . }}

If nothing to commit and branch has commits ahead of base, skip to the escalation step.
If nothing to commit and branch is up to date with base, stop.

## Escalation

<constraints>
If there is an open PR and the escalation requires a merge, the new commit must be pushed before `gh pr merge` so the server-side merge includes it. Push to the PR's existing branch — do not create a new PR.
</constraints>

### Push path (`push` token or user picks Push)

{{ template "push-flow" . }}

### PR path (`pr` token or user picks Open PR)

{{ template "push-flow" . }}

{{ template "pr-flow" . }}

### Merge path (`merge` token or user picks Merge to base)

{{ template "merge-flow" . }}

### Squash path (`squash` token, no `merge`, or user picks Squash branch)

{{ template "squash-flow" . }}

### Squash + merge path (`squash merge` tokens or user picks Squash + merge)

{{ template "squash-flow" . }}

{{ template "merge-flow" . }}

## Interactive path

If no escalation token was present in args, after the commit completes, ask via `AskUserQuestion`:

> Committed. Ship further?
> - Done — just the commit
> - Push to remote
> - Open PR
> - Merge to base
> - Squash + merge

Route the answer to the matching escalation path above. "Done" stops immediately.

{{ template "git-safety" . }}
