# Spec: wiki stale summary repo resolution

**Status: removed.** This spec fixed how `atomic wiki stale` matched a summary file to its member repo before comparing the summary's commit fingerprint. `atomic wiki stale` no longer compares summaries at all: it reports membership drift only, and summaries carry no fingerprint. Nothing in this spec describes current behavior. The current contract is `docs/spec/wiki.md` § `atomic wiki stale`; the removal is `docs/spec/realm-wiki-drop-refresh.md`.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Removed with summary staleness | none | `docs/spec/realm-wiki-drop-refresh.md` criterion 3 |


## Change log


- 2026-07-25 — Written. Fixes #157: summary mode resolved the member directory from
  the summary file's path, hitting two defects (unstripped `.md`, discarded parent
  path) that made the `reflects_rev` comparison unreachable for three of four shapes.

- 2026-10-18 — Removed: summary staleness. Body reduced to a status paragraph; the
  problem statement, shape matrix, resolver approach, checkpoints, and verification are
  gone. `atomic wiki stale` reports membership drift only, and summary fingerprints and
  `STALE summary` lines no longer exist, so the resolver has nothing to resolve.
  **Removed:** the summary-to-member resolver contract and its `STALE summary` behavior.
  See `docs/spec/realm-wiki-drop-refresh.md`.
