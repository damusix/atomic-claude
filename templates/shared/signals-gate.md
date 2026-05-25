{{define "signals-gate"}}<signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Check `atomic signals stale`. If fresh (exit 0), skip.
3. Both pass → invoke the `atomic-signals` skill in silent mode. Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md`.

The `atomic signals stale` command is the source of truth — it fast-fails when nothing changed and catches structural shifts that a file-extension allowlist would miss.
</signals-refresh>{{- end}}
