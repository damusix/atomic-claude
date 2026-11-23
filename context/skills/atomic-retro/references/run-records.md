# Record retro decisions


Write one JSON record under `~/.atomic/retro-runs/` for a run with user decisions. Use a unique, timestamp-sortable filename and include an RFC 3339 `run_ts`, the absolute project path in `project`, the review `scope`, and a `decisions` array. The `run_ts` field also supports the CLI's default extraction date.

Record a short `summary` and the `disposition` (`accept`, `modify`, `skip`, or `failed`) per decision. For a verified edit, record the absolute `target_file` and a short, distinctive `verify_phrase` present because of the change. For a verified deletion, record `target_file` and `verify_absence: true`. Omit verification evidence for failed or skipped writes. Never store secrets or raw transcripts. Do not prune older records as a side effect.
