---
description: Open an issue against the atomic-claude repo itself (bugs/feature requests with the installed config, not the user's current project).
---

## Workflow

File only issues about Atomic itself against the public `damusix/atomic-claude` repository. For the user's project, use `/report-issue`. `/retro` may recommend this command, but only file on request.

1. Check `command -v gh` and `gh auth status`. If missing or unauthenticated, stop and give the install or login instructions.
2. Classify as bug, enhancement, or question; ask if ambiguous. Use one issue per unrelated problem. State the target repository.
3. Capture `atomic --version`, or note that the binary is absent. Search all states with `gh issue list --repo damusix/atomic-claude --search "<key terms>" --state all --limit 5`; show close matches and ask before creating a duplicate.
4. Check target templates using `gh api repos/damusix/atomic-claude/contents/.github/ISSUE_TEMPLATE` and use an applicable template when available. Draft a title of at most 70 characters and a body with the relevant shape below. Check labels on the target; use `bug`, `enhancement`, or `question` when available, plus valid user-requested labels.
5. Apply the privacy rules below. Preview the complete title, body, target and labels, and obtain explicit approval before posting, including when using a template.
6. Run `gh issue create --repo damusix/atomic-claude` with the approved content and print the resulting URL. Specify the target for every repository-specific `gh` operation.

{{ template "report-issue-privacy" . }}

## Body

- **Bug:** summary, exact reproduction, expected and actual behavior, Atomic version, install method, OS, harness version when known, and affected artifact.
- **Enhancement:** problem, proposal, optional timing and out-of-scope boundaries.
- **Question:** the question, Atomic version and relevant context.

Use exact commands and errors in code blocks after redaction. Do not invent missing facts or add AI attribution.
