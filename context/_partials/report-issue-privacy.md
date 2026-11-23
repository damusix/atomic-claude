{{- define "report-issue-privacy" -}}
Treat issue text as public regardless of the tracker. Redact source material before drafting:

<constraints>

- **Redact PII and secrets** into neutral placeholders, preserving technical structure so the report stays actionable:
  - Emails → `user@example.com`; person names not needed to reproduce → `<name>`.
  - Real domains / hostnames / internal URLs → `example.com` / `<host>` (keep a public name only when it IS the subject of the bug).
  - Absolute paths carrying a username (`/Users/<you>/…`, `/home/<you>/…`, `C:\Users\<you>\…`) → repo-relative or `<path>`.
  - API keys, tokens, passwords, connection strings, IP addresses → `<redacted>`.
  - Client / company / project names not needed to reproduce → `<project>`.
- **Keep the signal.** Exact error text, command names, `atomic` / artifact identifiers, versions, and stack frames stay verbatim — they are what makes the issue reproducible. Redact identity, not mechanics.
- If redaction removes a fact needed to reproduce the issue, ask what can be shared. Do not invent a substitute.
- Preview the title, body, target, and labels. Get explicit approval before creating an issue, including when using a template.

</constraints>
{{- end -}}
