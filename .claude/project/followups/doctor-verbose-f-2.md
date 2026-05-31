---
id: doctor-verbose-f-2
title: FixApplied/FixSummary render but no repair path populates them
created: "2026-05-31"
origin: |
    atomic doctor --verbose fix, strategist review 2026-05-31
kind: finding
severity: nit
review_by: "2026-07-30"
status: open
file: atomic/internal/doctor/format.go, atomic/internal/doctor/fix.go
---

format.go renders a ✓ fixed: <FixSummary> line when Result.FixApplied is true, and TestFormatHumanFixAppliedLine covers the rendering. But no code in the --fix repair path (fix.go / fix_impls.go) sets FixApplied or FixSummary on any Result, so the line can never appear in practice — it is render-only scaffolding. Either wire the repair path to populate these fields after a successful per-item fix, or drop the fields and the render branch if --fix is meant to report separately.
