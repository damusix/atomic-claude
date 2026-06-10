# config

## What it does

User config, state directory, session hooks, reminders, follow-ups, and git state management. Everything that persists user preferences or project-local operational state across sessions. [`atomic/internal/config`](../../../atomic/internal/config) owns the TOML schema; [`atomic/internal/followups`](../../../atomic/internal/followups) owns the per-entry folder; [`atomic/internal/hooks`](../../../atomic/internal/hooks) installs the session-start hook.

## Artifacts

**Follow-ups and git state:**

- [`commands/follow-up.md`](../../../commands/follow-up.md) — `/follow-up [due <id> | review]`. Reviews pending follow-ups. `/follow-up review` triages stale [`.claude/project/followups/`](../followups) entries with per-item `extend|close|promote|skip`.
- [`commands/git-cleanup.md`](../../../commands/git-cleanup.md) — `/git-cleanup [<name>]` scans stale git state via `atomic-git-scout`. Presents indexed report; confirms before deleting anything. Plain-text indexed selection (axiom 4).
- [`agents/atomic-git-scout.md`](../../../agents/atomic-git-scout.md) — read-only scanner. Classifies cleanup candidates (`remove`/`delete`/`prune`/`ask`/`flag`/`skip`). Returns indexed report. Never mutates state.

**Reminders and scheduling:**

- [`commands/remind-me.md`](../../../commands/remind-me.md) — `/remind-me <duration> <text>` schedules reminder via cron. Degrades to file-only without `CronCreate`.
- [`commands/watch-ci.md`](../../../commands/watch-ci.md) — `/watch-ci` spawns background `atomic-haiku` to watch CI. Provider auto-detected from signals.
- [`agents/atomic-haiku.md`](../../../agents/atomic-haiku.md) — generic background runner (haiku). Polling, status checks, log scraping. Read-only by default.

**CLAUDE.md merge:**

- [`commands/atomic-claude-merge.md`](../../../commands/atomic-claude-merge.md) — `/atomic-claude-merge` merges `~/.claude/.atomic/proposed/CLAUDE.md` into live `~/.claude/CLAUDE.md`. Preserves content outside `<atomic>` tags (user-owned); replaces content inside `<atomic>...</atomic>` tags (atomic-owned).
- [`agents/atomic-claude-merger.md`](../../../agents/atomic-claude-merger.md) — the merge agent. Reads ownership boundary via `<atomic>...</atomic>` tags. Backs up prior [`CLAUDE.md`](../../../CLAUDE.md) to `~/.claude/.atomic/backups/<ts>/` before writing. Writes `~/.claude/CLAUDE.md.atomic-merged` (never modifies live `~/.claude/CLAUDE.md` directly).

## CLI code

