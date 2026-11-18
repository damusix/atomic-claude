# Linear issue tracking

Operations for `issue-tracker: linear`. If `linear` looks like the wrong tracker, see `../resolution.md`.

Linear has no vendor CLI, so each row gives the GraphQL call. Send every request as `POST https://api.linear.app/graphql` with `Content-Type: application/json`, a `{"query": "...", "variables": {...}}` body, and the header `Authorization: $LINEAR_API_KEY` for a personal API key, or `Authorization: Bearer <access-token>` for OAuth. A response can return HTTP 200 and still carry an `errors` array; check it before reading `data`.

`issue-tracker-project` is the team key, the prefix of every issue identifier (`ENG` in `ENG-123`). When it is unset, list the teams with `query { teams { nodes { id key name } } }`. If the account reaches exactly one team, use it. If it reaches several, an interactive caller asks the user which one; the follow-up sink treats the tracker as unusable, records through `filesystem`, and reports why. Either way, suggest `atomic workflow set issue-tracker-project <KEY> --level repo`.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `auth-check` | `unsupported` as a CLI. GraphQL: `query { viewer { id name } }`. An `errors` array or a 401 means the key failed. | https://linear.app/developers/graphql |
| `issue-create` | `unsupported` as a CLI. GraphQL: `mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { identifier url } } }` with `teamId`, `title`, `description` (Markdown), and `labelIds`. Resolve `teamId` from the `project-identifier` row. | https://linear.app/developers/graphql, https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql |
| `issue-view` | `unsupported` as a CLI. GraphQL: `query { issue(id: "<KEY>-<n>") { identifier title description url state { name type } labels { nodes { name } } } }`. `id` takes the UUID or the `<KEY>-<n>` identifier. | https://linear.app/developers/graphql |
| `issue-search` | `unsupported` as a CLI. GraphQL: `query($term: String!) { searchIssues(term: $term, includeArchived: true) { nodes { identifier title url state { name } } } }`. Pass `filter: { team: { key: { eq: "<KEY>" } } }` to stay inside one team. | https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql |
| `issue-close` | `unsupported` as a CLI. GraphQL: find the team's closing state with `query { workflowStates(filter: { team: { key: { eq: "<KEY>" } }, type: { eq: "completed" } }) { nodes { id name } } }`, using `canceled` when the work will not happen. Then run `mutation { issueUpdate(id: "<KEY>-<n>", input: { stateId: "<state-id>" }) { success } }`. | https://linear.app/developers/graphql, https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql |
| `label-list` | `unsupported` as a CLI. GraphQL: `query { issueLabels(first: 100) { nodes { id name team { key } } pageInfo { hasNextPage endCursor } } }`, passing `endCursor` as `after` while `hasNextPage` is true. The result holds workspace labels (`team` is null) and team labels; keep the workspace labels and the target team's. | https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql, https://linear.app/developers/pagination |
| `label-apply` | `unsupported` as a CLI. GraphQL: `mutation { issueAddLabel(id: "<KEY>-<n>", labelId: "<label-id>") { success } }`, once per label, or `labelIds` on `issueCreate`. Apply only labels `label-list` reports. | https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql |
| `template-location` | No repository file. Templates live in Linear under Workspace settings > Templates or Team settings > Templates. GraphQL: `query { templates { id name type team { key } } }` lists them; pass `templateId` on `issueCreate` to start from one. | https://linear.app/docs/issue-templates, https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql |
| `commit-reference-syntax` | A magic word before the issue identifier, for example `Fixes ENG-123`. Closing words (`close`, `fix`, `resolve`, `complete`, `implement`, and their other tenses) move the issue to the team's merge status when the commit reaches the default branch. Non-closing words (`ref`, `refs`, `references`, `part of`, `contributes to`, `toward`, `towards`) link without that move; `Ignore ENG-123` stops a link. Commit linking needs the Linear GitHub integration with commit linking turned on. | https://linear.app/docs/github |
| `project-identifier` | The team key, for example `ENG`. `issue-tracker-project`, when set, is this value. `issueCreate` needs the team UUID: `query { teams(filter: { key: { eq: "<KEY>" } }) { nodes { id } } }`. | https://linear.app/developers/graphql, https://linear.app/docs/teams |

## Gotchas

- Authentication is a personal API key in `LINEAR_API_KEY`, sent without a `Bearer` prefix, or an OAuth access token sent with one. Keep it in the variable and never print its value. Source: https://linear.app/developers/graphql
- A personal API key can be limited to some permissions and some teams. A key that cannot see the target team fails `issue-create` even when `auth-check` passes. Source: https://linear.app/docs/security-and-access
- Rate limits: 2,500 requests per hour per user for API keys, shared by every key that user holds; 5,000 per hour for an OAuth app. `searchIssues` has its own limit of 30 requests per minute. Source: https://linear.app/developers/rate-limiting, https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql
- Never create a label. Report every label you skipped because `label-list` did not list it.
