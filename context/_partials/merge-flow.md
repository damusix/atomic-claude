{{define "merge-flow-preflight"}}
<merge-preflight>

Invoke `atomic-verify` to confirm the branch is ready before merging.

A clean feature branch merges through the provider when it has an open change request, and locally otherwise.

```mermaid
flowchart TD
    B{"On base?"} -->|yes| S1["Stop: nothing<br/>to merge"]
    B -->|no| D{"Dirty tree?"}
    D -->|yes| S2["Stop: commit<br/>or stash first"]
    D -->|no| N{"source-control: none?"}
    N -->|yes| L["Local path"]
    N -->|no| G{"Provider CLI usable?"}
    G -->|no| L
    G -->|yes| P{"Change request open?"}
    P -->|yes| R["Remote path"]
    P -->|no| L
```

- **On base?**: compare `git branch --show-current` with the base from:
   {{ template "base-resolution" . }}
- **Dirty tree?**: `git status --porcelain`.
- **source-control: none?**: the resolved block says so. There is no provider to merge through, so take the local-merge path without a note.
- **Provider CLI usable?**: the `auth-check` operation passes. When it does not, take the local path and say so in a note.
- **Change request open?**: the `change-request-view` operation for the current branch returns one in the open state.
- **Remote path**: preferred, because a local merge and push does not close the change request on most providers; it stays open as not merged. The `change-request-merge` operation closes it cleanly.

</merge-preflight>{{- end}}

{{define "merge-flow-steps"}}
<merge-steps>

Both paths end on a fast-forwarded base with the merge SHA recorded, and a failing test run after the merge offers a rollback that fits the path.

```mermaid
flowchart TD
    P{"Which path?"} -->|remote| R["Merge through<br/>the provider"]
    P -->|local| L["Merge locally"]
    R --> T{"Tests pass?"}
    L --> T
    T -->|no| F["Offer rollback"]
    T -->|yes| I["Update<br/>implementation logs"]
    F -->|declined| I
    I --> G["Signals refresh"]
    G --> X["Delete local branch"]
    X --> W["Worktree check"]
```

- **Which path?**: the one the preflight chose. Record the feature branch name and change request number (if any) first.
- **Merge through the provider**: the `change-request-merge` operation in its merge-commit form, deleting the head branch. The server-side merge closes the change request and removes the remote branch. Then `git checkout <base>`, `git pull`, and `MERGE_SHA=$(git rev-parse HEAD)`.

    A merge commit keeps every commit on the branch, so a changelog generator reads their subjects and the change request title never matters. **A squash merge inverts that**: the title becomes the sole commit subject on the base branch and the branch's own conventional subjects survive only as body bullets, which release-please does not parse for a type. Before any squash merge, read the title with `change-request-view`, and if it lacks the prefix the base branch's history uses, fix it first with `change-request-edit-title` to `<type>(<scope>): <subject>`. After the squash it takes a force-push of the base branch to correct, which is the one repair the flow otherwise refuses.
- **Merge locally**: the local-merge path. `git checkout <base>`, then `git pull` when `<base>` tracks a remote, then `git merge <feature>`, then `MERGE_SHA=$(git rev-parse HEAD)`.
- **Offer rollback**: local path → ask about rolling back with `git reset --hard ORIG_HEAD`. Remote path → the merge SHA is already published, so offer `git revert` instead; never force-push the base branch. An accepted rollback ends the flow.
- **Update implementation logs**: find spec files with an `## Implementation log` section in the merged diff:

    ```bash
    git diff --name-only ORIG_HEAD..HEAD | grep '^docs/spec/.*\.md$' | while read f; do
      grep -q '^## Implementation log' "$f" && echo "$f"
    done
    ```

    For each match, append `**Merged into <base> as <MERGE_SHA> — <date>.**`, then stage and commit as a follow-up.
- **Signals refresh**: if signals regenerate, commit as a follow-up: `chore(signals): refresh after merge of <feature>`. Push on the remote path.
    {{ template "signals-gate" . }}
- **Delete local branch**: `git branch -d <feature>`.
- **Worktree check**: {{ template "worktree-cleanup-prompt" . }}

</merge-steps>{{- end}}

{{define "merge-flow"}}
{{ template "merge-flow-preflight" . }}

{{ template "merge-flow-steps" . }}{{- end}}
