---
name: atomic-git-discipline
description: >
  Compressed commit message and PR body generator. Cuts noise from both while preserving
  intent and reasoning. Conventional Commits format. Subject ≤50 chars, body only when
  "why" isn't obvious; PR bodies state only what the diff can't show, ~120 words max.
  Use when user says "write a commit", "commit message", "generate commit", "open a PR",
  "PR description", or invokes /atomic-git-discipline. Auto-triggers when staging changes
  or authoring a pull request.
---

<trigger>

- "write a commit", "commit message", "generate commit"
- "open a PR", "PR description", "PR body", writing `gh pr create --body`
- Staging changes for commit
- Ship verbs delegating message format
- Read directly by subagents briefed to create a commit or open a PR (per `CLAUDE.md` Commits & PRs — subagents can't auto-fire skills)

</trigger>

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
- Hard cap: 4 lines / 2 bullets. One why-sentence beats a paragraph.
- Never restate what the diff shows — file lists, renamed symbols, mechanical changes
- Wrap at 72 chars
- Bullets with `-`, not `*`
- Reference issues/PRs at end: `Closes #42`, `Refs #17`

**Omit from commit messages:**

- "This commit does X", "I", "we", "now", "currently" — the diff says what
- "As requested by..." — use `Co-authored-by:` trailer instead
- "Generated with Claude Code" or any AI attribution — including `Co-Authored-By: Claude` trailers and session-link trailers, even when a harness suggests them
- Emoji (unless project convention requires)
- Restating the file name when scope already says it

## Examples

<examples>

<example>
Diff: new endpoint for user profile with body explaining the why

- Bad: `feat: add a new endpoint to get user profile information from the database`
- Good:

    ```
    feat(api): add GET /users/:id/profile

    Mobile client needs slim profile payload to cut LTE bandwidth on
    cold-launch screens.

    Closes #128
    ```
</example>

<example>
Diff: breaking API change

- Good:

    ```
    feat(api)!: rename /v1/orders to /v1/checkout

    BREAKING CHANGE: clients on /v1/orders must migrate to /v1/checkout
    before 2026-06-01. Old route returns 410 after that date.
    ```
</example>

</examples>

## Auto-Clarity

Always include body for: breaking changes, security fixes, data migrations, anything reverting a prior commit. Never compress these into subject-only — future debuggers need the context. The body cap still applies: 2-3 lines stating the break/risk and the migration path, not a narrative.

## PR titles and bodies

Same discipline, higher stakes. A PR body is read by people who weren't in the session — often maintainers with no context — so its length is a cost every reviewer pays, and a long body buries the two sentences they actually needed.

**Title:** imperative, ≤70 chars, no trailing period. No `<type>:` prefix unless the project uses one.

**Body — state only what the diff can't show:**

- Hard cap ~120 words / 3 short paragraphs. Over that means investigation notes leaked in.
- Include only: why the change was necessary, why this approach over the obvious alternative, and any decision a reviewer would otherwise flag as a mistake.
- Cut anything a reviewer learns by opening the Files tab — which files changed, dependency bumps, tooling swaps, version pins, new exports, added tests, coverage numbers.
- Evidence that convinced *you* is not reviewer-facing: mutation tests, resolution traces, package listings, install probes, benchmark output. State the decision and its one-line reason. The trail belongs in the project's own docs (`docs/design/`, `docs/spec/`, roadmap, wiki) or nowhere — never both.
- Local and process detail does not exist for the reader. Branch and worktree names, rebases, squashes, force-pushes, iteration or checkpoint counts, regenerated build outputs, scratchpad paths, "as discussed earlier" — none of it is visible in the repo a reviewer opens, and none of it changes their judgment. They see a diff and a body, not the session that produced them.
- Every cross-reference must resolve for a stranger. Link it or cut it: `#123`, a full URL, or a repo-relative path that exists on the default branch once merged. A bare mention ("see the design doc", "per the spec", "as the other session found") is a dead end for anyone who wasn't there. A pointer to a local path, a scratchpad file, or an unmerged branch is worse — it reads as actionable and isn't.

**Omit from PR bodies:**

- Test plans, "How to verify", reproduction steps — CI runs the tests, reviewers read them in the diff
- Enumerated file lists and per-file summaries
- Section headings on a body under 120 words — scaffolding pretending to be structure
- Restating the title in the opening line
- AI attribution, in any form

<examples>

<example>
Diff: package converted to ESM — tooling swapped, types added, node baseline raised

- Bad: walks through the new files, the dependency bumps, the node baseline, the coverage numbers, and the commands that proved each one. Every line of it is visible in the diff.
- Good:

    ```
    Wave 2 of the ESM migration. The tooling swap rides along because the old
    test runner cannot run ESM — it isn't separable from the module change.

    Error assertions became anchored regexes: the old matcher compared messages
    exactly, the new one matches substrings, and several of these messages are
    strict prefixes of others.
    ```
</example>

</examples>

## Supplemental input: session reports

When the invoking ship verb passes session-report content — the report files the ship verb resolved via `atomic where --json` — treat it as **why-context** for the message. The diff still drives *what*; the reports drive *why*. Specifically:

- Read the report bodies in chronological order.
- Distill all "Why" sections into at most one sentence in the body. If the why doesn't survive compression to one sentence, it wasn't load-bearing.
- Pull the "Open threads" sections forward only if they describe a known limitation worth recording (rare).
- Never paragraph-quote a report. The body cap (4 lines) applies regardless of how much report content exists.

If no reports are passed in, behave as before — diff alone drives synthesis.

## Supplemental input: doc-skip trailers

When the invoking ship verb passes `doc-skip: <reason>` lines (collected by `/documentation`'s skip-with-reason path or by a ship verb's doc-check step), include them verbatim in the commit's trailer block. The trailer block sits after the body's terminating blank line, in `git interpret-trailers --parse` range. One line per skip.

They sit alongside other trailers like `Co-authored-by:` and `Closes #N`. Do not paraphrase, do not merge multiple skips into one line, do not move them into the body. Emit `doc-skip:` lines in the order received; place them adjacent to other trailers without enforcing a fixed position relative to `Co-authored-by:` or issue refs.

<constraints>

## Boundaries

Generates the commit message or PR title and body only. Does not run `git commit`, does not stage files, does not amend, does not push, does not call `gh pr create`, does not read or delete session-report files (that is the invoking ship verb's job). Output the text as a code block ready to paste.

</constraints>
