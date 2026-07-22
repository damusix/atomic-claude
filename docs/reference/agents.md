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


## Model and effort overrides

Each agent's `model:` frontmatter defaults to its bundled tier (shown in the tables above). You can pin any installed atomic agent to a different model and reasoning effort via `atomic config agents`, which prompts interactively per agent and writes the choice to `config.toml [claude.agents]`.

```
atomic config agents
```

For each agent, the prompt asks for:

- a **model** (free text, blank = bundled default): a tier alias (`haiku`, `sonnet`, `opus`) or an exact Claude Code model id, e.g. `claude-opus-4-8`. No provider prefix (`anthropic/` etc.). Validation is lenient: any well-formed value is accepted and passed through to the frontmatter, and Claude Code resolves it at runtime.
- an **effort** level: `low`, `medium`, `high`, `xhigh`, or `max`. Claude Code downgrades gracefully per model at runtime if a model doesn't support the requested level.

Model and effort are independent. Set either one alone, both, or neither.

**Bundled defaults:**

| Agent | Default tier |
|-------|-------------|
| `atomic-investigator` | haiku |
| `atomic-implementer` | sonnet |
| `atomic-reviewer` | sonnet |
| `atomic-wiki-inferrer` | sonnet |
| `atomic-strategist` | opus |

(`fable` is forward-reserved and may not correspond to a live Claude Code model tier yet.)

**How it works.** Overrides are stored as nested `[claude.agents.<name>]` tables in `config.toml` (machine-owned, not hand-edited), each with optional `model` and `effort` fields, namespaced under the Claude Code harness so pi's own `[pi.agents.<name>]` overrides stay separate:

```toml
[claude.agents.atomic-implementer]
model = "claude-opus-4-8"
effort = "high"
```

Nested tables are the only accepted shape. A scalar entry (`atomic-implementer = "opus"`) is a config parse error.

On every `atomic claude install` or `atomic claude update` the installer reads the map and patches `model:` and `effort:` in each agent file's frontmatter before writing it to `~/.claude/agents/`, applying only the fields that are set. An absent field leaves the bundled default for that field unchanged. Upgrades never clobber the choice because both fields are re-derived from config on every install, not baked into the installed file.

**Applied immediately.** `atomic config agents` no longer requires a separate reinstall: after saving, it re-patches your already-installed `~/.claude/agents/*.md` files with the new `model:`/`effort:` values. Running Claude Code sessions must be restarted to pick up the new frontmatter.

This immediate re-patch only touches agent files that are already installed under the default `~/.claude` root; it never performs a first-time install. A custom `--target` install directory is not covered. Re-sync it by re-running `atomic claude install --target <dir>`.

**Drift detection and repair.** `atomic doctor`'s install-integrity check compares each installed agent's frontmatter against what your `[claude.agents]` config would produce (the bundle patched with your `model`/`effort` overrides). An installed agent missing a configured override reports WARN, the same way any other install drift does. `atomic doctor --fix` re-applies the patch and clears it. This is not a separate check; it reuses the same install-integrity check that already covers every installed artifact.

**Viewing active overrides.** `~/.atomic/config.resolved.md` (auto-loaded every session) includes a `[claude]` section listing any active overrides:

```
## [claude]

- `claude.agents.atomic-implementer.model` = `claude-opus-4-8`
- `claude.agents.atomic-reviewer.effort` = `high`
```

No override stored → no `[claude]` section in the file.

**Note:** only bundled artifacts tracked by `[install.artifacts]` are patched. Agents you added manually to `~/.claude/agents/` are not touched.
