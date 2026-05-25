---
description: Commit current changes, push, then open a PR via gh. Delegates message + body format to atomic-commit and atomic-review skills.
---

## 1. Commit


<commit-flow>

Invoke the `atomic-commit` skill for message format.

1. Read the current state: `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
2. **Session reports** — check for `.claude/.scratchpad/session-reports/<branch>/`. If the dir exists and has `*.md` files, read them chronologically and pass their content to `atomic-commit` as supplemental why-context.
3. **Stage files** explicitly by path. Skip secrets, build artifacts, and large binaries. If the intent is ambiguous, ask.
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
5. <signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Check `atomic signals stale`. If fresh (exit 0), skip.
3. Both pass → invoke the `atomic-signals` skill in silent mode. Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md`.

The `atomic signals stale` command is the source of truth — it fast-fails when nothing changed and catches structural shifts that a file-extension allowlist would miss.
</signals-refresh>
6. **Commit** using a HEREDOC message.
7. **Clean up session reports** — on successful commit, delete `.claude/.scratchpad/session-reports/<branch>/`. The reports were consumed by the commit message. If the commit failed, leave them for the next attempt.
8. `git status` to confirm.

One commit per invocation. If the diff spans unrelated concerns, ask how to split.

</commit-flow>

If nothing to commit and branch has unpushed commits, skip to push.
If nothing to commit and branch is up to date, stop.

## 2. Push


<push-flow>

1. `git branch --show-current` — record the branch.
2. `git status --porcelain` — if dirty, stop and tell the user to commit first.
3. `git log @{u}..HEAD --oneline 2>/dev/null` — show what is about to ship. If the branch has no upstream, that is expected (set in step 4).
4. Push:
    - No upstream → `git push -u origin <branch>`.
    - Upstream exists and branch is ahead → `git push`.
    - Already up to date → stop.
5. Print the resulting `<old>..<new> <branch> -> <branch>` line.

If push is rejected (non-fast-forward), stop and tell the user. Let them decide how to resolve it.

</push-flow>

## 3. PR


<pr-flow>

Invoke the `atomic-review` skill for PR title and body tone.

1. `git branch --show-current` — if on base branch, stop.
2. Determine base: `gh repo view --json defaultBranchRef -q .defaultBranchRef.name`.
3. Read what is shipping: `git log <base>..HEAD --oneline` + `git diff <base>...HEAD --stat` (parallel).
4. Check for existing PR: `gh pr view --json url 2>/dev/null` — if one exists, print its URL and stop.
5. Push if needed: `git push -u origin <branch>` (no upstream) or `git push` (behind).
6. Create the PR:
    ```
    gh pr create --title "<imperative, ≤70 chars>" --body <HEREDOC>
    ```
    Body sections: `## Summary` (1-3 bullets), `## Why` (skip if obvious), `## Test plan` (checklist).
7. Print the PR URL.

If the working tree is dirty, stop and tell the user to commit first.

</pr-flow>

<git-safety>
- Use relative paths for `git add` based on the current working directory.
- Run each `git` command as a separate Bash call.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit. The hook exists for a reason.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history.
</git-safety>
