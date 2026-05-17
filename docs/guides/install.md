# Install


## Prerequisites


Atomic Claude assumes a POSIX-shaped environment. Everything below should be on `PATH` before you install.


- **Claude Code CLI** — the host this config plugs into. Install via `npm install -g @anthropic-ai/claude-code`. Requires Node.js 18+.
- **Claude subscription or API key** — Pro/Max/Team plan for OAuth login, or an `ANTHROPIC_API_KEY` for direct billing. Some features (Routines, push notifications, Remote Control) require a paid claude.ai plan and are unavailable on Bedrock / Vertex / Foundry.
- **git** — every ship verb, worktree command, and `atomic-git-scout` shell out to `git`. 2.30+ recommended (for modern `git worktree` and `git switch`).
- **GitHub CLI (`gh`)** — required by `/commit-and-pr`, `/pr-only`, `/report-issue`. Authenticated via `gh auth login`.
- **POSIX shell + core utilities** — `bash` (or `zsh`), `diff`, `grep`, `sed`, `awk`, `find`, `xargs`, `cp`, `mv`, `rm`, `cat`, `jq`. These are assumed by commands, hooks, and the user's global CLAUDE.md (e.g. `sed -i ''` for macOS, `gtimeout` instead of `timeout`).
- **macOS only** — `coreutils` from Homebrew if you want GNU-flavored `sed`/`timeout`. BSD defaults work; just match the syntax.
- **Docker** — only for the [evaluation flow](./evaluations.md). Not required for normal use.


## Windows


Use **WSL2** (Ubuntu, Debian, or similar). PowerShell is *not* supported:

- Claude Code's PowerShell tool is a preview feature with known gaps (no profile loading, no sandboxing on Windows, opt-in only).
- This repo's commands, hooks, and global CLAUDE.md assume POSIX semantics — `sed -i ''`, `mv`, `&&` chaining rules, `gtimeout`, etc. They will misbehave or fail outright under `cmd.exe` / PowerShell.
- Install WSL2 → install your distro → install Node + Claude Code + git inside the distro → run `claude` from the WSL shell. Treat the Windows filesystem as foreign; keep repos inside the Linux home (`~/projects/...`) for sane file watching and performance.


Native Windows (cmd / PowerShell) is unsupported. Patches welcome if you want to make it work, but the default assumption is POSIX.


## Quick install


The `atomic` binary backs cron and signals workflows. End users:

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```

This installs `atomic` to `~/.local/bin/atomic` (override via `ATOMIC_INSTALL_DIR`). To pin a specific version: `ATOMIC_VERSION=v0.1.0 curl ... | bash`.

After installing the binary, install the artifact bundle (CLAUDE.md, agents, commands, skills, output-styles, rules) into `~/.claude/`:

```bash
atomic claude install
```

For a project-scoped install instead of global: `atomic claude install --target ./.claude`.

Refresh later: `atomic claude update`.

If you have customized `~/.claude/CLAUDE.md` locally, `install` and `update` will not overwrite it. Instead, they write the new version to `~/.claude/CLAUDE.md.atomic-proposed` and print a hint to run `/atomic-claude-merge` from any Claude Code session. That command dispatches the `atomic-claude-merger` agent to produce `~/.claude/CLAUDE.md.atomic-merged`, shows a diff, and prompts Accept / Show diff / Open editor / Abort. On Accept the prior `CLAUDE.md` is backed up under `~/.claude/.atomic-backups/<timestamp>/`. Full spec: [`../spec/install-workflow.md`](../spec/install-workflow.md).

The install also registers a `SessionStart` hook (`atomic hooks install`) that injects any pending reminders at session open — supplementing cron-fired surfacing for missed cron fires, post-7-day cron expiry, tool unavailability, and post-restart catchup.


## Manual install


Download an archive from [GitHub Releases](https://github.com/damusix/atomic-claude/releases), verify with `shasum -c checksums.txt`, and move the `atomic` binary into any directory on your `$PATH`.


## Build from source


```bash
git clone https://github.com/damusix/atomic-claude.git
cd atomic-claude/atomic
make build       # or: go build -o ../bin/atomic ./cmd/atomic
```
