# Spec: cron workflow (reminders)


Convenience commands that wrap Claude's scheduling primitives. Two transports are used based on duration: in-session crons (`CronCreate`) for short reminders, durable cloud Routines (via the `schedule` skill) for long ones. The `atomic` binary stores reminder bodies as markdown files. Claude owns scheduling state. Two slash commands: `/remind-me` to create, `/follow-up` to list and manage. The reminder file tracks a `due:` timestamp so the session-start hook can re-surface past-due items regardless of which transport was used (or whether either fired).


This spec depends on [`atomic-binary.md`](./atomic-binary.md). It is *not* a replacement for Claude's native cron — it is a thin opinionated layer over it.


## Model


A reminder is a markdown file with a `due:` timestamp. Its lifecycle:


- Created by `/remind-me <duration> <text>` → binary writes the file with `due: <now + duration>` and `transport: cron|routine|none`. Claude then schedules via the appropriate transport.
- Surfaces three ways: (a) cron/routine fires `/follow-up due <id>`, (b) session-start hook injects all past-due reminders into context, (c) user runs `/follow-up`.
- Snoozed by Claude rescheduling on the same transport with new time AND rewriting the `due:` field. File body unchanged.
- Done by `atomic reminder rm <id>` + cancel on the appropriate transport. File and schedule both gone.


Tracked fields: `id`, `created`, `due`, `transport`. No `status`. No `snooze_count`. No `acknowledged_at` (planned escape hatch if injection volume becomes annoying — see open follow-ups). The binary is dumb storage; Claude owns scheduling.


## Storage


- `.claude/.scratchpad/reminders/<YYYY-MM-DD>-<slug>.md`


Gitignored. Persists across sessions on the same machine. Never travels with the repo.


**Scope is per-project.** The scratchpad is project-local, so reminders created in project A do not surface in project B. The session-start hook reads `$cwd/.claude/.scratchpad/reminders/`. Users who want cross-project reminders today have to create them in each project; a future user-global storage path (`~/.atomic/reminders/`) is an open follow-up.


### Scratchpad cleanup interaction


The `/subagent-implementation` flow deletes `.claude/.scratchpad/<YYYY-MM-DD>-<task>/` on task completion. The `reminders/` subdirectory lives alongside, not inside, those task dirs. It is exempt from cleanup by path.


## Commands


| Command | Role |
|---------|------|
| `/remind-me <duration> <text>` | Create. Schedules a Claude cron. |
| `/follow-up` | List + manage. |


### `/remind-me [<duration>] <text>`


1. Parse `$ARGUMENTS`. Detect whether the first token is a valid duration (`<n>(s|m|h|d|w)`, `<n>(months)`, or ISO date `YYYY-MM-DD`).
    - **Text missing entirely**: refuse with usage hint. No reminder created.
    - **Duration missing, text present**: invoke `AskUserQuestion` with options `3d` (Recommended), `1h`, `1d`, `1w`. The recommended option is listed first per the global `CLAUDE.md` convention for `AskUserQuestion`. If the user picks one, use it. If the user declines, ignores, or "Other" with empty input → **default `3d`**. The default is intentional: a forgotten reminder is worse than a slightly-wrong duration.
    - **Both present**: proceed.
2. Compute `due = now + <duration>`. For day/week/month durations, the time component defaults to 09:00 local; for hour/second durations, use absolute time.
3. Detect binary: `command -v atomic`.
    - **Binary present**: `atomic reminder add --due "<iso>" --transport "<cron|routine|none>" "<text>"`. Capture the id from stdout. (Binary CLI must accept these new flags; see § Binary changes.)
    - **Fallback**: generate id (`r-<6 random hex>`). `mkdir -p` the reminders dir. `Write` the file with frontmatter (`id`, `created`, `due`, `transport`) and the body.
