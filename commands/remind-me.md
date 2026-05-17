---
description: Schedule a reminder. Creates a reminder file and sets a one-shot Claude cron to surface it later via /follow-up.
---

Schedule a reminder that fires at a future time.

## Usage

```
/remind-me <duration> <text>
```

Examples:

```
/remind-me 1w benchmark the new query plan
/remind-me 3d follow up on auth PR review
/remind-me 2h check if deploy finished
/remind-me 2026-06-01 revisit error handling in ingest
```

Duration formats: `2h`, `1d`, `3d`, `1w`, `2w`, `1m`, or an ISO date (`YYYY-MM-DD`). For day/week/month durations, the time component defaults to 09:00 local.

## Steps

1. **Parse arguments.** Split `$ARGUMENTS` on the first space: `<duration>` = first token, `<text>` = remainder. If either is missing, refuse:

    ```
    usage: /remind-me <duration> <text>
    examples: /remind-me 1w fix auth race | /remind-me 3d check deploy
    ```

2. **Store the reminder.**

    - **Binary present** (`command -v atomic` exits 0):

        ```bash
        atomic reminder add "<text>"
        ```

        Capture the id printed to stdout (e.g. `r-7b21ef`).

    - **Fallback** (binary absent): generate an id (`r-` + 6 random hex chars via `openssl rand -hex 3`). Create the file:

        ```bash
        mkdir -p .claude/.scratchpad/reminders/
        ```

        Write `.claude/.scratchpad/reminders/<YYYY-MM-DD>-<slug>.md` with frontmatter and body:

        ```
        ---
        id: r-<hex>
        created: <ISO 8601 timestamp>
        ---

        <text>
        ```

        where `<slug>` is the first 4 words of `<text>` joined with hyphens, lowercased.

3. **Schedule via `CronCreate`.** Call `CronCreate` with:

    - trigger: one-shot at `now + <duration>` (resolve the time component to 09:00 local for day/week/month durations; use absolute time for `h`-suffix durations and ISO dates)
    - prompt: `/follow-up due <id>`

    Capture the returned cron id. If `CronCreate` is unavailable (tool not present in this environment), skip steps 3–4; print the fallback message instead.

4. **Confirm to the user.**

    ```
    reminder scheduled. id: <id>. fires: <human-readable when>.
    ```

    Example:

    ```
    reminder scheduled. id: r-7b21ef. fires: Thu 29 May 09:00 local.
    ```

5. **`CronCreate` unavailable fallback.** If `CronCreate` is not available, the reminder file is still written (step 2 always runs). Print:

    ```
    scheduling tools unavailable — reminder stored but won't auto-fire; review via /follow-up.
    ```

## Rules

- The reminder file is written before `CronCreate` is attempted. A failed or unavailable `CronCreate` never loses the reminder body.
- No state is written back to the reminder file after creation. The cron lives in Claude's cron registry, keyed by its prompt content (`/follow-up due <id>`).
- Reminder storage is project-scoped (`.claude/.scratchpad/reminders/`). Gitignored. Persists across sessions on the same machine.
- The slug in the filename is cosmetic. The `id` field in frontmatter is the canonical key.
