---
description: Capture what changed this session and why, scoped to the current branch. Read by ship verbs when synthesizing the commit message; deleted after a successful commit.
---

## When to use

Long-running branch spanning multiple Claude Code sessions. The eventual commit (or squash) needs the *why* behind the work, but `git diff` alone loses that. Run `/session-report` at the end of a session to capture intent for the future commit-message synthesis.

Opt-in only. Does not auto-fire.

## Refuse to run if

- Working tree clean AND no unstaged changes since last commit → `refused: no changes since last commit — nothing to report.`

## Steps

1. **Determine branch scope key:**
    - `git branch --show-current`. If empty (detached HEAD), use `git rev-parse --short HEAD` and warn the user: `detached HEAD — report scoped to <sha> not a branch.`
2. **Compute paths:**
    - Dir: `.claude/.scratchpad/session-reports/<branch>/` (sanitize `/` in branch name to `-`).
    - Filename: `<YYYY-MM-DD-HHMM>-<slug>.md`. Slug from `$ARGUMENTS` if provided; otherwise infer from the most prominent change in the working tree (one or two kebab-case words).
    - On filename collision (same minute + same slug): append `-2`, `-3`, etc.
3. **Gather signal:**
    - `git status --porcelain` — list of touched files (staged + unstaged).
    - `git diff --stat` and `git diff --cached --stat` — magnitude per file.
    - Recent conversation context — what was tried, what was rejected, what the user clarified mid-flight.
4. **Write the report** to the computed path with this structure:

    ```markdown
    ---
    branch: <branch>
    date: <YYYY-MM-DD HH:MM>
    session_summary: <one line — what this session was about>
    ---

    ## What changed

    - `<path>` — <short description of edit>
    - `<path>` — <short description of edit>

    ## Why

    <Short paragraphs explaining the reasoning. What problem was being solved.
    What alternatives were rejected and why. What the user clarified that shaped
    the approach. Aim for a future-reader who needs enough context to write the
    commit message — not exhaustive prose.>

    ## Open threads (optional)

    - <unresolved question or follow-up deferred to a later session>
    ```

5. **Report path** to the user: `wrote .claude/.scratchpad/session-reports/<branch>/<file>.md`.

## Voice

Working-memory voice — bullets + short paragraphs. Not the atomic TUI fragment style (that's TUI replies). Not `atomic-prose` (that's enduring narrative docs). Internal context that the commit-message synthesis will read.

## Lifecycle

Reports persist on disk until consumed by a successful commit on the same branch. Each report is consumed and deleted by the next successful ship-verb commit (see "Ship-verb integration" in `docs/spec/session-report.md`). Failed or aborted commits leave reports in place for the next attempt.

If a branch is abandoned without a commit, the reports stay in `.claude/.scratchpad/session-reports/<branch>/` until `/git-cleanup` removes the branch (cleanup of the scratchpad subtree tracks the branch's lifecycle, not the reports'). Manual `rm -rf` is always safe — scratchpad is LLM-only working memory.

## Cross-references

- **`atomic-commit` skill** — receives the concatenated reports for the current branch as supplemental context when synthesizing the commit message.
- **Ship verbs that consume reports:** `/commit-only`, `/commit-and-pr`, `/commit-and-push`, `/commit-and-merge`, `/commit-and-squash`, `/squash-only`, `/squash-and-merge`. Each reads all reports for the current branch before message synthesis and deletes the branch's reports dir after a successful commit.
- **Exempt verbs** (no commit-message generation): `/pr-only`, `/push-only`, `/merge-to-main`. These ship existing commits unchanged.
- **Full spec:** `docs/spec/session-report.md`.

## Rules

- Never stage the report file. `.claude/.scratchpad/` is gitignored — staging would be a no-op and pollutes intent.
- One report per invocation. If the user wants two slices of the same session captured separately, they call `/session-report` twice with different slug arguments.
- No follow-up commits. The session report is consumed by the next commit on the branch; do not generate one of your own.
