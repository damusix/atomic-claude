---
id: ts-type-annotation-refs-not-extracted
title: TS type-annotation references not extracted — interface/type_alias nodes orphan
created: "2026-08-08"
origin: |
    code-intel plan implementation validation 2026-08-08
kind: finding
severity: risk
review_by: "2026-10-07"
status: open
file: atomic/internal/codeintel/extraction/languages/typescript.go
---

Only extends/implements heritage produces type references. Type annotations (params, returns, generics, satisfies) never link to interface/type_alias nodes: on taxgentic/server 503 of 524 interfaces and 177 of 182 type aliases have no edge beyond contains — the largest remaining orphan class (680 nodes) after local-variable suppression landed.
