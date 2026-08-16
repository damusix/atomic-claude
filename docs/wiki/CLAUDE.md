---
type: Steering
description: Steering context for the docs/wiki/ project-wiki directory; loaded by Claude when any file under docs/wiki/ is read.
---

# docs/wiki/ steering

This directory is the repo-local project wiki for atomic-claude, written and maintained by `atomic-wiki-inferrer` and `atomic signals scan`. Do not edit these files by hand except to correct a factual error.

## What lives here

| File | Role | When it loads |
|------|------|---------------|
| [`index.md`](index.md) | Router: framework signals and the domain map. `@`-ref'd from [`claude.local.md`](../../claude.local.md). | Every turn |
| `<domain>.md` | One page per feature domain, carrying `type: Domain`. | On demand, when a task reaches that domain |
| [`scan.md`](scan.md) | Deterministic snapshot written by `atomic signals scan`. Runs to thousands of lines. | Never in context; the inferrer reads it on demand |
| `CLAUDE.md` | This file. Nested-memory steering for anything read under `docs/wiki/`. | Whenever a file here is read |

Eleven domains: signals, bundle, doctor, workflow, config, docs-meta, wiki, code-intel, serve, bus, repl.

## Writing a page here

Page shape is a contract, not a convention. Every domain page carries these five sections, in this order:

```
What it does  ->  How it works  ->  Where it lives  ->  Constraints  ->  Coupling
what is this      every shape       one table,          what breaks     what else
and why do        the domain has,   grouped by          if you get      this touches
I care            drawn             responsibility      it wrong
```

There is no sixth section. A fact that fits none of the five belongs inside one of them or nowhere, because a catch-all heading is where facts start landing in discovery order.

There is no cap on diagrams. A domain with a pipeline, a lifecycle, and a request path owes three, each with its own claim and its own sub-heading. Under-drawing is the usual failure: a shape left in prose is a picture the reader never got.

The full contract lives in [`skills/atomic-wiki/references/repo.md`](../../skills/atomic-wiki/references/repo.md) (page shape, reviewer checklist) and [`skills/atomic-writing/SKILL.md`](../../skills/atomic-writing/SKILL.md) (voice, and the rule that every diagram caption states a claim rather than naming its subject).

Write repo-root-relative paths in backticks and let `atomic signals linkify` render them. It skips this file, so any link here is written by hand.
