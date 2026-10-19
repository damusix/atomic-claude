---
id: commit-escalation-early-stop
title: /commit pr and squash merge stop before the next path
created: "2026-10-19"
origin: |
    docs/spec/artifacts-mermaid-flows.md, final audit
kind: finding
severity: risk
review_by: "2026-12-18"
status: open
file: context/commands/commit.md:55
---

In /commit pr, push-flow runs before pr-flow, and push-flow's 'up to date → Stop' ends the whole verb, so a branch already pushed with nothing new to commit never reaches pr-flow's own push-if-needed step and no PR opens. /commit squash merge has the same shape: squash-flow's 'one commit only → Stop' runs before merge-flow. Behavior predates #298; the Mermaid conversion made the stop visible. Fix: make those stops end the step rather than the verb when a later path follows.
