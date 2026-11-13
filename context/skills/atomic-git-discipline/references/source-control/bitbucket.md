# Bitbucket source control

Operations for `source-control: bitbucket`, which covers Bitbucket Cloud at `bitbucket.org`. Bitbucket calls a change request a pull request. If `bitbucket` looks like the wrong provider, see `../resolution.md`.

Bitbucket has no vendor CLI, so each row gives the REST call. Requests go to `https://api.bitbucket.org/2.0` with `--user '<atlassian-email>:<api-token>'`, or `--header "Authorization: Bearer <api-token>"`. `<workspace>` and `<repo>` come from the `origin` path.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `default-branch` | `unsupported` as a CLI. REST: `GET /repositories/<workspace>/<repo>` and read `mainbranch.name`. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/ |
| `change-request-view` | `unsupported` as a CLI. REST: `GET /repositories/<workspace>/<repo>/pullrequests/<id>` | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/ |
| `change-request-create` | `unsupported` as a CLI. REST: `POST /repositories/<workspace>/<repo>/pullrequests` with `title`, `description`, `source.branch.name`, and `destination.branch.name`. Without a destination, the main branch is used. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/ |
| `change-request-edit-title` | `unsupported` as a CLI. REST: `PUT /repositories/<workspace>/<repo>/pullrequests/<id>` with `title`. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/ |
| `change-request-merge` | `unsupported` as a CLI. REST: `POST /repositories/<workspace>/<repo>/pullrequests/<id>/merge` with `merge_strategy` set to `merge_commit` or `squash`. Set `close_source_branch` to `true` to delete the source branch. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/ |
| `change-request-attach-images` | `unsupported`: the pull request endpoints carry no attachment upload. Describe the image in text, or link a copy hosted elsewhere. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/ |
| `push-permission-check` | `unsupported` as a CLI. REST: `GET /user/workspaces/<workspace>/permissions/repositories?q=repository.name="<repo>"` and read `permission`. `write` or `admin` can push. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/ |
| `ci-target-resolve` | `unsupported` as a CLI. REST: `GET /repositories/<workspace>/<repo>/pullrequests/<id>/statuses` lists the build statuses for the pull request. For one commit, read `source.commit.hash` from the pull request and call `GET /repositories/<workspace>/<repo>/commit/<hash>/statuses`. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/, https://developer.atlassian.com/cloud/bitbucket/rest/api-group-commit-statuses/ |
| `auth-check` | `unsupported` as a CLI. REST: `GET /user` returns the signed-in account; a 401 means the credentials failed. | https://developer.atlassian.com/cloud/bitbucket/rest/api-group-users/ |
| `install-hint` | `unsupported` as a CLI: there is none to install. Create a scoped API token in Bitbucket personal settings, and use it with your Atlassian account email. | https://support.atlassian.com/bitbucket-cloud/docs/using-api-tokens/ |
| `closing-keywords` | `unsupported`: Bitbucket Cloud removed its issue tracker, so no keyword closes an issue. The issue tracker for this provider resolves to `filesystem` unless one is set. | https://developer.atlassian.com/cloud/bitbucket/changelog/ |

## Gotchas

- API tokens replace app passwords and must carry scopes for the calls you make. Source: https://support.atlassian.com/bitbucket-cloud/docs/api-tokens/
- Query values in `q=` must be URL-encoded (`=` becomes `%3D`). Source: https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/
- Authenticated API calls are limited per user over a rolling hour, 1,000 requests by default. Source: https://support.atlassian.com/bitbucket-cloud/docs/api-request-limits/
- Keep the token in a variable and never print its value.
