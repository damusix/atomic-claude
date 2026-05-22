---
id: atomic-update-doctor-f-1
title: "`Resolved()` bool default heuristic piggybacks on intensity sentinel"
created: 2026-05-22
origin: |
  docs/spec/atomic-update-doctor.md, iter 2 reviewer (CP-3..CP-7 re-review).
  Deferred to project followups at Phase 3 finalize 2026-05-22.
severity: risk
review_by: 2026-07-21
status: open
file: atomic/internal/config/render.go:69
---

Backfill for `update.run_doctor` uses `cfg.Output.Intensity == ""` as the zero-value sentinel for "Config was constructed bare, not via `Default()`/`Load()`". If a future `outputSection` field gets a meaningful zero value, the sentinel breaks silently and `Resolved` could misclassify an explicitly-false `RunDoctor` as needing the default. Acceptable today (limited schema), but worth a dedicated `fromDefault bool` flag on `Config` or a `Default()`-call indirection the next time the schema grows.
