---
description: Stage and commit current changes. Delegates message format to the atomic-commit skill. Does not push.
---

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

<git-safety>
- Use relative paths for `git add` based on the current working directory.
- Run each `git` command as a separate Bash call.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit. The hook exists for a reason.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history.
</git-safety>
