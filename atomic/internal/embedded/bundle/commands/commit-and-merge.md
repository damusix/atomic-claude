---
description: Pipeline — commit pending changes (atomic-commit skill), then /merge-to-main flow. One change to base, in one shot. Prefers `gh pr merge --merge` when a PR is open so GitHub closes the PR cleanly.
---

## Step 1 — Commit


1. Invoke the `atomic-commit` skill. Follow it for message format.
2. `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
3. **Read session reports for the current branch** (if any):
    - `BRANCH=$(git branch --show-current)` (or short SHA on detached HEAD).
    - `REPORTS_DIR=.claude/.scratchpad/session-reports/<BRANCH-sanitized>/`.
    - If the dir exists and contains `*.md`, read all files in chronological order and pass their content to the `atomic-commit` skill as supplemental why-context for the commit message. If the dir is empty or missing, proceed normally.
4. Stage relevant files explicitly by path. No `git add -A` / `.`. Skip secrets, build artifacts, large binaries. If staged/unstaged intent is ambiguous, ask.
5. **Documentation impact check** — invoke the `atomic-documentation` skill on the staged diff (`git diff --cached`). Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, has no `surfaces` key, or `surfaces` is empty, skip this step silently. For each non-empty surface:
    - Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
    - Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
    - **edit**: open the file, apply the suggested change, stage it with `git add <path>`.
    - **skip**: ask for a typed reason; record `doc-skip: <reason>` to append to the commit trailer block (after the body's blank line, in `git interpret-trailers --parse` range). One line per skip.
    - **continue**: treat as misclassification; no edit, no `doc-skip` line.

    Why doc-before-signals: new doc files staged at step 5 must be picked up by signals at step 6 in a single pass. Doc-after-signals would force a second stale-gate. One pass.

6. **Signals pre-commit** — evaluate these gates in order; stop at the first that fails:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If it exits 0 (fresh), skip.

    Both pass → invoke the `atomic-signals` skill in silent mode (no report line). If signals regenerate, stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md`.

    No file-extension allowlist. `atomic signals stale` is the source of truth; it fast-fails when nothing changed and catches structural shifts (e.g. a new `commands/*.md` file) that an extension list would miss.
7. Commit using a HEREDOC message.
8. **On successful commit (exit 0): delete the branch's session-reports dir.**
    - `rm -rf .claude/.scratchpad/session-reports/<BRANCH-sanitized>/`
    - Silent; this is the documented contract from `docs/spec/session-report.md`. The reports were consumed by the commit message — they have served their purpose. Leaving them would pollute future commits on the same branch with stale context.
    - If the commit failed or was aborted (pre-commit hook rejection, user interrupt): **do not delete.** Reports persist for the next attempt.
9. `git status` to confirm.

On pre-commit hook failure: fix root cause, re-stage, create a NEW commit. No `--no-verify`. No `--amend`. Session-reports dir stays in place across hook-failure retries; it is only deleted after a commit that actually succeeds.

No push. No PR. One commit per invocation — if diff spans unrelated concerns, ask how to split.

If nothing to commit AND branch has commits not on base → skip to step 2.
If nothing to commit AND branch up to date with base → stop.

## Step 2 — Merge



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
    3. Stale → invoke the `atomic-signals` skill (non-interactive: append `@-refs` to `CLAUDE.md` without confirmation). Stage `.claude/project/deterministic-signals.md`, `.claude/project/signals.md`, and `CLAUDE.md` if it was wired. Commit as a follow-up: `chore(signals): refresh after merge of <feature>`. Push after commit on remote path. Never amend.
6. **Delete local feature branch**: `git branch -d <feature>`.
    - **Remote path**: `gh pr merge --delete-branch` already removed the remote branch.
    - **Local path**: no remote branch to clean up.
7. Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.

## Report

`committed <sha>, merged <feature> into <base> as <MERGE_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

## Rules

- No AI bylines in commit messages.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- **Never force-push the base branch.** Remote-path rollback = `git revert`.
- **Remote path is preferred whenever a PR is open.** Step 1's new commit must be pushed BEFORE `gh pr merge` so the server-side merge includes it. The push goes to the PR's existing branch; no new PR is created.
