---
id: atomic-doctor-f-4
title: '`defaultManifestRepair` does not stream `make` output'
created: "2026-05-17"
origin: |
    docs/spec/atomic-doctor.md, iter 9 reviewer (CP-7). Deferred to project followups at Phase 3 finalize 2026-05-17.
severity: risk
review_by: "2026-07-16"
status: open
file: atomic/internal/doctor/fix_impls.go:54
---

Uses `CombinedOutput()` then discards on success — user sees `$ make -C atomic bundle` with no confirmation of what regenerated. Forward to the repair's `io.Writer` for transparency. Also: `cmd.Stdout = nil` + `cmd.Stderr = nil` before `CombinedOutput` are redundant.
