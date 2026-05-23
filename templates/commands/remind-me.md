---
description: Schedule a reminder. Creates a reminder file and schedules it via cron (< 1h) or Routines (>= 1h). Degrades silently to file-only when scheduling tools are unavailable.
---

Schedule a reminder that fires at a future time.

Spec: `docs/spec/cron-workflow.md § /remind-me <prose>`

## Usage

```
/remind-me <prose>
```

`$ARGUMENTS` is free-form prose. Extract a duration and a body. All of these say the same thing:

```
/remind-me 3d fix this issue
/remind-me fix this issue in 3 days
/remind-me in about 3 days fix this issue
/remind-me fix this issue in 3d
/remind-me check if deploy finished in 30m
/remind-me 2026-06-01 revisit error handling in ingest
/remind-me fix that flaky test before Monday
```

Canonical duration units: `<n>s`, `<n>m`, `<n>h`, `<n>d`, `<n>w`, `<n>months`, or ISO date `YYYY-MM-DD`. Prose forms ("in 3 days", "about a week", "tomorrow morning") are accepted; round to the nearest canonical unit silently. Duration is optional — if absent, Claude prompts.

## Steps

### Step 1 — Parse arguments

Read `$ARGUMENTS` as natural-language prose. Infer two things:

- **`<duration>`** — the time offset before the reminder fires. May appear anywhere in the prose (commonly leading or trailing). May be a bare token (`3d`, `2h`), a phrase (`"in 3 days"`, `"about a week"`, `"tomorrow morning"`), or an ISO date.
- **`<body>`** — the user's intent, with the duration phrase removed. Strip glue prepositions (`in`, `after`, `within`, etc.) that only existed to connect the duration to the rest. Strip extra whitespace.

Round fuzzy phrases ("about 3 days", "around a week", "next Tuesday morning") to the nearest canonical unit without confirming.

Edge cases:

- **Arguments empty**: refuse and exit.

    ```
    usage: /remind-me <prose>
    examples:
      /remind-me 3d fix the deploy
      /remind-me fix the deploy in 3 days
      /remind-me follow up on the PR
    ```

- **Input is only a duration, body empty** (e.g. `/remind-me 3d`): refuse with the same usage hint.

- **Two or more candidate durations in the prose, no clear primary** (e.g. `"fix bug A by 3d, bug B by 1w"`): refuse and surface both candidates so the user picks. Reasonable input rarely contains two.

- **Duration missing, body present**: invoke `AskUserQuestion` to pick a duration:

    | Option | Label |
    |--------|-------|
    | `3d` | 3 days (Recommended) |
    | `1h` | 1 hour |
    | `1d` | 1 day |
    | `1w` | 1 week |

    Present `3d` first as the recommended default. If the user picks one, use it as `<duration>`. If the user declines, provides no answer, or provides an empty "Other" input → default to `3d`. The default is intentional: a forgotten reminder is worse than a slightly-wrong duration.

- **Both present**: proceed with the inferred `<duration>` and `<body>`.

### Step 2 — Compute due time

`due = now + <duration>`.

- For `d`, `w`, `months` durations and ISO dates: time component defaults to **09:00 local**.
- For `h`, `m`, `s` durations: use absolute time from now.

Format `due` as ISO 8601 UTC (`YYYY-MM-DDTHH:MM:SSZ`) for storage and scheduling.

### Step 3 — Pick transport

| Duration | Transport | Scheduling tool |
|----------|-----------|-----------------|
| `< 1h` | `cron` | `CronCreate` (session-only) |
| `>= 1h` | `routine` | `schedule` skill / Routines (cloud-durable) |

The `1h` boundary is inclusive: `1h` → `routine`. ISO dates are always `routine`.

### Step 4 — Store the reminder

- **Binary present** (`command -v atomic` exits 0):

    ```bash
    atomic reminder add --due "<iso>" --transport "<cron|routine>" "<body>"
    ```

    Capture the id from stdout (e.g. `r-7b21ef`).

- **Fallback** (binary absent): generate an id (`r-` + 6 random hex chars):

    ```bash
    id="r-$(openssl rand -hex 3)"
    mkdir -p .claude/.scratchpad/reminders/
    ```

    Write `.claude/.scratchpad/reminders/<YYYY-MM-DD>-<slug>.md` where `<slug>` is the first 4 words of `<body>` joined with hyphens, lowercased:

    ```
    ---
    id: r-<hex>
    created: <ISO 8601 timestamp>
    due: <ISO 8601 timestamp>
    transport: <cron|routine>
    ---

    <body>
    ```

### Step 5 — Schedule via chosen transport

In both cases, the cron/routine prompt is: `/follow-up due <id>`

- **`cron` transport**: call `CronCreate` with a one-shot trigger at `due` and the prompt above.

- **`routine` transport**: invoke the `schedule` skill with the same trigger time and prompt. Capture the routine id if returned.

**Transport unavailable → silent degradation.** If `CronCreate` is not loaded, or the `schedule` skill / Routines auth is not configured:

1. Rewrite `transport: none` in the reminder file's frontmatter via Bash `sed` (works for both binary and fallback paths):

    ```bash
    sed -i '' 's/^transport: .*/transport: none/' .claude/.scratchpad/reminders/<file>
    ```

2. Do **not** raise an error. Print:

    ```
    reminder stored. id: <id>. transport unavailable — will surface via session-start hook when past due (<human-readable due>).
    ```

    Then exit. The session-start hook will re-surface the reminder once past-due.

### Step 6 — Confirm to the user

On success:

```
reminder scheduled. id: <id>. fires: <human-readable when>. transport: <cron|routine>.
```

Example:

```
reminder scheduled. id: r-7b21ef. fires: Thu 29 May 09:00 local. transport: routine.
```

## Rules

- The reminder file is always written before scheduling is attempted. A failed or unavailable transport never loses the reminder body.
- No state other than `due:` is ever rewritten after creation (snooze/reschedule rewrite `due:` via `atomic reminder set-due`; see `/follow-up`).
- The `transport:` field in frontmatter reflects the actual scheduling outcome (`cron`, `routine`, or `none` on degradation).
- The transport-specific schedule id (cron id, routine id) is **not** stored in the file. Claude finds it at action time by matching the prompt content (`/follow-up due <id>`) via `CronList` or routine listing.
- Reminder storage is project-scoped (`.claude/.scratchpad/reminders/`). Gitignored. Persists across sessions on the same machine.
- The slug in the filename is cosmetic. The `id` field in frontmatter is the canonical key.
