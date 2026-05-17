---
description: Squash all commits on current branch into one. No merge. Synthesized commit message via atomic-commit skill. Does not touch base.
---

## Pre-flight

1. Determine base:
   ```
   gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
     || git config init.defaultBranch \
     || echo main
   ```
2. `git branch --show-current`. If on base: `refused: already on <base>. nothing to squash.`
3. `git status --porcelain`. If dirty: `refused: working tree dirty. commit or stash first.`
4. Count commits: `git rev-list --count <base>..HEAD`. If 1: `refused: only one commit on branch. nothing to squash.`

## Steps

1. Gather subjects (oldest-first): `SUBJECTS=$(git log <base>..HEAD --format='%s' --reverse)`.
2. `git reset --soft $(git merge-base HEAD <base>)` — collapses all branch commits into the index.
3. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from `SUBJECTS`. Present it for user review/edit. Commit via HEREDOC once confirmed.
4. **Update implementation logs.** Find spec files in the just-squashed commit's diff that carry an `## Implementation log` section:

    ```bash
    git show --name-only --pretty=format: HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```

    For each match, append at end-of-file:

    ```
    **Squashed to `<new-sha>` — <YYYY-MM-DD>.** Per-iteration SHAs above are historical (unreachable from any branch).
    ```

    Stage by explicit path. Commit as a follow-up: `docs(spec): record squash SHA <new-sha>`. Never amend the squash commit. If no specs match: skip silently.
5. **Post-squash signals refresh.** Defense in depth — even if each branch commit ran `/commit-only`, manual commits or rebased history may have bypassed it. Evaluate in order; stop at first failure:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If 0 (fresh), skip.
    3. Stale → invoke the `atomic-signals` skill (non-interactive: append `@-refs` to `CLAUDE.md` without confirmation). Stage `.claude/project/deterministic-signals.md`, `.claude/project/inferred-signals.md`, and `CLAUDE.md` if it was wired. Commit as a follow-up: `chore(signals): refresh after squash`. Never amend the squash commit.
6. `git status` to confirm.

## Report

`squashed N commits into <new-sha>. branch still <branch>.`

## Rules

- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- This command does NOT merge into base and does NOT delete the branch.
