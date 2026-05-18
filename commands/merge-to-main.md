---
description: Merge current branch into base. No squash, no push. Re-runs tests on merged tip. Detects worktree provenance and prompts to delete. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
---

## Pre-flight

1. Invoke `atomic-verify` skill — gate: no merge claim without fresh evidence.
2. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
3. `git branch --show-current`. If on base: `refused: already on <base>. nothing to merge.`
4. `git status --porcelain`. If dirty: `refused: working tree dirty. /commit-only first, then /merge-to-main.`
5. **Detect open PR for this branch.** Decides remote vs local path:
   ```
   gh pr view --json number,state -q '.state' 2>/dev/null
   ```
   - Output `OPEN` → capture PR number; use **remote path** (preferred — closes the PR cleanly).
   - Otherwise → **local path**. A local merge of a branch that has an open PR leaves the PR open as "Not merged" because the merge commit on base carries no PR reference; prefer remote whenever a PR exists.
   - If `gh` is missing or unauthed: fall through to local path with a one-line note.

## Steps

1. Record feature branch name and PR number (if any).
2. **Execute merge — pick path:**

    - **Remote path** (PR open):
        1. `gh pr merge <PR#> --merge --delete-branch`. Server-side merge (default strategy; preserves commits). Auto-closes the PR. `--delete-branch` removes the remote branch.
        2. `git checkout <base>`.
        3. `git pull` to fast-forward local base to the new tip.
        4. Record SHA: `MERGE_SHA=$(git rev-parse HEAD)`.

    - **Local path** (no PR):
        1. `git checkout <base>`.
        2. `git pull`.
        3. `git merge <feature>` (default strategy; no `--no-ff` forced, no `--squash`).
        4. Record SHA: `MERGE_SHA=$(git rev-parse HEAD)`. This is the merge commit if non-FF, otherwise the new HEAD (= feature tip).

3. Re-run tests. If fail:
    - **Local path**: ask user about rolling back with `git reset --hard ORIG_HEAD`.
    - **Remote path**: the merge SHA is already published on `origin/<base>`. `git reset --hard` is wrong (would diverge from origin). Offer `git revert -m 1 <MERGE_SHA>` (if it's a merge commit) or `git revert <MERGE_SHA>` (if FF). Never force-push the base branch.
4. **Update implementation logs.** After tests pass, find spec files whose content changed across the merge (not just the merge commit itself — fast-forward merges have no merge commit, and non-FF merge commits don't carry file diffs):

    ```bash
    git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```

    For each match, append at end-of-file:

    ```
    **Merged into `<base>` as `<MERGE_SHA>` — <YYYY-MM-DD>.** Iteration commits above remain reachable in history.
    ```

    Stage by explicit path. Commit as a follow-up: `docs(spec): record merge SHA <MERGE_SHA>`. Push after commit on remote path. Never amend. If no specs match: skip silently.
5. **Post-merge signals refresh.** Defense in depth — even if the branch's commits each ran `/commit-only`, a merged PR from another contributor or manual commits may have bypassed it. Evaluate in order; stop at first failure:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If 0 (fresh), skip.
    3. Stale → invoke the `atomic-signals` skill (non-interactive: append `@-refs` to `CLAUDE.md` without confirmation). Stage `.claude/project/deterministic-signals.md`, `.claude/project/inferred-signals.md`, and `CLAUDE.md` if it was wired. Commit as a follow-up: `chore(signals): refresh after merge of <feature>`. Push after commit on remote path. Never amend.
6. **Delete local feature branch**: `git branch -d <feature>`.
    - **Remote path**: `gh pr merge --delete-branch` already removed the remote branch.
    - **Local path**: no remote branch to clean up.
7. Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.

## Report

`merged <feature> into <base> as <MERGE_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

## Rules

- No `--no-verify`. No `--amend` on hook failure — fix root cause and recommit.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- **Never force-push the base branch.** If a remote-path rollback is needed post-merge, use `git revert` — the bad SHA stays in history, a new commit reverses it.
- **Remote path is preferred whenever a PR is open.** GitHub does not auto-close PRs on local merge + push of the base branch; the PR stays open as "Not merged" indefinitely. `gh pr merge` is the only way to close it cleanly without manual `gh pr close`.
