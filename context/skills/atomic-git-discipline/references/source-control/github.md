# GitHub source control

Operations for `source-control: github`, which covers `github.com`, `*.ghe.com`, and GitHub Enterprise Server. If `github` looks like the wrong provider, see `../resolution.md`.

The CLI is `gh`. When `source-control-host` is not `github.com`, set `GH_HOST=<source-control-host>` for every command below, and pass `--hostname <source-control-host>` to `gh auth`.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `default-branch` | `gh repo view --json defaultBranchRef --jq .defaultBranchRef.name` | https://cli.github.com/manual/gh_repo_view |
| `change-request-view` | `gh pr view <number> --json number,title,state,url,headRefName,baseRefName` | https://cli.github.com/manual/gh_pr_view |
| `change-request-create` | `gh pr create --base <base> --head <branch> --title "<title>" --body-file <file>` | https://cli.github.com/manual/gh_pr_create |
| `change-request-edit-title` | `gh pr edit <number> --title "<title>"` | https://cli.github.com/manual/gh_pr_edit |
| `change-request-merge` | Merge commit: `gh pr merge <number> --merge`. Squash: `gh pr merge <number> --squash`. Add `--delete-branch` to delete the head branch after the merge. | https://cli.github.com/manual/gh_pr_merge |
| `change-request-attach-images` | `gh pr create --attach '<path>#<caption>'` on create, or `gh pr edit <number> --attach '<path>#<caption>'` on an open change request. The file uploads, and a body image that names the same local path is rewritten to the hosted URL. Needs a `gh` release whose `gh pr create --help` lists `--attach`. | https://cli.github.com/manual/gh_pr_create |
| `push-permission-check` | `gh api repos/{owner}/{repo} --jq .permissions.push` prints `true` when you can push. | https://cli.github.com/manual/gh_api, https://docs.github.com/en/rest/repos/repos#get-a-repository |
| `ci-target-resolve` | `gh pr view <number> --json headRefOid --jq .headRefOid` gives the head SHA; `gh run list --commit <sha>` lists its workflow runs. | https://cli.github.com/manual/gh_pr_view, https://cli.github.com/manual/gh_run_list |
| `auth-check` | `gh auth status --hostname <source-control-host>` exits 1 when the account has an authentication problem. With `--json` it always exits 0, so do not add it. | https://cli.github.com/manual/gh_auth_status |
| `install-hint` | Install `gh` from the vendor instructions, then run `gh auth login`. | https://github.com/cli/cli#installation |
| `closing-keywords` | `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved`, followed by `#<n>`, or `<owner>/<repo>#<n>` for another repository. Repeat the keyword for each issue. The issue closes only when the change request merges into the default branch. | https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue |

## Gotchas

- `GH_TOKEN` or `GITHUB_TOKEN` overrides the stored credentials for `github.com`; `GH_ENTERPRISE_TOKEN` or `GITHUB_ENTERPRISE_TOKEN` does the same for other hosts. Check a variable by name only; never print its value. Source: https://cli.github.com/manual/gh_help_environment
- `gh pr create --body-file` resolves image paths in the body relative to the body file's directory, then the working directory. Write the body file inside the repository so a path such as `tmp/shots/<name>.png` matches. Source: https://cli.github.com/manual/gh_pr_create
- Authenticated REST requests count against a personal limit of 5,000 an hour, and secondary limits also apply. Source: https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api
- `change-request-attach-images` caps each image at 10 MB. Source: https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/attaching-files. A cap of 50 images per command is `unverified`.
- `gh pr merge --admin` bypasses the merge queue and branch requirements. Never run it unattended.
