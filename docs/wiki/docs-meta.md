---
type: Domain
description: Two-voice taxonomy, diff-driven surface routing, prose style, design axioms.
---

# docs-meta

## What it does

Two-voice documentation taxonomy, surface routing, and the design axiom set. `atomic-documentation` classifies diffs against indexed surfaces and emits a structured YAML handoff. `atomic-prose` drafts human-readable docs. `/documentation` is the user-facing orchestrator (two modes: bootstrap and authoring). [`output-styles/atomic.md`](../../output-styles/atomic.md) governs Claude's TUI reply style, including a three-rung presentation ladder (prose → structure → rendered page) whose third rung is served by the [`skills/atomic-legible/SKILL.md`](../../skills/atomic-legible/SKILL.md) skill.

## Artifacts

- [`output-styles/atomic.md`](../../output-styles/atomic.md) — governs Claude's TUI reply style. Terse, fragments OK, clarity over compression. Reframed around clarity (substance stays, fluff dies); intensity levels removed. Carries a `# Presentation ladder` section (three rungs, each denser than the last): rung 1 prose (≤2 entities), rung 2 structure (table for comparison, tree for hierarchy, ASCII flow for sequencing), rung 3 rendered page — offered via the `atomic-legible` skill when a reply crosses a density threshold (3+ headed sections, a table past ~6 columns or ~20 rows, or ~50+ lines of structured content); the TUI reply stays an atomic summary plus a one-line offer, and the page is produced without offering only when the request is already presentation-shaped or the user already accepted a render earlier this session. Includes an explicit `# Subagents` section noting that agents carry the response-voice rule directly via `agent-atomic-voice` partial. Includes a `# Boundaries` section carrying the Two-voices taxonomy: atomic style governs how Claude talks; how files are written is a separate axis (`atomic-prose` skill for narrative docs; terse technical prose for specs/designs/CLAUDE.md/signals/agents/commands; `atomic-documentation` skill routes the diff to the right surface).
- [`skills/atomic-legible/SKILL.md`](../../skills/atomic-legible/SKILL.md) — re-renders a previous (or current) answer as a single self-contained HTML page written to `.claude/.scratchpad/<YYYY-MM-DD>-<slug>/legible.html` for legible reading; the TUI reply stays atomic (short summary plus the `file://` path, no auto-open). Auto-fires on "show me that legibly", "render that", "make that readable", "show that as a page", "pretty version of that", "re-render that answer". Also invoked by the output style's presentation-ladder rung 3, and fires directly (no offer round-trip) when the request itself is presentation-shaped or the user already accepted a render earlier this session. Content rule: re-presentation only, never re-derivation — every fact, number, and code block matches exactly what was said; compressed fragments may expand to full sentences, ASCII trees/tables become real markup. Iteration overwrites the same file, no versioning. Shares the file contract with `atomic-visual-options` (starts with `<!DOCTYPE html>`, CSS inline, no external requests/JS, `prefers-color-scheme` support, code blocks with `font-variant-ligatures: none`, throwaway/never committed) but differs in purpose: `atomic-visual-options` presents unmade decisions for a typed pick during planning; `atomic-legible` presents one already-finished answer, no decision capture.
- [`skills/atomic-documentation/SKILL.md`](../../skills/atomic-documentation/SKILL.md) — diff-driven surface classifier and content generator. Two modes: **maintenance** (fires during ship verbs — flags stale/incomplete surfaces, never suggests new pages) and **authoring** (invoked by `/documentation` — full discovery, gap detection, content generation). Auto-fires on "doc this change", "what surfaces does this touch", "doc impact for this diff". Reads `## Documentation surfaces` table from project's Claude instructions (search order: [`claude.local.md`](../../claude.local.md)/[`CLAUDE.local.md`](../../CLAUDE.local.md) → [`CLAUDE.md`](../../CLAUDE.md)). Emits a fenced `yaml` block as structured handoff for callers. Per stale surface prompts: Yes (edit now) / Later (create follow-up) / Remind me (schedule reminder) / Skip. For new pages in authoring mode: generates full draft (ERD, flowchart, API table as appropriate). Emits `doc-skip:` trailers via `atomic-commit` when user skips with reason.
- [`skills/atomic-prose/SKILL.md`](../../skills/atomic-prose/SKILL.md) — voice and tone rules for human-readable developer documentation written to files. Governs [`README.md`](../../README.md), [`docs/guides/`](../../docs/guides), CHANGELOG narrative. Invoked when `atomic-documentation` routes to `atomic-prose` voice. Also auto-fires on documentation-editing phrases. Does not overlap with `atomic-documentation` (which classifies; this drafts).
- [`commands/documentation.md`](../../commands/documentation.md) — `/documentation` two-mode orchestrator. **Bootstrap mode** (no `## Documentation surfaces` table in CLAUDE.md): runs `atomic docs scan`, presents discovered markdown files as numbered list, user picks which to index, writes `## Documentation surfaces` table to committed CLAUDE.md. **Maintenance/authoring mode** (table present): compares diff against indexed surfaces, classifies each as stale/incomplete/missing, walks user through each with Yes/Later/Remind/Skip prompts. Ship verbs run the same check in maintenance mode automatically (between stage and signals). `atomic docs scan` runs discovery; `atomic docs stale` checks cache freshness.

