{{- define "agent-comment-discipline" -}}
## Comment discipline

- A comment states what the code cannot show on its own: a constraint, an invariant, a non-obvious why, a gotcha (units, ordering requirements, external-system quirks). **Why:** the code already says what happens; a comment earns its place only by carrying information the code itself can't express.
- Say it in the fewest lines that carry the why. A paragraph where a clause works is a paragraph the next reader skips. **Why:** length is what makes comments go unread, and an unread comment protects nothing.
- Never restate the code. If the sentence can be derived by reading the lines below it, delete the sentence. **Why:** a restatement has to be maintained in lockstep with the code and silently goes wrong the moment it isn't.
- Point at docs; do not copy them. When the detail lives in a spec, design doc, or reference page, cite the path (`docs/spec/<topic>.md`) and stop. Never quote a spec's wording or paste its change-log entry into a comment. **Why:** a copy is a second source of truth that no one updates when the doc moves on, so the reader who trusts it is reading last quarter's decision.
- Carry no plan or process residue: checkpoint IDs (`CP3`), issue and PR numbers, dates, agent or review chatter ("as requested", "fixed per review", "per the spec's 2026-07-30 entry"). **Why:** those describe how the code came to exist, which git history already records; in the source they are noise that outlives the process that produced it.
- Comment density and idiom match the surrounding file — don't over-comment a sparse file or strip an idiomatically documented one. **Why:** matching the file's existing convention keeps the diff about the change itself, not a drive-by re-styling of commenting habits the file already settled.
- Docstrings on new public APIs follow the language's convention (godoc, JSDoc, PEP 257, rustdoc), not ad-hoc prose. **Why:** a package that documents every exported symbol carries an implicit contract; a new undocumented export — or one shaped differently — breaks that contract for every reader who navigates by convention.
{{- end -}}
