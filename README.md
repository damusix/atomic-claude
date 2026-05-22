<h1 align="center">Atomic Claude</h1>

<p align="center">
 <img src="./assets/atomic-claude.png" alt="Atomic Claude" />
</p>

<p align="center">
 <strong>An opinionated Claude Code configuration that trades narrative for signal — compressed replies, an idea-to-PR workflow, and a clean split between auto-firing skills and explicit slash commands.</strong>
</p>

<p align="center">
 <a href="docs/guides/install.md">Install</a> •
 <a href="docs/reference/workflow.md">Workflow</a> •
 <a href="docs/reference/commands.md">Commands</a> •
 <a href="docs/reference/skills.md">Skills</a> •
 <a href="docs/reference/agents.md">Agents</a> •
 <a href="docs/credits.md">Credits</a>
</p>

<p align="center">
 <img src="https://img.shields.io/badge/status-evolving-orange" alt="Status" />
 <a href="https://github.com/damusix/atomic-claude/releases/latest"><img src="https://img.shields.io/github/v/release/damusix/atomic-claude?label=latest" alt="Latest Release" /></a>
 <a href="./LICENSE"><img src="https://img.shields.io/github/license/damusix/atomic-claude" alt="License" /></a>
</p>

> **Still evolving.** Commands, agents, and skills may shift between releases. Breaking changes are flagged in the changelog. Ship verbs are safe to interrupt; when in doubt, `git reflog`.


## Install

Two commands. The first lays down the `atomic` binary; the second wires everything else up.

Download the `atomic` binary (macOS / Linux / WSL2):

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```

Install the artifact bundle into `~/.claude/`:

```bash
atomic claude install
```

That's it. Activate the output style with `/config` → **Output style** → **Atomic** in any Claude Code session, and you're set.

**On update.** Run `atomic claude update` to refresh. If you've customized `~/.claude/CLAUDE.md`, the new version lands at `~/.claude/.atomic/proposed/CLAUDE.md` — run `/atomic-claude-merge` to reconcile. All atomic-owned per-user state (backups, proposed merges, the config TOML and its rendered view) lives under `~/.claude/.atomic/`.

Prereqs, flags, full merge contract, `atomic doctor` verification, Docker eval → [docs/guides/install.md](docs/guides/install.md).


## What a fresh repo looks like

First session inside a new project — `/atomic-setup` audits conventions, then `/initialize-signals` teaches Claude the repo's shape. Abridged transcript from a fictional `acme-payments` repo (NestJS, pnpm):

```text
❯ /atomic-setup

⏺ Audit

  ┌─────────────────────────────────────┬────────────────────────┐
  │             Convention              │         Status         │
  ├─────────────────────────────────────┼────────────────────────┤
  │ .gitignore exists                   │ ✓                      │
  │ .gitignore has .claude/.scratchpad/ │ ✗                      │
  │ .gitignore has .worktrees/          │ ✗                      │
  │ CLAUDE.md exists                    │ ✓                      │
  │ docs/spec/                          │ ✗                      │
  │ docs/design/                        │ ✗                      │
  │ atomic binary                       │ ✓                      │
  │ SessionStart hook                   │ ✗ (no settings.json)   │
  │ Signals files                       │ ✗                      │
  │ CLAUDE.md @-refs                    │ ✗                      │
  └─────────────────────────────────────┴────────────────────────┘

  Classification: partial

  Proposed actions

  [1] Append .claude/.scratchpad/ and .worktrees/ to .gitignore
  [2] Create docs/spec/.gitkeep + docs/design/.gitkeep
  [3] Run `atomic hooks install`
  [4] Append signals @-refs to CLAUDE.md
  [5] Print follow-up: run /initialize-signals

  Apply which actions? → All (1-5)

  ✓ .gitignore: appended .claude/.scratchpad/ and .worktrees/
  ✓ docs/spec/.gitkeep + docs/design/.gitkeep created
  ✓ atomic hooks install (scope=user)
  ✓ CLAUDE.md: appended signals @-refs

  Next: /initialize-signals

❯ /initialize-signals

⏺ atomic signals scan
  ⎿  Tree · Manifests · Languages → .claude/project/deterministic-signals.md

⏺ atomic-signals-inferrer (35 tool uses · 80.9k tokens · 3m 3s)
  ⎿  Framework: NestJS · package manager: pnpm
  ⎿  Build: pnpm build · test: pnpm test · lint: pnpm lint
  ⎿  → .claude/project/inferred-signals.md

  signals initialized.

  Notable findings worth your attention:
  - PaymentProcessor instantiated in src/payments/ but missing from src/index.ts public exports
  - src/helpers/template/ appears to be placeholder scaffolding, not a real domain
