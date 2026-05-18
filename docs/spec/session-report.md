# Session report

## Goal

Capture the *why* behind work-in-progress on a branch across multiple Claude Code sessions, so that when the branch is eventually committed (or squashed), the resulting commit message carries the reasoning — not just the diff.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Ship `/session-report` command per Surface + Storage layout sections | `commands/session-report.md`, `.claude/.scratchpad/session-reports/` | Manual: command writes reports to expected path; ship verbs read and delete them after commit. |

## Problem statement

A single feature branch frequently spans multiple Claude Code sessions. Each session's context (what was tried, why an approach was rejected, what the user clarified mid-flight) is lost when the session ends. By the time `/commit-only` or `/squash-only` runs, the commit message has to be synthesized from `git diff` alone — which loses the reasoning. Recent commits on this very repo exemplify the failure: subject lines are accurate, but the "why" is gone.

## Non-goals

- Not a replacement for `STATE.md` inside `/subagent-implementation` (per-task iteration log, not per-session).
- Not auto-fired. Opt-in command only (axiom 5).
- Not a changelog. Reports are ephemeral; deleted after the commit that consumes them.
- Not a debugging journal. Captures *what changed and why*, not hypothesis trees.

## Surface

| Verb | Behavior |
|------|----------|
| `/session-report` | Generate one report covering the current session. Writes to the branch's session-reports folder. |
| `/session-report <description>` | Same, with a user-supplied slug for the filename. |

No flags. No subcommands.

## Storage layout

```
.claude/.scratchpad/session-reports/<branch>/<YYYY-MM-DD-HHMM>-<slug>.md
```

- **Root** under `.claude/.scratchpad/` — same gitignored scratchpad as `/subagent-implementation`, distinct subtree.
- **Branch-scoped** subdirectory so multiple branches don't cross-contaminate. Branch name sanitized for filesystem (slashes → `-`).
- **Timestamped filename** so multiple reports on one branch sort chronologically.
- **Slug** derived from `<description>` arg if provided, otherwise from the most prominent change (LLM-generated).

## Report contents

Required sections:

- **Frontmatter:** `branch`, `date`, `session_summary` (one line).
- **What changed** — bulleted list of files touched. One line per file: `path — short description of edit`.
- **Why** — short paragraphs explaining the reasoning. What problem was being solved, what alternatives were rejected, what the user clarified. Not exhaustive; aim for a future-reader who needs enough to write the commit message.
- **Open threads** (optional) — unresolved questions or follow-ups deferred to a later session.

Voice: spec/design terse-structured. Not `atomic-prose` (that's for enduring narrative docs). Not atomic TUI style (that's for replies). This is internal working-memory voice — bullets + short paragraphs, no fluff.

## Ship-verb integration

Affected verbs (all generate a commit message): `/commit-only`, `/commit-and-pr`, `/commit-and-push`, `/commit-and-merge`, `/commit-and-squash`, `/squash-only`, `/squash-and-merge`.

Sequence at commit-message time:

1. Compute the branch's session-reports dir: `.claude/.scratchpad/session-reports/<branch>/`.
2. If the dir exists and contains `*.md` files, read all of them in chronological order.
3. Pass the concatenated reports to the `atomic-commit` skill as **additional context** for message generation (the skill already reads the staged diff; reports supplement the *why*).
4. After the commit succeeds (post `git commit`, verified by exit code 0), delete the branch's session-reports dir: `rm -rf .claude/.scratchpad/session-reports/<branch>/`.
5. If the commit fails or is aborted, do **not** delete. Reports persist for the next attempt.

Cleanup rule (axiom 3 — destructive ops): the delete is silent and automatic only after a successful commit on the same branch. No prompt; this is the documented contract.

Exempt (no commit message generated): `/pr-only`, `/push-only`, `/merge-to-main`. These verbs ship existing commits and do not consult session reports.

## Edge cases

| Case | Behavior |
|------|----------|
| No staged or unstaged changes when `/session-report` runs | Refuse with: "No changes since last commit — nothing to report." |
| Branch is `main` / `master` / base | Allowed. Folder still scoped by branch name. |
| Detached HEAD | Use short SHA as the branch-scope key. Warn user. |
| Existing report file collides (same timestamp + slug) | Append `-2`, `-3`, etc. |
| Reports exist for a different branch than the one being committed | Ignored. Only the current branch's folder is read or deleted. |
| User runs `/squash-only` and the squash collapses N commits | Read once, feed once, delete once. Reports describe the branch's *aggregate* intent, which matches what a squash needs. |

## Cross-references

- **`atomic-commit` skill** — receives session-report content as supplemental context for message generation. The skill must declare this input source in its description.
- **Ship verbs** — each one's command file must document the read-and-delete behavior in its body.
- **`claude.local.md` → "Where things live"** — add a bullet for the session-reports scratchpad subtree.
- **Bundle inclusion** — the new `commands/session-report.md` ships automatically (top-level command, no allowlist; see `bundlemirror/mirror.go`).

## Open questions

- Should reports survive a failed commit indefinitely, or expire after N days? **Resolved:** survive until success. Stale reports on abandoned branches are cleaned by `/git-cleanup` if/when the branch itself is cleaned.
- Should `/session-report` itself stage the report file? **Resolved:** no. The report lives in `.claude/.scratchpad/` which is gitignored — staging would be a no-op.
- Multiple developers on the same branch (rare for this repo, common elsewhere)? **Resolved:** scratchpad is local-only. Each developer has their own reports.

## Change log

### 2026-05-17 — Initial spec

**What changed:** New spec for `/session-report` command and its integration with the commit/squash verb family.

**Why:** Branches frequently span multiple Claude sessions. Without an explicit capture mechanism, the reasoning behind incremental work is lost by the time the branch is committed or squashed. Recent commits on this repo show the gap: accurate subjects, missing why.


### 2026-05-17 — Conform to validator rules

**What changed:** Added `## Checkpoints` section (was missing) with one backfilled row covering the shipped `/session-report` command.

**Why:** `atomic validate spec` rule S5 flagged the file when the validator landed (CP-5 of `atomic-validate`).


### 2026-05-18 — Move Checkpoints section to after Goal

**What changed:** Relocated `## Checkpoints` from just before `## Change log` to immediately after `## Goal`, matching the canonical spec section order.

**Why:** The previous placement was a cleanup-pass artifact; specs in this project list Checkpoints near the top (after Goal) so they're visible without scrolling past the full body.
