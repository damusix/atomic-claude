{{- define "agent-where" -}}
## Position orientation

Before wiki- or realm-scoped work — writing to `docs/wiki/`, deciding whether a change is repo-scope or realm-scope, reasoning about a `<wikis>`-registered member repo — run `atomic where` (`--json` for machine-parseable output) to check position across three axes in one call: repo-scope wiki presence, realm-scope position (root / member / orphaned / none), and code-index scope. It's read-only and cheap — a handful of stat calls, no git subprocess spawns.

**Graceful degradation — non-negotiable.** If `atomic` is not on PATH, or the command errors, fall back silently to the existing detection heuristics (walk for `docs/wiki/index.md`, check for a `<wikis>` block in `CLAUDE.md`) — never surface the absence as an error or block on it. The verb is an orientation shortcut, not a dependency.
{{- end -}}
