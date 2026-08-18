# Install


## Prerequisites

You need these tools on your `PATH` before installing:

- **Claude Code CLI** — `npm install -g @anthropic-ai/claude-code` (Node.js 18+)
- **Claude subscription or API key** — Pro, Max, or Team plan for OAuth; or set `ANTHROPIC_API_KEY` for direct billing
- **git** 2.30+ — used by every ship verb, worktree command, and cleanup scan
- **GitHub CLI** (`gh`) — used by `/commit` and `/report-issue`. Authenticate with `gh auth login`
- **POSIX shell** — `bash` or `zsh`, plus standard utilities (`grep`, `sed`, `awk`, `find`, `jq`, etc.)
- **Docker** — only needed for the [evaluation environment](./evaluations.md), not for normal use


## Quick install

Two commands. The first installs the `atomic` binary; the second wires everything else up.

Download the binary:

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```

This puts `atomic` in `~/.local/bin/` (override with `ATOMIC_INSTALL_DIR`). To pin a version: `ATOMIC_VERSION=v5.4.0 curl ... | bash`.

Install the artifact bundle (CLAUDE.md, agents, commands, skills, output styles, rules) into `~/.claude/`:

```bash
atomic claude install
```

This also registers the session-start hook by default; pass `--no-hooks` to skip it (see "After installing").

That is it. Verify the install with `atomic doctor`, which runs integrity checks and names anything missing. Then activate the output style with `/config` → **Output style** → **Atomic** in any Claude Code session.

For a project-scoped install instead of global: `atomic claude install --target ./.claude`.


## After installing

The installer prints two manual steps it cannot automate:

1. **Activate the output style** — run `/config` in Claude Code, select **Output style**, pick **Atomic**

    ![The /config screen with Output style highlighted](/img/output-style-config.png)

    ![The output style picker with Atomic selected](/img/output-style-picker.png)

2. **Scan your repos** — run `/refresh-wiki` in each repo. It builds the repo wiki, Claude's standing map of that repo's framework, commands, and layout

A few optional steps go further:

- **Check the session-start hook.** `atomic claude install` already registered a Claude Code session-start hook that refreshes your profile, injects pending reminders, and nudges you when a wiki falls stale. Some managed or enterprise setups disable hooks; if yours does, remove it with `atomic hooks uninstall`, or install with `atomic claude install --no-hooks` next time. To add the hook later (or after removing it), run `atomic hooks install`; the scope defaults to your user config, and `--scope project` limits it to one repo.
- **Map related repos with a wiki.** If you work across a folder of services, libraries, or client projects, run `/refresh-wiki` to build a cross-repo wiki. It summarizes each member repo and writes up the concerns they share, so Claude can reason about a whole realm of projects rather than one repo at a time. See the [wiki workflow](/reference/realm-wiki).
- **Index a project's symbols.** Run `atomic code index` in a project to build a symbol graph of it. Once indexed, `atomic code explore "<question>"` returns a context digest of the relevant symbols and call edges in one query, and the implementation agents use the graph for blast-radius checks and domain clustering. Indexing is opt-in and degrades to plain search when absent; see the [code-intel reference](/reference/code-intel).

On first install, the binary also creates `~/.atomic/profile.md` and prints a one-line nudge. The file starts with your git name, email, OS, architecture, and CPU count filled in from the environment. The remaining sections are empty; Claude fills them in as facts surface naturally in conversation. You do not need to edit the file by hand.

`atomic claude uninstall` removes `~/.atomic/` in its final step, and `profile.md` lives there — copy it somewhere else first if you want to keep it. The `@`-ref that loads it into sessions is removed along with the rest of the atomic-owned block in `~/.claude/CLAUDE.md`.

From here, you are ready to work. The [getting started guide](/guides/getting-started) walks the first session step by step; the [workflow reference](/reference/workflow) covers the full lifecycle.


## Updating

Update the binary:

```bash
atomic update
```

One command updates everything: it swaps the binary, refreshes the `~/.claude` artifact bundle, and finishes with a health check that prints what to look at if anything fails. It is usually near-instant because a background process pre-downloads and checksum-verifies each release ahead of time; the swap re-verifies version and checksum regardless, so the binary is never stale or unverified. The refresh respects your hook setup: if the session-start hook is not registered, the update will not add it.

To skip the artifact refresh, pass `--skip-claude-update` and run it yourself when ready:

```bash
atomic claude update
```

Five useful flags for `atomic update`:

- `--check` — just check if an update is available, do not download
- `--channel prerelease` — track release candidates instead of stable
- `--no-doctor` — skip the post-update health check
- `--skip-claude-update` — replace the binary only, skip the artifact refresh
- `--force` — take over an update lock held by another process; never skips checksum verification

`atomic update` refuses to run if another update looks to be in progress, unless that lock is more than 10 minutes old (then it is assumed abandoned and taken over automatically). `--force` is the manual override for a lock you know is stale.

Two config keys control the background check that makes updates fast; both default to `true`:

```bash
atomic config set update.check false   # stop the hourly background version check entirely
atomic config set update.stage false   # keep checking for updates, but never pre-download
```

The last check time, what's staged, and whether an update is in progress all live in the machine-managed `~/.atomic/state.json`. You never need to edit it by hand.

To suppress the health check permanently:

```bash
atomic config set update.run_doctor false
```


## Migrations

`atomic update` auto-applies versioned migration steps after refreshing the artifact bundle. These steps handle breaking changes across releases — restructured directories, updated config keys, and similar one-time transforms — and are idempotent, so re-running them is always safe.

To apply migrations manually (for example, after a manual binary swap or a fresh install on a machine that missed an update):

```bash
atomic migrate               # apply pending install-scope steps to ~/.claude/
atomic migrate --repo <path> # run repo-scope migrations for one project
atomic migrate --realm <path> # fan-out across all atomic'd member repos (prompts per-repo)
```

`atomic doctor` nudges you to run `atomic migrate` whenever the binary version is newer than the recorded install version. The nudge is suppressed for development builds (`dev` version string).

One migration runs automatically on every invocation rather than through `atomic migrate`: v6 moved per-user state from `~/.claude/.atomic/` to `~/.atomic/`. The first run of any verb on a v6 binary renames the legacy directory and leaves a symlink at the old path, so a CLAUDE.md installed by v5 keeps resolving its `@`-references until `atomic claude install` rewrites them. `atomic doctor` warns while either the legacy directory or the old references remain.


## If you already have a CLAUDE.md

One check decides everything: whether your file already carries an `<atomic>...</atomic>` block. Either way, your own sections are never touched and the prior version is backed up to `~/.atomic/backups/` before any change.

| Your `~/.claude/CLAUDE.md` | What the installer does | What you do |
|---|---|---|
| has an `<atomic>` block (prior install) | updates the block in place; a current block counts as no drift in `atomic claude diff` and `atomic doctor` | nothing |
| no block yet (pre-block install, or hand-edited tags) | never overwrites; writes the new version to `~/.atomic/proposed/CLAUDE.md` | run `atomic prompt claude-merge` in any Claude Code session; Claude merges into a staging file (`~/.claude/CLAUDE.md.atomic-merged`) and gives you the command to apply it |

The one-time merge wraps the atomic content in `<atomic>` tags, so every later update lands in the first row and applies on its own.


## Manual install

Download an archive from [GitHub Releases](https://github.com/damusix/atomic-claude/releases), verify with `shasum -c checksums.txt`, and move the `atomic` binary into any directory on your `$PATH`.


## Build from source

```bash
git clone https://github.com/damusix/atomic-claude.git
cd atomic-claude/atomic
make build
```


## Uninstall

Run from inside a Claude Code session:

```bash
atomic claude uninstall
```

The CLI reads the snapshot taken during install, figures out what to restore and what to delete, and hands Claude a structured plan. Claude shows you the plan, waits for confirmation, and then:

1. Merges back any changes you made to `settings.json` or `CLAUDE.md` after install
2. Restores files that existed before install
3. Removes files that atomic introduced
4. Deletes `~/.atomic/`
5. Prints the `rm` command to remove the binary (it never auto-removes the binary)

If you run the command in a plain terminal instead of a Claude session, it detects this and tells you how to proceed.


## Windows

Use **WSL2** (Ubuntu, Debian, or similar). Native Windows (cmd / PowerShell) is not supported.

Install WSL2, install your distro, install Node + Claude Code + git inside the distro, and run `claude` from the WSL shell. Keep repos inside the Linux home (`~/projects/...`) for sane file watching and performance.
