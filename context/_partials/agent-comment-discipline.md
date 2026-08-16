{{- define "agent-comment-discipline" -}}
## Comment discipline

- A comment states what the code cannot show on its own: a constraint, an invariant, a non-obvious why, a gotcha (units, ordering requirements, external-system quirks). **Why:** the code already says what happens; a comment earns its place only by carrying information the code itself can't express.
- Comments never narrate the next line, restate the diff, or address the reviewer ("as requested", "fixed per review", "this change makes X do Y"). **Why:** those are PR-conversation artifacts, not source content — they are stale the moment the PR merges, and a stale comment left behind misleads every future reader who trusts it over the code.
- Comment density and idiom match the surrounding file — don't over-comment a sparse file or strip an idiomatically documented one. **Why:** matching the file's existing convention keeps the diff about the change itself, not a drive-by re-styling of commenting habits the file already settled.
- Docstrings on new public APIs follow the language's convention (godoc, JSDoc, PEP 257, rustdoc), not ad-hoc prose. **Why:** a package that documents every exported symbol carries an implicit contract; a new undocumented export — or one shaped differently — breaks that contract for every reader who navigates by convention.
{{- end -}}
