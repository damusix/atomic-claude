---
name: atomic-documentation
description: >
  Diff-driven documentation surface classifier. Given a diff (staged, branch, or
  range), reads the project's indexed ## Documentation surfaces table from CLAUDE
  instructions, matches the diff against it, and emits a structured list of
  proposed edits. Two modes: maintenance (commit flow — stale/incomplete only,
  never suggests new pages) and authoring (/documentation explicit — full
  discovery, gap detection, content generation). Auto-fires on "doc this change",
  "what surfaces does this touch", "doc impact for this diff", "what needs
  documenting". Also invoked by /documentation (authoring mode) and by ship verbs
  (maintenance mode, between stage and signals).
  Boundary: for raw prose drafting (README intro, guide narrative), atomic-prose owns.
  This skill owns diff-driven surface impact and content generation for stale/incomplete docs.
---

This skill reads the project's indexed documentation surfaces, matches a diff against them, and either flags stale/incomplete docs (maintenance mode) or runs the full discovery + generation pipeline (authoring mode). It emits a structured YAML block listing affected surfaces. When the user picks "Yes", it opens the file and makes the edit.

## Four voices, four surfaces

| Voice | Surface | Audience | Style rules |
|-------|---------|----------|-------------|
| **Atomic TUI** | Claude's chat replies | The human at the terminal, right now | Terse, fragments OK, drop articles. Governed by `output-styles/atomic.md`. Never appears in files. |
| **Atomic-prose** | `README.md`, `docs/guides/*`, CHANGELOG narrative | Humans skimming for what + why + how | Clear, specific, active-voice technical prose. No em dashes, no marketing, no AI-tell. Skill `atomic-prose` enforces. |
| **Spec/design** | `docs/spec/*`, `docs/design/*` | Future implementers + agents | Tables, Mermaid, terse bullets. Prose only where a contract needs sentences. Token-cost-aware. Append-mostly for specs. **Never** invokes `atomic-prose`. |
| **LLM-reference** | `CLAUDE.md`, `.claude/project/*-signals.md`, `claude.local.md` | Future Claude sessions | Technical-imperative. Conventions, paths, dispatch contracts. No restating code, no tutorial, no narrative. Lean: every line earns its slot. |

## How surface routing works

The skill reads the `## Documentation surfaces` table from the project's CLAUDE instructions. Search order: `claude.local.md` or `CLAUDE.local.md` (treated as a pair) → `CLAUDE.md`. First file containing the heading wins.

Expected table format:

```markdown
## Documentation surfaces

| Path | Covers | Voice |
|------|--------|-------|
| `docs/architecture/payments.md` | billing, webhooks, Stripe | atomic-prose |
| `docs/models/README.md` | data model, ERD, migrations | atomic-prose |
| `docs/api/endpoints.md` | REST API, auth, rate limits | atomic-prose |
```

The `Covers` column is the matching key. The skill compares diff file paths and changed symbols against these terms to classify each surface as `stale`, `incomplete`, or `missing`. If no `## Documentation surfaces` section exists in any of the searched files, the skill returns empty surfaces — clean degradation with no false positives.

## Two modes

### Maintenance mode (commit flow — invoked by ship verbs)

Fires automatically during ship verbs after staging, between stage and signals refresh. Reads the indexed surfaces table, matches the staged diff against it, flags affected surfaces. **Never emits `impact_type: missing`** — suggesting new pages during a commit is outside the user's mental context.

Per stale or incomplete surface, prompt the user:

```
docs/models/README.md may be stale.
reason: migration adds refund_status column to orders table
action: update orders entity in ERD, add field to orders table

  [y] Yes — update now
  [l] Later — create a follow-up (atomic followups add)
  [r] Remind me — schedule a reminder (/remind-me flow)
  [s] Skip — no action, no record
```

**Yes** → skill opens the file, reads current content, reads the diff, makes the targeted edit (see Content generation below), stages the file.

**Later** → runs `atomic followups add` with `severity: nit`, `origin: /commit-only doc-impact`, title derived from surface path and diff summary.

**Remind me** → natural-language time inference same as `/remind-me`. Creates a reminder.

**Skip** → no action, no record.

### Authoring mode (invoked by `/documentation`)

Full pipeline. User opted in to spending time on docs.

