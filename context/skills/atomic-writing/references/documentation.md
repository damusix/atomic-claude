# Documentation and wiki pages

Match length to the job: guides teach, references answer lookups, research records evidence and reasons, and wikis orient readers to an area. Explain what a wiki subject does and how it works before source details. Use tables and lists for lookup, and enough prose for the reasoning they cannot carry.

## Reading order

Unless a surface defines its own structure, answer these questions in order where relevant: what is it and why use it, how does it work, where is it, what can go wrong, and what else does it touch? Before the second `##` heading, show what it is, when to use it, and the thing itself: a transcript, invocation, or model. Explain up front when its name differs from its path, command, or output.

Group paths and symbols by responsibility, not file type. Give headings specific names rather than “Notes” or “Other.” Show the happy path cleanly, then collect conditions and failures together. Use `exemplar-reference-page.md` for lookup pages and `exemplar-tool-page.md` for CLI tools, daemons, and protocols.

## Shapes and evidence

Use paragraphs for reasoning, lists for enumerable facts, tables for comparisons, and diagrams for relationships. Read `mermaid.md` before writing a Mermaid block. In a `docs/` draft, these forms are required:

| Draft | Form |
|---|---|
| Three or more sentences narrating one process with “then,” “after,” or “once” | Flowchart, or sequence diagram for messages between actors |
| Three or more named states with transition rules | State diagram |
| Two or more things compared on three or more shared attributes | Table |

Ground explanations in source paths, symbols, examples, transcripts, or error strings where they support the claim. Keep documented contracts and exceptions accurate; do not invent evidence.

## Rule 17: Wiki counts

A wiki page never counts things in the repo, including in labels and table cells. Omit LOC totals, file counts, percentages, language tables, and counts of verbs, artifacts, domains, checks, resolvers, steps, lenses, icons, registry entries, tests, or sections. Name the things or let the list stand alone. Versions and documented contract values are not counts: `Go 1.25`, a pinned dependency, a 3s timeout, a 16 MiB buffer, a 500-file gate, and a port stay.
