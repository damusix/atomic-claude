---
description: Review and act on pending reminders. Bare invocation shows all reminders as an indexed list; cron-fired invocation (/follow-up due <id>) surfaces the specific reminder and waits for a response.
---

Handle pending reminders. Two modes: bare (`/follow-up`) and cron-fired (`/follow-up due <id>`).

## Mode detection

Inspect `$ARGUMENTS`:

- Empty → **bare flow** (interactive list, steps 1–6 below).
- `due <id>` → **cron-fired flow** (steps A–D below).

---

## Bare flow

### Step 1 — Build the indexed reminder list

Detect binary:

- **Binary present** (`command -v atomic` exits 0):

    ```bash
    atomic reminder list
    ```

    Output is already indexed. Capture it.

- **Fallback** (binary absent): list and parse files manually:

    ```bash
    ls .claude/.scratchpad/reminders/*.md 2>/dev/null
    ```

    For each file, extract frontmatter fields via grep:

    ```bash
    grep -m1 '^id:' <file>       # → id
    grep -m1 '^created:' <file>  # → created timestamp
    ```

    Then read the body (lines after the closing `---`). Build an indexed list from those fields.

    If no files found: print `no reminders.` and exit.

### Step 2 — Optionally enrich with cron schedule

For each reminder id, call `CronList` and search results for an entry whose prompt contains the id (pattern: `/follow-up due <id>`). If found, note the next-fire time as "fires in X days" (or "fires at <time>"). Append to the row if available. Skip this enrichment silently if `CronList` is unavailable or if it runs but returns no match for the id — both cases are intentional.

### Step 3 — Print the indexed list

```
Reminders (N)
  [1] r-7b21 — benchmark the new query plan (created 2 days ago)
  [2] r-3f9a — fix auth race in middleware (created 5 days ago, fires in 2 days)
  [3] r-1c7e — revisit error handling in ingest (created 1 week ago)
```

### Step 4 — Prompt for action

```
Type one of:
  done <indices>             — mark done (delete file + cancel cron)
  snooze <index> <duration>  — reschedule cron; file unchanged
  reschedule <index> <when>  — same as snooze, explicit semantic
  show <index>               — print body
  none                       — exit

Examples: done 1 3 | snooze 2 3d | reschedule 4 2026-06-01 | none
```

### Step 5 — Parse and validate selection

Accept typed input. Parse the verb and operands:

- `done 1 3` → mark indices 1 and 3 done.
- `snooze 2 3d` → snooze index 2 by 3 days.
- `reschedule 4 2026-06-01` → reschedule index 4 to that date.
- `show 2` → print the body of index 2.
- `none` → exit, no action.

Validate every index against the printed list. If any index is out of range: print `invalid index <N>. retry your selection.` and re-prompt. If the verb is unrecognized: print `unrecognized verb. retry your selection.` and re-prompt. Repeat until input is valid or user types `none`.

### Step 6 — Apply actions

#### `done <indices>`

For each index in the selection:

1. Look up the reminder id from the indexed list.
2. Call `CronList`. Find the entry whose prompt contains `/follow-up due <id>`. If found, call `CronDelete` on that cron id.
3. Delete the reminder:
    - Binary present: `atomic reminder rm <id>`
    - Fallback: `Bash rm .claude/.scratchpad/reminders/<file>`
4. Report: `[<index>] r-<id> — done.`

#### `snooze <index> <duration>` and `reschedule <index> <when>`

Both verbs follow the same steps (snooze implies a relative duration; reschedule implies an absolute time — treat them identically in execution):

1. Look up the reminder id.
2. Call `CronList`. Find the cron whose prompt contains `/follow-up due <id>`. If found, call `CronDelete` on the old cron id.
3. Call `CronCreate` with:
    - trigger: `now + <duration>` for snooze, or the specified `<when>` for reschedule (time-component resolution follows the duration grammar described in spec §Open follow-ups; defaults to 09:00 local for day-granularity inputs)
    - prompt: `/follow-up due <id>`
4. Report: `[<index>] r-<id> — rescheduled. fires: <human-readable when>.`

#### `show <index>`

Print the reminder body (from `atomic reminder show <id>` or by reading the file in fallback). Then re-display the prompt from step 4 so the user can take further action.

#### Final report

After all actions are applied:

```
Done:
  ✓ [1] r-7b21 — benchmark the new query plan — deleted.
  ✓ [3] r-1c7e — revisit error handling in ingest — deleted.

Rescheduled:
  ✓ [2] r-3f9a — fix auth race in middleware — fires: Mon 26 May 09:00 local.
```

---

## Cron-fired flow (`/follow-up due <id>`)

Claude wakes with this prompt when a reminder's cron fires.

### Step A — Load the reminder

- Binary present: `atomic reminder show <id>`
- Fallback: find `.claude/.scratchpad/reminders/*.md` where the `id:` frontmatter field matches `<id>`, then `Read` that file.

If the reminder is not found (file missing or `atomic reminder show` returns empty): print `reminder <id> not found; cron may be stale` and exit.

### Step B — Surface the body

```
reminder fires: <id>

<body text>
```

### Step C — Wait for user response

After surfacing the body, wait. The user has four options — detect their intent from what they type or do next:

- **Acknowledge / mark done**: call `CronDelete` (find the cron via `CronList` matching `/follow-up due <id>`), then `atomic reminder rm <id>` (or `Bash rm` in fallback).
- **Snooze N**: call `CronCreate` again for `now + N`, same prompt (`/follow-up due <id>`). File untouched.
- **Reschedule to specific time**: call `CronCreate` with the new time, same prompt. File untouched.
- **Ignore** (user addresses something else, or the session ends without an explicit action): take no action. The reminder persists in storage and continues to appear in bare `/follow-up` and the session-start hook. It will not re-fire on its own — the one-shot cron already fired. The user must snooze it back into the cron registry or mark it done. This is intentional; no silent auto-reschedule.

### Step D — Confirm action taken

After any explicit action:

- Done: `r-<id> — marked done.`
- Snoozed/rescheduled: `r-<id> — rescheduled. fires: <human-readable when>.`

---

## Degraded mode

If `CronList`, `CronDelete`, or `CronCreate` are unavailable in this environment, file operations still proceed. After completing any action that would have involved scheduling tools, print:

```
scheduling tools unavailable — reminders stored but not auto-fired; review via /follow-up.
```

---

## Rules

- Never delete a reminder file without the user explicitly selecting `done` or acknowledging in the cron-fired flow.
- Never auto-reschedule. The user drives all scheduling decisions.
- The cron lookup key is the prompt content containing the reminder id (`/follow-up due <id>`). Match this string when searching `CronList` results.
- Reminder storage is project-scoped (`.claude/.scratchpad/reminders/`). Gitignored.
- The `id` frontmatter field is the canonical key. Filenames are cosmetic.
