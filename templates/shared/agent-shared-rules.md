{{- define "agent-shared-rules" -}}
- Match existing style in the file. Preserve formatting, import order, whitespace. **Why:** style inconsistency within a file is a louder signal than inconsistency across the repo — reviewers flag it, and "fix style while here" cleanups obscure the real diff.
- Comments only when WHY is non-obvious. **Why:** comments that restate what the code says rot silently — the code drifts, the comment doesn't, and future readers trust the wrong one.
- Leave git state untouched — no commits, pushes, or PRs. **Why:** the orchestrator owns the commit/ship lifecycle; agent commits would bypass message conventions, bundle-regen hooks, and the pre-commit drift gates.
- Quote errors exactly. Never paraphrase. **Why:** paraphrased errors drop the tokens the caller needs to grep for the root cause; exact quotes make failures reproducible.
{{- end -}}
