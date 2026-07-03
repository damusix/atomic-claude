---
id: signals-stale-repo-scope-gap
title: 'atomic signals stale / wiki stale: no repo-scope docs/wiki/ staleness check'
created: "2026-07-03"
origin: |
    docs/spec/wiki-deterministic-setup.md finalize, Phase 3 step 5
kind: finding
severity: risk
review_by: "2026-09-01"
status: open
---

Post-relocation (workstream B moved repo-scope signals to docs/wiki/), there is no
deterministic staleness check for repo scope. `atomic signals stale` still hardcodes
the pre-relocation path (`.claude/project/deterministic-signals.md`) and exits 2
(hard error) on any repo using the new layout. `atomic wiki stale` is realm-scope
only (checks <root>/wiki/, not docs/wiki/). `/subagent-implementation` Phase 3 step 5
("run atomic signals stale") hard-errors on any repo-scope-migrated repo, including
this one — the finalize signals-refresh step has to skip entirely instead of running.

Likely workstream-E territory (design doc: scan-sha drift scope, git-as-prev-store
baseline) — the git-diff-based staleness mechanism decided there was never wired to
a CLI verb repo-scope callers (like /subagent-implementation, /commit's signals-gate)
can actually invoke. Discovered while finalizing docs/spec/wiki-deterministic-setup.md;
unrelated to that task's scope.
