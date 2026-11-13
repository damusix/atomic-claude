# Resolving the provider

Which source-control provider, issue tracker, and worktree location a session uses, and which reference file each value selects.

## Where the values come from

1. The session-start hook adds a `<workflow-preferences level='resolved'>` block to context. It outranks any other `<workflow-preferences>` block you read.
2. If no resolved block is in context, run `atomic workflow resolve` once and use its output. `atomic workflow resolve --json` prints the same facts as JSON.
3. Each line names its value and the level it came from: `repo`, `realm`, `profile`, `origin`, `derived`, or `default`. Unset keys are absent.
4. `warning:` lines report problems the resolver found, such as an explicit `source-control` that differs from the `origin` host.

Levels are consulted in the order repo, realm, profile. The realm level is skipped outside a registered realm. For each key, the resolver takes:

1. The first explicit value.
2. For `source-control`: the provider the `origin` host maps to. For `source-control-host`: the `origin` host, when its provider equals the resolved `source-control`.
3. For `issue-tracker`: the first soft value, which must be `linear`, `jira`, or `trello`; else the tracker of the resolved `source-control` (`github`, `gitlab`, `codeberg`), or `filesystem` for `bitbucket` and `none`, reported at level `derived`.
4. For every key except `issue-tracker`: the first soft value, a line marked `# detected`.
5. The default: `source-control` is `github`, `source-control-host` is the provider's public host, `issue-tracker-project` is unset, and `worktrees` is `auto`.

## Origin host inference

The resolver reads the `origin` remote URL in its `git@HOST:PATH`, `https://[user@]HOST/PATH`, and `ssh://git@HOST[:PORT]/PATH` forms, case-insensitively, and maps the host:

| Host | Provider |
|------|----------|
| `github.com`, `*.ghe.com` | `github` |
| `gitlab.com` | `gitlab` |
| `bitbucket.org` | `bitbucket` |
| `codeberg.org` | `codeberg` |
| any other host | no provider (falls through to the next rule); the resolver warns and asks for an explicit `source-control` and `source-control-host`, unless both are already explicit at some level |

A self-hosted GitHub Enterprise Server or GitLab instance maps to nothing, so it needs both keys set explicitly, for example `atomic workflow set source-control gitlab --level repo` and `atomic workflow set source-control-host git.example.com --level repo`.

## Block to file

The `Read:` line lists the files to read before a provider operation, with paths that start at the skill folder. From this file, the same files are:

| Resolved key | File |
|--------------|------|
| `source-control: <value>` | `source-control/<value>.md` |
| `issue-tracker: <value>` | `issue-tracking/<value>.md` |
| `worktrees: <value>` | `worktrees.md` |

Pass `source-control-host` to the provider CLI as its host, as the provider file describes. `none` and `filesystem` are values, not gaps: they select the file that says what to do without a provider.
