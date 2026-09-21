---
description: "Review and act on pending reminders. Bare invocation shows all reminders as an indexed list; cron-fired invocation (/follow-up due <id>) surfaces the specific reminder and waits for a response; /follow-up review lists stale project follow-up entries for per-item disposition (extend/close/promote/skip). Transport-aware: handles both session-scoped (cron) and durable schedules."
---

Handle pending reminders and project follow-up entries. Three modes: bare (`/follow-up`), cron-fired (`/follow-up due <id>`), and review (`/follow-up review`).

Spec: `docs/spec/cron-workflow.md § /follow-up`

`<reminders-dir>` below is `atomic where --json`'s `reminders` field when the `atomic` binary is present, else the `.claude/.scratchpad/reminders/` fallback dir used when it is absent — the legacy path `reminder.List` already unions and `atomic migrate` already relocates. Paths come from `atomic scratchpad` / `atomic where --json`; if what you find on disk does not match, run `atomic migrate --show-log` for the change history.

`<followups-dir>` below is the follow-ups directory under the selected repository state root. The `atomic followups` CLI owns the exact path — prefer its verbs over hand-constructing entry paths.

Transport vocabulary: the `cron` transport is the harness's session-scoped one-shot scheduler; the `routine` transport is the harness's durable scheduler (survives the session). Both are accessed through the harness's scheduling surface — the adapter owns the concrete tool names. When neither is available, the reminder degrades to `none` and surfaces through the session-start injection only.

## Mode detection

Inspect `$ARGUMENTS`:

- Empty → **bare flow** (interactive list, steps 1–6 below).
- `due <id>` → **cron-fired flow** (steps A–D below).
- `review` → **review flow** (steps R1–R6 below).

---

<workflow>

## Bare flow

### Step 1 — Build the indexed reminder list

Detect binary:

- **Binary present** (`command -v atomic` exits 0):

    ```bash
    atomic reminder list
    ```

    Output is indexed and includes the `transport` column. Capture it.

- **Fallback** (binary absent): list and parse files manually:

    ```bash
    ls .claude/.scratchpad/reminders/*.md 2>/dev/null
    ```

    For each file, extract frontmatter fields via grep:

    ```bash
    grep -m1 '^id:'        <file>   # → id
    grep -m1 '^created:'   <file>   # → created timestamp
    grep -m1 '^transport:' <file>   # → transport (cron|routine|none)
    ```

    Then read the body (lines after the closing `---`). Build an indexed list from those fields.

    If no files found: print `no reminders.` and exit.

### Step 2 — Optionally enrich with schedule info

For each reminder, attempt to find its live schedule entry:

- **`cron` transport**: list the harness's session schedules and search for an entry whose prompt contains `/follow-up due <id>`. If found, note the next-fire time as "fires in X" or "fires at <time>".
- **`routine` transport**: if the harness's durable-schedule listing is accessible, search for an entry with the matching prompt. Note fire time if found.
- **`none` transport**: no schedule lookup needed. The reminder will surface via the session-start injection only.

Skip enrichment silently if the listing surface is unavailable or returns no match — both are expected.

### Step 3 — Print the indexed list

<example>
```
Reminders (3)
  [1] r-7b21 — benchmark the new query plan (created 2 days ago) [routine]
  [2] r-3f9a — fix auth race in middleware (created 5 days ago, fires in 2 days) [cron]
  [3] r-1c7e — revisit error handling in ingest (created 1 week ago) [none]
```
</example>

Include the `[transport]` tag so the user knows how each reminder is scheduled.

### Step 4 — Prompt for action

<example>
```
Type one of:
  done <indices>             — mark done (delete file + cancel schedule)
  snooze <index> <duration>  — reschedule; may change transport
  reschedule <index> <when>  — same as snooze, explicit semantic
  show <index>               — print body
  none                       — exit

Examples: done 1 3 | snooze 2 3d | reschedule 2 next friday | none
```
</example>

