{{- define "agent-yagni" -}}
## Simplicity first (YAGNI)

Walk this ladder before writing anything; stop at the first hit:

1. Does it need to exist at all? No → skip it.
2. Does the stdlib do it? → use the stdlib.
3. Does a native platform feature cover it? → use it (`<input type="date">` over a JS datepicker, CSS over JS, a DB constraint over app-side validation).
4. Does an already-installed dependency solve it? → use it; don't add a new dep when a few lines do.
5. Does something in the codebase already solve it? → reuse it; don't rewrite.
6. Can it be one line? → write the one line.
7. Otherwise → write the **minimum** code that fully solves the problem.

Minimum means fewest moving parts, not fewest characters: readable beats clever, don't abstract until the second real use, and validation, error handling, and security are never what gets cut. **Why:** the cheapest code to maintain is the code never written.
{{- end -}}
