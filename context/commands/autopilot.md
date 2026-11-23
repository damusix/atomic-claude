---
description: Plan, implement, and ship a task or issue without routine approvals. Consult atomic-strategist when stuck.
argument-hint: "[task or issue number, optional shipping instructions]"
---

Use the request and conversation. If given a GitHub issue number, read the issue first.

If shipping remotely, resolve the PR base using `atomic-git-discipline/references/shipping.md` before creating or reusing a task branch. Never use the base branch as the PR head.

1. Follow `/atomic-plan`, then `/implement` (the commands). Decide without routine questions. If a planning challenge would resolve a material uncertainty, use the automated route. Keep the spec current and fix every review and audit finding. Preserve independent review, end-result verification, audit, and the repo scan.
2. When stuck, ask `atomic-strategist` to investigate read-only. Use its recommendation to continue. If the blocker remains, report it and stop without shipping.
3. Load `atomic-git-discipline` and its shipping reference. Follow the requested action; otherwise this invocation authorizes opening or updating a PR on the configured remote. With no remote, keep verified commits locally. Merge or rewrite published history only with explicit authorization. Report what shipped and how you verified it.
