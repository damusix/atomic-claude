{{- define "handoff" -}}
<handoff>

## Hand off

Check these at entry, and again whenever an implementer report or a reviewer finding surfaces one. On a match: name the signal in one line, print the handoff, keep `$SCRATCH` (its `STATE.md` records the work so far), stop.

| Signal | Handoff |
|--------|---------|
| Root cause unknown, or it shifted mid-loop | `/subagent-diagnose <task>` |
| Two viable approaches, or success criteria still open | `/atomic-plan <task>` |
| A new public API, schema migration, or cross-service contract is implied | `/atomic-plan <task>` |
| Implementer reports `BLOCKED` or `NEEDS_CONTEXT` | surface the report to the user |

The policy table may add rows. No row counts files.

</handoff>
{{- end -}}
