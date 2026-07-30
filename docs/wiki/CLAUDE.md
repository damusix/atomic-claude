---
type: Steering
description: Steering context for the docs/wiki/ project-wiki directory; loaded by Claude when any file under docs/wiki/ is read.
---

# docs/wiki/ steering

This directory is the repo-local project wiki for atomic-claude. It is written and maintained by `atomic-wiki-inferrer` and `atomic signals scan`. Do not edit files here by hand except to correct factual errors.

## What lives here

| File | Role |
|------|------|
| `index.md` | Router: framework signals, domain map, cross-cutting notes. This is the `@`-ref'd entry point (`@docs/wiki/index.md` in `claude.local.md`). |
| `scan.md` | Raw deterministic snapshot written by `atomic signals scan`. Too large for context; read on demand by the inferrer only — never `@`-ref'd. |
| `CLAUDE.md` | This file. Steering / nested-memory context for sessions that read any file under `docs/wiki/`. |
| `<domain>.md` | One per feature domain (signals, bundle, doctor, workflow, config, docs-meta, wiki, code-intel, serve). Each carries `type: Domain` frontmatter. |

## Key cross-references

- Router: [`docs/wiki/index.md`](index.md)
- Scan dump: [`docs/wiki/scan.md`](scan.md)
- Domain files: [`signals.md`](signals.md), [`bundle.md`](bundle.md), [`doctor.md`](doctor.md), [`workflow.md`](workflow.md), [`config.md`](config.md), [`docs-meta.md`](docs-meta.md), [`wiki.md`](wiki.md), [`code-intel.md`](code-intel.md), [`serve.md`](serve.md)

## How to use

Read `index.md` for the high-level framework + domain map. Read a specific `<domain>.md` when working on that feature. Do not read `scan.md` in context — it is thousands of lines.
