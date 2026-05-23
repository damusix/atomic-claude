---
description: Squash all branch commits via git merge --squash on base. One clean commit on base. Re-runs tests. Detects worktree, prompts to delete. Prefers `gh pr merge --squash` when a PR is open so GitHub closes the PR cleanly.
---

## Pre-flight

1. Invoke `atomic-verify` skill — gate: no merge claim without fresh evidence.
2. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
3. `git branch --show-current`. If on base: `refused: already on <base>. nothing to squash-merge.`
4. `git status --porcelain`. If dirty: `refused: working tree dirty. commit or stash first.`
5. **Detect open PR for this branch.** Decides remote vs local path:
   ```
   gh pr view --json number,state -q '.state' 2>/dev/null
   ```
   - Output `OPEN` → capture PR number via `gh pr view --json number -q .number`; use **remote path** (preferred — closes the PR cleanly and lets GitHub manage the squash commit).
   - Otherwise → **local path** (no PR linkage; any prior `gh pr` reference would stay open as "Not merged" because the squash commit on base has no back-reference).
   - If `gh` is missing or unauthed: fall through to local path with a one-line note.

## Steps

1. Record feature branch name and PR number (if any).
2. Gather subjects (oldest-first) for the synthesized message: `git log <base>..<feature> --format='%s' --reverse`.
3. **Read session reports for the feature branch** (if any):
    - `REPORTS_DIR=.claude/.scratchpad/session-reports/<feature-sanitized>/`.
    - If the dir exists and contains `*.md`, read all files in chronological order and pass them to the `atomic-commit` skill as supplemental why-context alongside the gathered subjects. Read *before* the checkout in step 5 so the reports dir is resolved against the feature checkout.
4. **Synthesize the squash message.** Invoke `atomic-commit` skill with gathered subjects + session reports. Present for user review/edit. Capture the final subject and body — both paths consume them.
5. **Execute squash — pick path:**

    - **Remote path** (PR open):
        1. `gh pr merge <PR#> --squash --delete-branch --subject "<subject>" --body "$(cat <<'EOF'`
           ```
           <body>
           EOF
           )"
           ```
           Server-side squash. Auto-closes the PR. `--delete-branch` removes the remote branch regardless of repo "auto-delete head branches" setting.
        2. `git checkout <base>`.
        3. `git pull` to fast-forward local base to the new squash SHA.
        4. Record SHA: `SQUASH_SHA=$(git rev-parse HEAD)`.

    - **Local path** (no PR):
        1. `git checkout <base>`.
        2. `git pull`.
        3. `git merge --squash <feature>` — stages all changes, no commit yet.
        4. **Documentation impact check** — invoke the `atomic-documentation` skill on the staged diff (`git diff --cached`). Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, has no `surfaces` key, or `surfaces` is empty, skip silently. For each non-empty surface:
           - Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
           - Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
           - **edit**: open the file, apply the suggested change, stage it with `git add <path>`.
           - **skip**: ask for a typed reason; record `doc-skip: <reason>` to append to the commit trailer block (after the body's blank line, in `git interpret-trailers --parse` range). One line per skip.
           - **continue**: treat as misclassification; no edit, no `doc-skip` line.

           Why doc-before-signals: new doc files staged here must be picked up by signals at step 9 in a single pass. Doc-after-signals would force a second stale-gate. One pass. Remote path skips this sub-step — no staged diff is available after a server-side squash.

        5. Commit via HEREDOC with the synthesized subject + body.
        6. Record SHA: `SQUASH_SHA=$(git rev-parse HEAD)`.

6. **On successful merge: delete the feature branch's session-reports dir.** `rm -rf .claude/.scratchpad/session-reports/<feature-sanitized>/`. Silent. If the merge/commit failed, leave the dir.
7. Re-run tests. If fail:
    - **Local path**: ask user about rolling back with `git reset --hard ORIG_HEAD`.
    - **Remote path**: the squash SHA is already published on `origin/<base>`. `git reset --hard` is wrong (would diverge from origin). Offer `git revert <SQUASH_SHA>` instead, or surface the failure for manual triage. Never force-push the base branch.
8. **Update implementation logs.** After tests pass (so a follow-up commit won't sit on top of a rolled-back squash), find spec files in the squash diff that carry an `## Implementation log` section:

    ```bash
    git show --name-only --pretty=format: $SQUASH_SHA | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```

    For each match, append at end-of-file:

    ```
    **Squashed onto `<base>` as `<SQUASH_SHA>` — <YYYY-MM-DD>.** Per-iteration SHAs above are historical (unreachable post-squash).
    ```

    Stage by explicit path. Commit as a follow-up: `docs(spec): record squash SHA <SQUASH_SHA>`. Push after commit on remote path (so the impl-log SHA ends up on origin too). Never amend the squash commit (the SHA in the log would then not match itself). If no specs match: skip silently.
9. **Post-squash signals refresh.** Defense in depth — even if the branch's commits each ran `/commit-only`, manual commits or external contributions in the squashed history may have bypassed it. Evaluate in order; stop at first failure:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If 0 (fresh), skip.
    3. Stale → invoke the `atomic-signals` skill (non-interactive: append `@-refs` to `CLAUDE.md` without confirmation). Stage `.claude/project/deterministic-signals.md`, `.claude/project/inferred-signals.md`, and `CLAUDE.md` if it was wired. Commit as a follow-up: `chore(signals): refresh after squash of <feature>`. Push after commit on remote path. Never amend the squash commit.
10. **Delete local feature branch**: `git branch -D <feature>` (force required — squash leaves merge-base check unresolved).
    - **Remote path**: `gh pr merge --delete-branch` already removed the remote branch. Verify with `git fetch --prune origin` if you want to confirm.
    - **Local path**: no remote branch to clean up.
11. Worktree check: `git worktree list`. If feature branch lived in `.worktrees/<feature>/`, ask via `AskUserQuestion`:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find root via `git rev-parse --show-toplevel` on main checkout. `git worktree remove <path>`. `git worktree prune`.

## Report

`squash-merged <feature> into <base> as <SQUASH_SHA> [via gh pr <PR#>]. branch deleted [local + remote]. worktree: <kept|removed>.`

## Rules

- No AI bylines in commit messages, PR titles, or PR bodies.
- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- `-D` on branch delete is safe here because the squash commit on base contains the same tree.
- **Never force-push the base branch.** If a remote-path rollback is needed post-merge, use `git revert <SQUASH_SHA>` — the bad SHA stays in history, a new commit reverses it.
- **Remote path is preferred whenever a PR is open.** GitHub does not auto-close PRs when the squash commit lands locally and gets pushed — the new commit on base carries no PR reference, so the PR stays open as "Not merged" indefinitely. `gh pr merge --squash` is the only way to close the PR cleanly without manual `gh pr close`.
