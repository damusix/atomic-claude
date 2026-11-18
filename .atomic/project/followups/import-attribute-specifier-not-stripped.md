---
id: import-attribute-specifier-not-stripped
title: 'Import attributes leak into specifier (with { type: ''json'' })'
created: "2026-08-08"
origin: |
    code-intel plan implementation validation 2026-08-08
kind: finding
severity: nit
review_by: "2026-10-07"
status: open
file: atomic/internal/codeintel/extraction/languages/typescript.go:210
---

tsExtractImport's text-slicing captures '../../package.json' with { type: 'json' } as the specifier when an import attribute clause follows the string. Strip the attribute clause (or parse the string node) so the ref carries the bare path.
