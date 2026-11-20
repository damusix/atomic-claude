# Share a realm

Share selected capture buckets and team guidance through Git while each developer works in their own realm. Start with a bucket written for the team, and keep your realm wiki local by default. Atomic links the selected paths to a separate Git checkout. Your member repositories, private instructions, and harness settings stay local.

## Publish a team bucket and join the realm


1. Create an empty Git remote for the realm. Leave it uninitialized: no README, license, or first commit. Use a separate remote from your member repositories. The publisher needs push access, and anyone who joins needs read access (and push access to publish changes).
2. From your existing realm root, publish a bucket whose contents you have reviewed. This example uses a registered `sops/` bucket. If you do not have one, run `atomic wiki bucket add sops` and add material written for the team. Replace the URL with the remote you created. The first branch defaults to `main`.

   ```sh
   remote=https://git.example.org/acme/team-realm.git
   atomic realm share sops/ --remote "$remote"
   ```

3. From a separate local realm root, a colleague joins and chooses which received paths to link. To start with an empty directory under the default state layout, create `.atomic/atomic.toml` with `scope = "realm"`. Leave the intended link destinations free.

   ```sh
   remote=https://git.example.org/acme/team-realm.git
   atomic realm join "$remote"
   atomic realm link sops/
   atomic realm status
   ```

   `join` downloads and validates the remote's default branch and previews destinations. It does not install links, run downloaded settings, or touch member repositories. `link` asks you to trust the reviewed selection before it installs symlinks. Check that the paths you approved are linked in `status`.

With a terminal, `atomic realm share` and `atomic realm link` open selection wizards when you omit paths. Naming paths in the command preselects them but still asks for confirmation. Check the destination, contents, exclusions, and local effects before approving. Review the contents themselves: selecting a path does not scan its text for secrets.

### Approve the exact plan without a prompt

Use a dry run with explicit paths. For a first `share`, give the remote explicitly, as in the example above. Read the preview and copy the full token after `Review digest:`. For example, a colleague linking the team bucket would run:

```sh
atomic realm link sops/ --dry-run
# After approving the preview, replace DIGEST with its full sha256:… token.
atomic realm link sops/ --yes --review DIGEST
```

