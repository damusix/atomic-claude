---
description: Merge current branch into base. No squash, no push. Re-runs tests on merged tip. Detects worktree provenance and prompts to delete. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
---

## Pre-flight

<staleness-check>

Before continuing, check whether signals or documentation may be out of date. This is advisory — ask the user and accept their answer. **Why:** the next session benefits from a fresh project snapshot; stale signals cause hallucinated file references.

1. **Signals** — run `command -v atomic && atomic signals stale`. If stale (exit 1), ask: "Signals are stale — refresh before continuing?" Accept yes or no.
2. **Documentation** — run `git diff <base>..HEAD --name-only` to get changed files. Invoke `atomic-documentation` in dry-run mode. If it identifies surfaces that may need updating, summarize them and ask: "These docs may be outdated: <list>. Update before continuing?" Accept yes or no.

If the user declines, proceed without further prompting.

</staleness-check>


<merge-preflight>

1. Invoke `atomic-verify` — confirm the branch is ready before merging.
2. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
3. `git branch --show-current` — if on base, stop: nothing to merge.
4. `git status --porcelain` — if dirty, stop: commit or stash first.
5. **Detect open PR:**
   ```
   gh pr view --json number,state -q '.state' 2>/dev/null
   ```
   - `OPEN` → use **remote path** (preferred — closes the PR cleanly via GitHub).
   - Otherwise → **local path**.
   - If `gh` is missing or unauthed, fall through to local path with a note.

   Remote path is preferred because a local merge + push does not auto-close the PR on GitHub — it stays open as "Not merged" indefinitely. `gh pr merge` is the only way to close it cleanly.

</merge-preflight>

## Steps

<merge-steps>

1. Record the feature branch name and PR number (if any).

2. **Execute the merge:**

    **Remote path** (PR open):
    1. `gh pr merge <PR#> --merge --delete-branch` — server-side merge, auto-closes the PR, removes remote branch.
    2. `git checkout <base>` then `git pull` to fast-forward local base.
    3. Record `MERGE_SHA=$(git rev-parse HEAD)`.

    **Local path** (no PR):
    1. `git checkout <base>` then `git pull`.
    2. `git merge <feature>`.
    3. Record `MERGE_SHA=$(git rev-parse HEAD)`.

3. **Re-run tests.** If tests fail:
    - Local path: ask about rolling back with `git reset --hard ORIG_HEAD`.
    - Remote path: the merge SHA is already published. Offer `git revert` instead — never force-push the base branch.

4. **Update implementation logs.** Find spec files with an `## Implementation log` section in the merged diff:
    ```bash
    git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```
    For each match, append: `**Merged into <base> as <MERGE_SHA> — <date>.**` Stage and commit as a follow-up. If none match, skip.

5. **Post-merge signals refresh:**
    <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Check `atomic signals stale`. If fresh (exit 0), skip.
3. Both pass → invoke the `atomic-signals` skill in silent mode. Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md`.

The `atomic signals stale` command is the source of truth — it fast-fails when nothing changed and catches structural shifts that a file-extension allowlist would miss.
</signals-refresh>
    If signals regenerate, commit as a follow-up: `chore(signals): refresh after merge of <feature>`. Push on remote path.

6. **Delete local feature branch:** `git branch -d <feature>`.
7. Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.

</merge-steps>

## Report

`merged <feature> into <base> as <MERGE_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

<git-safety>
- Stage explicitly by name (`git add <path>`), never `git add -A`. **Why:** `-A` can accidentally include secrets or untracked binaries.
- Use relative paths for `git add` based on the current working directory. **Why:** absolute paths and `git -C` can silently stage files outside the intended scope.
- Run each `git` command as a separate Bash call. **Why:** chaining with `&&` makes it impossible to inspect intermediate state and hides partial failures.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit — never `--amend`. **Why:** amending after a hook failure modifies the PREVIOUS commit, which may destroy unrelated work.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history. **Why:** force-pushing rewrites shared history, breaking every collaborator's checkout.
</git-safety>
