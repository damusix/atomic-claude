---
description: Squash all branch commits via git merge --squash on base. One clean commit on base. Re-runs tests. Detects worktree, prompts to delete. Prefers `gh pr merge --squash` when a PR is open so GitHub closes the PR cleanly.
---

## 1. Squash



<squash-preflight>

1. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
2. `git branch --show-current` — if on base, stop: nothing to squash.
3. `git status --porcelain` — if dirty, stop: commit or stash first.
4. Count commits: `git rev-list --count <base>..HEAD` — if only 1, stop: nothing to squash.

</squash-preflight>


<squash-steps>

1. Gather subjects (oldest-first): `SUBJECTS=$(git log <base>..HEAD --format='%s' --reverse)`.
2. **Session reports** — check for `.claude/.scratchpad/session-reports/<branch>/`. If the dir has `*.md` files, read them chronologically and pass as supplemental why-context alongside `SUBJECTS`.
3. `git reset --soft $(git merge-base HEAD <base>)` — collapse all branch commits into the index.
4. <doc-impact>
Check whether the staged changes affect documentation. Invoke the `atomic-documentation` skill on `git diff --cached`.

Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, or has no surfaces, skip silently.

For each surface found:
- Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
- Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
- **edit** — open the file, apply the change, stage with `git add <path>`.
- **skip** — ask for a typed reason; record `doc-skip: <reason>` as a commit trailer.
- **continue** — treat as misclassification; move on.

Run doc-impact before signals refresh so that new doc files get picked up by signals in one pass.
</doc-impact>
5. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from `SUBJECTS` (plus session reports if present). Present for review, then commit via HEREDOC.
6. **Clean up session reports** — on successful commit, delete `.claude/.scratchpad/session-reports/<branch>/`. If the commit failed, leave them.
7. **Update implementation logs.** Find spec files with an `## Implementation log` section in the squashed diff:
    ```bash
    git show --name-only --pretty=format: HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```
    For each match, append: `**Squashed to <new-sha> — <date>.** Per-iteration SHAs above are historical (unreachable from any branch).` Stage and commit as a follow-up. If none match, skip.
8. **Post-squash signals refresh:**
    <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Check `atomic signals stale`. If fresh (exit 0), skip.
3. Both pass → invoke the `atomic-signals` skill in silent mode. Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md`.

The `atomic signals stale` command is the source of truth — it fast-fails when nothing changed and catches structural shifts that a file-extension allowlist would miss.
</signals-refresh>
    If signals regenerate, commit as a follow-up: `chore(signals): refresh after squash`.
9. `git status` to confirm.

</squash-steps>

## 2. Merge



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

<git-safety>
- Use relative paths for `git add` based on the current working directory.
- Run each `git` command as a separate Bash call.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit. The hook exists for a reason.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history.
</git-safety>
