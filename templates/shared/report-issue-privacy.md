{{- define "report-issue-privacy" -}}
## Privacy: redact before posting

A GitHub issue is public and permanent — treat the body as world-readable. Before drafting, scrub the material the user pasted or that you gathered from their repo:

<constraints>

- **Redact PII and secrets** into neutral placeholders, preserving technical structure so the report stays actionable:
  - Emails → `user@example.com`; person names not needed to reproduce → `<name>`.
  - Real domains / hostnames / internal URLs → `example.com` / `<host>` (keep a public name only when it IS the subject of the bug).
  - Absolute paths carrying a username (`/Users/<you>/…`, `/home/<you>/…`, `C:\Users\<you>\…`) → repo-relative or `<path>`.
  - API keys, tokens, passwords, connection strings, IP addresses → `<redacted>`.
  - Client / company / project names not needed to reproduce → `<project>`.
- **Keep the signal.** Exact error text, command names, `atomic` / artifact identifiers, versions, and stack frames stay verbatim — they are what makes the issue reproducible. Redact identity, not mechanics.
- **When redaction would drop something load-bearing, ask** rather than guess or leak. Never invent a repro detail to replace a redacted one.
- **Preview and confirm.** Before running `gh issue create`, print the full rendered issue — title, body, target repo, labels — and get an explicit go-ahead from the user. Do not post on inference. (The `--template` editor path already lets the user review before submitting; this gate covers the inline HEREDOC path.)

</constraints>
{{- end -}}
