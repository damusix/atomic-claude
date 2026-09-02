{{define "commit-flow"}}
<commit-flow>

Invoke the `atomic-git-discipline` skill for message format.

1. Read the current state: `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
2. **Session reports** — resolve the report dir via `atomic where --json`'s `reports` field (never construct it). If it exists and has `*.md` files, read them chronologically and pass their content to `atomic-git-discipline` as supplemental why-context. Paths come from `atomic where --json`; if what you find on disk does not match, run `atomic migrate --show-log` for the change history.
3. **Stage files** explicitly by path. Skip secrets, build artifacts, and large binaries. **Why:** secrets in git history are irrecoverable even after rewrite; binaries bloat the repo permanently. If the intent is ambiguous, ask.
4. {{ template "review-gate" . }}
5. {{ template "doc-impact" . }}
6. {{ template "signals-gate" . }}
7. **Commit** using a HEREDOC message.
8. **Clean up session reports** — on successful commit, delete the `reports`-resolved dir from step 2. The reports were consumed by the commit message. If the commit failed, leave them for the next attempt.
9. `git status` to confirm.

One commit per invocation. If the diff spans unrelated concerns, ask how to split.

</commit-flow>{{- end}}
