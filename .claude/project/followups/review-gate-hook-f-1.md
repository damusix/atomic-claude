---
id: review-gate-hook-f-1
title: Per-hook opt-out for the Claude Code hooks
created: "2026-09-21"
origin: |
    docs/spec/review-gate-hook.md, finalize audit
kind: finding
severity: question
review_by: "2026-11-20"
status: open
file: atomic/internal/hooks/hooks.go
---

atomic hooks uninstall removes all three Claude Code hook registrations; a user who wants session-start reminders without the stop gate edits settings.json by hand. Recorded as a Non-goal in docs/spec/review-gate-hook.md. Shape if demand appears: an --only <event> flag on atomic hooks install|uninstall.
