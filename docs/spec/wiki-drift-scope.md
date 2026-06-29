# Spec: wiki drift scope (workstream E)

Child of [`docs/spec/wiki-storage-relocation.md`](./wiki-storage-relocation.md) (workstream B).
Contracts how a repo-scope wiki refresh decides between **incremental** and **full** re-infer.

**Approach:** see [`docs/design/signals-wiki-unification.md`](../design/signals-wiki-unification.md)
(Approach row 3, "git-as-prev-store"; Resolved items "Drift is repo-scope only" and "No new third
staleness mechanism"). The committed `docs/wiki/scan.md` is the diff baseline; `git diff` is the
change-set source; `<scan-sha>` in `index.md` is a tiebreaker for stale-scan cases only.


## Goal

Give the `atomic-wiki-inferrer` a cheap, correct scope signal at refresh time so it re-infers
only the domains that changed, and falls back to a full re-infer when the scan is out of sync
with the committed baseline.


## Non-goals

- Realm-scope drift is out of scope — realm wiki has no committed `scan.md` to diff against.
- No new third staleness mechanism. This spec reconciles with the existing `atomic signals stale`
  content-hash and the impl-phase SHA-range refresh (`signals-refresh-timing.md`); it does not
  replace or supplement them with an independent tracker.
- No Go changes — coordination is done by the agent / skill pipeline using existing `git` and
  `atomic signals stale`.


## Success criteria

- SC1 — A refresh where the committed `docs/wiki/scan.md` is unchanged and `<scan-sha>` in
  `index.md` matches its SHA → inferrer receives scope `incremental`.
- SC2 — A refresh where `git diff docs/wiki/scan.md` shows a large change set (above the
  line-delta threshold) → inferrer receives scope `full`.
- SC3 — A refresh where the committed `scan.md`'s SHA does not match the stored `<scan-sha>` →
  inferrer receives scope `full`, regardless of the diff size.
- SC4 — When `docs/wiki/scan.md` is not committed (untracked or absent) or the working tree is
  not a git repo, the pipeline falls back to the `atomic signals stale` content-hash mechanism
  and the scope defaults to `full`. The fallback is logged as a warning, not a hard error.
- SC5 — The scope decision is computed once per refresh, before the inferrer is dispatched, and
  passed as a single `scope: incremental|full` field in the dispatch brief.
- SC6 — No surface (skill, agent, command) makes its own independent staleness determination;
  all drift reasoning is consolidated in the scope-computation step of the
  `references/repo.md` pipeline.


## Approach

Git-as-prev-store: `git diff HEAD -- docs/wiki/scan.md` produces the change set (lines added +
removed); `<scan-sha>` disambiguates stale-scan commits; incremental vs full decision is a
threshold on line-delta + sha mismatch. See design doc for the full rationale.


## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|---------|
| 1 | Scope-computation step in repo pipeline | `skills/atomic-wiki/references/repo.md` | atomic-implementer (surgical) | 1 | `git diff HEAD -- docs/wiki/scan.md` + `<scan-sha>` read; threshold logic produces `incremental` or `full`; absent/untracked scan.md triggers fallback; fallback warning logged |
| 2 | Inferrer consumes `scope` field | `templates/agents/atomic-wiki-inferrer.md` | atomic-implementer (surgical) | 1–2 | `scope: incremental` limits re-infer to changed domains derived from the diff; `scope: full` runs all domains; absent `scope` defaults to `full`; render+bundle parity clean |
| 3 | Fallback integration with `atomic signals stale` | `skills/atomic-wiki/references/repo.md`, `templates/shared/signals-gate.md` (read-only reference) | atomic-implementer (surgical) | 1–2 | SC4 fallback path tested (no committed scan.md → `atomic signals stale` consulted → scope full); no duplicate staleness check when the git path succeeds; reconciliation note added |


## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| `git diff` line-delta threshold is wrong (too low → spurious full re-infers; too high → stale incremental) | Medium | Low (correctness cost, not a failure) | Default threshold in the skill text; revisit after one real full-project cycle; start conservative (prefer full) |
| `<scan-sha>` block absent in `index.md` (pre-B repo or first run) | High (first-run) | Low | Treat missing block as sha-mismatch → scope `full`; idempotent |
| `docs/wiki/scan.md` committed but not yet produced by B (workstream B not yet shipped) | Medium | Blocks E | Checkpoint 1 must declare B as a dependency gate; skip E until B produces the file |
| Not-a-git-repo edge (zip extract, detached HEAD in CI) | Low | Low | SC4 fallback handles this; `git diff` failure → fallback branch |
| Scope passed as string leaks into agent's `changed_range` path (from `signals-refresh-timing.md` C1) | Low | Low | `scope` field is wiki-pipeline-only; `changed_range` is signals-inferrer-only; the two are distinct dispatch fields on distinct agents |


## Change log

(empty)
