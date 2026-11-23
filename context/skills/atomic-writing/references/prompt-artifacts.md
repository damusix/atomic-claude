# Prompt artifacts

For commands, agents, skills, rules, output styles, and `CLAUDE.md`, give the instruction and its constraint in plain language. Keep it tight without losing the contract. Rationale belongs only where it changes an edge decision.

Before: “Run `make render` before `make bundle`. This is important because the bundle step reads the rendered output, and if you skip it you may find that your changes do not appear, which can be confusing to debug later.”

After: “Run `make render` before `make bundle`. **Why:** bundle embeds what render wrote; reversing the order embeds stale output.”

Use a numbered list for straight steps and prose for a single condition. Use Mermaid for substantial branching, loops, verdict gates, and handoffs when it makes the logic easier to follow. Keep conditions and outcomes together in the diagram; place exact commands, reasons, and exceptions beside the relevant step. Do not duplicate the workflow in prose. Move longer examples and specialized guidance to on-demand references. Read `mermaid.md` before writing a Mermaid block.
