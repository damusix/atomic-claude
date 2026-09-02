---
id: wiki-writer-stray-content-tag
title: atomic-wiki-writer leaks a stray </content> tag at EOF on long pages
created: "2026-09-02"
origin: |
    found reviewing a signals refresh
kind: finding
severity: nit
review_by: "2026-11-01"
status: open
---


`atomic-wiki-writer` has twice ended a page with a bare `</content>` line:

- `docs/wiki/docs-meta.md:155`, shipped in 99ed2017 (PR #236) and sitting on `next`
- `docs/wiki/repl.md:170`, produced by the refresh on this branch

Both were stripped by hand. The page is written straight to disk by the writer's own
Write call, so a leaked tag is committed unless a human reads the diff, and it renders
as literal text on the VitePress site.

Not a harness defect, and not an unfixable model quirk. No `<content>` tag exists
anywhere in `context/`, the installed artifacts, or the Go code — nothing emits it
literally. What invites it is the dispatch prompt's own shape: the template wraps
every section in XML (`<source_paths>`, `<steering>`, `<instructions>`,
`<output_format>`) and its last instruction reads "Output only the file content"
(`context/skills/atomic-wiki/references/repo.md:85`, and the same line at
`realm.md:68`). A model handed tag-delimited input and told to output "the file
content" has an obvious reason to close a `content` block on the way out.

The pipeline itself is consistent, so there is no contract bug to fix. The writer writes
its own page (`repo.md` Step 5, "After each sub-agent writes its domain file"; the agent
holds Write and is handed "where the output goes"), and concerns travel in the reply
under a `## Concerns (do not include in domain file)` heading. The "Output only the file
content" line sits directly above the `<output_format>` block defining the page schema —
it forbids padding the page with process narration, and says nothing about a return
channel.

What is left is a model artifact. The dispatch prompt is heavily XML-sectioned, the
writer emits a spurious closing tag at the end of a long page, and because it writes the
file itself the tag reaches disk rather than dying in a reply. No instruction asks for
it, so no rewording reliably prevents it.

Fix shape: scrub it where the pipeline already owns the file — strip a trailing lone
closing tag after the writer returns — or add a doctor check over `docs/wiki/*.md` for
stray closing tags, so the next one fails loudly instead of shipping. The doctor check is
the better of the two: it catches the class rather than the one tag seen so far, and
`checks_signals.go` already walks these files.