The `<duration>` and `<when>` accept natural language — same inference rules as `/remind-me` Step 1. "next friday", "end of week", "3 days" all work.

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

1. Look up the reminder id and its `transport` from the indexed list.
2. **Cancel the schedule** based on transport:
    - **`cron`**: list the harness's session schedules. Find the entry whose prompt contains `/follow-up due <id>`. If found, cancel that schedule through the same surface.
    - **`routine`**: invoke the harness's durable-schedule surface with a delete intent for the entry whose prompt contains `/follow-up due <id>`. If the surface does not expose a deletion interface at this level, list durable schedules, identify the matching one by prompt content, and ask the user to confirm which one to cancel before deleting. Note: the durable-schedule cancellation contract may differ from the session scheduler's — see `docs/spec/cron-workflow.md § Open follow-ups`.
    - **`none`**: no schedule to cancel; skip to deletion.
3. Delete the reminder:
    - Binary present: `atomic reminder rm <id>`
    - Fallback: shell `rm <reminders-dir>/<file>`
4. Report: `[<index>] r-<id> — done.`

#### `snooze <index> <duration>` and `reschedule <index> <when>`

Both verbs accept natural-language time expressions — same inference rules as `/remind-me` Step 1 ("next friday", "end of week", "3 days", "2026-06-01" all work). Both follow the same execution steps (snooze implies relative; reschedule implies absolute — treated identically):

1. Look up the reminder id and current `transport` from the indexed list.
2. **Cancel the old schedule** based on current transport (same logic as `done` step 2 above).
3. **Compute new `due`** from `<duration>` or `<when>`. Apply the same time-component rules as `/remind-me` (09:00 local for day-granularity, absolute for hour-granularity).
4. **Pick new transport** based on new duration:
    - `< 1h` → `cron`
    - `>= 1h` → `routine`

    This may differ from the original transport (e.g. snoozing a routine reminder by `30m` switches to cron).

5. **Rewrite `due:` in the file** via:
    - Binary present: `atomic reminder set-due <id> <iso>`
    - Fallback: use shell `sed` to rewrite the `due:` line in the file.

6. **Schedule on the new transport**:
    - `cron`: create a one-shot session schedule at the new `due`, prompt `/follow-up due <id>`.
    - `routine`: invoke the harness's durable-schedule surface with the new trigger time and the same prompt.

7. **Rewrite `transport:` in the file** via shell `sed` to match the new transport (it may differ from the original):

    ```bash
    sed -i '' 's/^transport: .*/transport: <new-transport>/' <reminders-dir>/<file>
    ```

    Apply this regardless of whether the transport changed — it ensures the file stays consistent with the actual scheduling outcome.

8. **Transport unavailable → degradation**: if the new transport's scheduling surface is missing, rewrite `transport: none` in the file via the same shell `sed` pattern, then print the degradation message from `## Degraded mode`. The session-start injection will surface the reminder once past-due.

9. Report: `[<index>] r-<id> — rescheduled. fires: <human-readable when>. transport: <new-transport>.`

#### `show <index>`

Print the reminder body (from `atomic reminder show <id>` or by reading the file in fallback). Then re-display the prompt from step 4 so the user can take further action.

#### Final report

After all actions are applied:

```
Done:
  [1] r-7b21 — benchmark the new query plan — deleted.
  [3] r-1c7e — revisit error handling in ingest — deleted.

Rescheduled:
  [2] r-3f9a — fix auth race in middleware — fires: Mon 26 May 09:00 local. transport: routine.
```

---

## Cron-fired flow (`/follow-up due <id>`)

The session wakes with this prompt when a reminder's session or durable schedule fires.

### Step A — Load the reminder

- Binary present: `atomic reminder show <id>`
- Fallback: find `<reminders-dir>/*.md` where the `id:` frontmatter field matches `<id>`, then read that file.

