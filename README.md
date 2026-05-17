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

- **Output style** (`output-styles/atomic.md`) — strips ceremony from Claude's TUI replies. Three intensity levels (lite, full, ultra). See [reference/output-style](docs/reference/output-style.md).
- **Commands** (`commands/`) — slash commands for planning, implementation, shipping, and repo hygiene. See [reference/commands](docs/reference/commands.md).
- **Skills** (`skills/`) — discipline skills that auto-fire on matching phrases (TDD, commit format, verification gate, debugging, code review). See [reference/skills](docs/reference/skills.md).
- **Agents** (`agents/`) — named subagents the orchestrator dispatches. See [reference/agents](docs/reference/agents.md).
- **Rules** (`.claude/rules/<lang>/*.md`) — path-scoped instructions, glob-gated via `paths:` frontmatter. Starter rules ship for TypeScript and Python; add more languages or topic subdirs as you grow.
- **Scratchpad convention** — `.claude/.scratchpad/<date>-<desc>/` for LLM working memory, gitignored, ephemeral.
- **Doc layout** — `docs/spec/` for implementation contracts, `docs/design/` for rationale and alternatives, `tmp/` for throwaway experiments.


## Quick install

```bash
curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash
atomic claude install
```

Full prerequisites, manual install, Windows/WSL2 notes, and build-from-source: [guides/install](docs/guides/install.md).

To try Atomic Claude in an isolated Docker container before installing globally: [guides/evaluations](docs/guides/evaluations.md).


## Workflow

1. **`/atomic-plan`** — collaborative checkpoint table written to `docs/design/` (rationale) or `docs/spec/` (contract). Human approval gate.
2. **`/subagent-implementation`** — autonomous from the spec. Implement → review loop with fresh-context subagents. Commits per green iteration. Non-blocking findings accumulate in `FOLLOWUPS.md` and get dispositioned at finalize.
3. **Ship** — pick a verb (`/commit-only`, `/commit-and-pr`, `/merge-to-main`, `/squash-and-merge`, etc.). Full lifecycle + ship table: [reference/workflow](docs/reference/workflow.md).


## Design axioms

Five enduring principles shape the system: cohesion-bounded scope, memory over config, explicit confirm for destructive ops, plain-text indexed selection, skills auto-fire vs commands explicit. See [`.claude/docs/axioms.md`](.claude/docs/axioms.md) before adding new artifacts.


## Where things live

| Path | Purpose | Audience |
|------|---------|----------|
| `.claude/.scratchpad/<date>-<desc>/` | Working memory for `/subagent-implementation` (BRIEF.md + STATE.md + FOLLOWUPS.md) | LLM only (gitignored) |
| `docs/design/<topic>.md` | Design rationale, alternatives, brainstorming | Humans |
| `docs/spec/<topic>.md` | Implementation contract (checkpoints, success criteria) | Humans + future implementers |
| `docs/guides/` | Install, evaluations, contributing | Humans |
| `docs/reference/` | Commands, skills, agents, output-style, workflow, signals, conventions | Humans |
| `.worktrees/<branch>/` | Isolated worktree per feature branch | LLM + user (gitignored) |
| `tmp/` | Throwaway experiments, ad-hoc test scripts | Anyone (gitignored) |
| `.claude/rules/<lang>/*.md` | Path-scoped instructions, glob-gated via `paths:` frontmatter | Humans (committed) |


## Documentation

| Topic | Path |
|-------|------|
| Install (prereqs, WSL2, manual, source) | [docs/guides/install.md](docs/guides/install.md) |
| Docker evaluation environment | [docs/guides/evaluations.md](docs/guides/evaluations.md) |
| Contributing (link-local.sh, dogfood loop) | [docs/guides/contributing.md](docs/guides/contributing.md) |
| Lifecycle + ship verbs | [docs/reference/workflow.md](docs/reference/workflow.md) |
| Commands reference | [docs/reference/commands.md](docs/reference/commands.md) |
| Skills reference | [docs/reference/skills.md](docs/reference/skills.md) |
| Agents reference | [docs/reference/agents.md](docs/reference/agents.md) |
| Output style + intensity levels | [docs/reference/output-style.md](docs/reference/output-style.md) |
| Signals workflow | [docs/reference/signals-workflow.md](docs/reference/signals-workflow.md) |
| Conventions | [docs/reference/conventions.md](docs/reference/conventions.md) |
| Implementation specs | [docs/spec/](docs/spec/) |


## License / status

Personal configuration. No license. No stability guarantee. Commands, agents, and skills may change in breaking ways between commits. Use at your own risk.
