---
id: code-web-explorer
title: Serve an htmx web UI to explore the atomic code index (DB schema + code graph)
created: "2026-06-08"
origin: |
    user request after SQL support shipped
kind: plan
review_by: "2026-08-07"
status: open
file: atomic/internal/codeintel/engine/engine.go
---

Serve a local htmx web UI that lets the user explore an `atomic code` index in a browser — a visual companion to the CLI query verbs. Primary use case (driven by the new SQL support): browse the **database schema graph** — click a table to see its columns, constraints, indexes, the foreign keys it references, and (via the new `writes` edges) which procedures/functions read vs write it. Generalizes to any indexed language (functions, callers/callees, impact radius, file map).

Shape:
- New `atomic code serve [--port N]` verb (or `atomic code web`) that boots a small Go HTTP server over the existing SQLite index (`.claude/.atomic-index/atomic.db`).
- Server-rendered HTML + **htmx** for interactivity (no SPA build step; fits the no-JS-toolchain ethos). Fragments returned from endpoints map to the existing engine query layer: search, node detail, callers, callees, impact, explore, files. The data + query functions already exist (`internal/codeintel/engine` + `graph` + `search` + `codectx`); this is a presentation layer, not new analysis.
- Views: (1) search/filter by kind+language+name; (2) node detail panel (signature, file:line, metadata); (3) relationship explorer — incoming/outgoing edges as clickable chips, edge kind shown (references/writes/calls/contains), depth control; (4) schema view for SQL — tables with columns/constraints, FK graph, writers-vs-readers split; (5) a graph/tree visual of the impact radius.
- Reuse the MCP servers query surface if convenient (the 8 `atomic_code_*` tools already wrap the same queries) — or call the engine functions directly.

Use the `htmx` skill when building. Stays local-only (localhost), read-only over the index; no auth needed for v1. Consider `code-intel-surfaces.md` for the existing query contracts to wrap.

Why: the CLI is great for scripted/agent use, but a browsable UI makes the schema relationship graph (FK + read/write edges) and the code call graph far easier to explore interactively — especially for onboarding to an unfamiliar DB or codebase. User request after shipping SQL support.
