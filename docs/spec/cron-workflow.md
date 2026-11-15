# Spec: cron workflow (reminders)


**Status: retired.** Atomic has no reminder feature: `/remind-me`, the `atomic reminder` verb, and session-start reminder injection do not exist. Build nothing from this file. The decision lives in [`workflow-preferences.md`](./workflow-preferences.md). The implementation log and change log below are history.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| - | None. The spec is retired. | - | - |


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


### shipped — 2026-05-17 (v2)


Built across 8 iterations of `/subagent-implementation` on branch `cron-workflow-v2` (worktree at `.worktrees/cron-workflow-v2/`). Implements the v2 amendment (Change log entry dated 2026-05-17 — Hybrid transport).

**Commits on branch (chronological — not squashed at log-write time):**

- `b292173` — feat(reminder): add due/transport frontmatter + set-due verb (CP-1 across iter 1+2)
- `bdbd298` — feat(hooks): filter session-start output by past-due reminders (CP-2 across iter 3+4)
- `6add4c1` — feat(commands): rewrite remind-me and follow-up for hybrid transport (CP-3 across iter 5+6+7; CP-4 bundle regen absorbed by pre-commit hook)
- `00917ba` — docs(cron-workflow): align spec body and reconcile degradation prose (Phase 3 polish — closed F-8 + F-9)

Ship verb to choose at handoff (`/squash-and-merge`, `/merge-to-main`, `/pr-only`). Not chosen by the orchestrator.

**Out-of-scope work performed during this build:**

- Orchestrator amended the spec body in `docs/spec/cron-workflow.md` to flip `AskUserQuestion` option order so `3d (Recommended)` is first (matches global `CLAUDE.md` convention). Logged as a **Correction** Change log entry dated 2026-05-17 alongside the Hybrid transport entry.
- F-8 (spec `/follow-up` bare-flow body still described v1 single-transport contract) was a pre-existing gap in the v2 amendment itself — caught by iter 7 reviewer and closed in Phase 3 polish, not by the original spec author.

**Unforeseens — surprises that emerged during implementation:**

- Pre-commit hook (`.githooks/pre-commit`) auto-regenerated the embedded bundle when CP-3 staged `commands/*.md` — collapsed CP-4 (bundle regen) into the CP-3 commit transparently. No manual `make bundle` was needed.
- The original v2 amendment's `## Slash → binary mapping` table was updated but the body of the `/follow-up` bare-flow section wasn't — a real spec drift inside a single amendment. Triggered F-8 fix.
- `AskUserQuestion` option-order convention (`Recommended` first) was not encoded in the v2 amendment; the brief overrode the spec body without flagging it as a spec amendment. Reviewer caught the divergence. Orchestrator corrected the spec body in-flight with a logged Correction entry.

**Dispositions at finalize (FOLLOWUPS triage):**

- **fix-now**: F-8 (spec body), F-9 (degradation prose conflict) — closed in commit `00917ba`.
- **defer** → promoted to `.claude/project/followups.md` with topic-prefixed ids:
    - `cron-workflow-v2-F-1` — `SetDue` double-read (🟡)
    - `cron-workflow-v2-F-2` — `orderedKVs` divergence risk (🔵)
    - `cron-workflow-v2-F-4` — `addReminderWithDue` helper conflates due+backdate (🔵)
    - `cron-workflow-v2-F-6` — `buildAdditionalContextFromRows` ≈ `buildBodyFromPastDue` (🔵)
    - `cron-workflow-v2-F-7` — `"1 reminders pending"` grammar (🔵)
- **drop**:
    - F-3 (`set-due` CLI test calls library, not `runReminder`) — accepted as repo convention; other `main_test.go` tests do the same.
    - F-5 (stderr log may print empty id) — defensive against a code path that does not produce empty `ID` today.

**Deferred items still open:** see the five `cron-workflow-v2-F-N` entries in `.claude/project/followups.md`. F-3 and F-5 dropped.


## Change log


### 2026-11-11 — Reminders removed

**What changed:** The spec is retired. The body is a status line and an empty checkpoint table; the model, storage, `/remind-me` and `/follow-up` flows, session-start hook, slash-to-binary mapping, `/setup-wiki` audit row, open follow-ups, success criteria, and checkpoints are gone.

**Why:** `docs/spec/workflow-preferences.md` drops the reminder feature: the `atomic reminder` verb, `/remind-me`, the `reminders` key in `atomic where --json`, and session-start reminder text no longer exist.

**Removed:** the whole reminder contract. Leftover reminder files stay on disk, unread.

### 2026-10-19 — Artifacts assume atomic is installed

**What changed:** `/remind-me` and `/follow-up` call the `atomic reminder` verbs directly, with no `command -v atomic` check and no Bash file-operation fallback. The `/setup-wiki` integration drops the binary-on-PATH audit row and the binary-missing actions. The session-start hook's shell fallback is hook code and is unchanged.

**Why:** Issue #298: artifacts ship inside and install through the `atomic` binary, so the binary is always present when they run. See `docs/spec/artifacts-mermaid-flows.md`.

**Superseded:** with the binary absent, the slash commands wrote, listed, rewrote, and deleted reminder files by hand in the legacy `.claude/.scratchpad/reminders/` directory, and `/setup-wiki` printed install instructions or registered the hook by hand.

### 2026-08-20 — Reminders relocate to the project-keyed state home

