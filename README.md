<h1 align="center">Atomic Claude</h1>

<p align="center">
 <img src="./assets/atomic-claude.png" alt="Atomic Claude" />
</p>

<p align="center">
 <strong>An opinionated Claude Code configuration that trades narrative for signal.</strong>
</p>

<p align="center">
 <a href="docs/guides/install.md">Install</a> &bull;
 <a href="docs/reference/concepts.md">Concepts</a> &bull;
 <a href="docs/reference/workflow.md">Workflow</a> &bull;
 <a href="docs/reference/commands.md">Commands</a> &bull;
 <a href="docs/reference/skills.md">Skills</a> &bull;
 <a href="docs/reference/agents.md">Agents</a> &bull;
 <a href="docs/credits.md">Credits</a>
</p>

<p align="center">
 <img src="https://img.shields.io/badge/status-evolving-orange" alt="Status" />
 <a href="https://github.com/damusix/atomic-claude/releases/latest"><img src="https://img.shields.io/github/v/release/damusix/atomic-claude?label=latest" alt="Latest Release" /></a>
 <a href="./LICENSE"><img src="https://img.shields.io/github/license/damusix/atomic-claude" alt="License" /></a>
</p>

> **Still evolving.** Commands, agents, and skills may shift between releases. Breaking changes are flagged in the changelog.


## Before / after

Same question, same model, default Claude Code vs. Atomic Claude:

**Default Claude Code:**
```
Sure! I'd be happy to help you understand that. The issue you're experiencing
is likely caused by the middleware not properly validating the token expiry
timestamp. What's happening is that the comparison uses a strict less-than
operator when it should be using less-than-or-equal-to, which means tokens
that expire at exactly the current second are incorrectly rejected.

I'd suggest updating the comparison on line 42 of src/auth/middleware.ts...
```

**Atomic Claude:**
```
Bug in auth middleware. Token expiry check at src/auth/middleware.ts:42
uses `<` not `<=` — tokens expiring at current second get rejected.

Fix: change `token.exp < now` to `token.exp <= now`.
```

Same accuracy. Fewer tokens. Faster to scan.


## What you get

Atomic Claude is a coherent system of output styles, discipline skills, subagents, and workflow commands designed to compose. The pieces:

**Compressed replies.** A tone layer that strips filler from Claude's responses. Three intensity levels switchable mid-session. Opt in via `/config` then Output style then Atomic. Details in [docs/reference/output-style.md](docs/reference/output-style.md).

**Project-state awareness.** The `atomic` binary scans your repo and writes context files that Claude loads every session. Claude knows your framework, package manager, and build commands instead of guessing. Details in [docs/reference/signals-workflow.md](docs/reference/signals-workflow.md).

**User profile.** Install creates `~/.claude/.atomic/profile.md`, a plain-markdown file with six sections (Identity, Work, Active projects, Interests, People mentioned, Environment). Claude reads it every session and appends new facts as they surface naturally. Facts that hold across all projects go here; project-specific preferences stay in that project's auto memory. Run `/atomic-improve` to review and accept or discard drift. Details in [docs/reference/concepts.md](docs/reference/concepts.md).

**A spec-to-PR workflow.** Verify a hunch with `/gather-evidence`, plan with `/atomic-plan`, implement with `/subagent-implementation`, diagnose failures with `/subagent-diagnose`, then close the loop with `/atomic-improve` to turn session friction into durable config changes. Each stage uses fresh-context subagents that write TDD, gate on review, and commit per green checkpoint. Close your laptop, rerun the command next week, pick up where you left off. Details in [docs/reference/workflow.md](docs/reference/workflow.md).

**Discipline skills that auto-fire.** Seven skills trigger on natural language: TDD enforcement, completion verification, debugging, commit messages, code review, prose editing, and documentation routing. No slash command needed. Details in [docs/reference/skills.md](docs/reference/skills.md).

**Workflow commands for every git operation.** Ten verbs covering every combination of commit, push, squash, PR, and merge-to-base. Plus utilities for CI watching, stale branch cleanup, worktree isolation, reminders, and integrity checks. Details in [docs/reference/commands.md](docs/reference/commands.md).

