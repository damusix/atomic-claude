---
id: atomic-doctor-f-1
title: '`bundlemirror.Run` double-reads files via path reconstruction'
created: "2026-05-17"
origin: |
    docs/spec/atomic-doctor.md, iter 3 reviewer (CP-2). Deferred to project followups at Phase 3 finalize 2026-05-17.
severity: risk
review_by: "2026-07-16"
status: open
file: atomic/internal/bundlemirror/mirror.go:196-216
---

After the CP-2 refactor, `Run` calls `enumerate` to get `[]embedded.Artifact`, then reconstructs paths via `filepath.Join(repoRoot, filepath.FromSlash(ea.Target))` inside `mirrorFile`, re-reading each file. Harmless today (all targets are repoRoot-relative by construction) but creates a hidden contract between `enumerate` output and `Run` consumption. If any future walker rule produces a target that doesn't map cleanly back under `repoRoot`, `Run` silently breaks. Document the contract, or have `enumerate` return both the artifact + the source path so `Run` doesn't reconstruct.
