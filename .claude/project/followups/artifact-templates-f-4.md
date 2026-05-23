---
id: artifact-templates-f-4
title: Orphan error message hardcodes commands/ path prefix
created: "2026-05-23"
origin: |
    docs/spec/artifact-templates.md, iter 1 reviewer (CP-1)
severity: nit
review_by: "2026-07-22"
status: open
file: atomic/internal/templaterender/templaterender.go:157-159
---

The orphan-check error literally writes "commands/<name>" in its remediation hints. When the renderer expands to agents/ and/or skills/ (currently out of v1 scope), the message will need to know which kind it's describing. Refactor to take kind as a parameter when that work lands.

Origin: docs/spec/artifact-templates.md, iter 1 reviewer (CP-1).
