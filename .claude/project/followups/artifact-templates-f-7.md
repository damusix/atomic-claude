---
id: artifact-templates-f-7
title: worktree-cleanup-prompt has 1 consumer (merge-flow only)
created: "2026-05-23"
origin: |
    docs/spec/artifact-templates.md, iter 6 investigation (CP-5)
severity: risk
review_by: "2026-07-22"
status: open
file: templates/shared/worktree-cleanup-prompt.md
---

SC 11 requires >=2 consumers for small partials. worktree-cleanup-prompt is consumed only by merge-flow. This is intentional in v1: squash-only never had worktree cleanup (confirmed at base SHA 592e5fc), and push-flow/pr-flow do not delete branches. squash-and-merge already uses merge-flow so it inherits the prompt indirectly. If a future squash-only variant gains worktree detection, wire it then. Accepted deviation from SC 11 in v1.

Origin: docs/spec/artifact-templates.md, iter 6 investigation (CP-5).
