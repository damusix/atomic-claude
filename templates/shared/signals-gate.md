{{define "signals-gate"}}<signals-refresh>
Refresh project signals so Claude's map stays current for the next session.

1. Check `command -v atomic`. If missing, skip.
2. Run `atomic signals stale` and act on the exit code:
   - **exit 0** (fresh) → skip the refresh.
   - **exit 1** (stale) → refresh is mandatory. Continue to step 3. Do NOT second-guess this with `atomic signals diff`, file counts, or a judgment that "the change was small" — exit 1 means a fresh scan would produce different deterministic content than the stored signals file, and the only correct response is to refresh. Skipping it accumulates drift. The command prints how much would change and the directive; follow it.
   - **exit 2** (error, e.g. signals file missing) → report the stderr message and skip; a refresh cannot run against a missing baseline.
3. Dispatch the `atomic-signals-inferrer` agent in silent mode:
   ```
   mode: silent
   first_run: false
   ```
   Stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md` after the agent completes.

`atomic signals stale` is content-based: it assembles the deterministic snapshot exactly as a scan would and compares it to the stored one, returning exit 1 only when they actually differ. A no-op regeneration that merely bumps file mtimes stays fresh; a real shift in the project map goes stale. Treat exit 1 as an unconditional trigger, not a hint.
</signals-refresh>{{- end}}
