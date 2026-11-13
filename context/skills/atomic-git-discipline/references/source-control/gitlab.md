# GitLab source control

Operations for `source-control: gitlab`, which covers `gitlab.com` and self-managed instances. GitLab calls a change request a merge request. If `gitlab` looks like the wrong provider, see `../resolution.md`.

The CLI is `glab`. When `source-control-host` is not `gitlab.com`, pass `--hostname <source-control-host>` to `glab auth` and `glab api`. Inside a clone, `glab` takes the project from the remote, and the `:id` placeholder in a `glab api` endpoint expands to the current project.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `default-branch` | `glab api projects/:id` and read `default_branch`. | https://docs.gitlab.com/cli/api/, https://docs.gitlab.com/api/projects/ |
| `change-request-view` | `glab mr view <id> --output json` | https://docs.gitlab.com/cli/mr/view/ |
| `change-request-create` | `glab mr create --source-branch <branch> --target-branch <base> --title "<title>" --description-file <file> --yes` | https://docs.gitlab.com/cli/mr/create/ |
| `change-request-edit-title` | `glab mr update <id> --title "<title>"` | https://docs.gitlab.com/cli/mr/update/ |
| `change-request-merge` | Merge commit: `glab mr merge <id>`. Squash: `glab mr merge <id> --squash`. Add `--remove-source-branch` to delete the source branch. Auto-merge is on by default, so the merge waits for the pipeline. | https://docs.gitlab.com/cli/mr/merge/ |
| `change-request-attach-images` | `unsupported` in `glab`. REST: `curl --header "PRIVATE-TOKEN: <token>" --form "file=@<path>" "https://<source-control-host>/api/v4/projects/<id>/uploads"` returns a `markdown` value to paste into the description. | https://docs.gitlab.com/api/project_markdown_uploads/ |
| `push-permission-check` | `glab api projects/:id` and read `permissions.project_access.access_level` and `permissions.group_access.access_level`. A value of 30 (Developer) or higher can push to unprotected branches. | https://docs.gitlab.com/api/projects/, https://docs.gitlab.com/api/members/ |
| `ci-target-resolve` | `glab mr view <id> --output json` and read `sha`; `glab ci list --sha <sha> --output json` lists its pipelines. | https://docs.gitlab.com/api/merge_requests/, https://docs.gitlab.com/cli/ci/list/ |
| `auth-check` | `glab auth status --hostname <source-control-host>` | https://docs.gitlab.com/cli/auth/status/ |
| `install-hint` | Install `glab` from the vendor instructions, then run `glab auth login --hostname <source-control-host>`. | https://docs.gitlab.com/cli/ |
| `closing-keywords` | `Close`, `Closes`, `Closed`, `Closing`, `Fix`, `Fixes`, `Fixed`, `Fixing`, `Resolve`, `Resolves`, `Resolved`, `Resolving`, `Implement`, `Implements`, `Implemented`, `Implementing`, any case, followed by `#<n>`, or `<group>/<project>#<n>` for another project. Issues close when the merge request merges into the default branch, unless the project turned off auto-close. | https://docs.gitlab.com/user/project/issues/managing_issues/ |

## Gotchas

- `glab mr merge --auto-merge=false` merges at once without waiting for the pipeline. Never run it unattended.
- Each instance applies its own API rate limits, and GitLab.com publishes separate ones. Source: https://docs.gitlab.com/rate_limits/
- The REST upload sends a token in a header. Pass it from a variable and never print its value.