**What changed:** Storage moves from `.claude/.scratchpad/reminders/<YYYY-MM-DD>-<slug>.md` to `<reminders>/<YYYY-MM-DD>-<slug>.md`, where `<reminders>` is `atomic where --json`'s `.reminders` field (`~/.atomic/<project-key>/reminders/`); the binary-absent fallback is the legacy `.claude/.scratchpad/reminders/` directory and no other location. Scope changes from per-project to per-clone, shared across every worktree of the same clone — closing the cross-project/user-global open follow-up this spec previously named (a genuinely cross-*project* view is still out of scope; this closes the cross-*worktree* gap). The Bare-flow fallback listing and the session-start hook's no-binary shell fallback both still reference the legacy directory, since that branch only runs when the binary is absent.

**Why:** `docs/spec/serve-plans-page.md` relocates reminders (and session reports, and archived scratchpad bundles) to one project-keyed home shared across every worktree of a clone, resolved via the new `atomic where --json` fields.

**Superseded:** the repo-local `.claude/.scratchpad/reminders/` path as reminders' only storage location; the "scope is per-project" claim and its accompanying "a future user-global storage path is an open follow-up" framing.

### 2026-05-30 — Session-start hook inlined (no wrapper script)

**What changed:** The session-start hook is now registered as the inline command `atomic hooks session-start` in `.claude/settings.json`; the `.claude/hooks/session-start-reminders.sh` wrapper script no longer exists. Updated the "Session-start hook" section, the `/atomic-setup` audit convention table (dropped the `test -f` script check), and checkpoint C-3. Full contract: [`atomic-binary.md`](./atomic-binary.md).

**Why:** The wrapper was a pure passthrough with identical PATH semantics to the inline command. See the atomic-binary.md change log entry of the same date.

**Superseded:** The hook was previously a script at `.claude/hooks/session-start-reminders.sh` registered by path in `settings.json`; `/atomic-setup` verified the script existed via `test -f`.


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


### 2026-05-17 — Update /follow-up bare-flow and cron-fired-flow body for v2

**What changed:** Rewrote the `## Commands → /follow-up → Bare flow` and `## Commands → /follow-up → Cron-fired flow` sections to match the v2 multi-transport contract that `commands/follow-up.md` already implements. Bare flow: adds transport column in the indexed list; transport-aware `done` (cron via `CronDelete`, routine via `schedule` skill, none is no-op); snooze/reschedule recomputes transport on the 1h boundary, calls `atomic reminder set-due` for `due:`, uses Bash `sed` for `transport:`, references the canonical degradation message on unavailability. Cron-fired flow: reads `transport:` from file for cancel routing; done/snooze/reschedule all route by transport; snooze step references degradation message instead of "skip silently".

**Why:** Spec body diverged from implementation. F-8 and F-9 of cron-workflow-v2 follow-ups. Caught by iter 7 reviewer.

**Superseded:** The bare-flow body previously described the v1 single-transport contract (`CronList` → `CronDelete` → `CronCreate` only, no transport column in list). The cron-fired flow used `CronDelete`/`CronCreate` unconditionally and said "skip scheduling silently" on transport unavailability — contradicting the `## Degraded mode` section in the command file. All v2 transport-aware behavior was present in `commands/follow-up.md` but missing from the spec body.

### 2026-05-17 — Correction: `AskUserQuestion` option order


**Correction:** The original v2 amendment listed the missing-duration prompt options as `1h` / `1d` / `3d` (Recommended) / `1w`. This violates the global `CLAUDE.md` convention that the recommended option must be listed first in `AskUserQuestion`. Corrected to `3d` (Recommended) / `1h` / `1d` / `1w`. How I know: reviewer of CP-3 caught the conflict between the spec body and the brief, which derived the order from the `CLAUDE.md` convention. The convention is the authoritative source for UI ordering across all atomic-claude slash commands; the spec body must align.

**What changed:** Reordered `AskUserQuestion` options in `/remind-me` so `3d` (Recommended) is first, then `1h`, `1d`, `1w`. Default behavior on decline (`3d`) is unchanged.

**Why:** Spec was inconsistent with a global convention. Aligning the spec body avoids a permanent contradiction that future implementers would have to re-discover.


### 2026-05-17 — `/remind-me` accepts free-form prose; Claude infers duration


**What changed:** `/remind-me` no longer requires the duration to be the first token. It accepts any natural-language phrasing where Claude can infer a duration and a body — `"3d fix this issue"`, `"fix this issue in 3 days"`, `"in about 3 days fix this issue"`, and `"fix this issue in 3d"` all yield the same reminder. Strict regex-style tokenization is dropped from the spec; Claude is the parser. Fuzzy phrases (`"about 3 days"`, `"around a week"`) round to the nearest canonical unit silently. The body in storage is the user's intent with the duration phrase (including glue prepositions like `in`) stripped — not the raw input.

**Why:** Forcing duration-first phrasing made natural usage awkward. Users type the way English actually flows, which is duration-trailing more often than duration-leading. Two examples surfaced from real use immediately after CP-3 shipped, including `"fix this issue in 3 days"`. Since Claude executes the slash command (not a shell parser), strict token rules add no robustness — they only constrain the surface against what Claude could already infer.

**Superseded:** The v2 amendment said "Detect whether the first token is a valid duration". The Correction entry above only re-ordered the prompt options, not the parsing rule. This entry replaces the parsing rule itself.


### 2026-05-17 — Conform to validator rules


**What changed:** Migrated `## Checkpoints` table to the canonical 4-column header `| # | Checkpoint | Files/areas | Verifies |` — existing rows preserved; `Files/areas` backfilled from checkpoint descriptions where file paths were evident; `Verifies` left blank.

**Why:** `atomic validate spec` rule S5 flagged the file when the validator landed (CP-5 of `atomic-validate`).
