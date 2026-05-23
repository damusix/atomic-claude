{{define "squash-flow-preflight"}}
1. Determine base:
   {{ template "base-resolution" . }}
2. `git branch --show-current`. If on base: `refused: already on <base>. nothing to squash.`
3. `git status --porcelain`. If dirty: `refused: working tree dirty. commit or stash first.`
4. Count commits: `git rev-list --count <base>..HEAD`. If 1: `refused: only one commit on branch. nothing to squash.`{{- end}}

{{define "squash-flow-steps"}}
1. Gather subjects (oldest-first): `SUBJECTS=$(git log <base>..HEAD --format='%s' --reverse)`.
2. **Read session reports for the current branch** (if any):
    - `BRANCH=$(git branch --show-current)`.
    - `REPORTS_DIR=.claude/.scratchpad/session-reports/<BRANCH-sanitized>/`.
    - If the dir exists and contains `*.md`, read all files in chronological order and pass their content to the `atomic-commit` skill as supplemental why-context alongside `SUBJECTS`. If the dir is empty or missing, proceed with `SUBJECTS` only.
3. `git reset --soft $(git merge-base HEAD <base>)` — collapses all branch commits into the index.
4. {{ template "doc-impact" . }}

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

    {{ template "signals-gate" . }}

    When signals regenerate: commit as a follow-up: `chore(signals): refresh after squash`. Never amend the squash commit.
9. `git status` to confirm.{{- end}}

{{define "squash-flow"}}
{{ template "squash-flow-preflight" . }}

{{ template "squash-flow-steps" . }}{{- end}}
