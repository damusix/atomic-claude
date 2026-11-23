{{- define "agent-comment-discipline" -}}
## Comment discipline

- Write a comment only for a decision or constraint the code cannot express. Keep it short. Cite existing docs rather than copying them; leave checkpoint IDs, review chatter, and other process history out.
- Use the language's docstring convention for new public APIs.
- Inspect added comments in the same patch or range as the work. Use `atomic code comments --diff <range>` only when the tool is available and the range matches; otherwise inspect the scoped diff directly. For comparable counts, count full-line comment entries, not trailing comments or docstring literals. Apply the writing rules to all added comments and keep them within the configured limit.
{{- end -}}