4. **Pick transport by duration:**

    | Duration | Transport | Tool used |
    |----------|-----------|-----------|
    | `< 1h` | `cron` | `CronCreate` (session-only, dies on session exit) |
    | `>= 1h` | `routine` | `schedule` skill / Routines (cloud-durable, 1h minimum) |

    The boundary is inclusive at `1h → routine` (matches the Routines minimum interval).

5. **Schedule via chosen transport.** Prompt: `/follow-up due <id>` in both cases.
    - `cron`: call `CronCreate` with one-shot trigger at `due`. Capture cron id.
    - `routine`: invoke the `schedule` skill with the same trigger and prompt. Capture the routine id if returned.
6. **Transport unavailable → degrade silently to file-only.** If the chosen transport's tool is missing (`CronCreate` not loaded, `schedule` skill not available, Routines auth not configured), set `transport: none` in the file, do not raise an error, and rely on the session-start hook to surface the reminder when it goes past-due. Print:

    ```
    reminder stored. id: <id>. transport unavailable — will surface via session-start hook when past due (<human-readable when>).
    ```

7. Print on success: `reminder scheduled. id: <id>. fires: <human-readable when>. transport: <cron|routine>.`

The `due:` field is set at create-time only and rewritten on snooze/reschedule. No other state is written back to the file. The transport-specific id (cron id, routine id) is *not* stored in the file — Claude looks it up by matching prompt content (`/follow-up due <id>`) via `CronList` / routine listing.


### `/follow-up`


Two invocation modes:


- **Bare**: `/follow-up` — interactive list of all reminders. User picks indices to act on.
- **Cron-fired**: `/follow-up due <id>` — Claude wakes with this prompt from a cron, surfaces the specific reminder, waits for response.


#### Bare flow


1. Detect binary.
    - **Binary present**: `atomic reminder list`. Output is indexed.
    - **Fallback**: `ls .claude/.scratchpad/reminders/*.md`, parse frontmatter via grep, build indexed list manually.
2. Print:

    ```
    Reminders (3)
      [1] r-7b21 — benchmark the new query plan (created 2 days ago)
      [2] r-3f9a — fix auth race in middleware (created 5 days ago)
      [3] r-1c7e — revisit error handling in ingest (created 1 week ago)
    ```

    Optionally, for each id, query `CronList` to enrich with "fires in X days" — best-effort, skip if `CronList` not available.

3. Prompt:

    ```
    Type one of:
      done <indices>             — mark done (delete file + cancel cron)
      snooze <index> <duration>  — reschedule cron; file unchanged
      reschedule <index> <when>  — same as snooze, explicit semantic
      show <index>               — print body
      none                       — exit

    Examples: done 1 3 | snooze 2 3d | reschedule 4 2026-06-01 | none
    ```

4. Parse selection. Validate indices. Re-prompt on invalid input.
5. Apply selected actions:
    - `done <indices>`:
        - Find the reminder's cron via `CronList` (match by prompt content containing the id).
        - `CronDelete` it.
        - `atomic reminder rm <id>` (or `Bash rm` in fallback).
    - `snooze <index> <duration>` and `reschedule <index> <when>`:
        - `CronList` to find the cron id.
        - `CronDelete` the old one.
        - `CronCreate` a new one for the new time, same prompt (`/follow-up due <id>`).
6. Final report.


#### Cron-fired flow (`/follow-up due <id>`)


Claude wakes with this prompt when a reminder's cron fires.


1. `atomic reminder show <id>` (or `Read` the file in fallback). If file missing → log "reminder <id> not found; cron may be stale" and exit.
2. Surface the body to the user with a brief preamble: "reminder fires: <id>".
3. Wait for user response. User options:
    - Acknowledge / mark done → `CronDelete` + `atomic reminder rm <id>`.
    - Snooze N → `CronCreate` again for `now + N`. File untouched.
    - Reschedule to specific time → `CronCreate` again with the new time.
    - Ignore (just talks about something else, or session ends without addressing) → no action; reminder persists; next session-start hook surfaces it again; it will not re-fire on its own (the one-shot cron already fired).


