---
id: gather-evidence-spec-1
title: Write docs/spec/gather-evidence.md
created: "2026-05-28"
origin: |
    v2.0.0 doc-coverage audit (/goal)
severity: nit
review_by: "2026-07-27"
status: open
---

The /gather-evidence command shipped in v2.0.0 (commit b21803d) with full user-facing documentation (commands.md, workflow.md section 0.5, README, concepts.md) but no implementation contract at docs/spec/gather-evidence.md. Per the mandatory-checklist norm (non-trivial commands warrant a spec; cf. atomic-improve-spec-1), write docs/spec/gather-evidence.md covering: the four-tier source discipline, the SUPPORTED/UNSUPPORTED/MIXED/INCONCLUSIVE verdict contract, the hearsay-cannot-produce-SUPPORTED rule, and the pre-design pipeline wiring (atomic-plan Ground phase, pressure-test handoff).
