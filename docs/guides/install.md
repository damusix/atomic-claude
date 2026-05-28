# Install


## Prerequisites

You need these tools on your `PATH` before installing:

- **Claude Code CLI** — `npm install -g @anthropic-ai/claude-code` (Node.js 18+)
- **Claude subscription or API key** — Pro, Max, or Team plan for OAuth; or set `ANTHROPIC_API_KEY` for direct billing
- **git** 2.30+ — used by every ship verb, worktree command, and cleanup scan
- **GitHub CLI** (`gh`) — used by `/commit-and-pr`, `/pr-only`, and `/report-issue`. Authenticate with `gh auth login`
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

This puts `atomic` in `~/.local/bin/` (override with `ATOMIC_INSTALL_DIR`). To pin a version: `ATOMIC_VERSION=v1.0.0 curl ... | bash`.

Install the artifact bundle (CLAUDE.md, agents, commands, skills, output styles, rules) into `~/.claude/`:

```bash
atomic claude install
```

That is it. Activate the output style with `/config` → **Output style** → **Atomic** in any Claude Code session.

For a project-scoped install instead of global: `atomic claude install --target ./.claude`.


## After installing

The installer prints two manual steps it cannot automate:

1. **Activate the output style** — run `/config` in Claude Code, select **Output style**, pick **Atomic**
2. **Scan your project** — run `/refresh-signals` in each repo where you want project-state awareness

On first install, the binary also creates `~/.claude/.atomic/profile.md` and prints a one-line nudge. The file starts with your git name, email, OS, architecture, and CPU count filled in from the environment. The remaining sections are empty; Claude fills them in as facts surface naturally in conversation. You do not need to edit the file by hand.

`atomic claude uninstall` preserves `profile.md`. It is user data with no pre-install counterpart, so the uninstall plan never touches it. After uninstall, the file stays on disk; the `@`-ref that loads it into sessions is removed along with the rest of the atomic-owned block in `~/.claude/CLAUDE.md`.

From here, you are ready to work. See the [workflow guide](/reference/workflow) for what comes next.


## Updating

Update the binary:

```bash
atomic update
```

This fetches the latest release, verifies its SHA256 checksum, replaces the binary, and runs a health check. If any check fails, it prints what to look at.

Update the artifact bundle:

```bash
atomic claude update
```

Three useful flags for `atomic update`:

- `--check` — just check if an update is available, do not download
- `--channel prerelease` — track release candidates instead of stable
- `--no-doctor` — skip the post-update health check

To suppress the health check permanently:

```bash
atomic config set update.run_doctor false
```


## If you already have a CLAUDE.md

The installer will not overwrite it. Instead, it writes the new version to `~/.claude/.atomic/proposed/CLAUDE.md` and tells you to run `/atomic-claude-merge` from any Claude Code session.

That command shows a diff, lets you accept or edit, and backs up your previous file. Your instructions are preserved; duplicates are resolved.


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
