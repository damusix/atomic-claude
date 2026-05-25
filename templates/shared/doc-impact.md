{{define "doc-impact"}}<doc-impact>
Check whether the staged changes affect any indexed documentation surfaces.

**Step 1 — find the surfaces table.**

Look for a `## Documentation surfaces` section in the CLAUDE instructions already loaded in context (search `CLAUDE.md`, `claude.local.md`, or any `@`-included file). If no such section exists, print exactly:

```
no documentation surfaces indexed. run /documentation to set up.
```

Then skip the remaining steps — no error, no blocking.

**Step 2 — match staged diff against surfaces.**

Run `git diff --cached` and read the output. For each row in the `## Documentation surfaces` table, use LLM judgment to decide whether the staged diff touches the domain described in that row's `Covers` column. Path overlap, changed symbols, and changed domain terms all count. This is a judgment call within the current context window — no separate dispatch.

Ignore any surface entry where `impact_type` is `missing`. Maintenance mode never suggests new pages.

If no surfaces match the diff, proceed silently. No output. No prompt.

**Step 3 — walk matched surfaces.**

For each surface that matches (stale or incomplete), print:

```
doc surface: <path> — <reason the diff makes this stale>
proposed: <one sentence describing the targeted edit>

  [y] Yes    — update now
  [l] Later  — create a follow-up
  [r] Remind — schedule a reminder
  [s] Skip   — no action
```

Wait for the user's response per surface before continuing to the next.

**Step 4 — act on response.**

- **Yes** — invoke the `atomic-documentation` skill scoped to this surface. Pass the path, the diff context, and the proposed change as the prompt. The skill opens the file, makes the edit, and stages it with `git add <path>`. Do not proceed to the next surface until the skill completes.

- **Later** — run:

  ```
  atomic followups add \
    --id "doc-<slugified-path>-<short-hash>" \
    --title "update <path> — <reason>" \
    --severity nit \
    --origin "ship-verb doc-impact"
  ```

  Use the first 6 characters of the HEAD commit SHA as `<short-hash>`. If `atomic` binary is absent, print the follow-up details as plain text and continue.

- **Remind** — prompt: `When should I remind you? (e.g. "after the PR", "tomorrow", "end of week")`. Accept natural-language input and invoke `/remind-me <timing> update <path> — <reason>`. Continue without blocking if `CronCreate` is unavailable.

- **Skip** — no action, no record.

Run doc-impact before signals refresh so new or updated doc files are picked up by signals in the same pass.
</doc-impact>{{- end}}