## CLI code

None. The docs-meta domain is entirely Claude Code artifacts. No Go packages implement documentation routing or prose generation.

## Docs

- `.claude/docs/axioms.md` — 5 design axioms governing the system (still loads from `.claude/docs/`; the authoritative copy also lives at [`.claude/rules/authoring/axioms.md`](../../rules/authoring/axioms.md)). Axioms: (1) cohesion-bounded scope, (2) prefer memory when durable state is not yet necessary, (3) destructive ops require explicit confirm, (4) plain-text indexed selection, (5) skills auto-fire / commands explicit-only. Read before adding artifacts.
- `.claude/docs/agent-config.md` — Claude Code agent configuration reference (`.claude/docs/` copy; authoritative copy at [`.claude/rules/authoring/agent-config.md`](../../rules/authoring/agent-config.md)).
- `.claude/docs/claude-code-references.md` — URL index for official Claude Code documentation (`.claude/docs/` copy; authoritative copy at [`.claude/rules/authoring/claude-code-refs.md`](../../rules/authoring/claude-code-refs.md)).
- [`.claude/rules/authoring/`](../../rules/authoring) — path-scoped rule versions of the above reference docs. Four files: `axioms.md`, `agent-config.md`, `claude-code-refs.md`, `prompting.md`. These are repo-only (in [`.claude/rules/`](../../rules), never bundled). They auto-load when working on artifact files because they live under [`.claude/`](../..).
- [`docs/spec/documentation-skill-split.md`](../../docs/spec/documentation-skill-split.md) — contract for `atomic-documentation` + `/documentation` split. Boundary: skill classifies and routes; command orchestrates interactively.
- [`docs/spec/documentation-as-maintenance.md`](../../docs/spec/documentation-as-maintenance.md) — spec for the two-mode `/documentation` system: `atomic docs scan` + `atomic docs stale` binary subcommands, bootstrap flow, authoring mode, maintenance mode (ship verb integration), surface classification criteria.
- [`docs/design/documentation-as-maintenance.md`](../../docs/design/documentation-as-maintenance.md) — design doc: goals, non-goals, success criteria for replacing hardcoded surface-index updates with discovery-based doc maintenance.
- [`docs/spec/legible-output.md`](../../docs/spec/legible-output.md) — implementation contract for GitHub issue #113: new `atomic-legible` skill plus the presentation-ladder rung 3 in [`output-styles/atomic.md`](../../output-styles/atomic.md); two checkpoints (skill + ladder, then discovery surfaces) with the density-threshold and offer-first rules as success criteria.
- [`docs/design/legible-output.md`](../../docs/design/legible-output.md) — design doc for GitHub issue #113: problem statement (atomic style's terseness makes dense terminal replies hard to scan), the two-piece shape (ladder rung + `atomic-legible` skill), density-threshold and offer-first rules, re-presentation-not-re-derivation rule, file contract inherited from `atomic-visual-options`, and approaches considered (skill chosen over a slash command, auto-produce, or markdown-file rendering).
- [`docs/guides/getting-started.md`](../../docs/guides/getting-started.md) — post-install quickstart: output style activation, repo setup (`/atomic-setup` + `/refresh-wiki`), first task via `/atomic-help`, `/atomic-plan`, and the implement→ship lifecycle. User-facing entry point for new installs.
- [`docs/reference/concepts.md`](../../docs/reference/concepts.md) — key concepts and full-session walkthrough. Covers signals, plan→implement→ship flow, TDD, reminders, follow-ups. Updated with documentation maintenance workflow.
- [`docs/reference/commands.md`](../../docs/reference/commands.md) — command roster reference table. Updated to include `/documentation` description.
- [`docs/reference/output-style.md`](../../docs/reference/output-style.md) — output style reference.
- [`docs/reference/conventions.md`](../../docs/reference/conventions.md) — naming and structural conventions.

