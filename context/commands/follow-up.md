---
description: Record a project follow-up or review stale entries.
---

Use `/follow-up <text>` to record one item, or `/follow-up review` to triage stale entries. With no text, show those two forms.

For a new item, draft a short title and body from the user's text. Include the origin and any named file. Mark proposed work as `plan` and other items as `finding`. Use the preview below for the single approval.

{{ template "followup-sink" . }}

## Review stale follow-ups


Run `atomic followups list --stale --json`. Exclude `kind: plan`. Show numbered IDs, titles, ages, and severities. If empty, report the open count from `atomic followups list --json` and stop.

| Choice | Action |
|---|---|
| Extend | Set `review_by` to today plus the TTL from `feedback_followups_ttl` (default `60d`). Locate the entry via `atomic followups path`. |
| Close | Confirm per item, then run `atomic followups close <id>` with optional `--reason`. |
| Promote | Resolve the issue tracker and apply the privacy rules above. Confirm per item. Skip if there is no hosted tracker; close the local entry only after creating a hosted issue. |
| Skip | Leave the entry unchanged. |

Accept `extend all` or `extend rest`, `skip all` or `skip rest`, and `done` or `stop`. Never batch close or promote. Run `atomic followups render` after any extension. Summarize entries by disposition.
