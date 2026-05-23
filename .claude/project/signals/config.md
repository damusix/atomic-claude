# config

User config, state directory, session hooks, reminders, follow-ups, and git state management. Everything that persists user preferences or project-local operational state.

## Artifacts

**Follow-ups and git state:**

- `commands/follow-up.md` — `/follow-up [due <id> | review]`. Reviews pending follow-ups. `/follow-up review` triages stale `.claude/project/followups/` entries with per-item `extend|close|promote|skip`.
- `commands/git-cleanup.md` — `/git-cleanup [<name>]` scans stale git state via `atomic-git-scout`. Presents indexed report; confirms before deleting anything. Plain-text indexed selection (axiom 4).
- `agents/atomic-git-scout.md` — read-only scanner. Classifies cleanup candidates (`remove`/`delete`/`prune`/`ask`/`flag`/`skip`). Returns indexed report. Never mutates state.

**Reminders and scheduling:**

- `commands/remind-me.md` — `/remind-me <duration> <text>` schedules reminder via cron. Degrades to file-only without `CronCreate`.
- `commands/watch-ci.md` — `/watch-ci` spawns background `atomic-haiku` to watch CI. Provider auto-detected from signals.
- `agents/atomic-haiku.md` — generic background runner (haiku). Polling, status checks, log scraping. Read-only by default.

**CLAUDE.md merge:**

- `commands/atomic-claude-merge.md` — `/atomic-claude-merge` merges `~/.claude/.atomic/proposed/CLAUDE.md` into live `~/.claude/CLAUDE.md`. Preserves user sections, replaces atomic-owned ones.
- `agents/atomic-claude-merger.md` — the merge agent. Backs up prior `CLAUDE.md` to `~/.claude/.atomic/backups/<ts>/` before writing.

## CLI code

- `atomic/internal/config/config.go` — TOML-backed user config. Load/validate/persist. v1 schema: `output.intensity` (`lite|full|ultra`, default `full`), `output.signals.max_depth` (int, default 3), `update.run_doctor` (bool, default `true`). Atomic write via `os.Rename`. Levenshtein typo-suggestion on unknown key names. Raw-map presence check distinguishes absent `update.run_doctor` (default `true`) from explicit `false`.
- `atomic/internal/config/cli.go` — `atomic config get|set|unset|list|path` subcommand dispatch.
- `atomic/internal/config/paths.go` — canonical path functions: `TOMLPath`, `ResolvedPath`, `BackupDir`, `ProposedCLAUDEMD`. Single source of truth for all `~/.claude/.atomic/` path derivation.
- `atomic/internal/config/render.go` — renders resolved config values to `~/.claude/.atomic/config.resolved.md` (the markdown snapshot `@`-ref'd from bundled `CLAUDE.md`).
- `atomic/internal/hooks/hooks.go` + `hooks_hujson.go` — session-start hook payload generation and install/uninstall. `hooks_hujson.go` uses `github.com/tailscale/hujson` for lenient JSON parsing of `settings.json`.
- `atomic/internal/reminder/reminder.go` — file-based reminder CRUD. Stored in repo root. `atomic reminder add|list|show|rm`.
- `atomic/internal/followups/` (16 files) — YAML-frontmatter follow-up parser, INDEX renderer, CLOSED.md append-only ledger, legacy migration. Implements `atomic followups list|add|close|render|migrate|path`. Anchors at git toplevel via `repoctx`. Key functions: `LoadEntriesWithErrors` (parse boundary), `RenderIndex` (INDEX.md generation). `add` is the deterministic LLM-free entrypoint called by `/subagent-implementation` Phase 3 defer and `/subagent-diagnose`.
- `atomic/internal/prompt/prompt.go` — TTY abstraction over `github.com/charmbracelet/huh`. Exposes `Confirm` and `Select[T]`. Returns `ErrNonInteractive` (no TTY) and `ErrAborted` (Ctrl+C) as typed sentinels. **All interactive prompts route through this package** — direct `huh.*` calls outside `prompt/` are a convention violation. `doctor/stdin_prompter.go` adapts `prompt` errors to doctor's `Decision*` shape.
- `atomic/internal/selfupdate/selfupdate.go` — GitHub Releases API lookup, SHA256-verified binary download, atomic replace via `os.Rename` with cross-filesystem fallback. `atomic update [--check] [--channel] [--no-doctor]`.

**State directory layout (`~/.claude/.atomic/`):**

| Path | Contents |
|------|----------|
| `config.toml` | TOML user config (v1 schema) |
| `config.resolved.md` | Rendered markdown snapshot, `@`-ref'd from bundled `CLAUDE.md` |
| `backups/<ts>/` | Files replaced during `atomic claude install/update` |
| `proposed/CLAUDE.md` | Proposed merge target when installed `CLAUDE.md` diverges |

**Follow-ups folder (`.claude/project/followups/`):**

Per-entry YAML-frontmatter `.md` files + auto-generated `INDEX.md` + append-only `CLOSED.md`. Frontmatter fields: `id`, `title`, `created`, `origin`, `severity`, `review_by`, `status`, optional `file`. Pre-commit hook stage 3 re-renders `INDEX.md` when followup entry files are staged.

## Docs

- `docs/spec/atomic-state-and-config.md` — config schema contract, state directory layout, config.toml v1 schema, path derivation rules. Supersedes old `~/.claude/.atomic-backups/` and `~/.claude/CLAUDE.md.atomic-proposed` paths.
- `docs/spec/follow-ups-folder.md` — per-entry follow-up folder layout, frontmatter schema, INDEX.md generation, CLOSED.md audit trail, `atomic followups` subcommand contract.
- `docs/spec/cron-workflow.md` — `/loop`, `CronCreate`/`CronList`/`CronDelete` tools, scheduled tasks. 5-field cron expressions, jitter, 7-day expiry.
- `docs/spec/docker-eval-environment.md` — Docker evaluation setup (`atomic docker init`).
- `docs/guides/evaluations.md` — how to run evaluations.
- `docs/design/atomic-state-and-config.md` — design rationale for the state directory consolidation and config schema.

## Coupling

- **→ doctor**: `checks_config.go` (doctor domain) imports `atomic/internal/config` directly. Config schema changes must be reflected in `checks_config.go` validation. `checks_followups.go` imports `atomic/internal/followups` — follow-up schema changes affect doctor.
- **→ doctor**: `updatedoctor` reads `update.run_doctor` from config and is controlled by `--no-doctor` flag. Config schema change to that key requires updating `updatedoctor.go` logic.
- **→ bundle**: `claudeinstall.Apply` (bundle domain) pre-creates `config.resolved.md` on every install run. The content of that file is owned by `atomic/internal/config/render.go` (this domain).
- **→ bundle**: `atomic-haiku`, `atomic-git-scout`, `atomic-claude-merger` agents ship in the bundle via `agents/atomic-*.md` bundlespec rule.
- **→ workflow**: `/subagent-implementation` Phase 3 `defer` block shells out to `atomic followups add`. Schema changes in followups (this domain) affect how workflow commands interact with the follow-up system.
- **→ signals**: `output.signals.max_depth` config key is read by `atomic/internal/signals/tree.go`. Config schema changes here propagate to signals behavior.
