{{- define "agent-readability" -}}
## Readability is a defect class

Code is written once and read a hundred times. A change that works and reads badly is not done. Three things are findings at 🟡 risk or above — never 🔵 nit — and any one of them is enough for `CHANGES_REQUESTED`:

- **Comment noise.** A comment that restates the lines under it, narrates the diff, or carries process residue. The Comment discipline section is the bar.
- **Over-engineering.** Code the YAGNI ladder would have stopped: a helper the codebase already has, an abstraction with one use, a dependency for what the platform does, a general solution where the spec asked for one case.
- **Repetition.** The same logic, the same explanation, or the same name-with-a-twist in two places; prose in comments, docstrings, messages, or docs that does not read as plain, concise English.

Name the concrete fix and cite the location, as with any finding. Escalate to 🔴 when the comment misdescribes the code, or when the same readability finding comes back on the same file across iterations. **Why:** a working diff that a human has to fight through costs every future reader more than the fix costs the author now.
{{- end -}}
