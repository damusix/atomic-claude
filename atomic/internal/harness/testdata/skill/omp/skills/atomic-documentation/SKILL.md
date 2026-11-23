---
name: atomic-documentation
description: Identify which docs a change affects and update them. Use for standalone documentation requests and implementation maintenance.
---
## Find affected docs


Read the changed source and the current docs. Match changed paths, symbols, and behavior against the documentation surfaces table and check related wiki pages, rules, designs, specs, business docs, guides, README, and `CLAUDE.md`. The table is a starting point; missing rows do not mean nothing is affected.

Use the first `## Documentation surfaces` table in a project-local override, then root `AGENTS.md` or `CLAUDE.md`. Edit the file that owns the table; for a new table, use the active steering mode in `atomic-wiki/references/repo-output.md`. Never create a loader that would shadow existing instructions. Use `Covers` to match changes and honor `**Ownership:**` so sibling pages link to the owner instead of repeating facts.

## Update the affected content


Use `atomic-writing`. Update stale claims, diagrams, examples, and links against the source. For each affected path, record the reason and the action taken. Distinguish stale content, incomplete content, and a missing page.

- **Standalone:** Follow `/documentation` for discovery, preview, indexing, and per-page choices. Preview is read-only. Do not edit or stage pages before the user selects them.
- **Implementation or ad-hoc shipping:** Use the scratch scan comparison and task diff. Make required in-scope updates before review without per-page prompts or automatic follow-ups. Add a page only when the task needs it and no existing page owns the material. Leave realm wikis and capture buckets alone. Report blocked updates and keep the accepted scan unchanged until required docs pass review. The caller owns review, baseline acceptance, and staging.

When paths or ownership change, update affected cards and links using `atomic-wiki/references/repo-output.md`. On a harness-aware CLI, project changed cards with `atomic wiki init --scope repo` before taking the final scan. Include canonical cards and repo-owned projections in review, not generated copies for external state roots.

## Documentation surfaces template


```markdown
## Documentation surfaces

| Path | Covers | Voice |
|---|---|---|
| `docs/architecture/payments.md` | billing, webhooks, payment states | atomic-writing |
```
