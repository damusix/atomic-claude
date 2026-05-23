---
description: Commit current changes, then push. No PR, no merge. Delegates message format to the atomic-commit skill.
---

## Pipeline

### 1. Commit


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

    Both pass → invoke the `atomic-signals` skill in silent mode (no report line). If signals regenerate, stage `.claude/project/deterministic-signals.md` and `.claude/project/inferred-signals.md`.

    No file-extension allowlist. `atomic signals stale` is the source of truth; it fast-fails when nothing changed and catches structural shifts (e.g. a new `commands/*.md` file) that an extension list would miss.
7. Commit using a HEREDOC message.
8. **On successful commit (exit 0): delete the branch's session-reports dir.**
    - `rm -rf .claude/.scratchpad/session-reports/<BRANCH-sanitized>/`
    - Silent; this is the documented contract from `docs/spec/session-report.md`. The reports were consumed by the commit message — they have served their purpose. Leaving them would pollute future commits on the same branch with stale context.
    - If the commit failed or was aborted (pre-commit hook rejection, user interrupt): **do not delete.** Reports persist for the next attempt.
9. `git status` to confirm.

On pre-commit hook failure: fix root cause, re-stage, create a NEW commit. No `--no-verify`. No `--amend`. Session-reports dir stays in place across hook-failure retries; it is only deleted after a commit that actually succeeds.

No push. No PR. One commit per invocation — if diff spans unrelated concerns, ask how to split.

If nothing to commit AND branch has unpushed commits → skip to step 2.
If nothing to commit AND branch up to date → stop.

### 2. Push


## Steps

1. `git branch --show-current`. Record the branch (pushing to base, e.g. `main`, is allowed here — this is the trunk-based counterpart to `/pr-only`).
2. `git status --porcelain`. If working tree is dirty, stop and tell the user to run `/commit-only` or `/commit-and-push` first.
3. `git log @{u}..HEAD --oneline 2>/dev/null` to read what's about to ship. If the branch has no upstream, the command errors — that is expected; the upstream is set in step 4.
4. Push:
    - No upstream → `git push -u origin <branch>`.
    - Upstream exists and branch is ahead → `git push`.
    - Branch up to date with upstream → stop, print `already up to date`.
5. Never `--force` or `--force-with-lease`. If push is rejected (non-fast-forward), stop and tell the user; do not rewrite history.
6. Print the resulting `<old>..<new> <branch> -> <branch>` line.

## Rules

No commits. No PR creation — use `/pr-only` if you want a PR. No force-push. If you need to push a fix you forgot to commit, use `/commit-and-push` instead.

## Rules

No AI bylines in the commit message. No force-push. No PR creation — use `/commit-and-pr` if you want a PR. One commit per invocation; if the diff spans unrelated concerns, ask how to split.
