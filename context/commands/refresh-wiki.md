---
description: Manually refresh a repository wiki using the deterministic scan and synthesis pipeline.
---

## Repo refresh

Run `/refresh-wiki` in the target Git worktree. This command refreshes `docs/wiki/` only. For realm capture and bucket operations, use the `atomic-wiki` skill; it does not synthesize realm wiki prose.

1. Set `first_run` from the absence of `docs/wiki/scan.md`. Announce initialization if absent.
2. Run `atomic wiki init --scope repo`. Relay any `created <path>` line and tell the user they can edit the steering file.
3. Use the router wiring contract in `atomic-wiki/references/repo-output.md` to check root steering. If it is missing, ask before creating it. Pass the decision to `atomic-wiki-inferrer` with `mode: interactive` and `first_run: <bool>`. Wait for the repo pipeline in `atomic-wiki/references/repo.md` to finish.
   On `blocked` or a missing acceptance result, report the reason and any partial changes, then stop. Do not offer follow-ups, wire root instructions, or suggest committing a refresh.
4. For `## Concerns`, preview the follow-ups through the sink below and offer All, Pick, or Skip. Record only the selected items; the selection approves the preview.
5. Report the accepted scan, index, steering and card projections. Tell the user which repo-owned files to include when committing; exclude generated out-of-repo projections.

{{ template "followup-sink" . }}