Also read the `transport:` field from frontmatter — needed for cancel operations.

If the reminder is not found (file missing or `atomic reminder show` returns empty): print `reminder <id> not found; schedule entry may be stale` and exit.

### Step B — Surface the body

```
reminder fires: <id>

<body text>
```

### Step C — Wait for user response

After surfacing the body, wait. Detect intent from what the user types or does next:

- **Acknowledge / mark done**:
    1. Cancel the schedule on the matching transport:
        - `cron`: list the harness's session schedules → find the entry matching `/follow-up due <id>` → cancel it.
        - `routine`: invoke the harness's durable-schedule delete intent for the matching entry (see bare flow `done` step 2 for the fallback if deletion is not directly available).
        - `none`: no cancel needed.
    2. `atomic reminder rm <id>` (or shell `rm` in fallback).
    3. Print: `r-<id> — marked done.`

- **Snooze N**:
    1. Pick new transport from `N` (`< 1h` → cron, `>= 1h` → routine).
    2. Rewrite `due:` via `atomic reminder set-due <id> <iso>` (binary) or shell `sed` (fallback).
    3. Rewrite `transport:` in the file via shell `sed` to match the new transport (may differ from original):

        ```bash
        sed -i '' 's/^transport: .*/transport: <new-transport>/' <reminders-dir>/<file>
        ```

    4. Create a one-shot session schedule or invoke the harness's durable-schedule surface with `now + N`, prompt `/follow-up due <id>`. If unavailable, rewrite `transport: none` in the file via shell `sed`:

        ```bash
        sed -i '' 's/^transport: .*/transport: none/' <reminders-dir>/<file>
        ```

        Then print the degradation message from `## Degraded mode` before continuing.
    5. Print: `r-<id> — rescheduled. fires: <human-readable when>. transport: <new-transport>.`

- **Reschedule to specific time**: same steps as snooze but with the specified absolute time.

- **Ignore** (user addresses something else, or the session ends without explicit action): take no action. The reminder persists in storage and continues to appear in bare `/follow-up` and the session-start injection. It will not re-fire on its own — the one-shot schedule already fired. The user must snooze it back into the registry or mark it done. This is intentional; no silent auto-reschedule.

### Step D — Confirm action taken (explicit actions only)

- Done: `r-<id> — marked done.`
- Snoozed/rescheduled: `r-<id> — rescheduled. fires: <human-readable when>. transport: <new-transport>.`

---

## Review flow (`/follow-up review`)

Reviews stale project follow-up entries (past `review_by`) with per-item disposition. `kind: plan` entries are excluded from staleness review — plans are a backlog, not findings going cold; they surface via INDEX, not via the stale nag.

### Step R1 — Fetch stale entries

```bash
atomic followups list --stale --json
```

Parse the JSON array. If empty: print `no stale follow-ups; <N> open, all within TTL` (get N from `atomic followups list --json` | count) and exit.

### Step R2 — Print numbered list (axiom 4)

```
Stale follow-ups (N)
  [1] <id> — <title> (<age>d old, severity: <severity>)
  [2] <id> — <title> (<age>d old, severity: <severity>)
  ...
```

Print one line per stale entry. Include id, title, severity, and age in days.

### Step R3 — Per-item disposition

For each stale entry (in order), ask the user:

```
[<N>] <id> — <title>
Disposition: extend | close | promote | skip
```

Ask the user in a batched question block with choices `["extend", "close", "promote", "skip"]` — binary/small-choice decision per axiom 4 guidance.

**Batch shortcuts.** If the user types text instead of picking an option, accept these accelerators:

- `extend all` / `extend rest` — extend this and all remaining entries without further prompting.
- `skip all` / `skip rest` — skip this and all remaining entries.
- `done` / `stop` — stop reviewing, skip all remaining entries.

These avoid fatigue when many entries are stale. Only non-destructive verbs (`extend`, `skip`) support batch — `close` and `promote` always require per-item confirmation per axiom 3.

