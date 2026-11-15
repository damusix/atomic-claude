# Filesystem issue tracking

Operations for `issue-tracker: filesystem`: follow-ups go to the repository's local ledger instead of a hosted tracker. If `filesystem` looks wrong, see `../resolution.md`.

To keep follow-ups local even when a hosted tracker is available, set `issue-tracker: filesystem`, for example `atomic workflow set issue-tracker filesystem --level repo`.

`atomic followups path` prints the ledger folder. Never assume or hard-code it; the folder moves with the repository state root. The commands below match `atomic followups <verb> --help`. No published page documents them, so each Source is `unverified`.

## Operations

| Operation | How | Source |
|-----------|-----|--------|
| `issue-create` | `atomic followups add --id <kebab-id> --title "<title>" --severity risk\|nit\|question --origin "<origin>" --body -`, with the body on stdin. Add `--file <path:lines>` when the follow-up points at code. `--kind plan` makes `--severity` optional. Prints the new entry's path and regenerates `INDEX.md`. | `unverified` |
| `issue-view` | Read `<id>.md` in the folder `atomic followups path` prints. | `unverified` |
| `issue-search` | `atomic followups list --json`, then filter by title or id. It returns open entries only; `--stale` narrows to stale ones. Closed entries are listed in `CLOSED.md` in the same folder. | `unverified` |
| `issue-close` | `atomic followups close <id> --reason "<reason>"` | `unverified` |
| `label-list` | `unsupported`: the ledger has no labels and no API. Its fixed fields stand in for them: `--kind` is `finding` or `plan`, `--severity` is `risk`, `nit`, or `question`. | `unverified` |
| `label-apply` | `unsupported`: no API changes an entry's fields. Set `--kind` and `--severity` when the entry is added. | `unverified` |
| `template-location` | `unsupported`: `atomic followups add` writes one fixed entry shape, so no template exists. | `unverified` |
| `commit-reference-syntax` | `unsupported`: no host links a commit to an entry or closes one on merge. Name the entry id in the commit body, and close the entry with `issue-close`. | `unverified` |
| `project-identifier` | `unsupported`: the ledger belongs to the repository that `atomic followups path` answers for, so `issue-tracker-project` is ignored. | `unverified` |

## Gotchas

- Tokens, scopes, and rate limits: not applicable, since no provider API is called.
- Whether the ledger is committed depends on the repository. Commit a new entry only when the path `issue-create` printed is not ignored: `git check-ignore -q <path>` exits 1.
