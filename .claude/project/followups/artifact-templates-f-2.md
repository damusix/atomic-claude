---
id: artifact-templates-f-2
title: Renderer success message on stderr (matches bundle-mirror)
created: "2026-05-23"
origin: |
    docs/spec/artifact-templates.md, iter 1 reviewer (CP-1)
severity: nit
review_by: "2026-07-22"
status: open
file: atomic/cmd/render-templates/main.go:62
---

Renderer's success message goes to stderr (), not stdout. Mirrors bundle-mirror precedent — consistent inside the repo, but stdout for success / stderr for errors is the convention. If revisited, change render-templates and bundle-mirror together to preserve convention symmetry.

Origin: docs/spec/artifact-templates.md, iter 1 reviewer (CP-1).
