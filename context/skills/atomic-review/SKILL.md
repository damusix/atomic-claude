---
name: atomic-review
description: Review a scoped change for correctness, reuse, and readability. Defines the finding and severity rules used by Atomic's reviewer and auditor.
---

Read enough surrounding code to understand the change. Report findings only in scope. For changed public symbols, inspect callers and references with LSP or source search.

## What to catch


- **Reuse:** Follow the implementer's YAGNI ladder as a review check. Ask whether the code is needed, whether an existing helper or repo pattern solves it, and whether an installed library, standard library, or platform feature already handles it. Check actual callers before recommending another abstraction. Flag duplicate helpers, drift from established library usage, unnecessary dependencies, and abstractions with no second use. Name the exact replacement or what to remove. Keep validation, error handling, and security intact.
- **Correctness and evidence:** Check failure paths, caller compatibility, and tests that exercise the changed behavior. Flag error handling that hides a failure rather than handling it; valid defensive handling is fine. Missing required test-first evidence or a check result contradicted by evidence is a bug. A weak test is a risk.
- **Comments and prose:** Flag misleading comments as bugs. Flag comments or repeated prose that bury a constraint, explain competing behavior, or make the change materially harder to maintain as risks. A harmless, isolated restatement, wordy comment, or naming cleanup is a nit. Keep short explanations of decisions or constraints the code cannot express. Leave license headers and directives alone.

Inspect all added comments in scope. For counts, use `atomic code comments --diff <range>` only if that command measures the same changes. Otherwise count full-line comment entries in the scoped diff, excluding trailing comments and docstring literals.

## Findings


Use `path:line: <emoji> <severity>: <problem>. <fix>.` Cite evidence and name the concrete fix. Explain serious security or design risks in full sentences if one line is not enough.

| Marker | Severity | Use |
|---|---|---|
| 🔴 | bug | Wrong behavior, data loss, security defect, misleading comments, or contradicted evidence |
| 🟡 | risk | Maintainability or correctness risk, including avoidable duplication, library drift, and speculative design |
| 🔵 | nit | Harmless polish that does not change or obscure behavior |
| ❓ | question | Missing decision or evidence prevents a judgment |

Standalone reviews report nits only on request. Gate reviews report actionable nits so the caller can fix or defer them. Sort findings by path and line and finish with `totals:`. Say `No issues.` only when there are none. Do not edit the work or approve a PR.
