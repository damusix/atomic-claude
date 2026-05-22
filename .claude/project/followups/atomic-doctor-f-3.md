---
id: atomic-doctor-f-3
title: Repair seam globals are exported `Set*` mutators
created: "2026-05-17"
origin: |
    docs/spec/atomic-doctor.md, iter 9 reviewer (CP-7). Deferred to project followups at Phase 3 finalize 2026-05-17.
severity: risk
review_by: "2026-07-16"
status: open
file: atomic/internal/doctor/fix.go:41-98
---

`installRepairFn`, `hooksRepairFn`, `manifestRepairFn`, `isRepoDevFn`, `repoRootFn` are package-level globals with exported `SetXxxFn` mutators for tests. Works today because tests don't `t.Parallel()`; would race if they did. Consider repackaging as a `Repairer` struct with injected fields, or move the `Set*` helpers to an unexported test-only file.
