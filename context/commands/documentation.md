---
description: Discover documentation surfaces, preview the impact of a diff, or walk and update affected pages.
---

Use `atomic-documentation` for the table template, classification, and content generation. Use `atomic-writing` for edits.

## Read the request


| Intent | Action |
|---|---|
| Show the table template | Print the skill's template and stop without scanning. |
| Preview documentation changes | Discover and classify without writing files, offering edits, or staging. |
| Discover or re-index docs | Offer to index unlisted pages, then stop. Keep existing rows. |
| Run a documentation pass | Discover and offer to index unlisted pages. If there was no table, stop after bootstrap; otherwise continue to classification. |

## Discover surfaces


Find the table using the skill's steering lookup. Run `atomic docs scan` and read `<state-root>/project/doc-surfaces.md`; resolve `state_root` with `atomic where --json` (older installs use `.claude`). For previews or if scanning fails, use `find` to list Markdown under `docs/`, `doc/`, `documentation/`, `wiki/`, `ADR/`, `adr/`, and `decisions/`, plus `README.md` files up to three levels deep. Exclude `.git` and `node_modules`.

Report if no docs exist. List unindexed paths and headings. In preview, stop here if there is no table. Otherwise, when indexing, number the paths and accept `all`, `none`, or indices and ranges. Suggest `Covers` from H1 and the first three H2s. Add only approved rows with `Voice: atomic-writing` to the owning steering file, using the skill's lookup and write rules. Stage only that edit; report if nothing was indexed.

## Classify and act


Use the requested Git range. Otherwise resolve the intended base through the shipping reference in `atomic-git-discipline` and diff from its merge base to `HEAD`. Classify affected surfaces with the skill. Skip stale and incomplete checks on an empty diff. For missing pages, check `docs/wiki/index.md` for domains with at least five source files and no corresponding surface within two directory levels.

Outside preview, when the changed symbols need broader investigation, ask a read-only scout on an inexpensive model to search their references and return a compact symbol-to-surface map. Use the diff and `Covers` alone if a scout is unavailable.

For a preview, report the classifications and reasons, then stop. Otherwise show each path, status, and reason, and ask for one action per surface:

| Status | Choice | Action |
|---|---|---|
| Stale or incomplete | Yes (`y`) | Read the page and diff. Update the affected content, diagrams, and examples, then stage the page. |
| Stale or incomplete | Later (`l`) | Record a follow-up through the sink below. Use severity `nit`, kind `finding`, and origin `/documentation`. |
| Missing | New (`n`) | Write and stage the page. Offer to index it and note where it should be linked. |
| Any | Skip (`s`) | Leave no record. |

For each offered follow-up, show the sink's redacted preview before the user chooses Later. Later approves that exact record; do not ask twice.

{{ template "followup-sink" . }}

Summarize updated, deferred, skipped, and created paths. Do not commit.
