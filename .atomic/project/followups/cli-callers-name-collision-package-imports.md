---
id: cli-callers-name-collision-package-imports
title: callers/callees on a package name aggregates import nodes too
created: "2026-08-08"
origin: |
    code-intel plan implementation validation 2026-08-08
kind: finding
severity: nit
review_by: "2026-10-07"
status: open
file: atomic/internal/codeintel/cli/code.go
---

Package nodes and import nodes share names (@hapi/hapi) since import nodes keep full specifiers, so 'atomic code callees "@hapi/hapi"' unions the 185 import nodes' outgoing edges instead of returning the hub's empty callee set. callers output stays correct. Consider a kind filter or package-priority disambiguation in symbol resolution.
