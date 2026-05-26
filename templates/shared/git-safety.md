{{define "git-safety"}}<git-safety>
- Stage explicitly by name (`git add <path>`), never `git add -A`. **Why:** `-A` can accidentally include secrets or untracked binaries.
- Use relative paths for `git add` based on the current working directory. **Why:** absolute paths and `git -C` can silently stage files outside the intended scope.
- Run each `git` command as a separate Bash call. **Why:** chaining with `&&` makes it impossible to inspect intermediate state and hides partial failures.
- On pre-commit hook failure: fix the root cause, re-stage, and create a new commit — never `--amend`. **Why:** amending after a hook failure modifies the PREVIOUS commit, which may destroy unrelated work.
- Keep force-push off the base branch. If a rollback is needed, use `git revert` so the bad SHA stays in history. **Why:** force-pushing rewrites shared history, breaking every collaborator's checkout.
</git-safety>{{- end}}
