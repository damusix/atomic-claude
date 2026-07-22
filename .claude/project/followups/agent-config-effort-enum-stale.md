---
id: agent-config-effort-enum-stale
title: agent-config.md effort enum stale (omits xhigh, lists int)
created: "2026-07-22"
origin: |
    agents-effort-config branch
kind: finding
severity: nit
review_by: "2026-09-20"
status: open
file: .claude/rules/authoring/agent-config.md:80
---

The effort frontmatter row says 'One of low/medium/high/max, or a positive integer.' Upstream Claude Code docs (confirmed via claude-code-guide) define effort as exactly {low, medium, high, xhigh, max} — no integers, and xhigh is a first-class level between high and max. Fix the row to match. Repo-only authoring reference (never bundled), so low urgency.
