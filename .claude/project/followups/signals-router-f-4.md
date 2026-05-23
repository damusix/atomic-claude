---
id: signals-router-f-4
title: ScanWithOptions mutates caller-passed opts pointer
created: "2026-05-23"
origin: |
    docs/spec/signals-router.md, iter 2 reviewer (CP-2)
severity: risk
review_by: "2026-07-22"
status: open
file: atomic/internal/signals/signals.go:93
---

ScanWithOptions assigns to opts.SignalsIgnoreGlobs, mutating the caller-passed struct. If a shared Options is reused across calls from different roots, globs from the first persist (len==0 guard short-circuits). No current callers harmed — Scan creates a fresh nil → &Options{}. Consider cloning opts at entry or documenting the mutation contract.