When ignored, the reminder is durable but no longer scheduled. It still shows up in `/follow-up` and the session-start hook. The user has to either snooze it back into the cron registry or `rm` it. That is intentional — no silent auto-reschedule. The user is responsible for the state.


## Hooks


### Session-start hook


Path: `.claude/hooks/session-start-reminders.sh`.


`atomic hooks install` does two things: (a) writes the script at the path above, and (b) registers it in `.claude/settings.json` under `hooks.SessionStart` so Claude Code actually fires it. Without the settings.json registration the script is dead — see [`atomic-binary.md`](./atomic-binary.md) for details. Manual fallback users must also edit `settings.json` themselves.


```bash
#!/usr/bin/env bash
if command -v atomic >/dev/null 2>&1; then
  atomic hooks session-start   # emits JSON with additionalContext (preferred form)
else
  # Fallback: emit plain markdown text. Claude Code treats non-JSON stdout
  # as plain context and prepends it. Crude but it works.
  shopt -s nullglob
  files=(.claude/.scratchpad/reminders/*.md)
  [[ ${#files[@]} -eq 0 ]] && exit 0
  now_epoch=$(date -u +%s)
  past_due=()
  for f in "${files[@]}"; do
    # Parse `due:` from frontmatter (ISO-8601). If absent (legacy file) → treat as past-due.
    due_line=$(grep -m1 '^due: ' "$f" | sed 's/^due: //')
    if [[ -z "$due_line" ]]; then
      past_due+=("$f")
      continue
    fi
    due_epoch=$(date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "$due_line" +%s 2>/dev/null || \
                date -u -d "$due_line" +%s 2>/dev/null)
    [[ -n "$due_epoch" && "$now_epoch" -ge "$due_epoch" ]] && past_due+=("$f")
  done
  [[ ${#past_due[@]} -eq 0 ]] && exit 0
  echo "## Pending reminders (${#past_due[@]} past due)"
  for f in "${past_due[@]}"; do
    echo "--- $(basename "$f") — should-remind-user: true ---"
    cat "$f"
  done
fi
```


**Past-due gating.** Both the binary path (`atomic hooks session-start`) and the fallback shell path filter to reminders where `now >= due`. Reminders not yet past-due are silent — they will surface when their transport fires (cron in this session, routine even across sessions), or at the next session-start after their due time passes.


**`should-remind-user: true` marker.** Each past-due reminder is prefixed with this marker in the injected context so Claude can recognize "this is a reminder the user wanted to hear about now" versus other contextual info. There is no `acknowledged_at` gate in v1; every session-start re-injects every past-due reminder until the user runs `/follow-up` → `done`. This is by design — see open follow-ups for the planned escape hatch if injection volume becomes noisy.


### How the hook output reaches Claude


