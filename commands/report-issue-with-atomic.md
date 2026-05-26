---
description: Open an issue against the atomic-claude repo itself (bugs/feature requests with the installed config, not the user's current project).
---

## Scope

This command files an issue against **atomic-claude** (`damusix/atomic-claude`) — the system that installs commands, skills, agents, and the output style into `~/.claude/`. Use it when the problem is with atomic-claude's behavior, not the user's current repo. For issues in the user's current project, use `/report-issue` instead.

Hardcoded target: `damusix/atomic-claude`. Do **not** infer the target from `gh repo view` or cwd — the user is almost always inside a different repo when invoking this.

## Prereqs

- `command -v gh` — if missing: tell user to install (`brew install gh` / `winget install --id GitHub.cli` / https://cli.github.com/) then `gh auth login`. Stop.
- `gh auth status` — if unauthed: tell user `gh auth login`. Stop.

## Steps

<workflow>

1. Read user's description. Classify: **bug** vs **feature/enhancement** vs **question**. If ambiguous, ask once.
2. Confirm target with one sentence: "Filing against `damusix/atomic-claude` (not your current repo)." Proceed unless user objects.
3. Capture installed version: run `atomic --version` if the binary is on PATH. Include in body. If missing, note "atomic binary not found on PATH".
4. Search for duplicates: `gh issue list --repo damusix/atomic-claude --search "<key terms>" --state all --limit 5`. If close match exists, surface URL + ask before opening new.
5. Check repo templates: `gh api repos/damusix/atomic-claude/contents/.github/ISSUE_TEMPLATE 2>/dev/null` — if templates exist, prefer `--template <file>`. Otherwise build body inline.
6. Draft title — imperative for features (`Add X`), declarative for bugs (`/commit-only skips signals refresh on staged-only changes`). ≤70 chars. No "Bug:" / "Feature:" prefix.
7. Draft body per shape below (HEREDOC). Atomic tone — drop filler, exact symbols in backticks, no hedging, no AI bylines.
8. Map classification → label: `bug` → `bug`, `feature/enhancement` → `enhancement`, `question` → `question`. Verify the label exists on the target repo first: `gh label list --repo damusix/atomic-claude --search <name>`. Skip the label if it doesn't exist (don't auto-create). User-specified labels stack on top.
9. `gh issue create --repo damusix/atomic-claude --title "<title>" --body "$(cat <<'EOF' … EOF)" [--label <classified>] [--label <user-specified>]`.
10. Print issue URL.

</workflow>

## Body shapes

### Bug

```markdown
## Summary

<one-line statement of what's broken in atomic-claude>

## Repro

1. <step — exact command or skill trigger>
2. <step>
3. <step>

## Expected

<what atomic-claude should do per its spec / docs>

## Actual

<what happens, including exact error message or wrong output in a code block>

## Environment

- `atomic --version`: <x>
- Install method: <curl install.sh | brew | source | docker>
- OS: <x>
- Claude Code version: <x if known>
- Affected artifact: <command/skill/agent/output-style name>
```

### Feature / enhancement

```markdown
## Problem

<the friction or gap in atomic-claude's current behavior>

## Proposal

<what to add or change — name the specific command/skill/agent if applicable>

## Why now

<context — skip if obvious>

## Out of scope

- <thing this issue is not>
```

### Question

```markdown
## Question

<the question, specific>

## Context

- `atomic --version`: <x>
- What was tried: <x>
- Where stuck: <x>
```

## Rules

<constraints>

- Hardcoded `--repo damusix/atomic-claude` on every `gh` call. Never omit.
- No AI bylines. No "I think" / "maybe" / "perhaps". State facts.
- Code blocks for exact errors and commands.
- One issue per invocation — if user describes multiple unrelated problems, ask which to file or split.
- If the user's description sounds like a problem with their *current project* (build error, test failure in their code, their PR not merging), redirect them to `/report-issue` instead.

</constraints>
