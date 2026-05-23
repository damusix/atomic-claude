---
description: Pipeline — commit pending changes, then /squash-only flow. Tidies history in one shot.
---

## Step 1 — Commit

Invoke `atomic-commit` skill. Follow it for message format.

Run `/commit-only` flow:

- `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
- Stage relevant files explicitly by path. No `git add -A` / `.`. Skip secrets, build artifacts, large binaries.
- **Documentation impact check** — invoke the `atomic-documentation` skill on the staged diff (`git diff --cached`). Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, has no `surfaces` key, or `surfaces` is empty, skip this step silently. For each non-empty surface:
    - Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
    - Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
    - **edit**: open the file, apply the suggested change, stage it with `git add <path>`.
    - **skip**: ask for a typed reason; record `doc-skip: <reason>` to append to the commit trailer block (after the body's blank line, in `git interpret-trailers --parse` range). One line per skip.
    - **continue**: treat as misclassification; no edit, no `doc-skip` line.
- Commit via HEREDOC. Include any `doc-skip:` trailer lines collected above in the trailer block. No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- `git status` to confirm.

If nothing to commit → skip to step 2.

## Step 2 — Squash

Determine base:
```
gh repo view --json defaultBranchRef -q .defaultBranchRef.name 2>/dev/null \
  || git config init.defaultBranch \
  || echo main
```

If on base: `refused: already on <base>. nothing to squash.`

Count commits: `git rev-list --count <base>..HEAD`. If 1 (only the just-landed commit): `refused: only one commit on branch after commit. nothing to squash.`

1. Gather subjects (oldest-first): `git log <base>..HEAD --format='%s' --reverse`.
2. `git reset --soft $(git merge-base HEAD <base>)` — collapses all branch commits into the index.
3. **Documentation impact check** — invoke the `atomic-documentation` skill on the staged diff (`git diff --cached`). Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, has no `surfaces` key, or `surfaces` is empty, skip this step silently. For each non-empty surface:
    - Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
    - Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
    - **edit**: open the file, apply the suggested change, stage it with `git add <path>`.
    - **skip**: ask for a typed reason; record `doc-skip: <reason>` to append to the commit trailer block (after the body's blank line, in `git interpret-trailers --parse` range). One line per skip.
    - **continue**: treat as misclassification; no edit, no `doc-skip` line.

    Why doc-before-signals: new doc files staged at step 3 must be picked up by signals at step 6 in a single pass. Doc-after-signals would force a second stale-gate. One pass.

4. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from gathered subjects. Include any `doc-skip:` trailer lines collected at step 3 in the trailer block. Present for user review/edit. Commit via HEREDOC once confirmed.
5. **Update implementation logs.** Find spec files in the just-squashed commit's diff that carry an `## Implementation log` section:

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
6. **Post-squash signals refresh.** Defense in depth — even if each branch commit ran `/commit-only`, manual commits or rebased history may have bypassed it. Evaluate in order; stop at first failure:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If 0 (fresh), skip.
    3. Stale → invoke the `atomic-signals` skill (non-interactive: append `@-refs` to `CLAUDE.md` without confirmation). Stage `.claude/project/deterministic-signals.md`, `.claude/project/inferred-signals.md`, and `CLAUDE.md` if it was wired. Commit as a follow-up: `chore(signals): refresh after squash`. Never amend the squash commit.
7. `git status` to confirm.

## Report

`committed pending change <sha-old>, squashed N commits into <sha-new>.`

## Rules

- No AI bylines in commit messages.
- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- Does NOT merge into base and does NOT delete the branch.
