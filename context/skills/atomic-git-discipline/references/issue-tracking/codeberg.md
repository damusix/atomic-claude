# Codeberg issue tracking

Operations for `issue-tracker: codeberg`, which covers issues on `codeberg.org`. Codeberg runs Forgejo. If `codeberg` looks like the wrong tracker, see `../resolution.md`.

The CLI is `tea`. Install, the access token, and the login are in `../source-control/codeberg.md`; run its `auth-check` row before the first operation below.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `issue-create` | `tea issues create --title "<title>" --description-file <file>`. Add `--labels <name>,<name>` for labels. | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `issue-view` | `tea issues <index>` shows one issue in detail; add `--comments` for its comments. | https://gitea.com/gitea/tea/raw/branch/main/cmd/issues.go |
| `issue-search` | `tea issues list --keyword "<query>" --state all --output json` | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `issue-close` | `tea issues close <index>` | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `label-list` | `tea labels list --output json` | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `label-apply` | `tea issues edit <index> --add-labels "<name>"`. Apply only labels `label-list` reports. | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |
| `template-location` | An `issue_template/` or `ISSUE_TEMPLATE/` folder inside `.forgejo/`, `.gitea/`, `.github/`, or `docs/`, holding `.md`, `.yaml`, or `.yml` files. `tea issues create` has no template flag; copy the template into the description file. | https://forgejo.org/docs/latest/user/repository/issue-pull-request-templates/ |
| `commit-reference-syntax` | `#<n>` for this repository, `<owner>/<repo>#<n>` for another. A closing keyword from the `closing-keywords` row of `../source-control/codeberg.md` closes the issue on merge. | https://forgejo.org/docs/latest/user/collaboration/linked-references/ |
| `project-identifier` | `<owner>/<repo>`. Inside a clone `tea` takes it from the remote; elsewhere pass `--repo <owner>/<repo>`. `issue-tracker-project`, when set, is this value. | https://gitea.com/gitea/tea/raw/branch/main/docs/CLI.md |

## Gotchas

- Tokens, scopes, environment variables, and rate limits are the same as for source control: see `../source-control/codeberg.md`.
- Never create a label. Report every label you skipped because `label-list` did not list it.
