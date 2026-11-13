---
id: wiki-rules-doctor-validation
title: Doctor validation of wiki pointer-rule cards
created: "2026-09-02"
origin: |
    docs/spec/wiki-pointer-rules.md (Checkpoint 3)
kind: plan
review_by: "2026-11-01"
status: open
file: atomic/internal/doctor/checks_signals.go
---

Add a doctor check that validates .claude/rules/wiki/<domain>.md cards between refreshes: exactly one card per domain in docs/wiki/index.md's router table, no orphan cards for domains that no longer exist, every link in each card's typed pointer index resolves on disk, and each card's paths: frontmatter parses as valid YAML globs. Drift insurance only, not generation — the pipeline rewrites cards on every refresh; this check catches manual edits or stale files that survive between refreshes.
