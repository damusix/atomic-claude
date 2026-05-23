# docs-meta

The documentation system itself: four-voice taxonomy, surface routing, prose drafting, output style, and the axiom set that governs design decisions.

## Artifacts

- `output-styles/atomic.md` — governs Claude's TUI reply style. Terse, telegraphic, fragments OK. Applied to main agent only (subagents do not receive output style sections).
- `skills/atomic-documentation/SKILL.md` — diff-driven surface taxonomy classifier. Auto-fires on "doc this change", "doc impact for this diff", "what surfaces does this touch". Classifies changed entities against the four-voice routing table. Emits a fenced `yaml` block as structured handoff for callers. Reads `## Documentation surfaces` override section from `claude.local.md` / `CLAUDE.md` for non-atomic repos. Invoked by `/documentation` and by ship verbs (staged-diff mode). Boundary: surface routing only — prose drafting delegated to `atomic-prose`.
- `skills/atomic-prose/SKILL.md` — voice and tone rules for human-readable developer documentation written to files. Governs `README.md`, `docs/guides/`, CHANGELOG narrative. Invoked when `atomic-documentation` routes to `atomic-prose` voice. Also auto-fires on documentation-editing phrases. Does not overlap with `atomic-documentation` (which classifies; this drafts).
- `commands/documentation.md` — `/documentation [--dry-run] [--print-template] [<range>]` thin orchestrator. Resolves git diff range (default `merge-base(HEAD, main)..HEAD`), invokes `atomic-documentation` skill, parses fenced YAML output, walks surfaces one at a time with `[e]dit / [s]kip / [c]ontinue` prompts. `--dry-run` prints proposed surface list and exits. `--print-template` prints override-table skeleton for non-atomic repos.
- `commands/atomic-compress.md` — `/atomic-compress <file>` compresses prose file into atomic style.

## CLI code

None. The docs-meta domain is entirely Claude Code artifacts. No Go packages implement documentation routing or prose generation.

## Docs

- `.claude/docs/axioms.md` — 5 design axioms governing the system. Load-bearing for any new command/agent/skill decision. Axioms: (1) cohesion-bounded scope, (2) memory over config, (3) destructive ops require explicit confirm, (4) plain-text indexed selection, (5) skills auto-fire / commands explicit-only. Read before adding artifacts.
- `.claude/docs/agent-config.md` — Claude Code agent configuration reference: frontmatter schema, tool restriction, subagent context isolation, memory system, output style mechanics.
- `.claude/docs/claude-code-references.md` — URL index for official Claude Code documentation. Fetch via WebFetch when verifying semantics — these are upstream sources of truth.
- `docs/spec/documentation-skill-split.md` — contract for `atomic-documentation` + `/documentation` split. Boundary: skill classifies and routes; command orchestrates interactively.
- `docs/reference/output-style.md` — output style reference.
- `docs/reference/conventions.md` — naming and structural conventions.

**Four-voice taxonomy (the core routing table):**

| Voice | Surface | Skill/artifact |
|-------|---------|---------------|
| Atomic TUI | Claude's chat replies | `output-styles/atomic.md` |
| atomic-prose | `README.md`, `docs/guides/`, CHANGELOG narrative | `skills/atomic-prose/SKILL.md` |
| Spec/design | `docs/spec/`, `docs/design/` | Tables/bullets, no dedicated skill |
| LLM-reference | `CLAUDE.md`, `claude.local.md`, `*-signals.md` | No dedicated skill |

`atomic-documentation` routes to the correct voice; it does not produce the content itself.

**Spec append-mostly rule (all spec files):**

Every `docs/spec/<topic>.md` ends with `## Change log`. New entry per amendment: `### YYYY-MM-DD — <title>` + **What changed** + **Why** + (if behavior changed) **Superseded:** one-line prior contract. The only case where the body mutates without an additive section is a factual correction — prefixed `**Correction:**` in the log.

**Artifact additions checklist (from `claude.local.md`):**

Adding a new artifact (command/agent/skill/output-style/rule) requires updating: (1) the artifact file, (2) `CLAUDE.md`, (3) `CLAUDE.md`, (4) `README.md`, (5) `docs/spec/<topic>.md` if non-trivial, (6) cross-references in other artifacts, (7) bundle inclusion if new artifact kind, (8) signals refresh, (9) `claude.local.md` if conventions change.

## Coupling

- **→ bundle**: `atomic-documentation` and `atomic-prose` skills ship in the bundle via `skills/atomic-*/` bundlespec rule. `output-styles/atomic.md` ships via `output-styles/atomic*.md` rule. Changes require `make bundle`.
- **→ bundle**: `/documentation` command ships via `commands/` render pipeline. Source at `templates/commands/documentation.md`. Changes require `make render` then `make bundle`.
- **→ workflow**: ship verbs invoke `atomic-documentation` on staged diffs (between stage and signals refresh). If the skill's fenced YAML output contract changes, ship verb templates must be updated to parse the new format.
- **→ workflow**: the four-voice taxonomy applies to all documentation produced during the workflow lifecycle. `/atomic-plan` uses spec/design voice for design docs and specs. Ship verbs use LLM-reference voice for signals files.
- **→ signals**: signals files (`signals.md`, `signals/*.md`) use LLM-reference voice. `atomic-documentation` routes changes to these files to LLM-reference — no prose drafting, no atomic-prose.
- **→ doctor**: `atomic-documentation` reads `## Documentation surfaces` override from `claude.local.md` / `CLAUDE.md`. Doctor check 4 (`refs`) validates that these files are present and correctly formed.
