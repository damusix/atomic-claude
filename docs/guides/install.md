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


## Self-update


The `atomic` binary updates itself. Run `atomic update` to fetch the latest stable release, verify its SHA256 checksum, and replace the running binary atomically.

After a successful binary swap, `atomic update` runs a doctor pass automatically. The doctor checks install coherence, hooks, signals, config validity, and related integrity categories. If everything is healthy, the command produces no additional output. If any check returns FAIL, the doctor prints the affected lines so you can diagnose and repair before your next Claude session.

Three flags control update behavior:

- `--check` — query the latest release version and print whether an update is available. Does not download or apply anything.
- `--channel <name>` — select a release channel. `stable` is the default. Use `prerelease` to track release candidates.
- `--no-doctor` — skip the post-update doctor pass for this invocation. Useful when running `atomic update` in a non-interactive script or CI step where the doctor output is noise.

To suppress the doctor pass permanently, set `update.run_doctor = false` in `~/.claude/.atomic/config.toml`:

```bash
atomic config set update.run_doctor false
```

Precedence: `--no-doctor` flag beats the config value, which beats the default (`true`). Passing `--no-doctor` disables the pass for that invocation only, regardless of what the config says.

If you have customized `~/.claude/CLAUDE.md` locally, `install` and `update` will not overwrite it. Instead, they write the new version to `~/.claude/.atomic/proposed/CLAUDE.md` and print a hint to run `/atomic-claude-merge` from any Claude Code session. That command dispatches the `atomic-claude-merger` agent to produce `~/.claude/CLAUDE.md.atomic-merged`, shows a diff, and prompts Accept / Show diff / Open editor / Abort. On Accept the prior `CLAUDE.md` is backed up under `~/.claude/.atomic/backups/<timestamp>/`. Full spec: [`../spec/install-workflow.md`](../spec/install-workflow.md).

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


## Uninstall


Run `atomic claude uninstall` from inside an active Claude Code session (Claude will execute it and receive the output as a prompt). If you run it in a plain terminal instead, the CLI detects the TTY and prints a hint explaining how to proceed.

**What the CLI does (deterministic):**

1. Reads `~/.claude/.atomic/pre-install/manifest.json`. If the file is missing (install predates the snapshot feature, or was never written), the command exits 1 with a clear error — there is nothing to restore.
2. Computes a restore plan from the manifest: files that existed before install are marked for restore; files that atomic introduced are marked for deletion.
3. Identifies which files need LLM mediation: `settings.json` and `CLAUDE.md` get flagged for merge if their current content differs from the pre-install snapshot (you modified them post-install).
4. Outputs a structured prompt to stdout that tells Claude exactly what to do.

**What Claude does (LLM-mediated):**

1. Shows you the full plan and waits for one confirmation before touching anything.
2. For files flagged for merge: reads the current file and the pre-install snapshot, identifies what you added post-install (permissions, MCP servers, env vars, custom sections), writes a merged result (pre-install base + your additions, minus atomic hook/config entries), and shows you the diff before writing.
3. Restores pre-install copies for files that existed before.
4. Deletes files that atomic introduced and you never had.
5. Removes `~/.claude/.atomic/`.
6. Prints the `rm <path>` command to remove the binary — the CLI never removes the binary itself.

**Pre-install snapshot.** Written during `atomic claude install` (and skipped on subsequent `install` / `update` calls if it already exists). Stored at `~/.claude/.atomic/pre-install/` alongside a `manifest.json` recording each file's path, SHA256, and whether it existed before install. Files that did not exist get `"existed": false` in the manifest; uninstall deletes them rather than trying to restore nothing.

Full spec: [`../spec/uninstall.md`](../spec/uninstall.md).
