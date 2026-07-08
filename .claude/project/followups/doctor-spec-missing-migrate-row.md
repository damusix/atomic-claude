---
id: doctor-spec-missing-migrate-row
title: 'atomic-doctor spec: Check categories table missing category 12 (migrate) row'
created: "2026-07-08"
origin: |
    graphignore CP3 review
kind: finding
severity: nit
review_by: "2026-09-06"
status: open
file: docs/spec/atomic-doctor.md
---

docs/spec/atomic-doctor.md's Check categories table jumps from 11 to 13 — category 12 (migrate) was registered in code but never got a table row. Pre-existing gap discovered while adding category 13 (repo-config, graphignore CP3). Fix: add the missing row describing the migrate check's contract.
