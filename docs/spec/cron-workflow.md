# Spec: cron workflow (reminders)


Convenience commands that wrap Claude's built-in scheduled-tasks feature (`CronCreate` / `CronList` / `CronDelete`). The `atomic` binary stores reminder bodies as markdown files. Claude owns all scheduling state. Two slash commands: `/remind-me` to create, `/follow-up` to list and manage. No status tracking, no due-date tracking, no snooze counting — a reminder exists or it doesn't.


This spec depends on [`atomic-binary.md`](./atomic-binary.md). It is *not* a replacement for Claude's native cron — it is a thin opinionated layer over it.


## Model


A reminder is just a markdown file. Its lifecycle:


- Created by `/remind-me <duration> <text>` → binary writes the file, Claude calls `CronCreate` with the duration.
- Surfaces when the cron fires (Claude wakes with `/follow-up due <id>`) or when the user runs `/follow-up`.
- Snoozed by Claude calling `CronCreate` again with new time. File unchanged.
- Done by `atomic reminder rm <id>` + `CronDelete`. File and cron both gone.


No `status` field. No `due` field. No `snooze_count`. If the user wants to know when something is scheduled, they ask Claude (which checks `CronList`). The binary is dumb storage.


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


### `/remind-me <duration> <text>`


1. Parse `$ARGUMENTS` as `<duration> <text>`. Refuse if either is missing. Print usage hint.
2. Detect binary: `command -v atomic`.
    - **Binary present**: `atomic reminder add "<text>"`. Capture the id from stdout.
    - **Fallback**: generate an id (`r-<6 random hex>`). `mkdir -p` the reminders dir. `Write` a file with frontmatter (`id`, `created`) and the body.
3. Schedule the wake-up via `CronCreate`:
    - Trigger: one-shot at `now + <duration>` (with time component defaulting to 09:00 local if duration is in days/weeks).
    - Prompt: `/follow-up due <id>`.
    - Capture the returned cron id.
4. Print: `reminder scheduled. id: <id>. fires: <human-readable when>.`


If `CronCreate` is unavailable: create the file anyway. Print: `scheduling tools unavailable — reminder stored but won't auto-fire; review via /follow-up.`


No state is written back to the reminder file. The cron lives in Claude's cron registry, addressable by its prompt content (which includes the reminder id) via `CronList`.


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
  echo "## Pending reminders (${#files[@]})"
  for f in "${files[@]}"; do
    echo "--- $(basename "$f") ---"
    cat "$f"
  done
fi
```


### How the hook output reaches Claude


Per the [Claude Code hooks contract](https://code.claude.com/docs/en/hooks): on exit 0, valid JSON stdout is parsed; the `additionalContext` field in `hookSpecificOutput` is injected as session context *without* showing in the transcript. Plain (non-JSON) stdout is added as context too but is also echoed in the transcript. The binary defaults to JSON for clean injection. The shell fallback emits plain text — still works, just chattier in the transcript. Both paths give Claude the same awareness of pending reminders at session open.


The binary path caps at 10 items. The fallback has no cap.


### No user-prompt hook


The cron firing surfaces reminders at their scheduled time; the session-start hook surfaces them at session open; `/follow-up` lets the user pull a list on demand. Re-injecting on every user prompt was over-design — three surfacing channels are plenty.


## Slash → binary mapping


| Slash command | Binary call | Cron tool |
|---------------|-------------|-----------|
| `/remind-me 1w "fix x"` | `atomic reminder add "fix x"` | `CronCreate` once at create-time |
| `/follow-up` → done | `atomic reminder rm <id>` | `CronDelete` (looked up via `CronList`) |
| `/follow-up` → snooze | (no binary call) | `CronDelete` old + `CronCreate` new |
| `/follow-up` → reschedule | (no binary call) | `CronDelete` old + `CronCreate` new |
| `/follow-up due <id>` (cron-fired) | `atomic reminder show <id>` | varies by user response |


If `CronCreate` / `CronList` / `CronDelete` tools are unavailable (older Claude Code, restricted environment), the slash commands degrade to file-only mode and print: "scheduling tools unavailable — reminders stored but not auto-fired; review via `/follow-up`."


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
- `CronList` lookup-by-id ergonomics — the spec assumes a cron created with prompt `/follow-up due <id>` is findable by matching the prompt string. If `CronList` does not expose prompt content reliably, fall back to storing the cron id in the reminder file's frontmatter (`cron_id` field) as a secondary index.
- Reminders summary cap (default 10 on session-start) — configurable later via memory.
- Cross-project / user-global reminders — current storage is project-scoped. Future: optional `--scope user` flag on `/remind-me` storing under `~/.atomic/reminders/`, surfaced by a user-level session-start hook.


## Success criteria


- `/remind-me 1w "benchmark queries"` creates a file with `id` and `created` only AND schedules a Claude cron with prompt `/follow-up due <id>` firing 7 days out.
- `/follow-up` lists the new reminder.
- After 7 days, the cron fires and Claude surfaces the reminder body.
- Marking a reminder `done` via `/follow-up` deletes the file AND cancels the cron.
- Snoozing a reminder via `/follow-up` reschedules its cron without touching the file.
- An ignored reminder remains durable (file present, no cron); next session-start hook surfaces it; user must explicitly snooze or `rm`.
- Without the binary, both slash commands still create and list files via Bash fallback.
- Without `CronCreate`/`CronDelete` tools, files are still created/managed; the user gets an explicit "no scheduling" warning.


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
