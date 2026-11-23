---
name: atomic-writing
description: >
  Write or edit repo documentation, wikis, plans, specs, and prompt artifacts in
  one plain, human-readable voice. Use for README, guides, reference pages,
  designs, research, commands, agents, skills, rules, and output styles.
---

Write for the person who owns the decision and the agent following the same contract. Be plain, specific, and direct. Choose length and form by what the reader needs, not a compression quota. Show branches, loops, and handoffs when a diagram makes them easier to follow; do not duplicate a workflow in prose.

## Read for the task

Read `references/style.md` for shared voice. Then read the applicable reference:

| Task | Reference |
|---|---|
| README, guide, reference, research, or wiki | `references/documentation.md` |
| Plan, spec, or design | `references/plans-specs.md` |
| Command, agent, skill, rule, or other prompt artifact | `references/prompt-artifacts.md` |
| Any Mermaid block in docs or prompt artifacts | `references/mermaid.md` |

For a lookup page, also read `references/exemplar-reference-page.md`; for a CLI tool, daemon, or protocol, read `references/exemplar-tool-page.md`.

## Boundaries

- This skill governs file contents, not terminal replies, which follow `output-styles/atomic.md`.
- Preserve frontmatter, fenced code, command examples, paths, identifiers, error strings, and Mermaid node labels. Do not restyle a contract.
- Do not rewrite unrelated sections for voice. Follow the existing CHANGELOG tone; source comments follow `CLAUDE.md`.
