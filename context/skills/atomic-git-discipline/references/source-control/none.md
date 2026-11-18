# No source-control provider

Operations for `source-control: none`: the repository has no hosted provider, so no change request, remote permission, or provider CI exists. If `none` looks wrong, see `../resolution.md`.

Every row below is `unsupported` and says why. When a command needs a provider, tell the user to add a remote or change the preference with `atomic workflow set source-control <provider>`.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `default-branch` | `unsupported`: no provider records a default branch. Ask the user which branch is the base. | `unverified` |
| `change-request-view` | `unsupported`: no provider holds change requests. | `unverified` |
| `change-request-create` | `unsupported`: no provider holds change requests. `/commit pr` refuses and tells the user to add a remote or change the preference. | `unverified` |
| `change-request-edit-title` | `unsupported`: no change request exists to edit. | `unverified` |
| `change-request-merge` | `unsupported` as a change request. `/commit merge` and `/commit squash merge` take their local-merge path instead, which uses `git merge` and needs no provider. | https://git-scm.com/docs/git-merge |
| `change-request-attach-images` | `unsupported`: no change request exists to attach to. | `unverified` |
| `push-permission-check` | `unsupported`: no remote to push to. `/commit push` refuses and tells the user to add a remote or change the preference. | `unverified` |
| `ci-target-resolve` | `unsupported`: no provider runs CI for this repository. | `unverified` |
| `auth-check` | `unsupported`: there is nothing to sign in to. | `unverified` |
| `install-hint` | `unsupported`: no CLI applies. To use a provider, add one with `git remote add origin <url>`, or set the preference. | https://git-scm.com/docs/git-remote |
| `closing-keywords` | `unsupported`: no provider closes issues on merge. Reference issues with the `commit-reference-syntax` row of the resolved issue-tracking reference. | `unverified` |

## Gotchas

- Tokens, scopes, and rate limits: not applicable, since no provider API is called.