For `close` and `promote` (destructive — axiom 3): confirm explicitly before acting:

- `close`: ask the user in a batched question block — "Close <id>? This writes to CLOSED.md and deletes the entry file. Yes / No"
  - If Yes: optionally prompt for a reason — a batched question block: "Reason (optional — leave blank to skip):"
- `promote`: ask the user in a batched question block — "Promote <id> to a GitHub issue? This creates a gh issue and then closes the entry. Yes / No"

`extend` and `skip` require no confirm — they are non-destructive.

### Step R4 — Apply dispositions

**`extend`**: rewrite `review_by` in the entry frontmatter to `today + ttl_days`.

- Read TTL from user memory key `feedback_followups_ttl` (format `Nd`). Default: `60d`.
- Compute new date: today + N days, formatted `YYYY-MM-DD`.
- Edit the frontmatter line `review_by: <old>` to `review_by: <new>` via shell `sed`:

    ```bash
    sed -i '' "s/^review_by: .*/review_by: $(date -v+60d +%Y-%m-%d)/" <followups-dir>/<id>.md
    ```

    Substitute the correct N in the `+Nd` expression from the TTL value.

- Print: `[<N>] <id> — review_by extended to <new-date>.`

**`close`**: run:

```bash
atomic followups close <id>          # no reason
atomic followups close <id> --reason "<r>"   # if user provided one
```

Print: `[<N>] <id> — closed.`

**`promote`**: create a GitHub issue, then close on success.

1. Run:

    ```bash
    gh issue create --title "<title>" --body "Origin: <origin>\n\nSee <followups-dir>/<id>.md"
    ```

2. Capture the issue URL from stdout. On success:

    ```bash
    atomic followups close <id> --reason "promoted to gh issue #<N>"
    ```

    Print: `[<N>] <id> — promoted to <issue-url> and closed.`

3. On `gh issue create` failure: print `WARN: gh issue create failed for <id>; entry left open. Error: <stderr>.` Do NOT close the entry.

**`skip`**: no-op. Print: `[<N>] <id> — skipped.`

### Step R5 — Regenerate INDEX

INDEX regeneration is automatic after each `atomic followups close` call (the CLI runs render internally). If only `extend` or `skip` dispositions were applied, run:

```bash
atomic followups render
```

### Step R6 — Summary

After all dispositions:

```
Review complete.
  Extended: <id-list or "none">
  Closed:   <id-list or "none">
  Promoted: <id-list or "none">
  Skipped:  <id-list or "none">
```

---

</workflow>

## Degraded mode

If the harness's scheduling surface (session schedules or durable schedules) is unavailable for a given step:

- File operations still proceed (delete or `set-due` rewrite).
- Skip the schedule cancel/create call (do not raise an error on the missing tool).
- After completing the action, print:

    ```
    scheduling tools unavailable — file updated; reminder won't auto-fire; review via /follow-up.
    ```

---

<constraints>

## Rules

- Never delete a reminder file without the user explicitly selecting `done` or acknowledging in the cron-fired flow.
- Never auto-reschedule. The user drives all scheduling decisions.
- The cron/routine lookup key is the prompt content containing the reminder id (`/follow-up due <id>`). Match this string when searching session-schedule or durable-schedule listings.
- Transport changes on snooze/reschedule are intentional: a reminder originally set for `1w` snoozed by `30m` correctly moves from routine to cron.
- `atomic reminder set-due <id> <iso>` is the canonical way to rewrite `due:`. Shell `sed` is the fallback when the binary is absent.
- Reminder storage is project-scoped, at the `reminders` path `atomic where --json` reports. Persists across sessions and worktrees of the same clone. Paths come from `atomic scratchpad` / `atomic where --json`; if what you find on disk does not match, run `atomic migrate --show-log` for the change history.
- The `id` frontmatter field is the canonical key. Filenames are cosmetic.

</constraints>
