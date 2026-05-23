# Agents


Agents are dispatched by the orchestrator (or directly by the user) via the Agent tool. Each runs in a fresh context.

| Agent | Dispatch when | Model |
|-------|--------------|-------|
| `atomic-builder` | Feature-checkpoint implementation. One cohesive slice (controller + service + DTO + tests, etc.). Refuses cross-cutting or architecturally ambiguous scope. | Sonnet |
| `atomic-surgeon` | Surgical 1-2 file edits. Typos, single-function rewrites, mechanical renames. Hard refuses 3+ file scope. | Sonnet |
| `atomic-investigator` | Read-only code location. "Where is X defined", "what calls Y", "list uses of Z". Returns `file:line — what` table, no prose, no speculation. | Haiku |
| `atomic-reviewer` | Diff review after each builder pass. Verifies TDD quality signals were actually run. Emits one line per finding + `VERDICT: PASS` or `CHANGES_REQUESTED`. | Sonnet |
| `atomic-strategist` | Heavyweight reasoning over plans, specs, designs, and hard problems. Restates problem, surfaces hidden assumptions, names tradeoffs, recommends approach with explicit confidence. Read-only; does not implement, gate, or locate. | Opus |
| `atomic-git-scout` | Read-only scanner for stale git state (worktrees, branches, optional remote tracking refs). Classifies cleanup candidates and returns indexed report. Dispatched by `/git-cleanup`. Never mutates state. | Sonnet |
| `atomic-signals-inferrer` | Reads `deterministic-signals.md` and writes `signals.md` (the router). On large repos, dispatches sub-agents per domain to write `signals/<domain>/` files with reviewer validation. On small repos, writes everything into `signals.md` directly. Dispatched by `atomic-signals`. Scoped to `.claude/project/`. | Sonnet |
| `atomic-haiku` | Lightweight background runner for polling, status checks, log scraping, structured reporting. Read-only by default. Used by `/watch-ci`; available for any task too lightweight for Sonnet. | Haiku |
| `atomic-claude-merger` | Merges `~/.claude/.atomic/proposed/CLAUDE.md` into the live `~/.claude/CLAUDE.md`. Preserves user sections, replaces atomic-owned ones. Dispatched by `/atomic-claude-merge`. | Sonnet |