1. Run `atomic docs scan` (deterministic heading extraction; falls back to Bash filesystem walk if binary absent). Lists discovered doc files with H1 + first 3 H2s.
2. Compare scan against the indexed surfaces table. Surface any unindexed docs: "Found N doc files not in your surfaces table. Add them?"
3. If a diff range is provided, match it against indexed surfaces. Classify as stale, incomplete, or missing.
4. For domains identified by signals with no doc surface and 5+ files: suggest creating a new page (heuristic gate — small utility modules don't need a page).
5. Walk surfaces with the user, per surface:
   - **Stale/incomplete** → same Yes/Later/Remind/Skip as maintenance mode. "Yes" generates the full content immediately.
   - **Missing** → `[n] New` generates a full page draft (ERDs, flowcharts, prose, tables as appropriate), writes the file, offers to add it to the surfaces table in CLAUDE.md, notes where to link it (index, sidebar, README).
6. Stage edited/created files.
7. Emit summary: edited / deferred / skipped / created.

## Content generation

When the user picks "Yes" (maintenance) or "New" (authoring), the skill opens the file and makes the edit. The user approves — they do not write.

### For stale/incomplete surfaces

Read the current doc file, read the diff, generate a targeted edit:

- **Stale ERD** (migration added a column): find the Mermaid ERD block, add the new field to the entity, find the field table, add the row with type + description.
- **Stale flow diagram** (new step added to a process): find the Mermaid flowchart, insert the new step at the correct position, update any prose description of the flow.
- **Incomplete API reference** (new endpoint added): find the section for the relevant resource, add a row to the endpoint table with method, path, auth, request/response shapes, add an example matching the existing format.

Stage the file after editing.

### For new pages (authoring mode only)

Generate a full draft using the doc type that fits the content:

- **New module with data models** → domain guide: intro sentence, Mermaid ERD, field tables per entity, business rules, edge cases.
- **New API routes** → API reference: endpoint table (method, path, auth, request, response), one example per endpoint.
- **New multi-step process** → flow doc: Mermaid flowchart (happy path → error/edge paths), step descriptions.
- **Architectural decision** → ADR: context, decision, consequences. Short. Point-in-time.

After generating: write the file, offer to add it to the `## Documentation surfaces` table in CLAUDE.md, note where to link it.

### Mermaid generation

Generate full Mermaid syntax — not descriptions. Pick the diagram type:

| Content | Diagram type | When |
|---------|-------------|------|
| Entities and relationships | `erDiagram` | Data model pages, domain guides |
| Multi-step process | `flowchart` | Architecture pages, flow docs |
| Actor interactions | `sequenceDiagram` | Integration docs, API flows |
| State transitions | `stateDiagram-v2` | Lifecycle docs (order states, auth flows) |

Every Mermaid block gets a one-sentence caption above it. When updating an existing diagram, preserve the existing style (orientation, naming, colors) and modify only the changed elements.

### Output quality standards

Generated docs are for humans navigating a codebase. Apply these standards:

- **Short intro sentence** before any detail — tells the reader what the page is about.
- **Tables for structured comparisons** — multiple items with shared attributes always use a table, not a prose list.
- **Categorized sections** — a reader scanning headings should know what each section covers without reading it.
- **Plain language** — "this handles saving and loading user data from the database," not "provides an abstraction layer over the persistence subsystem."
- **Code blocks only for things the user types or sees** — CLI commands, config snippets, API calls. Not for describing behavior.

Anti-patterns to avoid:
- Restating the code. Document *why*, not *what*.
- Wall of prose without a heading, table, diagram, or code block every ~15 lines.
- LLM-tell: "This comprehensive guide provides an in-depth overview of..." Write direct and specific.
- Orphan docs: note where generated pages should be linked so they're discoverable.
- Stale examples: when updating a doc because the API changed, update all examples too.

## Output contract

After completing analysis, emit as the **final block** of the response a fenced `yaml` block in the shape below. Callers (ship verbs and `/documentation`) parse the **last** `yaml` or `yml` fenced block in the model output. If no yaml block is present, callers treat the response as "no surfaces affected."

```yaml
surfaces:
  - path: docs/models/README.md
    voice: atomic-prose
    impact_type: stale
    reason: migration adds refund_status to orders table
    suggested_change: |
      Add refund_status field to Order entity in ERD.
      Add row to orders field table: refund_status | string | pending/approved/rejected
  - path: docs/api/endpoints.md
    voice: atomic-prose
    impact_type: incomplete
    reason: new POST /payments/refund endpoint added
    suggested_change: |
      Add row to payments endpoint table with method, path, auth, request/response.
```

Voice values: `atomic-prose | spec-design | llm-reference`.

`impact_type` values: `stale | incomplete | missing`. Maintenance mode never emits `missing`.

Parser contract (caller side):

1. Search model output for the last fenced code block tagged `yaml` or `yml` (alias; both accepted).
2. If found, parse as YAML. On parse error, fall back to "no surfaces."
3. If no fenced `yaml`/`yml` block is present, treat as "no surfaces."
4. If parsed YAML lacks a `surfaces` key or `surfaces` is not a list, treat as "no surfaces."
5. Surfaces with unknown `voice` values are logged and skipped; do not abort.
6. Surface entries missing required fields (`path`, `voice`) are logged and skipped; do not abort.
7. Surface entries with unknown fields (e.g. `impact_type`, `reason`, `suggested_change`) are accepted — only `path` and `voice` are required.
8. Empty `surfaces: []` is valid and means "explicitly nothing to update."

## Why structured handoff here

This is the only skill in the atomic system that emits a fenced YAML block for callers to parse. Other skills (`atomic-signals`, `atomic-commit`) emit free text that callers act on conversationally. The structured handoff here is justified by one concrete need: per-surface accept/reject prompts in ship verbs require a clear item list — the caller cannot reliably extract a structured list from free-text output. The YAML block provides that list without ambiguity.

Do not apply this pattern to other skills without a similarly concrete need for machine-readable per-item output. When in doubt, emit free text and let the caller act conversationally.
