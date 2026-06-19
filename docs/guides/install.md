# Install


## Prerequisites

You need these tools on your `PATH` before installing:

- **Claude Code CLI** — `npm install -g @anthropic-ai/claude-code` (Node.js 18+)
- **Claude subscription or API key** — Pro, Max, or Team plan for OAuth; or set `ANTHROPIC_API_KEY` for direct billing
- **git** 2.30+ — used by every ship verb, worktree command, and cleanup scan
- **GitHub CLI** (`gh`) — used by `/commit` and `/report-issue`. Authenticate with `gh auth login`
- **POSIX shell** — `bash` or `zsh`, plus standard utilities (`grep`, `sed`, `awk`, `find`, `jq`, etc.)
- **Docker** — only needed for the [evaluation environment](./evaluations.md), not for normal use


## Windows

Use **WSL2** (Ubuntu, Debian, or similar). Native Windows (cmd / PowerShell) is not supported.

Install WSL2, install your distro, install Node + Claude Code + git inside the distro, and run `claude` from the WSL shell. Keep repos inside the Linux home (`~/projects/...`) for sane file watching and performance.


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

That is it. Verify the install with `atomic doctor`, which runs integrity checks and names anything missing. Then activate the output style with `/config` → **Output style** → **Atomic** in any Claude Code session.

For a project-scoped install instead of global: `atomic claude install --target ./.claude`.


## After installing

The installer prints two manual steps it cannot automate:

1. **Activate the output style** — run `/config` in Claude Code, select **Output style**, pick **Atomic**
2. **Scan your repos** — run `/refresh-signals` in each repo. It builds the signals files, Claude's standing map of that repo's framework, commands, and layout

A few optional steps go further:

- **Enable the session-start hook.** `atomic hooks install` registers a Claude Code session-start hook that refreshes your profile, injects pending reminders, and nudges you when signals or a wiki fall stale. Hooks are optional, and some managed or enterprise setups disable them, so skip this step if your organization does not allow Claude Code hooks. The scope defaults to your user config; pass `--scope project` to limit it to one repo.
- **Map related repos with a wiki.** If you work across a folder of services, libraries, or client projects, run `/refresh-wiki` to build a cross-repo wiki. It summarizes each member repo and writes up the concerns they share, so Claude can reason about a whole realm of projects rather than one repo at a time. See the [wiki workflow](/reference/wiki-workflow).
- **Index a project's symbols.** Run `atomic code index` in a project to build a symbol graph of it. Once indexed, `atomic code explore "<question>"` returns a context digest of the relevant symbols and call edges in one query, and the implementation agents use the graph for blast-radius checks and domain clustering. Indexing is opt-in and degrades to plain search when absent; see the [code-intel reference](/reference/code-intel).

On first install, the binary also creates `~/.claude/.atomic/profile.md` and prints a one-line nudge. The file starts with your git name, email, OS, architecture, and CPU count filled in from the environment. The remaining sections are empty; Claude fills them in as facts surface naturally in conversation. You do not need to edit the file by hand.

`atomic claude uninstall` preserves `profile.md`. It is user data with no pre-install counterpart, so the uninstall plan never touches it. After uninstall, the file stays on disk; the `@`-ref that loads it into sessions is removed along with the rest of the atomic-owned block in `~/.claude/CLAUDE.md`.

From here, you are ready to work. The [getting started guide](/guides/getting-started) walks the first session step by step; the [workflow reference](/reference/workflow) covers the full lifecycle.


## Updating

Update the binary:

```bash
atomic update
```

This fetches the latest release, verifies its SHA256 checksum, and replaces the binary. It then refreshes the `~/.claude` artifact bundle automatically and finishes with a health check. One command updates everything; if any check fails, it prints what to look at. The refresh respects your hook setup: if the session-start hook is not registered, the update will not add it.

To skip the artifact refresh, pass `--skip-claude-update` and run it yourself when ready:

```bash
atomic claude update
```

Four useful flags for `atomic update`:

- `--check` — just check if an update is available, do not download
- `--channel prerelease` — track release candidates instead of stable
- `--no-doctor` — skip the post-update health check
- `--skip-claude-update` — replace the binary only, skip the artifact refresh

To suppress the health check permanently:

```bash
atomic config set update.run_doctor false
```


## If you already have a CLAUDE.md

If your file already contains an `<atomic>...</atomic>` block from a prior install, the installer updates that block in place and leaves everything outside it alone. Your own sections are never touched, and a file whose block is current does not count as drift in `atomic claude diff` or `atomic doctor`. The previous version is backed up to `~/.claude/.atomic/backups/` before any change.

If your file has no `<atomic>` block yet (a pre-block install, or hand-edited tags), the installer will not overwrite it. Instead, it writes the new version to `~/.claude/.atomic/proposed/CLAUDE.md` and tells you to run `atomic prompt claude-merge` inside a subagent from any Claude Code session.

That command emits a merge brief for a disposable subagent. The subagent reads both files, writes the merged result to `~/.claude/CLAUDE.md.atomic-merged`, and returns a report with the proposed apply command. Your instructions are preserved; the `<atomic>` block is updated. After this one-time merge wraps the atomic content in `<atomic>` tags, future updates apply on their own.


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
4. Deletes `~/.claude/.atomic/`
5. Prints the `rm` command to remove the binary (it never auto-removes the binary)

If you run the command in a plain terminal instead of a Claude session, it detects this and tells you how to proceed.
