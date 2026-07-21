---
description: Open a atomic GitHub issue via gh. Bug report or feature request — auto-detect from user's description.
---

## Prereqs

- `command -v gh` — if missing: tell user to install (`brew install gh` / `winget install --id GitHub.cli` / https://cli.github.com/) then `gh auth login`. Stop.
- `gh auth status` — if unauthed: tell user `gh auth login`. Stop.

## Steps

<workflow>

1. Read user's description. Classify: **bug** vs **feature/enhancement** vs **question**. If ambiguous, ask once.
2. Check repo: `gh repo view --json nameWithOwner,hasIssuesEnabled`. If issues disabled, stop.
3. List existing issue templates: `gh issue create --help` notes `--template`. Check `.github/ISSUE_TEMPLATE/` — if templates exist, prefer `--template <file>` and let user fill in via editor. Otherwise build body inline.
4. Search for duplicates: `gh issue list --search "<key terms>" --state all --limit 5`. If close match exists, surface URL + ask before opening new.
5. Draft title — imperative for features (`Add X`), declarative for bugs (`X crashes when Y`). ≤70 chars. No "Bug:" / "Feature:" prefix unless repo convention demands.
6. Draft body per shape below (HEREDOC). Atomic tone — drop filler, exact symbols in backticks, no hedging, no AI bylines. Redact per the **Privacy** section as you draft — never write raw PII, secrets, or third-party identifiers into the body.
7. Map classification → label: `bug` → `bug`, `feature/enhancement` → `enhancement`, `question` → `question`. Verify the label exists on the repo first: `gh label list --search <name>`. Skip the label if it doesn't exist (don't auto-create). User-specified labels stack on top.
8. Preview: print the full rendered issue — title, body, target repo, labels — and get an explicit go-ahead from the user before creating. See **Privacy**.
9. `gh issue create --title "<title>" --body "$(cat <<'EOF' … EOF)" [--label <classified>] [--label <user-specified>]`.
10. Print issue URL.

</workflow>

## Privacy: redact before posting

A GitHub issue is public and permanent — treat the body as world-readable. Before drafting, scrub the material the user pasted or that you gathered from their repo:

<constraints>

- **Redact PII and secrets** into neutral placeholders, preserving technical structure so the report stays actionable:
  - Emails → `user@example.com`; person names not needed to reproduce → `<name>`.
  - Real domains / hostnames / internal URLs → `example.com` / `<host>` (keep a public name only when it IS the subject of the bug).
  - Absolute paths carrying a username (`/Users/<you>/…`, `/home/<you>/…`, `C:\Users\<you>\…`) → repo-relative or `<path>`.
  - API keys, tokens, passwords, connection strings, IP addresses → `<redacted>`.
  - Client / company / project names not needed to reproduce → `<project>`.
- **Keep the signal.** Exact error text, command names, `atomic` / artifact identifiers, versions, and stack frames stay verbatim — they are what makes the issue reproducible. Redact identity, not mechanics.
- **When redaction would drop something load-bearing, ask** rather than guess or leak. Never invent a repro detail to replace a redacted one.
- **Preview and confirm.** Before running `gh issue create`, print the full rendered issue — title, body, target repo, labels — and get an explicit go-ahead from the user. Do not post on inference. (The `--template` editor path already lets the user review before submitting; this gate covers the inline HEREDOC path.)

</constraints>

## Body shapes

### Bug

```markdown
## Summary

<one-line statement of what's broken>

## Repro

1. <step>
2. <step>
3. <step>

## Expected

<what should happen>

## Actual

<what happens, including exact error message in a code block>

## Environment

- Version / commit: <x>
- OS: <x>
- Runtime: <x>
```

### Feature / enhancement

```markdown
## Problem

<the user-facing pain, not the proposed solution>

## Proposal

<what to add or change>

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

<what was tried, what was read, where stuck>
```

## Skill invocation

Atomic-review skill not invoked — issue bodies are not finding lists. Tone comes from the active output style.

## Rules

<constraints>

No AI bylines. No "I think" / "maybe" / "perhaps". State facts. Code blocks for exact errors and commands. One issue per invocation — if user describes multiple unrelated problems, ask which to file or split.

</constraints>
