---
id: serve-pipe-escaping
title: Serve domain one-liner has unescaped pipes (mis-renders table)
created: "2026-06-29"
origin: |
    autopilot signals-wiki unification
kind: finding
severity: nit
review_by: "2026-08-28"
status: open
file: docs/wiki/index.md:serve-row
---

The serve domain one-liner in the signals router contains unescaped pipe chars (md|code search, [page|system] toggle). In a markdown table cell these create spurious extra columns, so atomic serve renders the serve row with too many columns. Pre-existing (was in .claude/project/signals/serve.md before the docs/wiki relocation). Fix: escape as md\|code and [page\|system]. The doctor parser was already made robust to this (parseRouterDomains uses the last content column), so this is cosmetic-only for serve rendering.
