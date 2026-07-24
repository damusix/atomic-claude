---
name: {{BUCKET}}-management
description: >
  Fires when creating or editing docs under {{BUCKET}}/ — new topic files,
  router subtrees, or edits to existing entries. Use when the user asks to
  add a note, doc, or topic to {{BUCKET}}, or references a file inside it.
---

## Purpose

{{PURPOSE}}

## Doc shape

Each topic is one file: `<slug>.md`. A topic that outgrows a single file
becomes a router — keep `<slug>.md` as the summary and break subtopics into
a sibling `<slug>/` directory:

    atomic wiki bucket doc {{BUCKET}} <slug> --router

## Frontmatter contract

Six recognized keys. `created` is stamped by the scaffold; the rest are
written by whoever authors the doc.

| Key | Writer |
|-----|--------|
| `title` | author |
| `type` | author |
| `description` | author |
| `tags` | author |
| `status` | author |
| `created` | code (scaffold) |

## {{BUCKET}}-specific rules

<!-- Add this bucket's own authoring rules here. -->
