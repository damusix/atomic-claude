# Agents

Agents are specialized workers that run in a fresh context. The orchestrator dispatches them during `/subagent-implementation` and `/subagent-diagnose`, but you can also invoke them directly via the Agent tool.


## Code agents

These write and review code.

| Agent | What it does | Model |
|-------|-------------|-------|
| `atomic-builder` | Implements a feature checkpoint — one cohesive slice across however many files it touches (controller + service + DTO + tests, etc.). Writes a failing test first. Refuses cross-cutting or ambiguous scope. | Sonnet |
| `atomic-surgeon` | Makes surgical 1-2 file edits. Typo fixes, single-function rewrites, mechanical renames. Hard refuses anything larger. | Sonnet |
| `atomic-reviewer` | Reviews a diff after each builder pass. Re-runs the quality signals it verifies (tests, type checks). One line per finding, ends with PASS or CHANGES_REQUESTED. Flags suppression patterns — error-catching added to dodge a failure without investigating it. | Sonnet |


## Research agents

These read code but never write it.

| Agent | What it does | Model |
|-------|-------------|-------|
| `atomic-investigator` | Locates code. "Where is X defined?", "What calls Y?", "List all uses of Z." When an index is present, leads with `atomic code explore` for broad scoping (one natural-language query returns the relevant symbols, files, and relationships), then uses `atomic code search/callers/callees/impact` for targeted follow-up; falls back to `sg`/`grep` otherwise. Returns a file:line table with no speculation. | Haiku |
| `atomic-strategist` | Reasons through hard problems — plans, specs, architectural tradeoffs. Surfaces hidden assumptions and recommends approaches. Read-only; never implements. Dispatched for root-cause analysis when the implement→review loop gets stuck on the same failure. | Opus |


## Infrastructure agents

These handle system-level tasks.

| Agent | What it does | Model |
|-------|-------------|-------|
| `atomic-git-scout` | Scans for stale worktrees, branches, and remote tracking refs. Classifies each as safe-to-delete, needs-confirmation, or skip. Used by `/git-cleanup`. | Sonnet |
| `atomic-signals-inferrer` | Owns the full signals pipeline: scans the repo via `atomic signals scan`, infers domain structure (using real import/call edges from the code-intel index when present; filename heuristics otherwise), writes `signals.md` (and per-domain files on large repos), wires the `@-ref` into `CLAUDE.md`. Dispatched by `/refresh-signals` and silently by ship verbs. | Sonnet |
| `atomic-haiku` | Lightweight background runner for CI polling, log scraping, and status checks. Used by `/watch-ci`. | Haiku |
| `atomic-claude-merger` | Reconciles your `CLAUDE.md` with updates from an install or upgrade. Preserves your sections, replaces atomic-owned ones. | Sonnet |