For a walkthrough of how the pieces fit together, see [docs/reference/concepts.md](docs/reference/concepts.md).


## Start here

Not sure where to begin? Run `/atomic-help` in any repo. It reads your git state and recommends one next command, or run `/atomic-help tour` for a four-stage guided walkthrough of the whole system.

Otherwise, pick your depth:

1. **Compressed replies only.** Install, activate the output style via `/config`. Done. Everything else is optional.
2. **Project-state awareness.** Run `/atomic-setup` + `/refresh-signals` in your repo. Claude stops hallucinating build commands.
3. **Full plan, implement, review loop.** Read the [workflow reference](docs/reference/workflow.md).


## Install

Two commands. The first downloads the `atomic` binary (macOS / Linux / WSL2):

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
```

The second wires up the artifact bundle into `~/.claude/`:

```bash
atomic claude install
```

Activate the output style with `/config` then Output style then Atomic, and you're set.

For prereqs, flags, existing `~/.claude/CLAUDE.md` handling, updates, Docker evaluation, and uninstall: [docs/guides/install.md](docs/guides/install.md).


## What a fresh repo looks like

First session inside a new project. `/atomic-setup` audits conventions, `/refresh-signals` teaches Claude the repo's shape:

```text
❯ /atomic-setup

⏺ Audit

  ┌─────────────────────────────────────┬────────────────────────────────────┐
  │             Convention              │              Status                │
  ├─────────────────────────────────────┼────────────────────────────────────┤
  │ .gitignore has .claude/.scratchpad/ │ ✗                                  │
  │ .gitignore has .worktrees/          │ ✗                                  │
  │ docs/spec/                          │ ✗                                  │
  │ docs/design/                        │ ✗                                  │
  │ Signals files                       │ ✗                                  │
  │ CLAUDE.md @-refs                    │ ✗                                  │
  └─────────────────────────────────────┴────────────────────────────────────┘

  Proposed actions

  [1] Append .claude/.scratchpad/ and .worktrees/ to .gitignore
  [2] Create docs/spec/.gitkeep + docs/design/.gitkeep
  [3] Append signals @-refs to CLAUDE.md

  Apply which actions? → All (1-3)

  ✓ Applied. Next: /refresh-signals

❯ /refresh-signals

⏺ atomic signals scan
  ⎿  Tree · Manifests · Languages → .claude/project/deterministic-signals.md

⏺ atomic-signals-inferrer
  ⎿  Framework: NestJS · package manager: pnpm
  ⎿  Build: pnpm build · test: pnpm test · lint: pnpm lint
  ⎿  → .claude/project/signals.md

  signals initialized.
```

From here: `/atomic-plan` opens the spec, `/worktree-start` isolates the branch, `/subagent-implementation` drives the loop.


## Reference

| Topic | Link |
|-------|------|
| Workflow lifecycle | [docs/reference/workflow.md](docs/reference/workflow.md) |
| Commands | [docs/reference/commands.md](docs/reference/commands.md) |
| Skills | [docs/reference/skills.md](docs/reference/skills.md) |
| Agents | [docs/reference/agents.md](docs/reference/agents.md) |
| Output style | [docs/reference/output-style.md](docs/reference/output-style.md) |
| Signals workflow | [docs/reference/signals-workflow.md](docs/reference/signals-workflow.md) |
| Concepts (how it flows) | [docs/reference/concepts.md](docs/reference/concepts.md) |
| Conventions | [docs/reference/conventions.md](docs/reference/conventions.md) |
| Install / update / uninstall | [docs/guides/install.md](docs/guides/install.md) |
| Docker evaluation | [docs/guides/evaluations.md](docs/guides/evaluations.md) |
| Contributing | [docs/guides/contributing.md](docs/guides/contributing.md) |
| Credits | [docs/credits.md](docs/credits.md) |
| Specs | [docs/spec/](docs/spec/) |


## Contributing

Atomic Claude dogfoods itself. The root artifacts are both the live config and the bundle source. See [docs/guides/contributing.md](docs/guides/contributing.md).


## License

[MIT](LICENSE)
