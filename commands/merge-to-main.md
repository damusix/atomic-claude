---
description: Merge current branch into base. No squash, no push. Re-runs tests on merged tip. Detects worktree provenance and prompts to delete.
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

## Steps

1. `git checkout <base>`.
2. `git pull`.
3. `git merge <feature>` (default strategy; no `--no-ff` forced, no `--squash`).
4. Re-run tests. If fail: report failures. Ask user: roll back with `git reset --hard ORIG_HEAD`?
5. **Update implementation logs.** After tests pass, find spec files whose content changed across the merge (not just the merge commit itself — fast-forward merges have no merge commit, and non-FF merge commits don't carry file diffs):

    ```bash
    git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```

    For each match, append at end-of-file:

    ```
    **Merged into `<base>` as `<merge-or-tip-sha>` — <YYYY-MM-DD>.** Iteration commits above remain reachable in history.
    ```

    `<merge-or-tip-sha>` = the merge commit if non-FF, otherwise the new HEAD (which equals the feature tip). Stage by explicit path. Commit as a follow-up: `docs(spec): record merge SHA <sha>`. Never amend. If no specs match: skip silently.
6. **Post-merge signals refresh.** Defense in depth — even if the branch's commits each ran `/commit-only`, a merged PR from another contributor or manual commits may have bypassed it. Evaluate in order; stop at first failure:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If 0 (fresh), skip.
    3. Stale → invoke the `atomic-signals` skill (non-interactive: append `@-refs` to `CLAUDE.md` without confirmation). Stage `.claude/project/deterministic-signals.md`, `.claude/project/inferred-signals.md`, and `CLAUDE.md` if it was wired. Commit as a follow-up: `chore(signals): refresh after merge of <feature>`. Never amend.
7. `git branch -d <feature>`.
8. Worktree check: `git worktree list`. If the feature branch lived in `.worktrees/<feature>/`, ask via AskUserQuestion:
   > Branch was checked out in worktree at `<path>`. Delete it?
   > - Yes, remove worktree
   > - No, keep it

   On Yes: find repo root via `git rev-parse --show-toplevel` on the main checkout (not the worktree). `git worktree remove <path>`. `git worktree prune`.

## Report

`merged <feature> into <base>. branch deleted. worktree: <kept|removed>.`

## Rules

- No `--no-verify`. No `--amend` on hook failure — fix root cause and recommit.
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
