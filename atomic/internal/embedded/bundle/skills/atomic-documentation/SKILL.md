---
name: atomic-documentation
description: >
  Diff-driven documentation surface classifier. Given a diff (staged, branch, or
  range), identifies which documentation surfaces need updating and routes each to
  the correct voice. Auto-fires on "doc this change", "what surfaces does this touch",
  "doc impact for this diff", "what needs documenting". Also invoked by /documentation
  (full-scope mode) and by ship verbs (staged-diff mode, between stage and signals).
  Boundary: for raw prose drafting (README intro, guide narrative), atomic-prose owns.
  This skill owns diff-driven surface impact only.
---

This skill classifies a diff against the documentation surface taxonomy below, routes each affected surface to the correct voice, and emits a structured list of proposed edits. It does not draft prose; that is `atomic-prose`'s job. It identifies *what* needs changing and *where*, then hands off.

## Four voices, four surfaces

| Voice | Surface | Audience | Style rules |
|-------|---------|----------|-------------|
| **Atomic TUI** | Claude's chat replies | The human at the terminal, right now | Terse, fragments OK, drop articles. Governed by `output-styles/atomic.md`. Never appears in files. |
| **Atomic-prose** | `README.md`, `docs/guides/*`, CHANGELOG narrative | Humans skimming for what + why + how | Clear, specific, active-voice technical prose. No em dashes, no marketing, no AI-tell. Skill `atomic-prose` enforces. |
| **Spec/design** | `docs/spec/*`, `docs/design/*` | Future implementers + agents | Tables, Mermaid, terse bullets. Prose only where a contract needs sentences. Token-cost-aware. Append-mostly for specs. **Never** invokes `atomic-prose`. |
| **LLM-reference** | `CLAUDE.md`, `CLAUDE.md`, `.claude/project/*-signals.md`, `claude.local.md` | Future Claude sessions | Technical-imperative. Conventions, paths, dispatch contracts. No restating code, no tutorial, no narrative. Lean: every line earns its slot. |

## Surface routing

When this skill receives a diff, it classifies each changed entity against this routing table:

| Diff signal | Surface(s) | Voice |
|-------------|-----------|-------|
| New file in `commands/<name>.md` | `README.md` commands table + `CLAUDE.md` "Other commands" line + `CLAUDE.md` mirror | atomic-prose (README) + LLM-reference (CLAUDE.md) |
| New file in `agents/atomic-*.md` | `README.md` agents table + `CLAUDE.md` "Subagents available" entry | atomic-prose + LLM-reference |
| New file in `skills/atomic-*/SKILL.md` | `README.md` skills table + commands that invoke it | atomic-prose + LLM-reference |
| Public-API change in `atomic/cmd/atomic/main.go` (new top-level flag, new subcommand) | `docs/reference/commands.md` + matching `docs/spec/<topic>.md` change-log | spec/design |
| New key in `atomic/internal/config/config.go` | `docs/spec/atomic-state-and-config.md` change-log + `config.resolved.md` (auto-rendered, no manual edit) | spec/design |
| New check in `atomic/internal/doctor/checks_*.go` | `docs/spec/atomic-doctor.md` change-log | spec/design |
| Behavior-changing edit to existing `docs/spec/<topic>.md` body | Same file's `## Change log` section | spec/design |
| Surface listed in user's `claude.local.md` "documentation surfaces" override | Surface as listed | as declared |
| Pure refactor / internal rename with no public surface change | (no surface) | n/a — skill returns "no doc impact" |

## Override format for non-atomic repos

Repos that don't use atomic conventions can declare their own surface routing. The skill reads the first file in the following search order that contains a `## Documentation surfaces` heading:

1. `claude.local.md` or `CLAUDE.local.md` (treated as a pair — check whichever exists; accommodates case-sensitive and case-insensitive filesystems)
2. `claude.md` or `CLAUDE.md` (same pair semantics)

First file with the heading wins; remaining files are ignored.

The `## Documentation surfaces` section must contain a markdown table with columns `Diff signal`, `Surface`, and `Voice`:

```markdown
## Documentation surfaces

| Diff signal | Surface | Voice |
|-------------|---------|-------|
| New file in `src/api/routes/*.ts` | `docs/api.md` | atomic-prose |
| Public function added to `pkg/*/exports.go` | `docs/reference.md` | spec-design |
```

User-declared overrides win on collision with built-in defaults. If no override section exists in any of the searched files, the skill emits atomic-defaults-only routing, which returns empty surfaces on non-atomic repos — clean degradation with no false positives.

To generate a starter template for a new repo, run `/documentation --print-template`. This emits the table skeleton to stdout for pasting into the appropriate file.

## Output contract

After completing analysis, emit as the **final block** of the response a fenced `yaml` block in the shape below. Callers (ship verbs and `/documentation`) parse the **last** `yaml` or `yml` fenced block in the model output. If no yaml block is present, callers treat the response as "no surfaces affected."

```yaml
surfaces:
  - path: README.md
    voice: atomic-prose
    reason: new file commands/foo.md
    suggested_change: |
      Add row to commands table:
      | `/foo` | Description |
  - path: CLAUDE.md
    voice: llm-reference
    reason: new file commands/foo.md
    suggested_change: |
      Append to "Other commands" line: `/foo` (<one-line behavior>)
```

Voice values: `atomic-prose | spec-design | llm-reference`.

Parser contract (caller side):

1. Search model output for the last fenced code block tagged `yaml` or `yml` (alias; both accepted).
2. If found, parse as YAML. On parse error, fall back to "no surfaces."
3. If no fenced `yaml`/`yml` block is present, treat as "no surfaces."
4. If parsed YAML lacks a `surfaces` key or `surfaces` is not a list, treat as "no surfaces."
5. Surfaces with unknown `voice` values are logged and skipped; do not abort.
6. Surface entries missing required fields (`path`, `voice`) are logged and skipped; do not abort.
7. Empty `surfaces: []` is valid and means "explicitly nothing to update."

## Why structured handoff here

This is the only skill in the atomic system that emits a fenced YAML block for callers to parse. Other skills (`atomic-signals`, `atomic-commit`) emit free text that callers act on conversationally. The structured handoff here is justified by one concrete need: per-surface accept/reject prompts in ship verbs require a clear item list — the caller cannot reliably extract a structured list from free-text output. The YAML block provides that list without ambiguity.

Do not apply this pattern to other skills without a similarly concrete need for machine-readable per-item output. When in doubt, emit free text and let the caller act conversationally.
