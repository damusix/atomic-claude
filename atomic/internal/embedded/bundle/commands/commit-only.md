---
description: Stage and commit current changes. Delegates message format to the atomic-commit skill. Does not push.
---

1. Invoke the `atomic-commit` skill. Follow it for message format.
2. `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
3. Stage relevant files explicitly by path. No `git add -A` / `.`. Skip secrets, build artifacts, large binaries. If staged/unstaged intent is ambiguous, ask.
4. Commit using a HEREDOC message.
5. `git status` to confirm.

On pre-commit hook failure: fix root cause, re-stage, create a NEW commit. No `--no-verify`. No `--amend`.

No push. No PR. One commit per invocation — if diff spans unrelated concerns, ask how to split.
