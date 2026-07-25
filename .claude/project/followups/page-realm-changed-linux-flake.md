---
id: page-realm-changed-linux-flake
title: Page 'refetches on realm.changed (cap exceeded)' flakes 10-20% on Linux
created: "2026-07-25"
origin: |
    found while reproducing the frontend CI failure in a Linux container (PR #166)
kind: finding
severity: risk
review_by: "2026-09-23"
status: open
file: atomic/internal/serve/frontend/src/pages/Page/Page.test.tsx
---

`Page > refetches on realm.changed when the changed list is omitted (cap exceeded —
refetch-all)` fails intermittently, roughly 10-20% of runs, on Linux only.

A genuine CPU-timing race in the test, not the MiniGraph mock-leak bug fixed in PR #166 —
reproducible independently of that fix and present before that branch.

**Why it matters:** an intermittent failure in the `frontend` CI job is indistinguishable at a
glance from a real regression, and it trains people to re-run red CI instead of reading it.

**How to reproduce:** copy the repo onto a Linux container's native filesystem (a macOS bind
mount masks timing bugs of this class — `docker cp` rather than `-v`), bun 1.3.13, then loop
the suite: `bun test src/pages/Page/Page.test.tsx --rerun-each 20`.

**How to verify a fix:** 40+ consecutive green runs in that container.
