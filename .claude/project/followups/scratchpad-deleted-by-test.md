---
id: scratchpad-deleted-by-test
title: A go test deletes .claude/.scratchpad/<today>-* against the real repo
created: "2026-06-29"
origin: |
    autopilot signals-wiki unification
kind: finding
severity: risk
review_by: "2026-08-28"
status: open
---

During the 6-workstream autopilot run, the session scratchpad dir .claude/.scratchpad/2026-06-29-wiki-storage-relocation/ was deleted twice, each time around a 'go test ./...' run, while OTHER (older-dated) scratchpad dirs survived. Hypothesis: a Go test creates+removes a fixture under .claude/.scratchpad/<current-date>-* using the REAL repo root instead of t.TempDir(), so it clobbers a live session scratchpad sharing today's date. Only ephemeral LLM working memory was lost (no tracked work). Investigate: grep test files for '.scratchpad' / time.Now()-dated paths against a non-temp root; fix to use t.TempDir().