Per the [Claude Code hooks contract](https://code.claude.com/docs/en/hooks): on exit 0, valid JSON stdout is parsed; the `additionalContext` field in `hookSpecificOutput` is injected as session context *without* showing in the transcript. Plain (non-JSON) stdout is added as context too but is also echoed in the transcript. The binary defaults to JSON for clean injection. The shell fallback emits plain text — still works, just chattier in the transcript. Both paths give Claude the same awareness of pending reminders at session open.


The binary path caps at 10 items. The fallback has no cap.


### No user-prompt hook


The cron firing surfaces reminders at their scheduled time; the session-start hook surfaces them at session open; `/follow-up` lets the user pull a list on demand. Re-injecting on every user prompt was over-design — three surfacing channels are plenty.


## Slash → binary mapping


| Slash command | Binary call | Scheduling tool |
|---------------|-------------|-----------------|
| `/remind-me 30m "ping"` (cron) | `atomic reminder add --due <iso> --transport cron "ping"` | `CronCreate` |
| `/remind-me 1w "fix x"` (routine) | `atomic reminder add --due <iso> --transport routine "fix x"` | `schedule` skill / Routines |
| `/remind-me "no duration"` | prompts for duration → defaults to `3d` → routine path | `schedule` skill / Routines |
| `/follow-up` → done | `atomic reminder rm <id>` | cancel on matching transport (`CronDelete` for cron, routine delete for routine) |
| `/follow-up` → snooze | `atomic reminder set-due <id> <iso>` | cancel old + reschedule on appropriate transport for new duration |
| `/follow-up` → reschedule | `atomic reminder set-due <id> <iso>` | cancel old + reschedule on appropriate transport for new duration |
| `/follow-up due <id>` (cron/routine fired) | `atomic reminder show <id>` | varies by user response |


**Binary changes required:**

- `atomic reminder add` gains `--due <iso>` and `--transport <cron|routine|none>` flags. Both written into frontmatter.
- `atomic reminder set-due <id> <iso>` is a new subcommand that rewrites the `due:` field in place. Used for snooze/reschedule.
- `atomic hooks session-start` filters output to reminders where `now >= due`. Reminders without a `due:` field (legacy) are treated as past-due to avoid losing them on the upgrade.


If a scheduling transport is unavailable, the slash commands degrade to file-only — the reminder still gets written with `transport: none`, and the session-start hook surfaces it once past due. No "scheduling unavailable" warning is treated as an error; it is the normal degraded path.


## Integration with `/atomic-setup`


Add to the audit table (the "binary on PATH" row may already be present from the signals-workflow integration — `/atomic-setup` deduplicates by convention name):


| Convention | Check |
|-----------|-------|
| `atomic` binary on PATH | `command -v atomic` |
| `.claude/hooks/session-start-reminders.sh` exists | `test -f` |
| `SessionStart` hook registered in `.claude/settings.json` | grep / parse for the registration |


Proposed actions when missing:


- Binary missing → print install instructions.
- Script + registration missing, binary present → action: `atomic hooks install`.
- Script + registration missing, binary missing → write the fallback script manually AND manually edit `settings.json` to register it.
- Script present but registration missing → action: `atomic hooks install` (idempotent — re-runs both write steps; existing script is overwritten with the canonical content, settings entry is added).


## Open follow-ups


- Snooze duration vocabulary — current spec covers `1d`, `1w`, `1m`, `2h`, ISO date. Add `tomorrow`, `next week` later if asked.
- Cron firing across timezones — initial version uses local time. Travelers may see schedules shift; revisit if it bites.
- Lookup-by-id ergonomics — the spec assumes the scheduled prompt `/follow-up due <id>` is findable by matching the prompt string (`CronList` for cron, routine listing for Routines). If either listing does not expose prompt content reliably, fall back to storing the transport id in the reminder file's frontmatter (`schedule_id` field) as a secondary index.
- Reminders summary cap (default 10 on session-start) — configurable later via memory.
- Cross-project / user-global reminders — current storage is project-scoped. Future: optional `--scope user` flag on `/remind-me` storing under `~/.atomic/reminders/`, surfaced by a user-level session-start hook.
- **Acknowledgement state.** v1 re-injects every past-due reminder at every session-start until the user runs `/follow-up` → `done`. If this becomes noisy, add an `acknowledged_at` frontmatter field set by `/follow-up` → `ack <index>` (new action) that suppresses hook re-injection for N hours/days. Deferred — wait for real-world annoyance before adding the gate.
- **Routine cancellation semantics.** `/follow-up` → `done`/`snooze` for a routine-transport reminder needs a way to delete the underlying routine. The `schedule` skill's deletion contract may differ from `CronDelete`. Verify on first implementation; if listing/deletion is awkward, store the routine id in frontmatter (matches the prior `schedule_id` follow-up).


## Success criteria


- `/remind-me 30m "ping"` creates a file with `id`, `created`, `due` (= now+30m), `transport: cron` AND schedules a `CronCreate` one-shot with prompt `/follow-up due <id>`.
- `/remind-me 1w "benchmark queries"` creates a file with `transport: routine` AND schedules a Routine via the `schedule` skill firing 7 days out.
- `/remind-me "no duration here"` prompts via `AskUserQuestion`, defaults to `3d` if user declines.
- `/follow-up` lists all reminders.
- When `due` passes:
    - cron-transport: fires `/follow-up due <id>` within the same session; if session died first, the next session-start hook injects it past-due.
    - routine-transport: fires `/follow-up due <id>` even across session boundaries.
- Session-start hook injects every reminder where `now >= due`, prefixed with `should-remind-user: true`. Not-yet-due reminders stay silent.
- Marking a reminder `done` via `/follow-up` deletes the file AND cancels the schedule on the appropriate transport.
- Snoozing a reminder rewrites the file's `due:` field and reschedules on the appropriate transport for the new duration (may change transport — e.g. snoozing a routine reminder by `30m` should rebook as cron).
- An ignored reminder remains durable; every session-start hook re-surfaces it until `done`.
- Without the binary, slash commands fall back to Bash file operations.
- Without `CronCreate` or `schedule`/Routines, files are still created with `transport: none`; no error printed; hook surfaces them past-due.


## Checkpoints


| CP | Lands |
|----|-------|
| C-1 | `/remind-me` command (binary + fallback + `CronCreate`) |
| C-2 | `/follow-up` command (binary + fallback + `CronCreate`/`CronDelete`/`CronList`) |
| C-3 | Session-start hook script installable via `atomic hooks install`; manual fallback documented |
| C-4 | `/atomic-setup` audit + propose flow updated |
| C-5 | `CLAUDE.md` + `CLAUDE.md` + `README.md` updated to document cron workflow |


## Implementation log


### shipped — 2026-05-17


Built across 5 implement→review iterations + 1 polish pass of `/subagent-implementation` on branch `cron-workflow`, then squash-merged to `main`.

**Squash commit on main:** `130427e` — `feat(cron-workflow): add /remind-me + /follow-up slash commands`.

**Pre-squash iteration commits (chronological, on `cron-workflow` before rebase):**

- CP-1 `/remind-me` slash command
- CP-2 `/follow-up` slash command (bare + cron-fired modes)
- CP-3 session-start hook: no-op — backend (`atomic hooks install/uninstall/session-start` + `atomic/internal/hooks/`) already shipped with full test coverage in prior work; no new artifact required
- CP-4 `/atomic-setup` audit + propose additions for the hook
- CP-5 README + CLAUDE.md + CLAUDE.md doc additions; install URL canonicalized to `github.com/damusix/atomic-claude/atomic/cmd/atomic@latest`
- Polish pass: closes 6 reviewer follow-ups (wording precision in remind-me, follow-up, atomic-setup, README)

Per-iteration SHAs were rewritten during the pre-merge rebase onto `main` and do not exist after squash. The squash diff captures all six.

**Out-of-scope work performed during this build:**

- Wrong install URL in `commands/atomic-setup.md` (`atomicclaudedev/atomic@latest`) discovered by CP-4 reviewer (F-5); corrected as part of CP-5's canonical-install-instructions roll-out across README, CLAUDE.md, atomic-setup.

**Unforeseens — surprises that emerged during implementation:**

- CP-3 dissolved into a no-op: the binary side of the cron workflow (reminder add/list/show/rm, hooks install/uninstall/session-start, JWCC-tolerant settings.json mutation, 14 hooks tests) was already complete before this loop began. The spec's checkpoints assumed binary work was pending; in practice only the frontend (slash commands + docs + `/atomic-setup` audit) needed to ship.
- During the pre-merge rebase onto `main`, a pre-existing local commit on `cron-workflow` (`b0a37f8` — `fix(bundlemirror): tolerate missing .claude/rules dir`) became obsolete. Main had since landed `e7eebeb` which moves bundle rules sourcing from `.claude/rules/` to repo-root `rules/`, making the tolerance check redundant. The commit was dropped during conflict resolution; main's version was taken verbatim.

**Deferred items still open:**

- None. All 7 follow-ups harvested during the loop were addressed or closed before squash: F-1, F-2, F-3, F-4, F-6, F-7 by the polish pass; F-5 by CP-5.


## Change log


### 2026-05-17 — Hybrid transport (cron + Routines), `due:` field, missing-duration default


**What changed:**

- Added a second scheduling transport: cloud Routines (via the `schedule` skill / Anthropic-hosted infrastructure) alongside the existing session-scoped `CronCreate` path. Transport is chosen by duration: `< 1h` → cron (short, session-likely-alive), `>= 1h` → routine (durable across sessions).
- Reminder frontmatter gains two fields: `due:` (ISO-8601 timestamp of when the reminder is past-due) and `transport:` (`cron` | `routine` | `none`). Both set at create-time; `due:` rewritten on snooze/reschedule.
- The session-start hook now filters output to reminders where `now >= due` and prefixes each with a `should-remind-user: true` marker. Reminders not yet past-due stay silent. Reminders without a `due:` field (legacy) are treated as past-due to avoid losing them on the upgrade.
- `/remind-me` no longer refuses when `<duration>` is missing. It invokes `AskUserQuestion` with options `3d` (Recommended) / `1h` / `1d` / `1w`. If the user declines or ignores, the duration defaults to `3d`.
- Transport unavailability is no longer an error. If `CronCreate` or `schedule`/Routines is missing, the file is written with `transport: none` and the hook is the only surface — silent degradation.
- Binary additions: `atomic reminder add` gains `--due <iso>` and `--transport <kind>` flags; new subcommand `atomic reminder set-due <id> <iso>` for snooze/reschedule; `atomic hooks session-start` filters by `due`.


**Why:**

Live testing of `atomic update` v1.0.0 → v1.1.0 surfaced that `CronCreate` advertises `durable: true` but is in fact session-only per the [scheduled-tasks docs](https://code.claude.com/docs/en/scheduled-tasks). Reminders scheduled for "next week" via the original spec would silently never auto-fire if the session exits — relying on the user happening to open a fresh session inside the project before the reminder felt stale. The hybrid transport + always-on past-due hook check makes the surfacing guarantee explicit and transport-independent.


**Superseded:**

- *Single-transport model.* Prior spec: "Created by `/remind-me <duration> <text>` → binary writes the file, Claude calls `CronCreate` with the duration." New: transport branches on duration; routines used for `>= 1h`.
- *No `due` field, no status tracking.* Prior spec: "No `status` field. No `due` field. No `snooze_count`." New: `due` and `transport` are tracked. `status`, `snooze_count`, `acknowledged_at` remain absent.
- *Refuse on missing duration.* Prior spec: "Parse `$ARGUMENTS` as `<duration> <text>`. Refuse if either is missing." New: missing duration prompts via `AskUserQuestion` and defaults to `3d`.
- *Scheduling-tools-unavailable warning is an error path.* Prior spec printed an explicit "scheduling tools unavailable" warning. New: silent file-only degradation with `transport: none`; the hook handles past-due surfacing.
- *Hook injects all reminders unconditionally.* Prior spec: hook script iterated every file in `reminders/` and dumped each one. New: hook filters by `due >= now`, prefixed with `should-remind-user: true`.


### 2026-05-17 — Correction: `AskUserQuestion` option order


**Correction:** The original v2 amendment listed the missing-duration prompt options as `1h` / `1d` / `3d` (Recommended) / `1w`. This violates the global `CLAUDE.md` convention that the recommended option must be listed first in `AskUserQuestion`. Corrected to `3d` (Recommended) / `1h` / `1d` / `1w`. How I know: reviewer of CP-3 caught the conflict between the spec body and the brief, which derived the order from the `CLAUDE.md` convention. The convention is the authoritative source for UI ordering across all atomic-claude slash commands; the spec body must align.

**What changed:** Reordered `AskUserQuestion` options in `/remind-me` so `3d` (Recommended) is first, then `1h`, `1d`, `1w`. Default behavior on decline (`3d`) is unchanged.

**Why:** Spec was inconsistent with a global convention. Aligning the spec body avoids a permanent contradiction that future implementers would have to re-discover.
