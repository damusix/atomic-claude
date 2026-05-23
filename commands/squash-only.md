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
2. **Read session reports for the current branch** (if any):
    - `BRANCH=$(git branch --show-current)`.
    - `REPORTS_DIR=.claude/.scratchpad/session-reports/<BRANCH-sanitized>/`.
    - If the dir exists and contains `*.md`, read all files in chronological order and pass their content to the `atomic-commit` skill as supplemental why-context alongside `SUBJECTS`. If the dir is empty or missing, proceed with `SUBJECTS` only.
3. `git reset --soft $(git merge-base HEAD <base>)` — collapses all branch commits into the index.
4. **Documentation impact check** — invoke the `atomic-documentation` skill on the staged diff (`git diff --cached`). Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, has no `surfaces` key, or `surfaces` is empty, skip this step silently. For each non-empty surface:
    - Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
    - Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
    - **edit**: open the file, apply the suggested change, stage it with `git add <path>`.
    - **skip**: ask for a typed reason; record `doc-skip: <reason>` to append to the commit trailer block (after the body's blank line, in `git interpret-trailers --parse` range). One line per skip.
    - **continue**: treat as misclassification; no edit, no `doc-skip` line.

    Why doc-before-signals: new doc files staged at step 4 must be picked up by signals at step 8 in a single pass. Doc-after-signals would force a second stale-gate. One pass.

5. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from `SUBJECTS` (+ session reports if read). Present it for user review/edit. Commit via HEREDOC once confirmed.
6. **On successful commit: delete the branch's session-reports dir.** `rm -rf .claude/.scratchpad/session-reports/<BRANCH-sanitized>/`. Silent. If the commit failed, leave the dir.
7. **Update implementation logs.** Find spec files in the just-squashed commit's diff that carry an `## Implementation log` section:

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
8. **Post-squash signals refresh** (defense in depth — even if each branch commit ran `/commit-only`, manual commits or rebased history may have bypassed it):

    **Signals pre-commit** — evaluate these gates in order; stop at the first that fails:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If it exits 0 (fresh), skip.

    Both pass → invoke the `atomic-signals` skill in silent mode (no report line). If signals regenerate, stage `.claude/project/deterministic-signals.md` and `.claude/project/inferred-signals.md`.

    No file-extension allowlist. `atomic signals stale` is the source of truth; it fast-fails when nothing changed and catches structural shifts (e.g. a new `commands/*.md` file) that an extension list would miss.

    When signals regenerate: commit as a follow-up: `chore(signals): refresh after squash`. Never amend the squash commit.
9. `git status` to confirm.

## Report

`squashed N commits into <new-sha>. branch still <branch>.`

## Rules

- No `--no-verify`. On hook failure: fix root cause, re-stage, NEW commit (no `--amend`).
- Use relative paths for `git add`. No `git -C`. No `cd && git`.
- Separate Bash calls for each `git` command — no `&&` chaining.
- This command does NOT merge into base and does NOT delete the branch.
