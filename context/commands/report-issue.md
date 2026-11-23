---
description: Open a bug, enhancement, or question in the resolved issue tracker.
---

## Workflow

1. Resolve the tracker: {{ template "resolved-provider" "issue-tracker" }}
2. For a hosted tracker, run its `auth-check` and resolve its `project-identifier`. Stop on failure with the tracker reference's guidance. Ask which project to use if several match. With `issue-tracker: filesystem`, use the ledger without authentication.
3. Classify the request as bug, enhancement, or question. Search existing items, including closed ones, using `issue-search`; show close matches before proposing a duplicate.
4. Use a matching tracker template if available. Draft a title of at most 70 characters and a body using the shape below. Keep only existing labels. For the ledger use `--severity risk` for a bug, `--severity question` for a question, and `--kind plan` for an enhancement.
5. Redact and preview the full title, body, target, and labels. After explicit approval, run `issue-create`, apply any labels that require a separate operation, and print the resulting URL or key.

{{ template "report-issue-privacy" . }}

## Body

- **Bug:** summary, reproduction steps, expected and actual behavior, and relevant version, OS and runtime.
- **Enhancement:** problem, proposal, optional timing and out-of-scope boundaries.
- **Question:** the question and relevant context, including what was tried.

Use exact commands and errors in code blocks after redaction. Do not invent missing facts or add AI attribution.
