---
id: hooks-tests-read-real-home
title: hooks tests read real HOME — user's <wikis> block leaks into empty-output expectations
created: "2026-06-12"
origin: |
    wiki summarized-discovery fix session 2026-06-11
kind: finding
severity: risk
review_by: "2026-08-11"
status: open
file: atomic/internal/hooks/hooks_test.go
---

TestSessionStart_EmptyReminders, TestSessionStart_FormatText_EmptyReminders, TestSessionStart_FutureDue_Silent fail on any machine with registered dirty wikis: the session-start wiki-staleness check reads the real ~/.claude/CLAUDE.md <wikis> block instead of a test-scoped HOME. Inject the CLAUDE.md path (or HOME) as a seam in internal/hooks tests.
