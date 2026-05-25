{{define "squash-flow-preflight"}}
<squash-preflight>

1. Determine base:
   {{ template "base-resolution" . }}
2. `git branch --show-current` — if on base, stop: nothing to squash.
3. `git status --porcelain` — if dirty, stop: commit or stash first.
4. Count commits: `git rev-list --count <base>..HEAD` — if only 1, stop: nothing to squash.

</squash-preflight>{{- end}}

{{define "squash-flow-steps"}}
<squash-steps>

1. Gather subjects (oldest-first): `SUBJECTS=$(git log <base>..HEAD --format='%s' --reverse)`.
2. **Session reports** — check for `.claude/.scratchpad/session-reports/<branch>/`. If the dir has `*.md` files, read them chronologically and pass as supplemental why-context alongside `SUBJECTS`.
3. `git reset --soft $(git merge-base HEAD <base>)` — collapse all branch commits into the index.
4. {{ template "doc-impact" . }}
5. Invoke `atomic-commit` skill. Pre-fill a Conventional Commits message synthesized from `SUBJECTS` (plus session reports if present). Present for review, then commit via HEREDOC.
6. **Clean up session reports** — on successful commit, delete `.claude/.scratchpad/session-reports/<branch>/`. If the commit failed, leave them.
7. **Update implementation logs.** Find spec files with an `## Implementation log` section in the squashed diff:
    ```bash
    git show --name-only --pretty=format: HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```
    For each match, append: `**Squashed to <new-sha> — <date>.** Per-iteration SHAs above are historical (unreachable from any branch).` Stage and commit as a follow-up. If none match, skip.
8. **Post-squash signals refresh:**
    {{ template "signals-gate" . }}
    If signals regenerate, commit as a follow-up: `chore(signals): refresh after squash`.
9. `git status` to confirm.

</squash-steps>{{- end}}

{{define "squash-flow"}}
{{ template "squash-flow-preflight" . }}

{{ template "squash-flow-steps" . }}{{- end}}