For `share`, use the same paths, remote, and branch in both invocations. If an agent is running the commands, have it show you the preview and get your approval before applying it. A dry run changes nothing. If the digest changes or Atomic refuses the approval, review a fresh preview instead of reusing the old token. See the [realm sharing CLI reference](/reference/realm-wiki#realm-sharing-cli) for flags and exit behavior.

## Know which files you are editing

This realm shares a team bucket, one document, and team guidance. Its wiki and private capture bucket stay local. The optional Claude files appear only when set up locally.

```text
~/work/acme/                                logical realm, not a Git checkout
├── AGENTS.local.md                         real file, your private realm guidance
├── AGENTS.md                               local steering, if present
├── CLAUDE.md                               optional local loader
├── .claude/settings.local.json              optional local settings
├── .codex/config.toml                      optional trusted local settings
├── .atomic/                                default per-realm state directory
│   ├── atomic.toml                         local realm configuration
│   ├── realm-share.toml                    private binding, trust and receipts
│   └── realm-share-backups/                 retained originals, when present
├── .agents/realm.md                        symlink to the team guidance
├── wiki/                                   local realm wiki
├── sops/                                   symlink to the shared team bucket
├── glossary.md                             symlink to one shared file
├── scratch/                                private capture bucket
└── repos/billing-api/                       independent member Git repository

~/.atomic/realms/acme/                      managed Git checkout, separate from the realm
├── .git/                                   shared history from the first publication
├── realm.toml                              portable identity, mappings and selected definitions
├── .agents/realm.md                        team guidance bytes
├── sops/                                   shared team procedures
└── glossary.md                             shared file bytes
```

Run `atomic where --json` for the realm's resolved `state_root` if you [use a different state directory](/reference/concepts#state-root-resolution-order). Run `atomic realm status` for the managed checkout path. One local realm binds to one managed checkout. Each developer has a separate local realm, checkout, and binding; the remote exchanges commits between their checkouts. Use realm-local paths for work and the checkout path for permission settings or explicit Git recovery.

| Selection | What happens when you edit or add content |
|---|---|
| A directory such as `sops/` | Changes and new descendants land in the checkout through the directory symlink. `sync` includes eligible descendants without new symlinks, subject to exclusions and Git ignore rules. |
| A single file such as `glossary.md` | Only the selected file is shared. An editor that saves by renaming a new file over the final path can replace the symlink instead of updating the target. Use an editor that preserves the symlink. Check `status` to diagnose a replaced link. |
| A path outside the selection | It stays local until you share it explicitly. Member repositories keep their own Git history and are neither imported nor synchronized. |

Open linked roots explicitly when searching. Generic recursive tools may skip directory symlinks; Atomic's wiki and browser views use declared roots and do not follow arbitrary nested links. Realm sharing requires native symlink support. Atomic stops if it cannot create or safely manage links; it does not substitute copies.

## Add another shared bucket later


From the colleague's bound realm, register a second bucket. Fill in its purpose and conventions in `team-notes/index.md` and keep any private guidance local. Registration alone does not change the realm wiki or team instructions.

```sh
atomic wiki bucket add team-notes
atomic wiki bucket list
# Add material to team-notes/ and review it before publishing.
atomic realm share team-notes/
```

This second `share` reuses the bound remote and branch. It requires a clean checkout with existing work published first. The reviewed publication includes the bucket directory and its portable registration together. Atomic links the publisher's local `team-notes/` only after successful publication.

The first developer receives the bucket without activating it locally:

```sh
atomic realm sync
atomic realm status
atomic realm link team-notes/
```

`sync` receives the new mapping and bytes, and `status` shows the path as available but unlinked if its destination is free. Review it before linking. If you already have a private `team-notes/`, Atomic reports a collision and preserves its files and registration. Leave the received bucket unlinked until you have deliberately resolved the local name and path conflict. See [capture buckets](/reference/realm-wiki#capture-buckets) for registration and listing behavior.

## Share a whole wiki only if it was built for the team


Keep a wiki that summarizes personal notes or private repositories local. A wiki summary can quote a private note or describe a private repo even when the source path is excluded. Atomic shows you what will be published and does not redact prose. Deleting a published page does not remove it from Git history.

If you built `wiki/` for the team from approved sources, review the wiki's contents before selecting the directory. In an already bound realm, run `atomic realm share wiki/`. Colleagues run `atomic realm sync` and review `atomic realm link wiki/` before linking it. For an initial share, supply `--remote` as in the bucket example.

Use only team-approved sources for new wiki pages and summaries, too. `sync` publishes eligible edits in the shared wiki without a new path selection.

For an independent wiki with a real `.git` directory, Atomic publishes a snapshot without `.git` and retains the original repository and its history in a local backup. Later shared edits belong to the managed checkout's history. `atomic realm unlink` can restore the original wiki repository.


## Choose what a fresh agent session loads

Write team guidance in `.agents/realm.md` and private guidance in `AGENTS.local.md` at the local realm root. Move only reviewed team text into the shared source. Atomic does not separate mixed prose for you. To share the instructions and individual authored units, run the following with sources you have created and reviewed:

```sh
atomic realm share .agents/realm.md .agents/skills/review/ .agents/commands/triage.md .agents/agents/reviewer.md
```

Teammates use `sync` to receive the sources and `link` to review the units they want. A skill selection includes its reviewed supporting files; dependencies outside that unit need their own selection. Select individual units or expand the current units in the terminal wizard. An ordinary prompt file can be shared without registering a command.

Read the source and native destinations in the preview. Invocation can run tools or scripts under existing permissions. Trust covers updates to selected units from the same remote, while new units need their own approval. Linking does not grant tool permissions, install remote hooks, connect MCP servers, or replace your local artifacts. User-owned collisions stop activation rather than overwriting local entries.

### Startup guidance and native artifact limits

Start a fresh session at the realm root. The registered harness, its version, and its native trust requirements determine what it loads. A source can be linked and readable even when native activation is unsupported. Use `status` to distinguish them; do not assume that activation carries across a member repository boundary.

| Harness | Startup guidance | Native artifacts at the realm root |
|---|---|---|
| Claude Code 2.1.328 | The [startup probe](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_sharing_claude_test.go) observed both developers' requests carrying the same team guidance and only their own private guidance. | The [reviewed-link probe](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_artifacts_claude_test.go) observed a slash skill, a slash command, and a primary agent selected with `--agent`. Child delegation was not established. |
| OMP 18.22.2 | The [startup probe](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_sharing_omp_test.go) observed both instruction sources in an enrolled primary session. | The [artifact probe](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_artifacts_omp_test.go) observed a slash workflow expand in print mode. The skill was listed but explicit invocation was not verified; neither was agent invocation. |
| Codex 0.147.0 | The [startup probe](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_sharing_codex_test.go) observed both sources only after approving the installed native startup hook's current hash. Link approval does not provide that approval. | The [reviewed-link probe](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_artifacts_codex_test.go) observed a listed skill whose body reached the request on explicit selection. Realm command and agent activation were not established. |

These startup and artifact claims describe the [recorded capabilities](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/harness/capability.go) and model input observed in loopback fixtures. They do not establish a hosted model response or file-write access.

Claude's optional startup hook does not grant write access. When Claude is enrolled, `claude` is on `PATH`, and you trust the selected `.agents/realm.md`, Atomic can register a local `SessionStart` hook. The hook reads validated team text; a local `CLAUDE.md` imports `AGENTS.local.md` for private guidance. If you enroll or install Claude after linking, review the instruction link again to add the hook without replacing a correct symlink. A wiki-only selection does not install a Claude loader or hook. Unlink removes Atomic's owned shared startup registration and keeps personal text and unrelated hooks.

### Save permission to write through a link

A harness can resolve `sops/page.md` to a file outside the local realm and refuse a write. Preview `atomic realm link sops/ --dry-run`, or substitute the path you intend to edit, to find the resolved managed checkout. Add the absolute checkout path from the preview to your local settings. The examples use `/home/alex/.atomic/realms/acme`. Merge the entry into your existing settings and keep it off the shared remote. Atomic does not grant directory permissions.

**Claude Code:** In the realm's `.claude/settings.local.json`, add the checkout to [`permissions.additionalDirectories`](https://code.claude.com/docs/en/permissions#working-directories):

```json
{
  "permissions": {
    "additionalDirectories": ["/home/alex/.atomic/realms/acme"]
  }
}
```

**Codex:** In the trusted realm's local `.codex/config.toml`, use [`workspace-write`](https://developers.openai.com/codex/config-reference) and add the checkout to its writable roots:

```toml
sandbox_mode = "workspace-write"

[sandbox_workspace_write]
writable_roots = ["/home/alex/.atomic/realms/acme"]
```

Start a fresh session at the realm root after saving. The saved settings supply the directory without `--add-dir`. Normal tool approvals, filesystem permissions, and managed policies still apply. Check access with an intended edit to a regular file under the linked directory. A startup hook is not required.

Claude's native Edit and Write tools [refuse a symlink at the final file path](https://code.claude.com/docs/en/permissions#writes-through-a-symlink), even with the directory grant. For an individually linked file, edit the resolved target with an authorized tool and leave the realm symlink in place.

On macOS, the native write probes for [Claude Code 2.1.328](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_wiki_claude_write_test.go) in `acceptEdits` mode and [Codex 0.147.0](https://github.com/damusix/atomic-claude/blob/main/atomic/internal/runtimeproof/realm_wiki_codex_write_test.go) in `workspace-write` mode each wrote through `wiki/` with its saved grant. Both denied the write without the grant. The probes used loopback, with no hosted model, `--add-dir`, or startup hook.

The [original macOS OMP experiment](/research/realm-sharing-symlinks) observed reads, edits, new files, and follow-on local work without an extra directory grant. It did not use the current startup adapters or establish write behavior for other harnesses.

## Exchange edits and handle conflicts

Edit through the local link, then run `atomic realm sync` to exchange changes explicitly. `sync` commits only declared content and the shared definition. It does not install links or operate on member repositories. It also receives new commits when you have no edits to publish.

When a bound realm has unpublished shared changes, the installed Claude Code, Codex, or OMP startup hook can show a reminder. It appears at most once per local calendar day per realm, across harnesses, when you open a fresh session. The hook checks only local state. It does not fetch, publish, or ask for confirmation.

Use `atomic realm status` before acting on surprising output. It reports link ownership and drift, pending paths, local changes, conflicts, and native activation. `Cached remote (not queried)` is the last observation, not a live remote check. Ahead and behind may be unknown. `status` neither fetches nor writes.

If `sync` reports a merge conflict, it leaves the Git merge open and names the realm-local and checkout-relative paths. Resolve the files in the checkout reported by Atomic, not in a member repository. For a conflict in the shared bucket:

```sh
clone="$HOME/.atomic/realms/acme"  # use the Managed checkout path from status
git -C "$clone" status
# Resolve sops/handbook.md in that checkout before staging.
git -C "$clone" add -- sops/handbook.md
git -C "$clone" merge --continue
atomic realm sync
```

For an authentication or network failure, fix access and retry `sync`; Git keeps your local commits. If another publisher advances the remote again during integration, rerun `sync`. Do not reset or force-push to clear unpublished work.

## Disconnect or finish an interrupted share

Run `atomic realm unlink` to remove unchanged owned links and restore retained originals. The binding, checkout, backups, and unpublished checkout edits remain; `sync` can still operate on that checkout. Run `atomic realm link` to review and reconnect selected paths. On the publisher's machine, relinking can replace an original restored by `unlink` only if it still matches the restoration receipt. If you edited the restored original or replaced a link yourself, Atomic refuses to overwrite your work.

| Situation | What survives and what to do |
|---|---|
| Publication failed | Your selected local originals stay in place. Fix the reported problem and retry the same reviewed `share`. If there is a retained commit, `sync` may publish it, but cannot complete local linking. |
| Publication succeeded but local linking did not | The remote snapshot and the retained originals are separate. Inspect pending paths in `status`, resolve the reported local obstruction without deleting backups, and repeat `share` with the same paths and any original remote and branch flags. Use a fresh dry run and digest for noninteractive approval. |

For the local wiki and indexing workflow, see the [knowledge base guide](/guides/knowledge-base).
