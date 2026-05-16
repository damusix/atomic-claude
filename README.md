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
- **Scratchpad convention** — `.claude/.scratchpad/<date>-<desc>/` for LLM working memory, gitignored, ephemeral.
- **Doc layout** — `docs/spec/` for implementation contracts, `docs/design/` for rationale and alternatives, `tmp/` for throwaway experiments.


## Install

No install script yet. The artifacts are designed to live under `~/.claude/` (skills, commands, agents, output-styles) or be loaded via the Claude Code plugin mechanism. For now: clone the repo, copy the relevant directories into `~/.claude/`, and restart Claude Code. Which files belong in `~/.claude/` versus a project-local `.claude/` depends on whether you want things globally available or scoped to one repo.


## Workflow

The canonical lifecycle:

1. **`/atomic-plan`** — collaborative. You and Claude produce a checkpoint table written to `docs/design/<topic>.md` (brainstorm / rationale) or `docs/spec/<topic>.md` (implementation contract). Human-facing, Mermaid diagrams allowed. This is the human approval gate.

2. **`/subagent-implementation`** — autonomous from the spec. The orchestrator reads `docs/spec/`, writes a thin `BRIEF.md` + `STATE.md` to `.claude/.scratchpad/`, then drives an implement → review loop using fresh-context subagents. Each reviewer `VERDICT: PASS` triggers a commit before the next iteration.

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


## Design axioms


Five enduring principles shape the system: cohesion-bounded scope, memory over config, explicit confirm for destructive ops, plain-text indexed selection, skills auto-fire vs commands explicit. See [`.claude/docs/axioms.md`](.claude/docs/axioms.md) before adding new artifacts.


## Where things live

| Path | Purpose | Audience |
|------|---------|----------|
| `.claude/.scratchpad/<date>-<desc>/` | Working memory for `/subagent-implementation` (BRIEF.md + STATE.md) | LLM only (gitignored) |
| `docs/design/<topic>.md` | Design rationale, alternatives, brainstorming | Humans |
| `docs/spec/<topic>.md` | Implementation contract (checkpoints, success criteria) | Humans + future implementers |
| `.worktrees/<branch>/` | Isolated worktree per feature branch | LLM + user (gitignored) |
| `tmp/` | Throwaway experiments, ad-hoc test scripts | Anyone (gitignored) |


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


## Agents

Agents are dispatched by the orchestrator (or directly by the user) via the Agent tool. Each runs in a fresh context.

| Agent | Dispatch when | Model |
|-------|--------------|-------|
| `atomic-builder` | Feature-checkpoint implementation. One cohesive slice (controller + service + DTO + tests, etc.). Refuses cross-cutting or architecturally ambiguous scope. | Sonnet |
| `atomic-surgeon` | Surgical 1-2 file edits. Typos, single-function rewrites, mechanical renames. Hard refuses 3+ file scope. | Sonnet |
| `atomic-investigator` | Read-only code location. "Where is X defined", "what calls Y", "list uses of Z". Returns `file:line — what` table, no prose, no speculation. | Haiku |
| `atomic-reviewer` | Diff review after each builder pass. Verifies TDD quality signals were actually run. Emits one line per finding + `VERDICT: PASS` or `CHANGES_REQUESTED`. | Sonnet |
| `atomic-git-scout` | Read-only scanner for stale git state (worktrees, branches, optional remote tracking refs). Classifies cleanup candidates and returns indexed report. Dispatched by `/git-cleanup`. Never mutates state. | Sonnet |


## Conventions

- Atomic style applies to Claude's TUI replies, not to source files, comments, or documentation. Source files follow the codebase's own conventions.
- `claude.md` in any project should hold only meaningful context for that codebase — not general reminders, not duplicated tool lists. Keep it lean.
- No AI bylines in commit messages or PR descriptions.
- The scratchpad (`.claude/.scratchpad/`) is LLM working memory — ephemeral, gitignored, not for human consumption. Durable decisions go in `docs/`.
- Tests verify intent, not behavior. A test that still passes when the business logic changes is wrong.
- `tmp/` is for throwaway experiments and ad-hoc verification scripts. Not a scratch directory for checked-in work.
- When `/subagent-implementation` is about to start significant work (anything with three or more checkpoints), it prompts whether to use an isolated worktree. Already inside `.worktrees/*`? It skips the prompt.


## License / status

Personal configuration. No license. No stability guarantee. Commands, agents, and skills may change in breaking ways between commits. Use at your own risk.