```

From here you're set: `/atomic-plan` opens the spec, `/worktree-start` isolates the branch, `/subagent-implementation` drives the loop.


# What is Atomic Claude?

A holistic Claude Code configuration designed as one coherent system. Grouped by what each piece does for you.

**[Compressed TUI replies](docs/reference/output-style.md)**

- Tone layer that strips narrative scaffolding from Claude's responses — drop articles, kill filler, fragments OK.
- Three intensity levels (`atomic lite` / `atomic full` / `atomic ultra`) switchable mid-session.
- Diagrams and tables over prose when they carry the load.
- Opt in via `/config` → **Output style** → **Atomic**; takes effect next session.
- The compression is the smallest piece — skipping it still leaves most of the system intact.

**[Project-state awareness via signals](docs/reference/signals-workflow.md)**

- The `atomic` binary scans your repo and writes `.claude/project/{deterministic,inferred}-signals.md`, auto-loaded into every session via `@`-ref.
- Claude knows your framework, package manager, and build/test/lint commands instead of guessing.
- The `atomic-signals` skill auto-refreshes on phrases like "rescan the project" and runs silently when `/commit-only` detects source-tree changes.
- No hallucinated `npm run` scripts, no invented `make` targets.

**[Autonomous spec → implement → review loop](docs/reference/workflow.md)** that survives `/clear`

- `/atomic-plan` writes an append-mostly `docs/spec/<topic>.md` with a `## Change log` of every amendment.
- `/subagent-implementation` keeps `BRIEF.md`, `STATE.md`, `FOLLOWUPS.md` in `.claude/.scratchpad/` and dispatches fresh-context [subagents](docs/reference/agents.md): `atomic-builder` (feature slices), `atomic-surgeon` (1-2 file edits), `atomic-investigator` (read-only locator, haiku), `atomic-reviewer` (re-runs the quality signals it verifies), `atomic-strategist` (opus, heavyweight reasoning).
- Eight [discipline skills](docs/reference/skills.md) auto-fire on natural-language phrases — `atomic-tdd` ("let's implement X"), `atomic-verify` ("looks done"), `atomic-debug` ("this is broken"), `atomic-commit`, `atomic-review`, `atomic-signals`, `atomic-prose`, `atomic-documentation` ("doc this change", "what surfaces does this touch"). Everything else is an explicit slash command.
- Close your laptop for a week, rerun the same command, pick up where you left off.

**[Ten ship verbs](docs/reference/workflow.md)** covering every combination of commit / push / squash / PR / merge-to-base

- Commit messages via the `atomic-commit` skill; PR bodies via `atomic-review`.
- Session reports consumed and deleted on successful commit.
- Signals refreshed automatically on source-tree changes.
- `atomic-verify` runs before touching base; worktree-delete prompt on merge/squash; `/documentation` reminder on significant changes.

**Tooling for long-running and out-of-loop work**

- `/watch-ci` — background Haiku watches CI, reports back.
- `/git-cleanup` — scans stale worktrees, branches, optional remote refs; per-item confirm.
- `/worktree-start` — isolated `.worktrees/<branch>/` with auto-detected setup + baseline test.
- `/remind-me` + `/follow-up` — cron-fired reminders surfaced at session open or on demand.
- `/atomic-help` — reads git state, recommends the next verb.
- [`atomic doctor`](docs/spec/atomic-doctor.md) — nine integrity checks (install / hooks / signals / refs / manifest / followups / memory / binary / config). `--fix` interactive; `--json` for CI.
- `atomic validate` — lints spec markdown, cross-reference integrity, bundle parity against the embedded manifest.
- [`atomic config`](docs/spec/atomic-state-and-config.md) — `get | set | unset | list | path` over `~/.claude/.atomic/config.toml`. Shell-settable defaults that steer every Claude session via an `@-ref` from the bundled `CLAUDE.md`. Works with or without the session-start hook (universal file-based delivery), so enterprise environments that block hooks are still covered. Schema v1 starts with `output.intensity` only; more keys land per concrete steering need.
- [`atomic followups`](docs/spec/follow-ups-folder.md) — `list | add | close | render | migrate | path` over `.claude/project/followups/`. Per-entry YAML frontmatter files plus a regenerated `INDEX.md` auto-loaded into every session. `add` is the deterministic entrypoint shelled out to by `/subagent-implementation` Phase 3 `defer`; `/follow-up review` triages stale entries with per-item disposition.


