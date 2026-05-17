---
name: atomic-commit
description: >
  Compressed commit message generator. Cuts noise from commit messages while preserving
  intent and reasoning. Conventional Commits format. Subject ≤50 chars, body only when
  "why" isn't obvious. Use when user says "write a commit", "commit message",
  "generate commit", or invokes /atomic-commit. Auto-triggers when staging changes.
---

Write commit messages terse and exact. Conventional Commits. No fluff. Why over what.

## Rules

**Subject line:**

- `<type>(<scope>): <imperative summary>` — `<scope>` optional
- Types: `feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `chore`, `build`, `ci`, `style`, `revert`
- Imperative mood: "add", "fix", "remove" — not "added", "adds", "adding"
- ≤50 chars when possible, hard cap 72
- No trailing period
- Match project convention for capitalization after the colon

**Body (only if needed):**

- Skip entirely when subject is self-explanatory
- Add body only for: non-obvious *why*, breaking changes, migration notes, linked issues
- Wrap at 72 chars
- Bullets with `-`, not `*`
- Reference issues/PRs at end: `Closes #42`, `Refs #17`

**What NEVER goes in:**

- "This commit does X", "I", "we", "now", "currently" — the diff says what
- "As requested by..." — use `Co-authored-by:` trailer
- "Generated with Claude Code" or any AI attribution
- Emoji (unless project convention requires)
- Restating the file name when scope already says it

## Examples

Diff: new endpoint for user profile with body explaining the why

- Bad: `feat: add a new endpoint to get user profile information from the database`
- Good:

    ```
    feat(api): add GET /users/:id/profile

    Mobile client needs slim profile payload to cut LTE bandwidth on
    cold-launch screens.

    Closes #128
    ```

Diff: breaking API change

- Good:

    ```
    feat(api)!: rename /v1/orders to /v1/checkout

    BREAKING CHANGE: clients on /v1/orders must migrate to /v1/checkout
    before 2026-06-01. Old route returns 410 after that date.
    ```

## Auto-Clarity

Always include body for: breaking changes, security fixes, data migrations, anything reverting a prior commit. Never compress these into subject-only — future debuggers need the context.

## Supplemental input: session reports

When the invoking ship verb passes session-report content (markdown files from `.claude/.scratchpad/session-reports/<branch>/`), treat it as **why-context** for the message. The diff still drives *what*; the reports drive *why*. Specifically:

- Read the report bodies in chronological order.
- Pull the "Why" sections into the commit body when the reasoning is non-obvious from the diff.
- Pull the "Open threads" sections forward only if they describe a known limitation worth recording (rare).
- Do not paste the reports verbatim. Compress to fit the Body rules above (wrap at 72, bullets, no fluff).
- Reports describing rejected alternatives belong in the body when the chosen path is non-obvious.

If no reports are passed in, behave as before — diff alone drives synthesis.

## Boundaries

Generates the commit message only. Does not run `git commit`, does not stage files, does not amend, does not read or delete session-report files (that is the invoking ship verb's job). Output the message as a code block ready to paste.
