---
id: atomic-doctor-F-1
title: "bundlemirror.Run double-reads files via path reconstruction"
created: 2026-05-17
origin: |
  docs/spec/atomic-doctor.md, iter 3 reviewer (CP-2). Deferred to project
  follow-ups at Phase 3 finalize 2026-05-17.
severity: risk
review_by: 2026-05-20
status: open
file: atomic/internal/bundlemirror/mirror.go:196-216
---

After the CP-2 refactor, `Run` calls `enumerate` to get `[]embedded.Artifact`, then reconstructs paths via `filepath.Join(repoRoot, filepath.FromSlash(ea.Target))` inside `mirrorFile`, re-reading each file. Harmless today but creates a hidden contract.