## Canonical workflow

### One-time per repo

Bootstrap a fresh repo for atomic conventions, then teach Claude what it looks like.

| Step | Verb | What it does |
|------|------|--------------|
| 1 | `/atomic-setup` | Audits `.gitignore` (`.worktrees/`, `.claude/.scratchpad/`, `tmp/`), proposes `docs/{spec,design,guides,reference}/` scaffold, drops starter `CLAUDE.md` if missing. Proposes only what's missing; never overwrites. |
| 2 | `/initialize-signals` | Scans the project and writes `.claude/project/{deterministic,inferred}-signals.md`. Wires `@`-refs so Claude loads your repo's shape every session. |

Now you can get to work.

### Day-to-day

The loop you run every time you build something.

| Step | Verb | What it does |
|------|------|--------------|
| 1 | `/atomic-plan` | Human approval gate. Gauges triviality: trivial → inline `docs/spec/<topic>.md`; non-trivial → `docs/design/<topic>.md` (concepts) + `docs/spec/<topic>.md` (contract) authored via subagent loop. Optional investigator / strategist passes. |
| 2 | `/worktree-start <branch>` | Isolated `.worktrees/<branch>/` with auto-detected setup and baseline test run. |
| 3 | `/subagent-implementation` | Autonomous implement → review loop. Builder writes failing test, implements, runs quality signals. Reviewer re-runs and gates. Commits per green checkpoint. |
| 4 | `/subagent-diagnose [ci/bug]` | When something breaks. Same scratchpad + investigator + builder + reviewer loop as `/subagent-implementation`, but seeded from a failed CI run or a freeform bug symptom. |
| 5 | `/commit-and-pr` | Commit pending work, push, open PR. (Or pick another row from the [ship verbs](#ship-verbs) table.) |


## Ship verbs

| Verb | Commit pending? | Push? | Squash? | PR? | Merge to base? |
|------|-----------------|-------|---------|-----|----------------|
| `/commit-only` | yes | | | | |
| `/commit-and-push` | yes | yes | | | |
| `/commit-and-pr` | yes | yes | | yes | |
| `/commit-and-squash` | yes | | yes | | |
| `/commit-and-merge` | yes | | | | yes |
| `/push-only` | | yes | | | |
| `/pr-only` | | yes | | yes | |
| `/squash-only` | | | yes | | |
| `/squash-and-merge` | | | yes | | yes |
| `/merge-to-main` | | | | | yes |


## Reference

| Topic | Source path | Docs |
|-------|-------------|------|
| Commands | `commands/` | [docs/reference/commands.md](docs/reference/commands.md) |
| Skills | `skills/` | [docs/reference/skills.md](docs/reference/skills.md) |
| Agents | `agents/` | [docs/reference/agents.md](docs/reference/agents.md) |
| Output style | `output-styles/atomic.md` | [docs/reference/output-style.md](docs/reference/output-style.md) |
| Rules (path-scoped instructions) | `rules/` | — |
| Global CLAUDE.md | `CLAUDE.md` (installed at `~/.claude/CLAUDE.md`) | — |
| Lifecycle + ship verbs | — | [docs/reference/workflow.md](docs/reference/workflow.md) |
| Signals workflow | — | [docs/reference/signals-workflow.md](docs/reference/signals-workflow.md) |
| Conventions | — | [docs/reference/conventions.md](docs/reference/conventions.md) |
| Design axioms | — | [.claude/docs/axioms.md](.claude/docs/axioms.md) |
| Install (prereqs, WSL2, manual, source) | — | [docs/guides/install.md](docs/guides/install.md) |
| Docker evaluation environment | — | [docs/guides/evaluations.md](docs/guides/evaluations.md) |
| Contributing | — | [docs/guides/contributing.md](docs/guides/contributing.md) |
| Credits + comparison with caveman / superpowers | — | [docs/credits.md](docs/credits.md) |
| Implementation specs | — | [docs/spec/](docs/spec/) |


## Contributing

Atomic Claude dogfoods itself — the root artifacts are both the live config and the bundle source. See [docs/guides/contributing.md](docs/guides/contributing.md).


## License

[MIT](LICENSE). Use it in personal projects or at work. No warranty, no liability; standard MIT terms apply.
