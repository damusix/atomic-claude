{{define "commit-flow"}}
1. Invoke the `atomic-commit` skill. Follow it for message format.
2. `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
3. **Read session reports for the current branch** (if any):
    - `BRANCH=$(git branch --show-current)` (or short SHA on detached HEAD).
    - `REPORTS_DIR=.claude/.scratchpad/session-reports/<BRANCH-sanitized>/`.
    - If the dir exists and contains `*.md`, read all files in chronological order and pass their content to the `atomic-commit` skill as supplemental why-context for the commit message. If the dir is empty or missing, proceed normally.
4. Stage relevant files explicitly by path. No `git add -A` / `.`. Skip secrets, build artifacts, large binaries. If staged/unstaged intent is ambiguous, ask.
5. {{ template "doc-impact" . }}

    {{ template "doc-impact-why" . }}

6. {{ template "signals-gate" . }}
7. Commit using a HEREDOC message.
8. **On successful commit (exit 0): delete the branch's session-reports dir.**
    - `rm -rf .claude/.scratchpad/session-reports/<BRANCH-sanitized>/`
    - Silent; this is the documented contract from `docs/spec/session-report.md`. The reports were consumed by the commit message — they have served their purpose. Leaving them would pollute future commits on the same branch with stale context.
    - If the commit failed or was aborted (pre-commit hook rejection, user interrupt): **do not delete.** Reports persist for the next attempt.
9. `git status` to confirm.

On pre-commit hook failure: fix root cause, re-stage, create a NEW commit. No `--no-verify`. No `--amend`. Session-reports dir stays in place across hook-failure retries; it is only deleted after a commit that actually succeeds.

No push. No PR. One commit per invocation — if diff spans unrelated concerns, ask how to split.{{- end}}
