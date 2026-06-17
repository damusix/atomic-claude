---
name: atomic-review
description: >
  Compressed code review comments. Cuts noise from PR feedback while preserving the
  actionable signal. Each comment is one line: location, problem, fix. Use when user
  says "review this PR", "code review", "review the diff", or invokes /atomic-review.
  Auto-triggers when reviewing pull requests.
---

<trigger>

- "review this PR", "code review", "review the diff", "check this PR"
- "review this change", "review my changes"
- Reviewing pull requests or diffs

</trigger>

Write code review comments terse and actionable. One line per finding. Location, problem, fix. No throat-clearing.

## Pre-flight: blast radius (when a code-intel index is present)

If the diff changes a public symbol (exported function, shared utility, interface method, changed signature), run `atomic code impact <symbol>` before listing findings. Callers that assume the old behavior become `🟡 risk` findings. One targeted query per changed public symbol — no full-graph dump. Skip silently when no index is present.

<output_format>

## Format

`path:line: <emoji> <severity>: <problem>. <fix>.`

Single-file reviews may use `L<line>: ...` instead of `path:L<line>: ...`.

## Severity

| Emoji | Severity | Use for |
|-------|----------|---------|
| 🔴 | bug | Wrong output, crash, security hole, data loss |
| 🟡 | risk | Edge case, race, leak, perf cliff, missing guard |
| 🔵 | nit | Style, naming, micro-perf — emit only if user asked thorough |
| ❓ | question | Need author intent before judging |

End the review with a totals line: `totals: 1🔴 1🟡 1❓`. Zero findings → `No issues.` File order, ascending line numbers within file.

## Over-engineering

Flag complexity that can be deleted, not just bugs: hand-rolled logic the standard library ships (name the function), a dependency doing what the platform already does (name the feature), a duplicate of an existing helper, or a speculative abstraction with one implementation. Use 🟡 risk when it carries real cost, 🔵 nit for a pure shrink. Always name the concrete replacement, never "consider simplifying".

- `src/util.ts:12-38: 🟡 risk: hand-rolled email validator. Real validation is the confirmation mail; 26 lines go.`
- `src/date.ts:4: 🔵 nit: moment.js for one format call. Intl.DateTimeFormat, 0 deps.`
- `src/repo.ts:88: 🟡 risk: AbstractRepository with one impl. Inline until a second exists.`

## Drop

- "I noticed that...", "It seems like...", "You might want to consider..."
- "This is just a suggestion but..." — use `🔵 nit` instead
- "Great work!", "Looks good overall but..." — say it once at the top, not per comment
- Restating what the line does — reviewer can read the diff
- Hedging ("perhaps", "maybe", "I think") — if unsure use `❓ question`

## Keep

- Exact line numbers
- Exact symbol/function/variable names in backticks
- Concrete fix, not "consider refactoring this"
- The *why* when the fix isn't obvious from the problem statement

## Examples

Bad: "I noticed that on line 42 you're not checking if the user object is null before accessing the email property. This could potentially cause a crash if the user is not found in the database. You might want to add a null check here."

Good: `src/auth.ts:42: 🔴 bug: user can be null after .find(). Add guard before .email.`

Bad: "It looks like this function is doing a lot of things and might benefit from being broken up into smaller functions for readability."

Good: `src/handler.ts:88-140: 🔵 nit: 50-line fn does 4 things. Extract validate/normalize/persist.`

Bad: "Have you considered what happens if the API returns a 429? I think we should probably handle that case."

Good: `src/client.ts:23: 🟡 risk: no retry on 429. Wrap in withBackoff(3).`

## Auto-Clarity

Drop terse mode for:

- Security findings — CVE-class bugs need full explanation plus references. Write the prose first, then resume terse for the rest of the review.
- Architectural disagreements — need rationale, not a one-liner.
- Onboarding contexts where the author is new and needs the "why".

</output_format>

<constraints>

## Boundaries

Reviews only. Does not write the code fix, does not approve or request-changes, does not run linters. Output the comments ready to paste into the PR.

</constraints>
