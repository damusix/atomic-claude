---
name: {{BUCKET}}-management
description: >
  Fires when creating or editing docs under {{BUCKET}}/: new items, new
  stages, or edits to existing entries. Use when the user asks to add a
  note, doc, or item to {{BUCKET}}, or references a file inside it.
---

## Purpose

{{PURPOSE}}

## Doc shape

Each item is a folder: `<slug>/index.md` describes it, and its work lives in
numbered stage folders, starting with `01_raw/` for collected source
material. Later stages (`02_findings/`, `10_publish/`) build on the one
before and list in numeric order. Scaffold a new item with:

    atomic wiki bucket doc {{BUCKET}} <slug>

Every folder carries an `index.md`. Atomic rebuilds the `<bucket-docs>`
listing in each one; write only outside that region.

## Stages

<!-- List each stage folder used by items in {{BUCKET}}/, starting with 01_raw/, and what that stage's output contains. -->

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
