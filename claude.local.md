# claude.local.md

Project-local context for working **on** this repo. Not copied anywhere — read by Claude only when the cwd is this repo.


## What this repo is

A holistic Claude Code configuration. The artifacts here (`claude.md`, `commands/`, `agents/`, `skills/`, `output-styles/`) are designed as one coherent system — atomic output style, an opinionated command set, a small subagent roster, and discipline skills that interlock. Not a grab-bag; everything is meant to compose.

Replaces (for the author) heavier toolkits like superpowers and caveman. Personal config, no stability guarantee.


## File roles (this repo specifically)

| File | Role | Destination |
|------|------|-------------|
| `claude.md` | Global instructions. Gets copied to `~/.claude/CLAUDE.md`. Affects every workspace, not just this repo. | `~/.claude/CLAUDE.md` |
| `claude.local.md` | This file. Project-local context for editing this repo. Gitignored. | Stays here, cwd-scoped. |
| `CLAUDE.md` | The committed project instructions for anyone working in this repo. Mirrors `claude.md` content because this repo *is* the config source. | Repo root, committed. |
| `README.md` | Human-facing overview of what the config does and how to install it. | Repo root, committed. |
| `commands/*.md` | Slash command definitions. Copied to `~/.claude/commands/`. | `~/.claude/commands/` |
| `agents/*.md` | Subagent definitions. Copied to `~/.claude/agents/`. | `~/.claude/agents/` |
| `skills/*/SKILL.md` | Discipline skills. Copied to `~/.claude/skills/`. | `~/.claude/skills/` |
| `output-styles/*.md` | Output style definitions. Copied to `~/.claude/output-styles/`. | `~/.claude/output-styles/` |
| `rules/<lang>/*.md` | Path-scoped topic rules. `paths:` frontmatter globs against filetypes (e.g. `**/*.{ts,tsx}`, `**/*.py`) so the rule only loads when Claude touches a matching file. Currently: `typescript/`, `python/`. Expand with more languages or topic subdirs as needed. | `~/.claude/rules/` (via `atomic claude install`) |


## Bundle source-of-truth rule


The `atomic` binary's embedded bundle (see `atomic/internal/bundlemirror/`) is sourced **only** from the root of this repo — never from `.claude/`. Bundleable directories: `agents/`, `commands/`, `output-styles/`, `rules/`, `skills/`, and `claude.md`. The `.claude/` tree is the *installed* config for dogfooding inside this repo (symlinks to the same root dirs); it must not be a bundle input. If you add a new artifact kind to bundle-mirror, source it from the root path, not its `.claude/` mirror.


## Coherence rules (when editing here)

- Treat the four artifact types (commands, agents, skills, output-styles) as one system. A change to one often demands a matching change to the others.
- `claude.md` is the global contract. Adding a command/agent/skill that other artifacts reference? Update `claude.md` so every workspace knows it exists.
- `README.md` is the public-facing index. New artifact, removed artifact, or renamed verb → update the tables.
- Atomic output style applies to Claude's TUI replies, not to the files in this repo. Command/agent/skill prose stays in normal English so it reads cleanly when installed.
- Skill triggers, agent dispatch criteria, and command behaviors must not contradict each other. If `/atomic-plan` says it writes to `docs/spec/` and an agent expects `docs/specs/`, that's a bug.


## Signals `@-refs` must stay wired (in this repo: `claude.local.md`)


The whole point of the signals workflow is that Claude has a current map of the project before exploring. That only works if some auto-loaded Claude instructions file `@-references` both `.claude/project/deterministic-signals.md` and `.claude/project/inferred-signals.md`.


**In this repo specifically**, the refs live in `claude.local.md` (this file) — not in `claude.md`. Reason: `claude.md` is the bundle source (gets installed as every user's global `~/.claude/CLAUDE.md`), so project-specific paths there would leak into every install. `claude.local.md` is gitignored, project-local, and still auto-loaded by Claude Code when cwd is this repo. That's the correct home for project-scoped `@`-refs.


- The `atomic-signals` skill checks for the refs in `claude.local.md` / `CLAUDE.local.md` first, then `claude.md` / `CLAUDE.md`. If present in ANY of them, it skips wiring. Don't introduce commands that try to enforce a single canonical location — the skill's search order is the contract.
- For most repos, the refs end up in `claude.md` / `CLAUDE.md` (one file, no separation). For this repo and any other config-source repos, they live in `claude.local.md`. Both are valid.
- If you fork the layout (e.g. moving refs into a separate `@`-included file), update the skill's search order in lockstep.
- When a user says "the auth system is broken", a session with signals loaded already knows which modules, services, and use cases live where. Without the `@-refs`, the snapshot files exist but never reach context — wasted scan, wasted inference.


## Design axioms (load every session)


@.claude/docs/axioms.md


Read these before adding new commands, skills, or agents. They capture decisions that emerged from this work and shouldn't be re-litigated each session: cohesion-bounded scope, memory > config, destructive-ops explicit confirm, plain-text indexed selection, skills auto-fire vs commands explicit.


## Agent configuration reference (load every session)


@.claude/docs/agent-config.md


Reference for how Claude Code agents, skills, commands, and output styles are defined — frontmatter shapes, tool restrictions, model selection, dispatch semantics. Consult before editing any artifact in `agents/`, `skills/`, `commands/`, or `output-styles/`.


## Claude Code upstream docs (load every session)


@.claude/docs/claude-code-references.md


URL index for official Claude Code documentation: agents, sub-agents, skills, commands, hooks, hooks-guide, tools-reference, worktrees, scheduled-tasks, headless. Fetch via WebFetch when verifying semantics — these URLs are the source of truth, not the local snapshots in `agent-config.md`.


## Naming

- All custom artifacts use the `atomic-` prefix (`atomic-builder`, `atomic-tdd`, `atomic-commit`, etc.) so they're easy to spot among third-party installs.
- Slash commands are imperative verbs (`/commit-only`, `/merge-to-main`, `/worktree-start`).


## Install (for this repo's artifacts)

No install script yet. Manual: copy each top-level directory into `~/.claude/`, restart Claude Code. A future `/install` or Makefile target is on the table.


## Project signals (auto-loaded)

@.claude/project/deterministic-signals.md
@.claude/project/inferred-signals.md
