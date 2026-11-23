# Check earlier retro decisions


Read this only when the user asks whether earlier approved changes are still in place. Review the requested decisions in `~/.atomic/retro-runs/`. Use the current project unless the user requests a different project or a cross-project review. Apply any date range independently. Read legacy records without a project only when their targets can be attributed to the requested scope. You do not need a history export.

Use the latest verified decision when a later one supersedes it. Check only `accept` or `modify` decisions with a target and recorded verification evidence. Do not count skipped or failed changes as drift.

| What the target shows | Report |
|---|---|
| The recorded text is present, or the approved deletion is still absent | Still in place |
| The recorded text changed, or a deleted file reappeared | Changed; explain whether the intent appears to remain |
| The target moved, is missing, or cannot be verified | Unverified; name the reason |

A literal mismatch can be intentional. Report it with the original decision and current evidence. Recommend a change only if the intent no longer holds, and never apply it without approval.
