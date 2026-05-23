---
id: signals-router-f-2
title: Double file reads across assembleBody
created: "2026-05-23"
origin: |
    docs/spec/signals-router.md, iter 1 reviewer (CP-1)
severity: risk
review_by: "2026-07-22"
status: open
file: atomic/internal/signals/signals.go:120
---

ScanLanguages and ScanTree both call readFileMeta for the same files via separate passes in assembleBody. Every file read twice total. Within the tree pass, single-read is satisfied. Cross-pass optimization would share the file bytes and compute both LOC and metadata from one os.ReadFile call.
