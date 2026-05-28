{{define "signals-gate"}}<signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Check `atomic signals stale`. If fresh (exit 0), skip.
3. Both pass → dispatch the `atomic-signals-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md` after the agent completes.

The `atomic signals stale` command is the source of truth — it fast-fails when nothing changed and catches structural shifts that a file-extension allowlist would miss.
</signals-refresh>{{- end}}
