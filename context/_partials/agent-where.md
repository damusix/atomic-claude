{{- define "agent-where" -}}
## Position orientation

Before wiki- or realm-scoped work — writing to `docs/wiki/`, deciding whether a change is repo-scope or realm-scope, reasoning about a `<wikis>`-registered member repo — run `atomic where` (`--json` for machine-parseable output) to check position across three axes in one call: repo-scope wiki presence, realm-scope position (root / member / orphaned / none), and code-index scope. It's read-only and cheap — a handful of stat calls, no git subprocess spawns.

`--json` also carries the project's state paths and branch, so anything that would otherwise construct a scratchpad, report, reminder, or archive path by hand should read it from here instead:

- `repo_root`, `repo_scope`, `realm_scope`, `code_index` — the three position axes above.
- `branch` — the current branch, resolved from `.git/HEAD` directly (no git subprocess spawn), with a detached-HEAD fallback.
- `reports` — this branch's session-report dir, already branch-scoped with the legacy fallback folded in. Use this, not `reports_root` plus a hand-built branch suffix.
- `reports_root` — the unscoped parent of `reports`, for callers that need to enumerate across branches.
- `reminders` — this project's reminders dir.
- `archive` — this project's retired-scratchpad-bundle archive root, read by `atomic scratchpad list --archived`.

`reports`, `reports_root`, `reminders`, and `archive` are project-keyed off the main checkout root, not the current worktree — a worktree and its main checkout report the same paths for those four. `repo_root` and `branch` are not: `repo_root` reports the current worktree's own path, and `branch` is per-worktree by design. Never reconstruct any of the four project-keyed fields from `repo_root` plus a literal suffix; a value here can change shape across a migration in ways a hand-built path can't track.

**Graceful degradation — non-negotiable.** If `atomic` is not on PATH, or the command errors, fall back silently to the existing detection heuristics (walk for `docs/wiki/index.md`, check for a `<wikis>` block in `CLAUDE.md`) — never surface the absence as an error or block on it. The verb is an orientation shortcut, not a dependency.
{{- end -}}
