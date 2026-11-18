# Trello issue tracking

Operations for `issue-tracker: trello`. Trello calls an issue a card; a card sits in a list on a board. If `trello` looks like the wrong tracker, see `../resolution.md`.

Trello has no vendor CLI, so each row gives the REST call. Requests go to `https://api.trello.com/1` with the header `Authorization: OAuth oauth_consumer_key="$TRELLO_API_KEY", oauth_token="$TRELLO_TOKEN"`. The header keeps the token out of request URLs, where the `key=` and `token=` query parameters would put it.

`issue-tracker-project` is the board id. When it is unset, list the boards with `GET /members/me/boards?fields=name,url`. If the account reaches exactly one board, use it. If it reaches several, an interactive caller asks the user which one; the follow-up sink treats the tracker as unusable, records through `filesystem`, and reports why. Either way, suggest `atomic workflow set issue-tracker-project <board-id> --level repo`.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `auth-check` | `unsupported` as a CLI. REST: `GET /members/me` returns the signed-in member; a 401 means the key or token failed. | https://developer.atlassian.com/cloud/trello/guides/rest-api/authorization/ |
| `issue-create` | `unsupported` as a CLI. REST: `POST /cards` with `idList`, `name`, `desc`, and `idLabels`. Find `idList` with `GET /boards/<board-id>/lists`, and take the list the board uses for new work. When that is unclear, an interactive caller asks the user; a path that cannot ask, such as the follow-up sink, takes the first list `GET /boards/<board-id>/lists?filter=open` returns and names it in the result. | https://developer.atlassian.com/cloud/trello/rest/api-group-cards/, https://developer.atlassian.com/cloud/trello/rest/api-group-boards/ |
| `issue-view` | `unsupported` as a CLI. REST: `GET /cards/<card-id>` | https://developer.atlassian.com/cloud/trello/rest/api-group-cards/ |
| `issue-search` | `unsupported` as a CLI. REST: `GET /search?query=<query>&idBoards=<board-id>&modelTypes=cards`, with the query URL-encoded. | https://developer.atlassian.com/cloud/trello/rest/api-group-search/ |
| `issue-close` | `unsupported` as a CLI. REST: `PUT /cards/<card-id>` with `closed=true` archives the card. When the board tracks finished work in a list, move the card there with `idList=<done-list-id>` instead. | https://developer.atlassian.com/cloud/trello/rest/api-group-cards/ |
| `label-list` | `unsupported` as a CLI. REST: `GET /boards/<board-id>/labels`. Labels belong to the board. | https://developer.atlassian.com/cloud/trello/rest/api-group-boards/ |
| `label-apply` | `unsupported` as a CLI. REST: `POST /cards/<card-id>/idLabels` with `value=<label-id>`, once per label. Apply only labels `label-list` reports. | https://developer.atlassian.com/cloud/trello/rest/api-group-cards/ |
| `template-location` | No repository file. A template card on the board serves as one; pass its id as `idCardSource` on `POST /cards` to copy it into the new card. | https://developer.atlassian.com/cloud/trello/rest/api-group-cards/ |
| `commit-reference-syntax` | `unsupported`: Trello reads no commit message, so no keyword links or closes a card. Name the card URL in the commit body, and close the card with `issue-close`. | `unverified` |
| `project-identifier` | The board id. `issue-tracker-project`, when set, is this value; pass it as `<board-id>` and as `idBoards` in searches. | https://developer.atlassian.com/cloud/trello/rest/api-group-boards/, https://developer.atlassian.com/cloud/trello/rest/api-group-members/ |

## Gotchas

- Authentication needs both an API key in `TRELLO_API_KEY` and a user token in `TRELLO_TOKEN`. The token grants access to the whole account; keep it in the variable and never print its value. Source: https://developer.atlassian.com/cloud/trello/guides/rest-api/authorization/
- A token is issued with scopes from `read`, `write`, and `account`, and an expiration of `1hour`, `1day`, `30days`, or `never`. Creating cards and applying labels needs `write`. Source: https://developer.atlassian.com/cloud/trello/guides/rest-api/authorization/
- Rate limits: 300 requests per 10 seconds per API key, 100 per 10 seconds per token, and 100 per 900 seconds to `/1/members/`. A request over the limit returns HTTP 429. Source: https://developer.atlassian.com/cloud/trello/guides/rest-api/rate-limits/
- Never create a label. Report every label you skipped because `label-list` did not list it.
