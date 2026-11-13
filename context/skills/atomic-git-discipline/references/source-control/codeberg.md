# Codeberg source control

Operations for `source-control: codeberg`, which covers `codeberg.org`. Codeberg runs Forgejo and calls a change request a pull request. If `codeberg` looks like the wrong provider, see `../resolution.md`.

The CLI is `tea`. Inside a clone, `tea` finds the login from the remote, and `tea api` fills the `{owner}` and `{repo}` placeholders from the current repository. `tea api` prefixes a bare endpoint with `/api/v1/`. The REST base is `https://codeberg.org/api/v1`, with the header `Authorization: token <token>`.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `default-branch` | `tea api /repos/{owner}/{repo}` and read `default_branch`. | https://gitea.com/gitea/tea/raw/branch/main/cmd/api.go, https://codeberg.org/api/swagger |
| `change-request-view` | `tea pulls <index>` shows one pull request in detail. | https://gitea.com/gitea/tea/raw/branch/main/cmd/pulls.go |
| `change-request-create` | `tea pulls create --base <base> --head <branch> --title "<title>" --description-file <file>` | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `change-request-edit-title` | `tea pulls edit <index> --title "<title>"` | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `change-request-merge` | Merge commit: `tea pulls merge <index> --style merge`. Squash: `tea pulls merge <index> --style squash`. Delete the branch after the merge with `tea pulls clean <index>`. REST: `POST /repos/<owner>/<repo>/pulls/<index>/merge` with `Do` set to `merge` or `squash` and `delete_branch_after_merge` set to `true`. | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md, https://codeberg.org/api/swagger |
| `change-request-attach-images` | `unsupported` in `tea`. REST: `curl --header "Authorization: token <token>" --form "attachment=@<path>" https://codeberg.org/api/v1/repos/<owner>/<repo>/issues/<index>/assets`; a pull request shares its issue index. Put the returned `browser_download_url` in the description. | https://codeberg.org/api/swagger |
| `push-permission-check` | `tea api /repos/{owner}/{repo}` and read `permissions.push`. | https://gitea.com/gitea/tea/raw/branch/main/cmd/api.go, https://codeberg.org/api/swagger |
| `ci-target-resolve` | `tea api /repos/{owner}/{repo}/pulls/<index>` and read `head.sha`; `tea api /repos/{owner}/{repo}/commits/<sha>/status` returns the combined status for that commit. | https://gitea.com/gitea/tea/raw/branch/main/cmd/api.go, https://codeberg.org/api/swagger |
| `auth-check` | `tea whoami` shows the signed-in user. `tea logins list` shows which hosts have a login. | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `install-hint` | Install `tea` (for example `brew install tea`), create an access token in Codeberg settings, then run `tea logins add --url https://codeberg.org --token <token>`. | https://gitea.com/gitea/tea, https://docs.codeberg.org/advanced/access-token/ |
| `closing-keywords` | `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved`, followed by `#<n>`, or `<owner>/<repo>#<n>` for another repository. | https://forgejo.org/docs/latest/user/collaboration/linked-references/ |

## Gotchas

- `tea` is Gitea's CLI, and Codeberg runs Forgejo. The rows were checked against the `tea` docs and Codeberg's API reference, not run against Codeberg. When a `tea` command fails, fall back to the REST call through `tea api`.
- API rate limits: no Codeberg page states them. Source: `unverified`
- `force_merge` on the merge endpoint is a bypass flag. Never send it unattended.
- Keep the token in a variable and never print its value.
