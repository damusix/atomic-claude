---
type: Domain
description: User config, state dir (profile.md, config.toml), session hooks, reminders, follow-ups, self-update; cold-op briefs embedded in binary via coldprompt package.
---

# config

## What it does

User config, state directory, session hooks, reminders, follow-ups, and git state management. Everything that persists user preferences or project-local operational state across sessions. [`atomic/internal/config`](../../atomic/internal/config) owns the TOML schema; [`atomic/internal/followups`](../../atomic/internal/followups) owns the per-entry folder; [`atomic/internal/hooks`](../../atomic/internal/hooks) installs the session-start hook. Cold-op briefs for git cleanup and CLAUDE.md merge are embedded in the binary via [`atomic/internal/coldprompt`](../../atomic/internal/coldprompt) — they are NOT install artifacts and are never written to `~/.claude`.

## Artifacts

**Follow-ups and git state:**

- [`commands/follow-up.md`](../../commands/follow-up.md) — `/follow-up [due <id> | review]`. Reviews pending follow-ups. `/follow-up review` triages stale [`.claude/project/followups/`](../followups) entries with per-item `extend|close|promote|skip`.
- [`commands/git-cleanup.md`](../../commands/git-cleanup.md) — `/git-cleanup [<name>]` runs `atomic prompt git-cleanup` via a `general-purpose` subagent (no `subagent_type`; read-only scan only). Presents indexed report; confirms before deleting anything. Plain-text indexed selection (axiom 4).

**Reminders and scheduling:**

- [`commands/remind-me.md`](../../commands/remind-me.md) — `/remind-me <duration> <text>` schedules reminder via cron. Degrades to file-only without `CronCreate`.
- [`commands/watch-ci.md`](../../commands/watch-ci.md) — `/watch-ci` dispatches `general-purpose` with `model: haiku` as a background subagent to watch CI. Provider auto-detected from signals or file-tree heuristics.

## CLI code

