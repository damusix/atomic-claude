{{- define "agent-implementer-workflow" -}}
<workflow>
## Workflow

1. Read the brief. If `$SCRATCH/BRIEF.md` is provided, read it first — it points at the canonical spec at `docs/spec/<topic>.md`. Read the spec next if relevant.
2. Find the target code. {{ template "agent-search-tooling" . }} Read enough to understand callers and existing tests. Do NOT explore the whole repo. When reading multiple related files (e.g. implementation + its test), read them in parallel — don't read sequentially.
2b. **Reflect** on what you found. Does the surrounding code match what the brief or spec assumed? Check callers, edge cases, and patterns that change the approach. If something surprises you, re-read before writing — don't charge forward on a misread.
2c. **Code-intel sweep (when index present).** Before editing a symbol, if `.claude/.atomic-index/atomic.db` exists, run `atomic code impact <symbol>` to see the blast radius and `atomic code callers <symbol>` to find every call site — so the change accounts for all affected callers. Query one symbol at a time; skip silently if the binary is absent or the DB is missing.
2d. **Reuse check.** Before writing, run the ladder — standard library, then a native platform feature, then an already-installed dependency — and reach for those before custom code. Don't add a second helper for what an existing one already does. Reuse beats rewrite; simpler never means skipping validation, error handling, or security.
{{ template "agent-tdd-signals" . }}
4b. **Self-check**: if a spec or brief was provided, re-read its success criteria. Confirm each is met by the code you wrote. If any is unmet, go back — don't report done.
5. Report atomic.
</workflow>
{{ template "agent-code-intel" . }}
{{- end -}}
