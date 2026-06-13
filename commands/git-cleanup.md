---
description: Scan and clean up stale git state — worktrees, local branches, optionally remote tracking refs. Dispatches a read-only scan via `atomic prompt git-cleanup`, presents an indexed report, asks user which to clean. No destructive ops without explicit confirmation. Defaults: 30-day staleness, local-only.
---

You orchestrate git cleanup. A generic subagent runs `atomic prompt git-cleanup` (read-only scan). You present the report. The user picks. You execute.

<workflow>

## Pre-flight

1. Verify inside a git repo: `git rev-parse --is-inside-work-tree`. If not: refuse with `not in a git repo.` and stop.
2. Determine base branch (used by scout):
    ```
    BASE=$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
      || git config init.defaultBranch \
      || echo main)
    ```
3. Determine the orchestrator's current worktree path: `git rev-parse --show-toplevel`. Scout must skip this from candidates.

## Step 1 — Resolve staleness threshold

Default: 30 days.

Check user memory for an override. The auto-memory system stores user preferences. Look for a memory entry whose description mentions "worktree", "branch", or "staleness" threshold. If found and it specifies a different value, use it. Otherwise, use 30.

If during execution the user says something like "remember N days as my staleness threshold" or "I prefer N days", save that as a feedback-type memory before continuing. Future runs pick it up automatically.

## Step 2 — Determine scope

If `$ARGUMENTS` is non-empty: target = `.worktrees/<$ARGUMENTS>/` (the specific worktree). Branch + worktree inspected together. Skip step 3 (single-target runs don't need the remote question).

If `$ARGUMENTS` is empty: target = `all`. Continue to step 3.

## Step 3 — Ask about remote scope

Prompt via `AskUserQuestion`:

```
Question: Include remote branches in the staleness scan?
Options:
  - Local only (recommended) — scan .worktrees/* and local branches
  - Local + remote — also fetch and flag stale remote branches (no remote deletion)
```

Default to local-only if the user is ambiguous.

## Step 4 — Dispatch read-only scan subagent

Dispatch a generic subagent via the `Agent` tool (omit `subagent_type` or use `general-purpose`).

Prompt the subagent with:

```
Run `atomic prompt git-cleanup` and follow the brief it prints exactly.
Scan params for this run:
  staleness_days: <N>
  target: all | .worktrees/<name>/
  include_remote: true | false
  base_branch: <base>
  current_worktree_path: <absolute path>

Apply these params as the brief's staleness/scope/base-branch settings.
Return the full indexed candidate table (worktree candidates + branch candidates
+ summary). Do not execute any git mutations — scan only.
```

Wait for the subagent to return the indexed candidate report.


## Step 5 — Present report to the user

Print the scout's report verbatim (or near-verbatim — collapse only obvious whitespace).

Append the selection prompt:

```
---

Type the indices to clean up, separated by spaces or commas.
Examples: `1 3 5`  |  `1,3,5`  |  `all`  |  `none`  |  `1-3 5`

Items with action=ask will need a second confirm (one at a time).
Items with action=flag are reported only — they cannot be selected.
Items with action=skip are blocked (e.g. dirty worktrees) — they cannot be selected.

Your selection:
```

## Step 6 — Parse selection

Wait for user input. Accept:

- Space-separated: `1 3 5`
- Comma-separated: `1,3,5`
- Ranges: `1-3` expands to `1 2 3`
- `all` → every candidate with `action ∈ {remove, delete, prune, ask}`. (Skips `flag` and `skip`.)
- `none` → exit, no action.
- Mixed: `1-3 5 7`.

Validate each index against the scout's report:

- Index not in report → tell user `invalid index <N>. retry your selection.` and re-prompt.
- Index points at `action=flag` → tell user `[<N>] is flag-only, cannot be cleaned. retry.` and re-prompt.
- Index points at `action=skip` → tell user `[<N>] is blocked (<reason>). retry.` and re-prompt.

## Step 7 — Confirm `ask` items individually

For each selected item with `action=ask`, prompt via `AskUserQuestion`:

```
Question: [<N>] <type> <path-or-branch> — <reason>. Clean it up anyway?
Options:
  - Yes, clean it up (force where required)
  - No, skip this item
```

On Yes: include in execution. On No: drop from execution.

## Step 8 — Execute selected actions

For each remaining selected item, in the order the user picked them, apply the action:

### `action=remove` (worktree + branch)

```bash
# cd to main repo root, not the worktree being removed
MAIN_ROOT=$(git -C "$(git rev-parse --git-common-dir)/.." rev-parse --show-toplevel)
cd "$MAIN_ROOT"

git worktree remove <path>
git branch -d <branch>            # or -D if user confirmed in step 7 for an unmerged branch
git worktree prune                # self-heal any related stale state
```

### `action=delete` (branch only)

```bash
git branch -d <branch>            # or -D if user confirmed in step 7
```

### `action=prune` (stale registration)

```bash
git worktree prune
```

(Single prune handles all stale registrations in one shot. If multiple `prune` items are selected, run it once at the end.)

### `action=ask` (after user confirmed Yes in step 7)

Same as the corresponding `remove` / `delete` — use `-D` instead of `-d` for branch deletion to force.

</workflow>

<output_format>

## Step 9 — Report

```
Cleaned up:
  ✓ [1] removed worktree .worktrees/feat-x/, deleted branch feat-x
  ✓ [3] removed worktree .worktrees/feat-z/, deleted branch feat-z (forced — was unmerged)
  ✓ [4] pruned stale registration .worktrees/old/
  ✓ [5] deleted branch bugfix-old

Skipped (you said no):
  • [6] spike-thing (gone upstream, you kept it)

Not selected:
  • [2] .worktrees/feat-y/ (dirty)
  • [7] experiment (flagged only)
```

Delete `$SCRATCH` once done.

</output_format>

<constraints>

## Rules

- Never delete the current worktree, the base branch, or the main worktree. Scout already excludes these; double-check before executing. **Why:** deleting the branch you're on or the base branch destroys in-flight work and corrupts the repo state in ways that are hard to undo.
- Never use `-D` (force-delete branch) without an explicit Yes from step 7. **Why:** `-D` discards commits that haven't been merged — data loss without an explicit user decision violates the destructive-ops confirm axiom.
- Never run `git push --delete` against remote branches. Remote cleanup is out of scope for this command. **Why:** remote deletions affect the whole team and can't be undone locally; they belong to a separate, intentional workflow.
- Always `cd` to the main repo root before `git worktree remove` — running it from inside the worktree being removed fails silently. **Why:** git refuses (or silently no-ops) worktree removal when the cwd is inside the target; the error surfaces only if you inspect the exit code, making the bug invisible.
- Print every git command before running it. Atomic style — no narration. **Why:** destructive ops must be auditable; the user needs to see exactly what ran and in what order before trusting the result.
- If any execution step errors, stop, report which item failed and why. Do not continue with remaining items until user says to. **Why:** partial cleanup can leave repo state inconsistent (e.g. worktree removed but branch still present); stopping on first failure keeps the remaining items predictable.
- No commits during this command. No PRs. Just cleanup. **Why:** scope creep — cleanup is already destructive enough; mixing in commit or PR actions makes the command's effect surface unpredictable and harder to audit.

## Open behaviors

- Staleness threshold lives in memory, not config. Default 30 days. Override by telling Claude to remember a different value.
- Remote scope is asked per-run when `$ARGUMENTS` is empty. Single-target runs skip the question.
- `git worktree prune` is self-healing — running it as part of any cleanup pass also cleans up unrelated stale registrations. That's fine.

</constraints>
