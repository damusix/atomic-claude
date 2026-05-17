# Atomic Claude

```
            _.-^^---....,,--
        _--                  --_
      <      Atomic Claude       >
       \._                    _./
          ```--. . , ; .--'''
                | |   |
             .-=||  | |=-.
             `-=#$%&%$#=-'
                | ;  :|
       _____.,-#%&$@%#&#~,._____
```

A personal Claude Code configuration. Atomic output means better token consumption and faster decision-making — less narrative for the user to read and act upon.

The whole point: spend fewer tokens, decide faster, repeat less. Output is precise and condensed instead of the long explanations Claude defaults to. That accelerates the human-in-the-loop cycle. The commands exist for that. The output style exists for that. The skill/command split exists for that.

**Skills vs commands.** Skills auto-fire — used when we want subagents to pick things up implicitly on matching phrases (TDD, commit format, verification, debug, review). Commands are explicit — used when we want Claude to act only when called. This is the distinction Claude Code already intends; Atomic Claude leans on it hard.

**Why explicit over implicit.** Decisions here come from working with `caveman` and `superpowers`. Superpowers is overbearing — too much cognitive overhead when you want a small, precise change. Atomic Claude opts in explicitly via commands instead of having Claude pick up discipline implicitly across the board. Skills still auto-fire where the trigger is well-bounded; everything else is a slash command.

Smallest-unit responses: filler, pleasantries, hedging stripped; technical substance, code, errors kept intact. Work in progress, no stability guarantee.


## What's in here

- **Output style** (`output-styles/atomic.md`) — strips ceremony from Claude's TUI replies. Persists across the session. Three intensity levels: lite, full, ultra.
- **Commands** (`commands/`) — slash commands for planning, implementation, shipping, and repo hygiene.
- **Skills** (`skills/`) — discipline skills that auto-fire on matching phrases (TDD, commit format, verification gate, debugging, code review).
- **Agents** (`agents/`) — named subagents the orchestrator dispatches: a feature builder, a surgical editor, a read-only investigator, and a diff reviewer.
- **Rules** (`.claude/rules/<lang>/*.md`) — path-scoped instructions that auto-load only when Claude reads matching files. `paths:` frontmatter globs against filetypes (`**/*.{ts,tsx}` for TypeScript, `**/*.py` for Python). Starter rules ship for TypeScript and Python; add more languages or topic subdirs as you grow.
- **Scratchpad convention** — `.claude/.scratchpad/<date>-<desc>/` for LLM working memory, gitignored, ephemeral.
- **Doc layout** — `docs/spec/` for implementation contracts, `docs/design/` for rationale and alternatives, `tmp/` for throwaway experiments.


## Prerequisites


Atomic Claude assumes a POSIX-shaped environment. Everything below should be on `PATH` before you install.


- **Claude Code CLI** — the host this config plugs into. Install via `npm install -g @anthropic-ai/claude-code`. Requires Node.js 18+.
- **Claude subscription or API key** — Pro/Max/Team plan for OAuth login, or an `ANTHROPIC_API_KEY` for direct billing. Some features (Routines, push notifications, Remote Control) require a paid claude.ai plan and are unavailable on Bedrock / Vertex / Foundry.
- **git** — every ship verb, worktree command, and `atomic-git-scout` shell out to `git`. 2.30+ recommended (for modern `git worktree` and `git switch`).
- **GitHub CLI (`gh`)** — required by `/commit-and-pr`, `/pr-only`, `/report-issue`. Authenticated via `gh auth login`.
- **POSIX shell + core utilities** — `bash` (or `zsh`), `diff`, `grep`, `sed`, `awk`, `find`, `xargs`, `cp`, `mv`, `rm`, `cat`, `jq`. These are assumed by commands, hooks, and the user's global CLAUDE.md (e.g. `sed -i ''` for macOS, `gtimeout` instead of `timeout`).
- **macOS only** — `coreutils` from Homebrew if you want GNU-flavored `sed`/`timeout`. BSD defaults work; just match the syntax.
- **Docker** — only for the evaluation flow below. Not required for normal use.


### Windows


Use **WSL2** (Ubuntu, Debian, or similar). PowerShell is *not* supported:

- Claude Code's PowerShell tool is a preview feature with known gaps (no profile loading, no sandboxing on Windows, opt-in only).
- This repo's commands, hooks, and global CLAUDE.md assume POSIX semantics — `sed -i ''`, `mv`, `&&` chaining rules, `gtimeout`, etc. They will misbehave or fail outright under `cmd.exe` / PowerShell.
- Install WSL2 → install your distro → install Node + Claude Code + git inside the distro → run `claude` from the WSL shell. Treat the Windows filesystem as foreign; keep repos inside the Linux home (`~/projects/...`) for sane file watching and performance.


Native Windows (cmd / PowerShell) is unsupported. Patches welcome if you want to make it work, but the default assumption is POSIX.


## Install

The `atomic` binary backs cron and signals workflows (see [Signals workflow](#signals-workflow) below). End users:

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

If you have customized `~/.claude/CLAUDE.md` locally, `install` and `update` will not overwrite it. Instead, they write the new version to `~/.claude/CLAUDE.md.atomic-proposed` and print a hint to run `/atomic-claude-merge` from any Claude Code session. That command dispatches the `atomic-claude-merger` agent to produce `~/.claude/CLAUDE.md.atomic-merged`, shows a diff, and prompts Accept / Show diff / Open editor / Abort. On Accept the prior `CLAUDE.md` is backed up under `~/.claude/.atomic-backups/<timestamp>/`. Full spec: [`docs/spec/install-workflow.md`](docs/spec/install-workflow.md).

The install also registers a `SessionStart` hook (`atomic hooks install`) that injects any pending reminders at session open — supplementing cron-fired surfacing for missed cron fires, post-7-day cron expiry, tool unavailability, and post-restart catchup.

### Manual install

Download an archive from [GitHub Releases](https://github.com/damusix/atomic-claude/releases), verify with `shasum -c checksums.txt`, and move the `atomic` binary into any directory on your `$PATH`.

### Build from source

```bash
git clone https://github.com/damusix/atomic-claude.git
cd atomic-claude/atomic
make build       # or: go build -o ../bin/atomic ./cmd/atomic
```


## Workflow

The canonical lifecycle:

1. **`/atomic-plan`** — collaborative. You and Claude produce a checkpoint table written to `docs/design/<topic>.md` (brainstorm / rationale) or `docs/spec/<topic>.md` (implementation contract). Human-facing, Mermaid diagrams allowed. This is the human approval gate.

2. **`/subagent-implementation`** — autonomous from the spec. The orchestrator reads `docs/spec/`, writes a thin `BRIEF.md` + `STATE.md` + `FOLLOWUPS.md` to `.claude/.scratchpad/`, then drives an implement → review loop using fresh-context subagents. Each reviewer `VERDICT: PASS` triggers a commit before the next iteration. Non-blocking findings (🟡 risks, 🔵 nits, ❓ questions) accumulate in `FOLLOWUPS.md` and get dispositioned with you at finalize — fix now, file an issue, or drop. Nothing gets silently dropped just because the iteration passed.

3. **Ship** — pick the verb that matches your situation:

| Command | What it does |
|---------|-------------|
| `/commit-only` | Stage and commit. Does not push. |
| `/commit-and-pr` | Commit, push, open PR via `gh`. |
| `/pr-only` | Open PR for existing commits. |
| `/merge-to-main` | Merge current branch into base, no squash. |
| `/commit-and-merge` | `/commit-only` then `/merge-to-main`. |
| `/squash-only` | Squash all branch commits into one (no merge). |
| `/squash-and-merge` | `git merge --squash` from base, single commit, delete branch. |
| `/commit-and-squash` | `/commit-only` then `/squash-only`. |

All merge and squash commands invoke `atomic-verify` before touching the base, re-run tests on the merged tip, and prompt to delete the worktree if the branch came from `.worktrees/`.


## Evaluations


Try Atomic Claude in an isolated Docker container. Builds `atomic` from this repo's source, lays the bundle into a persistent `~/.claude`, drops you into Claude Code in a workspace dir that survives container removal.

Prereq: Docker + docker compose v2.


### Contributors (working in this repo)

Build the image once:

    make docker-build

Then drop into the Claude TUI:

    make docker-up

To bypass the entrypoint for fast iteration (raw bash shell, no Claude TUI):

    make docker-shell


### End users


If you're not a contributor to this repo and just want to evaluate atomic-claude on your own project, install atomic, then:

    atomic docker init

Writes `Dockerfile`, `docker-compose.yml`, `docker-entrypoint.sh`, `.dockerignore`, and a `tmp/` scaffold into `./atomic-docker/` (override with `--target some/path`). Refuses to overwrite existing files unless `--force` is passed.

From there:

    cd atomic-docker
    docker compose build
    docker compose run --rm atomic-eval

Drop your project files into `atomic-docker/tmp/workspace/` (or symlink your repo into it). Same volume layout and first-run `claude login` flow as the contributor setup above.


### Volume layout

Two directories under `tmp/` are bind-mounted into the container:

- `tmp/workspace/` → `/workspace` inside the container. Your eval project lives here. Persists across `docker compose run` invocations; only `.gitkeep` is tracked in git.
- `tmp/claude-home/` → `/home/atomic/.claude` inside the container. Holds Claude config, memory, and auth tokens. Persists `claude login` across runs. Only `.gitkeep` is tracked in git.

Both are gitignored. The `.gitkeep` placeholders keep them in the repo so the bind mounts exist on a fresh clone.


### First-run auth

On first `make docker-up`, Claude Code prompts you to authenticate. It emits a URL and code — open the URL in your host browser and paste the code. Auth tokens land in `tmp/claude-home/` and persist. Subsequent `make docker-up` runs skip the prompt.


### Linux UID note

Bind mounts use the host UID. On Linux, if `tmp/` files end up root-owned, rebuild with your UID:

    make docker-build HOST_UID=$(id -u)

Mac and Windows Docker Desktop handle UID mapping transparently; this step is not needed there.


### Reset

To start fresh (wipes auth and workspace):

    rm -rf tmp/claude-home/* tmp/claude-home/.[!.]* tmp/workspace/* tmp/workspace/.[!.]* 2>/dev/null; touch tmp/claude-home/.gitkeep tmp/workspace/.gitkeep


## Design axioms


Five enduring principles shape the system: cohesion-bounded scope, memory over config, explicit confirm for destructive ops, plain-text indexed selection, skills auto-fire vs commands explicit. See [`.claude/docs/axioms.md`](.claude/docs/axioms.md) before adding new artifacts.


## Where things live

| Path | Purpose | Audience |
|------|---------|----------|
| `.claude/.scratchpad/<date>-<desc>/` | Working memory for `/subagent-implementation` (BRIEF.md + STATE.md + FOLLOWUPS.md) | LLM only (gitignored) |
| `docs/design/<topic>.md` | Design rationale, alternatives, brainstorming | Humans |
| `docs/spec/<topic>.md` | Implementation contract (checkpoints, success criteria) | Humans + future implementers |
| `.worktrees/<branch>/` | Isolated worktree per feature branch | LLM + user (gitignored) |
| `tmp/` | Throwaway experiments, ad-hoc test scripts | Anyone (gitignored) |
| `.claude/rules/<lang>/*.md` | Path-scoped instructions, glob-gated via `paths:` frontmatter. Loads only when matching files enter context. | Humans (committed) |


## Output style

`output-styles/atomic.md` defines atomic style. Drop filler, articles, pleasantries, and hedging. Fragments are fine. Short synonyms preferred. Technical terms stay exact. Code blocks and error strings are never compressed. Style applies to Claude's TUI replies, not to source files or docs — those follow the codebase's own conventions.

Three intensity levels:

- **lite** — drop filler and hedging, keep articles and full sentences.
- **full** — drop articles, fragments OK, short synonyms. Default.
- **ultra** — abbreviate prose words (DB/auth/req/res/fn), arrows for causality (X → Y), one word when one word suffices.

Switch by saying "atomic lite", "atomic full", or "atomic ultra". Security warnings and irreversible-action confirmations revert to full prose automatically.


## Commands

| Command | What it does |
|---------|-------------|
| `/atomic-setup` | Bootstrap the current repo for atomic conventions. Audits .gitignore, docs/ layout, claude.md; proposes only what's missing. Never overwrites. |
| `/atomic-plan` | Collaborative plan → checkpoint table in `docs/design/` or `docs/spec/`. |
| `/atomic-compress <file>` | Compress a prose Markdown file into atomic style. Backs up original as `<file>.original.md`. |
| `/subagent-implementation` | Orchestrate implement → review subagent loop until task is complete. |
| `/worktree-start <name>` | Create isolated worktree at `.worktrees/<name>/`, new branch, auto-detected project setup. |
| `/git-cleanup [<name>]` | Scan stale git state (worktrees, branches, optional remote) via `atomic-git-scout`, present indexed report, ask before deleting. Local-only by default; asks about remote. |
| `/commit-only` | Stage and commit. Delegates message format to `atomic-commit` skill. |
| `/commit-and-pr` | Commit, push, open PR via `gh`. |
| `/commit-and-merge` | Commit then merge to base branch. |
| `/commit-and-squash` | Commit then squash all branch commits. |
| `/pr-only` | Open PR for the current branch (commits already exist). |
| `/merge-to-main` | Merge current branch into base, no squash. |
| `/squash-only` | Squash all branch commits into one (no merge). |
| `/squash-and-merge` | Squash-merge into base, delete branch. |
| `/remind-me <duration> <text>` | Schedule a reminder. Writes a reminder file and creates a one-shot cron that fires `/follow-up due <id>` at the given time. Degrades to file-only if `CronCreate` is unavailable. |
| `/follow-up [due <id>]` | Review pending reminders. Bare: indexed list + done/snooze/reschedule actions. Cron-fired: surfaces the specific reminder and waits for user response. |
| `/initialize-signals` | Bootstrap signals for a project that has never had them. Interactive, idempotent. Requires `atomic` binary. |
| `/documentation` | Update or create project docs (README, claude.md, docs/spec/, docs/design/) after significant changes. |
| `/report-issue` | Open a GitHub issue via `gh`. Auto-detects bug report vs. feature request. |


## Skills

Skills auto-fire when Claude encounters matching phrases. They can also be invoked explicitly.

| Skill | When it fires |
|-------|--------------|
| `atomic-commit` | "write a commit", "commit message", commit-time invocation from ship commands. |
| `atomic-review` | "review this PR", "code review", "review the diff". |
| `atomic-debug` | Error pastes, "broken", "doesn't work", "failing". |
| `atomic-tdd` | "let's implement X", "add feature Y", "fix bug Z", pre-code-change phrases. |
| `atomic-verify` | "done", "fixed", "passing", "ready to merge", "looks good" — any completion claim. |
| `atomic-signals` | "regenerate signals", "scan the project", "refresh project context", "what's in this repo", "rescan". Also fires from `/commit-only` when staged diff touches source files. |


## Agents

Agents are dispatched by the orchestrator (or directly by the user) via the Agent tool. Each runs in a fresh context.

| Agent | Dispatch when | Model |
|-------|--------------|-------|
| `atomic-builder` | Feature-checkpoint implementation. One cohesive slice (controller + service + DTO + tests, etc.). Refuses cross-cutting or architecturally ambiguous scope. | Sonnet |
| `atomic-surgeon` | Surgical 1-2 file edits. Typos, single-function rewrites, mechanical renames. Hard refuses 3+ file scope. | Sonnet |
| `atomic-investigator` | Read-only code location. "Where is X defined", "what calls Y", "list uses of Z". Returns `file:line — what` table, no prose, no speculation. | Haiku |
| `atomic-reviewer` | Diff review after each builder pass. Verifies TDD quality signals were actually run. Emits one line per finding + `VERDICT: PASS` or `CHANGES_REQUESTED`. | Sonnet |
| `atomic-git-scout` | Read-only scanner for stale git state (worktrees, branches, optional remote tracking refs). Classifies cleanup candidates and returns indexed report. Dispatched by `/git-cleanup`. Never mutates state. | Sonnet |
| `atomic-signals-inferrer` | Reads `deterministic-signals.md` and writes `inferred-signals.md`. Incremental: reads only the diff between scans and updates only dependent sections. Dispatched by `atomic-signals`. Scoped to `.claude/project/`. | Sonnet |


## Signals workflow

The signals workflow keeps Claude aware of the current shape of a project without hallucination. On first use, run `/initialize-signals` to generate two committed files:

- `.claude/project/deterministic-signals.md` — machine-generated facts: directory tree, manifests, languages, lockfile presence. Produced by `atomic signals scan`.
- `.claude/project/inferred-signals.md` — inferred meaning: framework, build/test/lint commands, architectural style, conventions. Produced by `atomic-signals-inferrer`.

Both files are auto-referenced in the project's `claude.md` via `@`-refs so Claude loads them on every session. The `atomic-signals` skill keeps them fresh: it auto-fires on project-state-change phrases and also runs silently from `/commit-only` when the staged diff touches source files. The inferrer uses an incremental diff path — on subsequent runs it reads only what changed and updates only the dependent sections, leaving everything else byte-identical.

Requires the `atomic` binary. Run without it for a degraded tree-only fallback. Full spec: `docs/spec/signals-workflow.md`.


## Conventions

- Atomic style applies to Claude's TUI replies, not to source files, comments, or documentation. Source files follow the codebase's own conventions.
- `claude.md` in any project should hold only meaningful context for that codebase — not general reminders, not duplicated tool lists. Keep it lean.
- No AI bylines in commit messages or PR descriptions.
- The scratchpad (`.claude/.scratchpad/`) is LLM working memory — ephemeral, gitignored, not for human consumption. Durable decisions go in `docs/`.
- Tests verify intent, not behavior. A test that still passes when the business logic changes is wrong.
- `tmp/` is for throwaway experiments and ad-hoc verification scripts. Not a scratch directory for checked-in work.
- When `/subagent-implementation` is about to start significant work (anything with three or more checkpoints), it prompts whether to use an isolated worktree. Already inside `.worktrees/*`? It skips the prompt.


## Contributing


This repo authors its artifacts at the top level (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`) — the shapes you'd copy into `~/.claude/` for install. But Claude Code only auto-loads artifacts from a project's `.claude/` directory, so editing a top-level file doesn't take effect in this repo's own session.


`scripts/link-local.sh` closes that loop. It symlinks each top-level artifact dir into `.claude/`, so the repo dogfoods its own config:


    ./scripts/link-local.sh


Idempotent (`ln -sfn`). Re-run any time you add a new agent, command, skill, output-style, or rule. The generated `.claude/{agents,commands,output-styles,skills,rules}/` symlinks are gitignored — they're machine-specific and exist only to make Claude Code load the work-in-progress sources.


Workflow when adding or editing an artifact:


1. Edit the source under `agents/`, `commands/`, `skills/<name>/`, `output-styles/`, or `rules/<lang>/`.
2. Run `./scripts/link-local.sh` if you added a *new* file (existing files are already linked).
3. Restart Claude Code (or start a new session) to pick up the change.
4. Test in this repo's session — that's the dogfood. If it doesn't feel right here, it won't feel right anywhere.


Do not commit anything under `.claude/agents/`, `.claude/commands/`, `.claude/output-styles/`, `.claude/skills/`, or `.claude/rules/` — those are generated. The `.claude/docs/` and `.claude/settings.local.json` files are real and tracked.


## License / status

Personal configuration. No license. No stability guarantee. Commands, agents, and skills may change in breaking ways between commits. Use at your own risk.