**Two-voice taxonomy (the core routing table):**

| Voice | Surface | Skill/artifact |
|-------|---------|---------------|
| Atomic TUI | Claude's chat replies | [`output-styles/atomic.md`](../../output-styles/atomic.md) |
| atomic-prose | [`README.md`](../../README.md), [`docs/guides/`](../../docs/guides), CHANGELOG narrative | [`skills/atomic-prose/SKILL.md`](../../skills/atomic-prose/SKILL.md) |

`atomic-documentation` routes to the correct voice; it does not produce the content itself. Spec/design and LLM-reference surfaces use terse technical prose directly — no dedicated skill routes to them.

**Spec append-mostly rule (all spec files):**

Every `docs/spec/<topic>.md` ends with `## Change log`. New entry per amendment: `### YYYY-MM-DD — <title>` + **What changed** + **Why** + (if behavior changed) **Superseded:** one-line prior contract. The only case where the body mutates without an additive section is a factual correction — prefixed `**Correction:**` in the log.

**Artifact additions checklist (from [`claude.local.md`](../../claude.local.md)):**

Adding a new artifact (command/agent/skill/output-style/rule) requires updating: (1) the artifact file, (2) [`CLAUDE.md`](../../CLAUDE.md), (3) [`CLAUDE.md`](../../CLAUDE.md), (4) [`README.md`](../../README.md), (5) `docs/spec/<topic>.md` if non-trivial, (6) cross-references in other artifacts, (7) bundle inclusion if new artifact kind, (8) signals refresh, (9) [`claude.local.md`](../../claude.local.md) if conventions change.

## Coupling

- **→ bundle**: `atomic-documentation` and `atomic-prose` skills ship in the bundle via `skills/atomic-*/` bundlespec rule. [`output-styles/atomic.md`](../../output-styles/atomic.md) ships via `output-styles/atomic*.md` rule. Changes require `make bundle`.
- **→ bundle**: `/documentation` command ships via [`commands/`](../../commands) render pipeline. Source at [`templates/commands/documentation.md`](../../templates/commands/documentation.md). Changes require `make render` then `make bundle`.
- **→ workflow**: ship verbs invoke `atomic-documentation` on staged diffs (between stage and signals refresh). If the skill's fenced YAML output contract changes, ship verb templates must be updated to parse the new format.
- **→ workflow**: the four-voice taxonomy applies to all documentation produced during the workflow lifecycle. `/atomic-plan` uses spec/design voice for design docs and specs. Ship verbs use LLM-reference voice for signals files.
- **→ signals**: signals files (`signals.md`, `signals/*.md`) use LLM-reference voice. `atomic-documentation` routes changes to these files to LLM-reference — no prose drafting, no atomic-prose.
- **→ doctor**: `atomic-documentation` reads `## Documentation surfaces` override from [`claude.local.md`](../../claude.local.md) / [`CLAUDE.md`](../../CLAUDE.md). Doctor check 4 (`refs`) validates that these files are present and correctly formed.
