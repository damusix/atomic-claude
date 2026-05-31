---
id: doctor-verbose-f-1
title: 'manifest verbose Findings: only drifted: prefix is tested'
created: "2026-05-31"
origin: |
    atomic doctor --verbose fix, strategist review 2026-05-31
kind: finding
severity: nit
review_by: "2026-07-30"
status: open
file: atomic/internal/doctor/checks_manifest.go
---

checks_manifest.go builds Findings with three prefixes (missing:/extra:/drifted:) but only the drifted: path has a test (checks_manifest_test.go TestCheckManifest_fail_findings). A real drift fixture for missing/extra requires a structurally broken bundle, which the synthetic repo-dev helper does not produce. Risk: low — the prefixes are trivial string concatenation, only firing on a broken bundle — but a typo there would ship untested. Add coverage when a missing/extra fixture is feasible.
