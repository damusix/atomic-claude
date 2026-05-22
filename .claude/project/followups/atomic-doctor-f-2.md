---
id: atomic-doctor-f-2
title: '`gitToplevel` called 3× per doctor run'
created: "2026-05-17"
origin: |
    docs/spec/atomic-doctor.md, iter 5 + iter 6 reviewers (CP-3 + CP-4). Deferred to project followups at Phase 3 finalize 2026-05-17.
severity: risk
review_by: "2026-07-16"
status: open
---

`atomic/internal/doctor/checks_manifest.go:38`, `checks_refs.go`, plus `repodev.go` `IsRepoDev`


Three call sites each spawn `git rev-parse --show-toplevel` per doctor run. Latency nit (~20-30ms total wasted); minor correctness surface if cwd-relative symlinks change between calls. Thread the resolved toplevel through Run, or hoist to a Run-level cache passed via `Opts`.
