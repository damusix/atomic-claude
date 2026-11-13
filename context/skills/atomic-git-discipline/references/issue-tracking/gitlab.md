# GitLab issue tracking

Operations for `issue-tracker: gitlab`, which covers issues on `gitlab.com` and self-managed instances. If `gitlab` looks like the wrong tracker, see `../resolution.md`.

The CLI is `glab`. Install, authentication, and the `--hostname` rule for a host other than `gitlab.com` are in `../source-control/gitlab.md`; run its `auth-check` row before the first operation below.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `issue-create` | `glab issue create --title "<title>" --description-file <file> --yes`. Add `--label <name>` for each label. | https://docs.gitlab.com/cli/issue/create/ |
| `issue-view` | `glab issue view <id> --output json` | https://docs.gitlab.com/cli/issue/view/ |
| `issue-search` | `glab issue list --search "<query>" --all --output json`. `--in title,description` sets the searched fields. | https://docs.gitlab.com/cli/issue/list/ |
| `issue-close` | `glab issue close <id>` | https://docs.gitlab.com/cli/issue/close/ |
| `label-list` | `glab label list --output json` | https://docs.gitlab.com/cli/label/list/ |
| `label-apply` | `glab issue update <id> --label "<name>"`. Apply only labels `label-list` reports. | https://docs.gitlab.com/cli/issue/update/ |
| `template-location` | `.gitlab/issue_templates/<name>.md` on the default branch. `glab issue create --template <name>` starts the description from one. | https://docs.gitlab.com/user/project/description_templates/, https://docs.gitlab.com/cli/issue/create/ |
| `commit-reference-syntax` | `#<n>` for this project, `<group>/<project>#<n>` for another. A closing keyword from the `closing-keywords` row of `../source-control/gitlab.md` closes the issue on merge. | https://docs.gitlab.com/user/markdown/ |
| `project-identifier` | `<group>/<project>`, or `<group>/<subgroup>/<project>`. Inside a clone `glab` takes it from the remote; elsewhere pass `--repo <group>/<project>`. `issue-tracker-project`, when set, is this value. | https://docs.gitlab.com/cli/issue/ |

## Gotchas

- Tokens, scopes, environment variables, and rate limits are the same as for source control: see `../source-control/gitlab.md`.
- Never create a label. Report every label you skipped because `label-list` did not list it.
