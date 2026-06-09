---
id: measure-subagent-loop-token-cost
title: Measure token cost of the subagent implement-review loop
created: "2026-06-09"
origin: |
    docs audit 2026-06-09: workflow.md cost note is anecdotal
kind: plan
review_by: "2026-08-08"
status: open
---

The workflow.md cost note (What the loop costs) is anecdotal: never hits the 5h window on Max 20x, ~50% of weekly limit under heavy 4-5 instance use, smaller Max plan may hit the window cap. Measure actual token spend per checkpoint (builder + reviewer + haiku log/CI watchers) across a few real runs and replace the anecdote with numbers.
