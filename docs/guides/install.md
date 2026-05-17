# Install


## Prerequisites


Atomic Claude assumes a POSIX-shaped environment. Everything below should be on `PATH` before you install.


| Tool | Why it's needed | Notes |
|------|-----------------|-------|
| Claude Code CLI | Host this config plugs into | `npm install -g @anthropic-ai/claude-code`. Node.js 18+. |
| Claude subscription or API key | Auth | Pro/Max/Team plan for OAuth, or `ANTHROPIC_API_KEY` for direct billing. Routines, push notifications, and Remote Control need a paid claude.ai plan; unavailable on Bedrock / Vertex / Foundry. |
| git | Every ship verb, worktree command, and `atomic-git-scout` shell out to `git` | 2.30+ recommended (modern `git worktree`, `git switch`). |
| GitHub CLI (`gh`) | `/commit-and-pr`, `/pr-only`, `/report-issue` | Authenticate with `gh auth login`. |
| POSIX shell + core utilities | Assumed by commands, hooks, global CLAUDE.md | `bash` (or `zsh`), `diff`, `grep`, `sed`, `awk`, `find`, `xargs`, `cp`, `mv`, `rm`, `cat`, `jq`. Examples: `sed -i ''` for macOS, `gtimeout` instead of `timeout`. |
| coreutils (macOS) | GNU-flavored `sed` / `timeout` | Optional. BSD defaults work; match the syntax. |
| Docker | [Evaluation flow](./evaluations.md) only | Not required for normal use. |


## Windows


Use **WSL2** (Ubuntu, Debian, or similar). PowerShell is *not* supported:

- Claude Code's PowerShell tool is a preview feature with known gaps (no profile loading, no sandboxing on Windows, opt-in only).
- This repo's commands, hooks, and global CLAUDE.md assume POSIX semantics: `sed -i ''`, `mv`, `&&` chaining rules, `gtimeout`, etc. They will misbehave or fail outright under `cmd.exe` / PowerShell.
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

By default `atomic claude install` also registers a `SessionStart` hook in `~/.claude/settings.json` that injects any pending reminders at session open. This supplements cron-fired surfacing for missed cron fires, post-7-day cron expiry, tool unavailability, and post-restart catchup. Pass `--no-hooks` to skip; you can register the hook later with `atomic hooks install`.

After the install completes, the command prints the two manual steps it can't automate: activating the **Atomic** output style via `/config` in Claude Code, and running `/initialize-signals` in each repo where you want project signals.


## Manual install


Download an archive from [GitHub Releases](https://github.com/damusix/atomic-claude/releases), verify with `shasum -c checksums.txt`, and move the `atomic` binary into any directory on your `$PATH`.


## Build from source


```bash
git clone https://github.com/damusix/atomic-claude.git
cd atomic-claude/atomic
make build       # or: go build -o ../bin/atomic ./cmd/atomic
```