- [`atomic/internal/coldprompt/coldprompt.go`](../../atomic/internal/coldprompt/coldprompt.go) — embeds two cold-op briefs via `//go:embed`: `briefs/git-cleanup.md` and `briefs/claude-merge.md`. Exposes `Get(name string) (string, error)` and `Names() []string`. The `atomic prompt <name>` binary verb prints the brief to stdout. Briefs are embedded in the binary at build time and are NOT part of the install bundle shipped to `~/.claude`.
- [`atomic/internal/coldprompt/briefs/git-cleanup.md`](../../atomic/internal/coldprompt/briefs/git-cleanup.md) — self-contained brief for a `general-purpose` subagent performing the read-only git state scan. Replaces the former `atomic-git-scout` agent.
- [`atomic/internal/coldprompt/briefs/claude-merge.md`](../../atomic/internal/coldprompt/briefs/claude-merge.md) — self-contained brief for a `general-purpose` subagent merging `~/.claude/.atomic/proposed/CLAUDE.md` into `~/.claude/CLAUDE.md`. Replaces the former `atomic-claude-merger` agent. Preserves `<wikis>` block verbatim on merge.
- [`atomic/internal/config/config.go`](../../atomic/internal/config/config.go) — TOML-backed user config. Load/validate/persist. v1 schema: `output.signals.max_depth` (int, default 3), `update.run_doctor` (bool, default `true`). `output.intensity` was removed — the schema no longer contains it. Atomic write via `os.Rename`. Levenshtein typo-suggestion on unknown key names. Raw-map presence check distinguishes absent `update.run_doctor` (default `true`) from explicit `false`.
- [`atomic/internal/config/cli.go`](../../atomic/internal/config/cli.go) — `atomic config get|set|unset|list|path` subcommand dispatch.
- [`atomic/internal/config/paths.go`](../../atomic/internal/config/paths.go) — canonical path functions: `TOMLPath`, `ResolvedPath`, `BackupDir`, `ProposedCLAUDEMD`, `ProfilePath`, `ProfileRelPath`. Single source of truth for all `~/.claude/.atomic/` path derivation. `ProfilePath(claudeHome)` returns the disk path to `~/.claude/.atomic/profile.md`; `ProfileRelPath()` returns the claudeHome-relative constant `.atomic/profile.md`.
- [`atomic/internal/config/render.go`](../../atomic/internal/config/render.go) — renders resolved config values to `~/.claude/.atomic/config.resolved.md` (the markdown snapshot `@`-ref'd from bundled [`CLAUDE.md`](../../CLAUDE.md)).
- [`atomic/internal/hooks/hooks.go`](../../atomic/internal/hooks/hooks.go) + `hooks_hujson.go` — session-start hook payload generation and install/uninstall. `Install` registers the inline command `atomic hooks session-start` directly in `settings.json`'s `SessionStart` hook entry — no wrapper script written to disk. `Uninstall` removes both the inline registration and any lingering legacy script. `IsInstalled(scopeRoot)` returns `(installed, drifted, err)`: `drifted=true` when a legacy wrapper-script registration is detected (functional but should be migrated). `migrateLegacy` removes the legacy `~/.claude/hooks/session-start-reminders.sh` registration and file on fresh `Install` calls. `legacyScriptName` / `legacyHooksSubdir` constants retained for migration and cleanup only. `hooks_hujson.go` uses `github.com/tailscale/hujson` for lenient JSON parsing of `settings.json`.
- [`atomic/internal/reminder/reminder.go`](../../atomic/internal/reminder/reminder.go) — file-based reminder CRUD. Stored in repo root. `atomic reminder add|list|show|rm`.
- [`atomic/internal/followups/`](../../atomic/internal/followups) (16 files) — YAML-frontmatter follow-up parser, INDEX renderer, CLOSED.md append-only ledger, legacy migration. Implements `atomic followups list|add|close|render|migrate|path`. Anchors at git toplevel via `repoctx`. Key functions: `LoadEntriesWithErrors` (parse boundary), `RenderIndex` (INDEX.md generation). `add` is the deterministic LLM-free entrypoint called by `/subagent-implementation` Phase 3 defer and `/subagent-diagnose`. `Kind` type (`KindFinding`/`KindPlan`) defined in `entry.go`; `parseKind` defaults empty string to `KindFinding` for back-compat. `AddOpts.Kind` defaults to `finding`; `--severity` is optional when `Kind=="plan"`. `isStale` returns false unconditionally for `KindPlan`. `Render` splits entries by kind before bucketing — plans to `writePlansBucket`, findings to severity buckets via `writeBucket`. `ListEntries --stale` excludes plans (via `isStale` returning false for them). Spec: [`docs/spec/typed-followups.md`](../../docs/spec/typed-followups.md).
- [`atomic/internal/prompt/prompt.go`](../../atomic/internal/prompt/prompt.go) — TTY abstraction over `github.com/charmbracelet/huh`. Exposes `Confirm` and `Select[T]`. Returns `ErrNonInteractive` (no TTY) and `ErrAborted` (Ctrl+C) as typed sentinels. All interactive prompts route through this package — direct `huh.*` calls outside `prompt/` are a convention violation. `doctor/stdin_prompter.go` adapts `prompt` errors to doctor's `Decision*` shape.
- [`atomic/internal/selfupdate/selfupdate.go`](../../atomic/internal/selfupdate/selfupdate.go) — GitHub Releases API lookup, SHA256-verified binary download, atomic replace via `os.Rename` with cross-filesystem fallback. Exposes `Client.Update` and `Client.Check`. Does NOT emit the out-of-sync nudge — that moved to `main.go:runUpdate`.
- [`atomic/cmd/atomic/main.go`](../../atomic/cmd/atomic/main.go) (`runUpdate`) — orchestrates the full update flow after binary swap: (1) re-execs the new binary as `<exe> claude update --no-update-check` (default; `--skip-claude-update` opts out) to refresh `~/.claude` artifacts — no detection gate, assumes update means refresh; (2) appends `--no-hooks` when `hooks.IsInstalled($HOME)` reports no session-start hook, so refresh never first-registers hooks; (3) fires post-update doctor after refresh. Re-exec is load-bearing: running process still embeds the OLD bundle after the swap. Refresh failure warns on stderr; never blocks update exit code. Helpers `detectManagedInstall`, `maybeRefreshArtifacts`, `resolveIfManagedGate` and `claudeinstall/managed.go` were removed; `--binary-only` was renamed to `--skip-claude-update`; the out-of-sync nudge is removed.
- [`atomic/internal/claudeinstall/install.go`](../../atomic/internal/claudeinstall/install.go) (`planArtifact`) — when `ActionMergeRequired` fires for CLAUDE.md (no parseable `<atomic>` block), the printed next-step message instructs the user to run `atomic prompt claude-merge` (not a slash command).

**State directory layout (`~/.claude/.atomic/`):**

| Path | Contents |
|------|----------|
| `config.toml` | TOML user config (v1 schema) |
| `config.resolved.md` | Rendered markdown snapshot, `@`-ref'd from bundled [`CLAUDE.md`](../../CLAUDE.md) |
| `backups/<ts>/` | Files replaced during `atomic claude install/update` |
| `proposed/CLAUDE.md` | Proposed merge target when installed [`CLAUDE.md`](../../CLAUDE.md) diverges and no `<atomic>` block is parseable |
| `profile.md` | User profile stub written at install time by `atomic/internal/profile.RenderStub`. Sections: Identity (`<stable>`), Work (`<volatile>`), Active projects (`<volatile>`), Interests (`<stable>`), People mentioned (`<volatile>`), Environment (`<deterministic>`). |

**Follow-ups folder ([`.claude/project/followups/`](../followups)):**

Per-entry YAML-frontmatter `.md` files + auto-generated `INDEX.md` + append-only `CLOSED.md`. Frontmatter fields: `id`, `title`, `created`, `origin`, `kind` (`finding`|`plan`, default `finding` when absent), `severity` (required for findings, optional for plans), `review_by`, `status`, optional `file`. `INDEX.md` renders `## 📋 plans` section first (no staleness), then severity buckets for findings. Pre-commit hook stage 3 re-renders `INDEX.md` when followup entry files are staged.

## Docs

- [`docs/spec/atomic-state-and-config.md`](../../docs/spec/atomic-state-and-config.md) — config schema contract, state directory layout, config.toml v1 schema, path derivation rules. Supersedes old `~/.claude/.atomic-backups/` and `~/.claude/CLAUDE.md.atomic-proposed` paths.
- [`docs/spec/follow-ups-folder.md`](../../docs/spec/follow-ups-folder.md) — per-entry follow-up folder layout, frontmatter schema, INDEX.md generation, CLOSED.md audit trail, `atomic followups` subcommand contract.
- [`docs/spec/cron-workflow.md`](../../docs/spec/cron-workflow.md) — `/loop`, `CronCreate`/`CronList`/`CronDelete` tools, scheduled tasks. 5-field cron expressions, jitter, 7-day expiry.
- [`docs/spec/docker-eval-environment.md`](../../docs/spec/docker-eval-environment.md) — Docker evaluation setup (`atomic docker init`).
- [`docs/guides/evaluations.md`](../../docs/guides/evaluations.md) — how to run evaluations.
- [`docs/design/atomic-state-and-config.md`](../../docs/design/atomic-state-and-config.md) — design rationale for the state directory consolidation and config schema.

## Coupling

- **→ doctor**: `checks_config.go` (doctor domain) imports [`atomic/internal/config`](../../atomic/internal/config) directly. Config schema changes must be reflected in `checks_config.go` validation. `checks_followups.go` imports [`atomic/internal/followups`](../../atomic/internal/followups) — follow-up schema changes affect doctor.
- **→ doctor**: `updatedoctor` reads `update.run_doctor` from config and is controlled by `--no-doctor` flag. Config schema change to that key requires updating `updatedoctor.go` logic.
- **→ bundle**: `claudeinstall.Apply` (bundle domain) pre-creates `config.resolved.md` on every install run. The content of that file is owned by [`atomic/internal/config/render.go`](../../atomic/internal/config/render.go) (this domain).
- **→ bundle**: [`atomic/internal/coldprompt/`](../../atomic/internal/coldprompt) briefs are embedded in the binary via `//go:embed`, NOT via the bundle mirror that ships [`agents/`](../../agents), [`commands/`](../../commands), [`skills/`](../../skills) to `~/.claude`. Adding a new brief requires editing `coldprompt.go`, not `bundlemirror/mirror.go`.
- **→ workflow**: `/subagent-implementation` Phase 3 `defer` block shells out to `atomic followups add`. Schema changes in followups (this domain) affect how workflow commands interact with the follow-up system.
- **→ signals**: `output.signals.max_depth` config key is read by [`atomic/internal/signals/tree.go`](../../atomic/internal/signals/tree.go). Config schema changes here propagate to signals behavior.