- [`atomic/internal/config/config.go`](../../../atomic/internal/config/config.go) — TOML-backed user config. Load/validate/persist. v1 schema: `output.signals.max_depth` (int, default 3), `update.run_doctor` (bool, default `true`). `output.intensity` was removed — the schema no longer contains it. Atomic write via `os.Rename`. Levenshtein typo-suggestion on unknown key names. Raw-map presence check distinguishes absent `update.run_doctor` (default `true`) from explicit `false`.
- [`atomic/internal/config/cli.go`](../../../atomic/internal/config/cli.go) — `atomic config get|set|unset|list|path` subcommand dispatch.
- [`atomic/internal/config/paths.go`](../../../atomic/internal/config/paths.go) — canonical path functions: `TOMLPath`, `ResolvedPath`, `BackupDir`, `ProposedCLAUDEMD`, `ProfilePath`, `ProfileRelPath`. Single source of truth for all `~/.claude/.atomic/` path derivation. `ProfilePath(claudeHome)` returns the disk path to `~/.claude/.atomic/profile.md`; `ProfileRelPath()` returns the claudeHome-relative constant `.atomic/profile.md`.
- [`atomic/internal/config/render.go`](../../../atomic/internal/config/render.go) — renders resolved config values to `~/.claude/.atomic/config.resolved.md` (the markdown snapshot `@`-ref'd from bundled [`CLAUDE.md`](../../../CLAUDE.md)).
- [`atomic/internal/hooks/hooks.go`](../../../atomic/internal/hooks/hooks.go) + `hooks_hujson.go` — session-start hook payload generation and install/uninstall. `Install` registers the inline command `atomic hooks session-start` directly in `settings.json`'s `SessionStart` hook entry — no wrapper script written to disk. `Uninstall` removes both the inline registration and any lingering legacy script. `IsInstalled(scopeRoot)` returns `(installed, drifted, err)`: `drifted=true` when a legacy wrapper-script registration is detected (functional but should be migrated). `migrateLegacy` removes the legacy `~/.claude/hooks/session-start-reminders.sh` registration and file on fresh `Install` calls. `legacyScriptName` / `legacyHooksSubdir` constants retained for migration and cleanup only. `hooks_hujson.go` uses `github.com/tailscale/hujson` for lenient JSON parsing of `settings.json`.
- [`atomic/internal/reminder/reminder.go`](../../../atomic/internal/reminder/reminder.go) — file-based reminder CRUD. Stored in repo root. `atomic reminder add|list|show|rm`.
- [`atomic/internal/followups/`](../../../atomic/internal/followups) (16 files) — YAML-frontmatter follow-up parser, INDEX renderer, CLOSED.md append-only ledger, legacy migration. Implements `atomic followups list|add|close|render|migrate|path`. Anchors at git toplevel via `repoctx`. Key functions: `LoadEntriesWithErrors` (parse boundary), `RenderIndex` (INDEX.md generation). `add` is the deterministic LLM-free entrypoint called by `/subagent-implementation` Phase 3 defer and `/subagent-diagnose`. `Kind` type (`KindFinding`/`KindPlan`) defined in `entry.go`; `parseKind` defaults empty string to `KindFinding` for back-compat. `AddOpts.Kind` defaults to `finding`; `--severity` is optional when `Kind=="plan"`. `isStale` returns false unconditionally for `KindPlan`. `Render` splits entries by kind before bucketing — plans to `writePlansBucket`, findings to severity buckets via `writeBucket`. `ListEntries --stale` excludes plans (via `isStale` returning false for them). Spec: [`docs/spec/typed-followups.md`](../../../docs/spec/typed-followups.md).
- [`atomic/internal/prompt/prompt.go`](../../../atomic/internal/prompt/prompt.go) — TTY abstraction over `github.com/charmbracelet/huh`. Exposes `Confirm` and `Select[T]`. Returns `ErrNonInteractive` (no TTY) and `ErrAborted` (Ctrl+C) as typed sentinels. **All interactive prompts route through this package** — direct `huh.*` calls outside `prompt/` are a convention violation. `doctor/stdin_prompter.go` adapts `prompt` errors to doctor's `Decision*` shape.
- [`atomic/internal/selfupdate/selfupdate.go`](../../../atomic/internal/selfupdate/selfupdate.go) — GitHub Releases API lookup, SHA256-verified binary download, atomic replace via `os.Rename` with cross-filesystem fallback. Exposes `Client.Update` and `Client.Check`. Does NOT emit the out-of-sync nudge — that moved to `main.go:runUpdate`.
- [`atomic/cmd/atomic/main.go`](../../../atomic/cmd/atomic/main.go) (`runUpdate`) — orchestrates the full update flow after binary swap: (1) calls `maybeRefreshArtifacts` to re-exec the new binary as `<exe> claude update --no-update-check` when a managed install is detected (`detectManagedInstall`); (2) emits the out-of-sync nudge when refresh did not run and `~/.claude/CLAUDE.md` exists; (3) fires post-update doctor. `--binary-only` flag skips step 1. `detectManagedInstall(home)` gates on `~/.claude/CLAUDE.md` present AND session-start hook registered (`hooks.IsInstalled`). `maybeRefreshArtifacts` re-execs the new binary (not in-process) so the old embedded bundle is not used.

**State directory layout (`~/.claude/.atomic/`):**

| Path | Contents |
|------|----------|
| `config.toml` | TOML user config (v1 schema) |
| `config.resolved.md` | Rendered markdown snapshot, `@`-ref'd from bundled [`CLAUDE.md`](../../../CLAUDE.md) |
| `backups/<ts>/` | Files replaced during `atomic claude install/update` |
| `proposed/CLAUDE.md` | Proposed merge target when installed [`CLAUDE.md`](../../../CLAUDE.md) diverges |
| `profile.md` | User profile stub written at install time by `atomic/internal/profile.RenderStub`. Sections: Identity (`<stable>`), Work (`<volatile>`), Active projects (`<volatile>`), Interests (`<stable>`), People mentioned (`<volatile>`), Environment (`<deterministic>`). |

**Follow-ups folder ([`.claude/project/followups/`](../followups)):**

Per-entry YAML-frontmatter `.md` files + auto-generated `INDEX.md` + append-only `CLOSED.md`. Frontmatter fields: `id`, `title`, `created`, `origin`, `kind` (`finding`|`plan`, default `finding` when absent), `severity` (required for findings, optional for plans), `review_by`, `status`, optional `file`. `INDEX.md` renders `## 📋 plans` section first (no staleness), then severity buckets for findings. Pre-commit hook stage 3 re-renders `INDEX.md` when followup entry files are staged.

## Docs

- [`docs/spec/atomic-state-and-config.md`](../../../docs/spec/atomic-state-and-config.md) — config schema contract, state directory layout, config.toml v1 schema, path derivation rules. Supersedes old `~/.claude/.atomic-backups/` and `~/.claude/CLAUDE.md.atomic-proposed` paths.
- [`docs/spec/follow-ups-folder.md`](../../../docs/spec/follow-ups-folder.md) — per-entry follow-up folder layout, frontmatter schema, INDEX.md generation, CLOSED.md audit trail, `atomic followups` subcommand contract.
- [`docs/spec/cron-workflow.md`](../../../docs/spec/cron-workflow.md) — `/loop`, `CronCreate`/`CronList`/`CronDelete` tools, scheduled tasks. 5-field cron expressions, jitter, 7-day expiry.
- [`docs/spec/docker-eval-environment.md`](../../../docs/spec/docker-eval-environment.md) — Docker evaluation setup (`atomic docker init`).
- [`docs/guides/evaluations.md`](../../../docs/guides/evaluations.md) — how to run evaluations.
- [`docs/design/atomic-state-and-config.md`](../../../docs/design/atomic-state-and-config.md) — design rationale for the state directory consolidation and config schema.

## Coupling

- **→ doctor**: `checks_config.go` (doctor domain) imports [`atomic/internal/config`](../../../atomic/internal/config) directly. Config schema changes must be reflected in `checks_config.go` validation. `checks_followups.go` imports [`atomic/internal/followups`](../../../atomic/internal/followups) — follow-up schema changes affect doctor.
- **→ doctor**: `updatedoctor` reads `update.run_doctor` from config and is controlled by `--no-doctor` flag. Config schema change to that key requires updating `updatedoctor.go` logic.
- **→ bundle**: `claudeinstall.Apply` (bundle domain) pre-creates `config.resolved.md` on every install run. The content of that file is owned by [`atomic/internal/config/render.go`](../../../atomic/internal/config/render.go) (this domain).
- **→ bundle**: `atomic-haiku`, `atomic-git-scout`, `atomic-claude-merger` agents ship in the bundle via `agents/atomic-*.md` bundlespec rule.
- **→ workflow**: `/subagent-implementation` Phase 3 `defer` block shells out to `atomic followups add`. Schema changes in followups (this domain) affect how workflow commands interact with the follow-up system.
- **→ signals**: `output.signals.max_depth` config key is read by [`atomic/internal/signals/tree.go`](../../../atomic/internal/signals/tree.go). Config schema changes here propagate to signals behavior.
