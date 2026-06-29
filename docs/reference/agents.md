# Agents

Agents are specialized workers that run in a fresh context. The orchestrator dispatches them during `/subagent-implementation` and `/subagent-diagnose`, but you can also invoke them directly via the Agent tool. Two of [Anthropic's agent patterns](https://www.anthropic.com/engineering/building-effective-agents) are built in: orchestrator-workers (a parent breaks the task down and delegates to workers) and evaluator-optimizer (the implementer writes, a separate reviewer critiques).


## Code agents

These write and review code.

| Agent | What it does | Model |
|-------|-------------|-------|
| `atomic-implementer` | Dual-mode implementation agent. The orchestrator declares the mode at dispatch time. **feature mode**: implements a feature checkpoint — one cohesive slice across however many files it touches (controller + service + DTO + tests, etc.); writes a failing test first; refuses cross-cutting or ambiguous scope. **surgical mode**: makes surgical 1-2 file edits (typo fixes, single-function rewrites, mechanical renames); hard refuses anything larger. | Sonnet |
| `atomic-reviewer` | Reviews a diff after each implementer pass. Re-runs the quality signals it verifies (tests, type checks). One line per finding, ends with PASS or CHANGES_REQUESTED. Flags suppression patterns — error-catching added to dodge a failure without investigating it. Flags over-engineering — reinvented stdlib, duplicate helpers, or one-implementation abstractions. | Sonnet |


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
| `atomic-wiki-inferrer` | Owns the full signals pipeline: scans the repo via `atomic signals scan`, infers domain structure (using real import/call edges from the code-intel index when present; filename heuristics otherwise), writes `signals.md` (and per-domain files on large repos), wires the `@-ref` into `CLAUDE.md`. Dispatched by `/refresh-wiki` and silently by ship verbs. | Sonnet |


## Model tier overrides

Each agent's `model:` frontmatter defaults to its bundled tier (shown in the tables above). You can pin any installed atomic agent to a different tier via `atomic config agents`, which prompts interactively and writes the choice to `config.toml [agents]`.

```
atomic config agents
```

Available tiers: `haiku`, `sonnet`, `opus`. (`fable` is forward-reserved and may not correspond to a live Claude Code model tier yet.)

**Bundled defaults:**

| Agent | Default tier |
|-------|-------------|
| `atomic-investigator` | haiku |
| `atomic-implementer` | sonnet |
| `atomic-reviewer` | sonnet |
| `atomic-wiki-inferrer` | sonnet |
| `atomic-strategist` | opus |

**How it works.** The choice is stored in `config.toml [agents]` (machine-owned — not hand-edited). On every `atomic claude install` or `atomic claude update` the installer reads the map and patches `model:` in each agent file before writing it to `~/.claude/agents/`. An absent entry leaves the bundled default unchanged. Upgrades never clobber the choice because the tier is re-derived from config on every install, not baked into the installed file.

**Viewing active overrides.** `~/.claude/.atomic/config.resolved.md` (auto-loaded every session) includes a `[agents]` section listing any active overrides:

```
## [agents]

- `agents.atomic-implementer` = `haiku`
```

No override stored → no `[agents]` section in the file.

**Note:** only bundled artifacts tracked by `[install.artifacts]` are patched. Agents you added manually to `~/.claude/agents/` are not touched.
