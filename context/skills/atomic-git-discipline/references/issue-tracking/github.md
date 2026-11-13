# GitHub issue tracking

Operations for `issue-tracker: github`, which covers issues on `github.com`, `*.ghe.com`, and GitHub Enterprise Server. If `github` looks like the wrong tracker, see `../resolution.md`.

The CLI is `gh`. Install, authentication, and the `GH_HOST` rule for a host other than `github.com` are in `../source-control/github.md`; run its `auth-check` row before the first operation below.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `issue-create` | `gh issue create --title "<title>" --body-file <file>`. Add `--label <name>` once per label. Prints the issue URL. | https://cli.github.com/manual/gh_issue_create |
| `issue-view` | `gh issue view <number> --json number,title,state,url,body,labels` | https://cli.github.com/manual/gh_issue_view |
| `issue-search` | `gh issue list --search "<query>" --state all --json number,title,state,url`. The query uses GitHub search syntax. | https://cli.github.com/manual/gh_issue_list |
| `issue-close` | `gh issue close <number> --comment "<comment>"`. Add `--reason "not planned"` when the work will not happen; the default reason is `completed`. | https://cli.github.com/manual/gh_issue_close |
| `label-list` | `gh label list --json name --limit 200` | https://cli.github.com/manual/gh_label_list |
| `label-apply` | `gh issue edit <number> --add-label "<name>"`. Apply only labels `label-list` reports. | https://cli.github.com/manual/gh_issue_edit |
| `template-location` | `.github/ISSUE_TEMPLATE/`, Markdown templates and YAML issue forms, with `config.yml` beside them. `gh issue create --template "<name>"` starts the body from a template. | https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/configuring-issue-templates-for-your-repository, https://cli.github.com/manual/gh_issue_create |
| `commit-reference-syntax` | `#<n>` or `GH-<n>` for this repository, `<owner>/<repo>#<n>` for another. A closing keyword from the `closing-keywords` row of `../source-control/github.md` closes the issue on merge. | https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/autolinked-references-and-urls |
| `project-identifier` | `<owner>/<repo>`. Inside a clone `gh` takes it from the remote; elsewhere pass `--repo <owner>/<repo>` or set `GH_REPO`. `issue-tracker-project`, when set, is this value. | https://cli.github.com/manual/gh_issue, https://cli.github.com/manual/gh_help_environment |

## Gotchas

- Tokens, scopes, environment variables, and rate limits are the same as for source control: see `../source-control/github.md`.
- A repository with issues disabled fails `issue-create`. `gh repo view --json hasIssuesEnabled` shows the setting. Source: https://cli.github.com/manual/gh_repo_view
- Never create a label. Report every label you skipped because `label-list` did not list it.
