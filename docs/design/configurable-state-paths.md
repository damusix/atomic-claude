# Configurable state paths (v6)


GitHub issue #150. Every path the CLI manages is hardcoded to Claude Code conventions: per-user state at `~/.claude/.atomic/`, repo-local state under `.claude/`. Using atomic with a different agent harness (e.g. a pi agent expecting `.pi/scratchpad`) is impossible. Target: v6, `next` branch — breaking change.


## Shape of the change


Two independent moves:

1. **User state relocation.** `~/.claude/.atomic/` → `~/.atomic/` (config.toml, config.resolved.md, profile.md, backups/, pre-install/, proposed/, retro-runs/, improve-runs/, improve-learnings.md). Fixed location, not configurable. Automatic first-run migration.
2. **Repo-local harness prefix knob.** A single config key, `harness.dir` (default `.claude`), that every repo-local path helper reads: `.scratchpad/`, `project/` (followups), `.atomic-index/`, `atomic.toml`, `worktrees/`, the nested `.gitignore`. Setting `harness.dir = ".pi"` makes the CLI resolve `.pi/.scratchpad`, `.pi/project/followups`, `.pi/.atomic-index/atomic.db`, and scaffold `.pi/` in `atomic repo init`.


## Decisions


### D1 — `~/.atomic` is fixed, not configurable

The user state root hosts `config.toml` itself — making its location a config key is a bootstrap cycle. And the location is already harness-neutral: nothing about `~/.atomic` assumes Claude. No env-var override either (axiom 2: no knob without a proven need; the issue's need is satisfied by the move itself).

### D2 — Migration: rename + compat symlink, automatic and idempotent

On every CLI invocation, before command dispatch: if `~/.claude/.atomic` is a real directory and `~/.atomic` does not exist, rename it to `~/.atomic`, then leave a symlink `~/.claude/.atomic` → `~/.atomic`.

Why the symlink: every installed `~/.claude/CLAUDE.md` (v5 bundles) carries `@~/.claude/.atomic/config.resolved.md` and `@~/.claude/.atomic/profile.md`. A hard move breaks both refs in every Claude session until the user runs `atomic claude install` — silent loss of config + profile context. The symlink keeps old refs resolving; the v6 bundle rewrites them to `@~/.atomic/...`; the symlink lingers harmlessly and doctor can report it as informational.

Edge handling:

- Rename fails with cross-device error → recursive copy; legacy dir left in place (symlink skipped — path occupied); doctor migrate check flags the leftover.
- Both `~/.atomic` and a *real* (non-symlink) `~/.claude/.atomic` exist → prefer `~/.atomic`, never merge, doctor WARNs.
- Migration failure of any kind → one stderr warning, run continues against `~/.atomic` (created on demand). Never crash the verb the user actually invoked.

### D3 — One global `harness.dir` key; no per-repo override in v1

A per-repo override has a chicken-egg problem: the repo config file (`atomic.toml`) lives *inside* the harness dir, so the CLI would have to probe a candidate list of harness dirs to find the config that names the harness dir. Probing is fragile (ordering, false positives on repos containing both dirs). One user-level key covers the issue's actual need — running atomic under a pi agent on a machine. A mixed-harness machine (Claude repos + pi repos side by side) is deferred until it's real.

Validation: single path segment, no separators, not `.` or `..`, non-empty. `.pi`, `.claude`, `pi` all legal; `foo/bar` rejected.

### D4 — Claude-specific integration paths stay hardcoded

`~/.claude` as the artifact install target, `.claude/settings.json` for session-start hooks, and the claude-merge flow's `~/.claude/CLAUDE.md` targets are the *Claude integration itself*, not harness-agnostic state. `atomic claude install` installs Claude-format artifacts; pointing it at `~/.pi` would install artifacts pi cannot read. Porting the bundle to other harness formats is explicitly out of scope (issue #150).

### D5 — Legacy-path literals stay literal

`internal/migrate/steps.go`, the wiki legacy-signals check, and similar sites describe *historical* layouts (pre-relocation signals paths). They must keep matching what old repos actually contain; deriving them from `harness.dir` would be wrong.

### D6 — Artifact prose is out of scope

Command/agent/skill markdown that names `.claude/.scratchpad/` etc. in its instructions is Claude-harness content — a pi port rewrites those artifacts wholesale (issue non-goal). Exception: strings naming the *user state dir* (`~/.claude/.atomic/...`) in bundle sources, embedded briefs, templates, and Go output — those reference a location this feature moves, and are swept to `~/.atomic/...`.


## Resolver shape


`internal/config` already owns user-state paths (`Dir`, `TOMLPath`, `ResolvedPath`, `BackupDir`, `PreInstallDir`, `ProfilePath`) — they re-point to `~/.atomic` with a `claudeHome` → `home` parameter rename. Repo-local helpers are new siblings in the same package (`ScratchpadDir`, `ProjectDir`, `FollowupsDir`, `IndexDir`, `IndexDBPath`, `WorktreesDir`, `RepoConfigPath`, `RemindersDir`), each `filepath.Join(root, harnessDir(), <suffix>)`. The harness dir resolves once per process (`sync.Once`) from `~/.atomic/config.toml`, lenient on load errors (fall back to `.claude`), with a test seam to override. Domain packages (reminder, followups, codeintel engine/daemon/realm, repoinit, signals tree, doctor checks) drop their private constants and call the helpers.


## Rejected approaches


- **Env-var overrides (`ATOMIC_STATE_DIR`, `ATOMIC_HARNESS_DIR`).** Axiom 2 — config graduation requires a proven need; none exists once the config key ships. Revisit if a per-repo case (direnv) materializes.
- **Auto-detecting the harness dir by probing the repo** (`.pi/` exists → use it). Fragile: needs a maintained list of known harness dirs, ambiguous when a repo contains several, surprising when a stray dir flips path resolution.
- **Hard move without compat symlink.** Breaks `@-refs` in every installed CLAUDE.md until refresh; the failure is silent (sessions just lose config/profile context).
- **Configurable user-state root.** Bootstrap cycle (D1).
- **Per-repo `harness.dir` in `atomic.toml`.** Chicken-egg (D3).
