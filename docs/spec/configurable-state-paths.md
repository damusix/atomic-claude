# Configurable state paths (v6)


## Goal


Decouple CLI-managed paths from Claude Code conventions (GitHub issue #150). Per-user state moves from `~/.claude/.atomic/` to `~/.atomic/` with an automatic, idempotent first-run migration (rename + compat symlink). Repo-local state paths (`.scratchpad/`, `project/`, `.atomic-index/`, `atomic.toml`, `rules/wiki/`, `workflow-preferences.md`) resolve through the harness-neutral state-location ladder in `docs/spec/atomic-state-and-config.md` (process-only `ATOMIC_STATE_DIR`, the persisted per-clone selection, a populated state folder, user `state.dir`, built-in `.atomic`), so one repo resolves the same root whichever harness runs it. Linked worktrees follow `config.WorktreeDir` instead: a `worktrees` preference path or `off`, else `.claude/worktrees` on Claude and `<state-root>/worktrees` on other harnesses. `harness.dir` is retired: a stored copy loads silently and is dropped on the next config write, and `atomic config set` refuses it as an unknown key.


## Approach


Chosen approach: re-point the existing `internal/config` path helpers and add repo-local sibling helpers over the resolved state location; see `docs/design/configurable-state-paths.md`.


## Non-goals


- No configurable user-state root: `~/.atomic` is fixed (config.toml lives inside it — bootstrap cycle).
- No env-var override of the user-state root — `~/.atomic` stays fixed. `ATOMIC_STATE_DIR` selects only the repo-local state root, never `~/.atomic`.
- No process-fingerprint or environment-variable selection of a harness for state paths; the ladder never consults one.
- No changes to Claude-specific integration paths: `~/.claude` install target, `.claude/settings.json` hooks path, claude-merge `~/.claude/CLAUDE.md` targets.
- No rewrite of legacy-migration literals (`internal/migrate/steps.go` legacy signals paths, wiki legacy-signals check, `main.go` member legacy check) — they describe historical layouts and must keep matching them.
- No sweep of `.claude/...` mentions in artifact prose (commands/agents/skills instructions) — Claude-harness content, out of scope per the issue. Only strings naming the user state dir (`~/.claude/.atomic/...`) are swept.
- No porting of the artifact bundle to other harness formats.
- No removal of the compat symlink by any automated flow.


## Success criteria


- [ ] Fresh machine: any `atomic` verb creates and uses `~/.atomic/`; nothing ever writes `~/.claude/.atomic/`.
- [ ] Machine with a legacy `~/.claude/.atomic/`: the first invocation of any verb renames it to `~/.atomic/` and leaves `~/.claude/.atomic` as a symlink to `~/.atomic`; a second invocation is a no-op; old `@~/.claude/.atomic/...` refs in installed CLAUDE.md files still resolve through the symlink.
- [ ] Migration failure (e.g. cross-device rename falls back to copy; permission error) emits one stderr warning and the invoked verb still runs; nothing panics or exits non-zero because of migration alone.
- [ ] `state.dir` accepts a repository-relative path of safe segments, such as `.config/atomic`, and rejects an absolute path, `.`, `..`, and an empty segment with the standard typo-suggesting error shape; `atomic config set` refuses the retired `harness.dir` key as an unknown key, and a stored copy of that key or of the retired `pi` table loads silently, changes no resolution, and is dropped on the next write.
- [ ] With a state root `<dir>` chosen by the ladder: `atomic repo init` scaffolds `<dir>/.scratchpad` and `<dir>/project`, writes the nested `<dir>/.gitignore` when `<dir>` is inside the repository, and ensures `<dir>/.scratchpad/`, `<dir>/.atomic-index/`, `<dir>/project/doc-surfaces.md`, and `<dir>/worktrees/` are ignored (plus `.claude/worktrees/` through `.claude/.gitignore` when Claude is enrolled or `.claude` exists); `atomic followups` reads and writes `<dir>/project/followups/`; `atomic code index` writes `<root>/<dir>/.atomic-index/atomic.db` and the MCP daemon and realm resolver agree; the repo config loads from `<dir>/atomic.toml`; the repo-level workflow preferences load from `<dir>/workflow-preferences.md` (the profile level is the `## Workflow preferences` block in `~/.atomic/profile.md`; contract: `docs/spec/workflow-preferences.md`); the signals tree skips `<dir>/.scratchpad/` and `<dir>/project/`.
- [ ] Default behavior (no selection record, no populated legacy folder, no `state.dir`): all repo-local paths resolve under `.atomic/`. A repository whose only populated state folder is legacy `.claude` resolves there in place until `atomic state migrate` moves it.
- [ ] Bundled `CLAUDE.md` carries `@~/.atomic/config.resolved.md` and `@~/.atomic/profile.md`; `atomic doctor` WARNs (with an `atomic claude install` hint) when the installed CLAUDE.md still carries the legacy refs.
- [ ] `atomic doctor` migrate category flags an unmigrated or half-migrated legacy state dir (real `~/.claude/.atomic` dir still present).
- [ ] Grep gate: no string emitted to users or embedded in the bundle references `~/.claude/.atomic`. Repo-wide, remaining mentions live only in: legacy-migration code and its tests, doctor legacy-detection literals and their tests, spec change-log/implementation-log sections, design-doc and research-note bodies of previously shipped features (point-in-time records — each affected design doc carries a one-line status banner pointing at this feature), `CHANGELOG.md`, machine-generated `docs/wiki/**` (self-heals on the signals refresh), and this feature's own spec/design.
- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .` clean; `make render` + `make -C atomic bundle` parity clean; `atomic validate` clean.


## Change tree


```
M atomic/internal/config/paths.go                — Dir(home) → <home>/.atomic; claudeHome param renamed home; ProfileRelPath drops the .atomic/ prefix logic to match
A atomic/internal/config/statemigrate.go         — MigrateUserState(home): rename + symlink, copy fallback, idempotent
A atomic/internal/config/statemigrate_test.go    — fresh / legacy / migrated / conflict / failure cases
A atomic/internal/config/harness.go              — repo-local path helpers over the resolved state location + test seam
A atomic/internal/config/harness_test.go         — default and override roots, lenient load fallback
A atomic/internal/config/harness_retired_test.go — retired keys and variables change no resolution
M atomic/internal/config/config.go               — schema: state.dir (strict on set, lenient on load); retired tables load silently and drop on write
M atomic/internal/config/render.go               — config.resolved.md renderer lists state.dir
M atomic/internal/config/repo.go                 — RepoConfigRelPath derives from the state root (RepoConfigPath(root) helper)
M atomic/cmd/atomic/main.go                      — early MigrateUserState call; config.* callers pass home (not home/.claude); runMigrateInstall splits its single claudeHome param into a config root (home) and an install root (home/.claude — migrate.Context.Root keeps its ~/.claude install-scope semantics); ~/.claude/.atomic help-text sweep
M atomic/internal/signals/signals.go             — resolveConfigPath passes home (not home/.claude) to config.TOMLPath
M atomic/internal/profile/refresh.go             — Refresh/RefreshIfStale resolve profile.md under the state root (home param, not home/.claude)
M atomic/internal/hooks/hooks.go                 — refreshProfile session-start ride-along passes home (settings.json path untouched per Non-goals)
M atomic/internal/serve/code_members.go          — localIndexRel const → config index-path helper (feeds codeexplorer/codegraph localDBPath)
M atomic/internal/cliusage/cliusage.go           — config key/verb descriptions if state-dir paths appear
M atomic/internal/claudeinstall/install.go       — ProfileNudge string → ~/.atomic/profile.md; state-dir callers use new signatures
M atomic/internal/claudeinstall/uninstall.go     — snapshot dir via config helper (drops filepath.Join(targetDir, ".atomic"))
M atomic/internal/doctor/checks_profile.go       — ProfileRef → @~/.atomic/profile.md; legacy ref detected → WARN + install hint
M atomic/internal/doctor/checks_followups.go     — path via config.FollowupsDir
M atomic/internal/doctor/checks_migrate.go       — new row: legacy real ~/.claude/.atomic dir present → WARN
M atomic/internal/doctor/checks_config.go        — state-dir paths via updated helpers
M atomic/internal/doctor/checks_repo_config.go   — Detail strings derive from the state-root-aware repo-config path (no hardcoded display literal)
M atomic/internal/followups/cli.go               — followupsFolder → config.FollowupsDir(root)
M atomic/internal/codeintel/engine/engine.go     — indexSubDir → config.IndexDir/IndexDBPath
M atomic/internal/codeintel/realm/resolver.go    — localIndexDB → config helper
M atomic/internal/codeintel/mcp/daemon.go        — hardcoded db path → config helper
M atomic/internal/codeintel/cli/code.go          — user-facing path strings reflect the resolved state root; EnsureGitignore's index ignore entry derives from it
M atomic/internal/docs/docs.go                   — doc-surfaces cache path derives from the state root (sibling of the other project/-scoped consumers)
M atomic/internal/repoinit/repoinit.go           — scaffolded layout derives from the state root
M atomic/internal/signals/tree.go                — skipPrefixes derive from the state root
M atomic/internal/coldprompt/briefs/claude-merge.md — proposed-merge paths → ~/.atomic/proposed/CLAUDE.md
M atomic/internal/doctemplate/templates/session-report.md — scratchpad path comment made harness-neutral
M CLAUDE.md                                      — @-refs → @~/.atomic/...; Where-things-live row; profile prose paths
M .claude/rules/authoring/axioms.md              — axiom 2 config path mention → ~/.atomic/config.toml
M docs/spec/atomic-state-and-config.md           — body amended: layout → ~/.atomic, schema gains state.dir; change-log entry
M docs/spec/atomic-binary.md, user-profile.md, uninstall.md, install-workflow.md, signals-router.md, atomic-update-doctor.md — spec bodies still naming ~/.claude/.atomic as current truth (change logs untouched)
M README.md, docs/credits.md, docs/reference/**, docs/guides/** — public-doc current-truth mentions of the old state dir
M docs/design/atomic-state-and-config.md, user-profile.md, uninstall.md, signals-wiki-unification.md — one-line status banner (paths below are pre-v6; points at this feature); bodies otherwise untouched (point-in-time records)
M atomic/internal/embedded/**                    — regenerated via make render + make -C atomic bundle
```


## Outline


- `atomic/internal/config/paths.go`
  - `Dir` — resolves the state root to `<home>/.atomic`; every existing subpath helper (TOMLPath, ResolvedPath, BackupDir, PreInstallDir, ProfilePath, ProfileRelPath) keeps its shape on top of it
- `atomic/internal/config/statemigrate.go`
  - `MigrateUserState` — takes the home dir (never calls os.UserHomeDir itself); rename legacy → new, then compat symlink at the old path; copy fallback stages into a sibling temp dir and renames it into place, so a partial copy never occupies `~/.atomic`; when `~/.atomic` already exists it still ensures the compat symlink (only when `~/.claude` exists and its `.atomic` entry is absent — a failed symlink is retried on the next run, and `~/.claude` itself is never created); both dirs real → prefer new, never merge; never returns a condition the caller must crash on
- `atomic/internal/config/harness.go`
  - `stateRoot` — delegates to `repositoryStateDir` (`statelocation.go`) for the repository-state root, a harness-neutral ladder (process-only `ATOMIC_STATE_DIR`, the persisted per-clone selection, a populated state folder, user `state.dir`, built-in `.atomic`); no rung consults a harness variable or fingerprint, an invalid configured `state.dir` falls through to `.atomic`, and a refusal (invalid override or record, ambiguous folders) exits before reaching filepath.Join. The ladder is specified in `docs/spec/atomic-state-and-config.md`
  - `SetStateDirForTest` — test seam that overrides the effective state segment and returns a restore func
  - `ScratchpadDir / ProjectDir / FollowupsDir / IndexDir / IndexDBPath / RepoConfigPath / WorkflowPrefsPath` — join repo root + state root + fixed suffix
  - `WorktreeDir(root, harness, preference)` — a preference path wins (absolute as-is, relative joined to root) and `off` returns verbatim; else `.claude/worktrees` for Claude, `<state-root>/worktrees` for other harnesses
- `atomic/internal/config/config.go`
  - `state.dir` schema entry — string, default `.atomic`; Set validation: repository-relative path whose every segment is non-empty and not `.`/`..`
  - retired tables — a stored `harness.dir` key or `pi` table loads silently and is dropped on the next write; `Set` refuses `harness.dir` as unknown
- `atomic/internal/config/statelocation.go`
  - `repositoryStateDir` — resolves the state root through `ATOMIC_STATE_DIR`, the persisted selection record, the populated-folder rung (a populated destination wins; else exactly one other populated folder answers; two or more refuse as ambiguous), the configured `state.dir`, then `.atomic`
- `atomic/cmd/atomic/main.go`
  - `runMigrateInstall` two-root split — config helpers receive the home dir; `migrate.Context.Root` keeps receiving `<home>/.claude` (install-scope, per D4); the shared single parameter is dissolved
- `atomic/internal/doctor/checks_migrate.go`
  - legacy-state-dir condition — joined into the category's single migrate Result (combined-detail style, as `checks_config.go` does): WARN detail when `~/.claude/.atomic` is a real directory (not symlink); result severity is the worst of version-drift and legacy-dir conditions; PASS when neither fires
- `atomic/internal/doctor/checks_profile.go`
  - legacy-ref detection — installed CLAUDE.md carrying `@~/.claude/.atomic/profile.md` → WARN naming `atomic claude install`
- Consumer packages (followups, codeintel engine/daemon/realm, repoinit, signals tree, doctor followups/config checks, claudeinstall/uninstall)
  - None — mechanical swap of private constants/joins for the config helpers; no new named pieces
- `CLAUDE.md` (bundle source)
  - `@-ref` block — both refs re-pointed to `~/.atomic/`
  - Where-things-live table — `~/.claude/.atomic/` row → `~/.atomic/`
- `docs/spec/atomic-state-and-config.md`
  - Layout + schema sections — rewritten to `~/.atomic` and the three-key schema; dated change-log entry with Superseded line


## Flows


1. Any CLI invocation → `main()` calls `config.MigrateUserState(home)` before dispatch → legacy real dir found → rename to `~/.atomic` → symlink `~/.claude/.atomic` → proceed to the verb. Second invocation: new dir exists → immediate no-op.
2. User → `atomic config set state.dir <segment>` → strict validation → atomic write of `config.toml` → `config.resolved.md` re-rendered including the key.
3. Repo verb (e.g. `atomic followups list`) → `config.FollowupsDir(root)` → the state-location ladder resolves the root → returns `<root>/<state-root>/project/followups` → verb operates there.
4. `atomic doctor` → migrate category probes `~/.claude/.atomic` → real dir → WARN with migration explanation; symlink or absent → PASS. Profile category sees legacy `@-ref` in installed CLAUDE.md → WARN naming `atomic claude install`.
5. `atomic claude install` (v6 bundle) → installs CLAUDE.md carrying `@~/.atomic/...` refs → pre-creates `~/.atomic/config.resolved.md` so the ref resolves on fresh installs.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | User state root moves to `~/.atomic` + automatic migration | `config/paths.go`, `config/statemigrate.go` (+test), `cmd/atomic/main.go` (early call + caller signature sweep + `runMigrateInstall` two-root split + `runProfile`), `signals/signals.go` (resolveConfigPath), `profile/refresh.go`, `hooks/hooks.go` (refreshProfile), `claudeinstall/install.go`, `claudeinstall/uninstall.go`, `doctor/checks_config.go` | unit: `Dir` returns `<home>/.atomic`; migration fresh/legacy/idempotent/conflict/staged-copy-fallback/symlink-retry cases; `migrate.Context.Root` still receives `<home>/.claude`; profile refresh writes `<home>/.atomic/profile.md` through the production wiring (`runProfile` / session-start ride-along), not just package internals; existing config+install+doctor tests green against new paths |
| 2 | `state.dir` config key + repo-local resolver | `config/config.go`, `config/render.go`, `config/harness.go` (+test), `config/repo.go` | unit: default `.atomic`; set/get/unset round-trip; invalid values rejected; renderer emits the key; helpers resolve under an override via test seam; retired keys change no resolution |
| 3 | Consumers thread through the resolver | `followups/cli.go`, `doctor/checks_followups.go`, `doctor/checks_repo_config.go` (Detail strings), `codeintel/engine/engine.go`, `codeintel/realm/resolver.go`, `codeintel/mcp/daemon.go`, `codeintel/cli/code.go` (incl. EnsureGitignore), `docs/docs.go`, `serve/code_members.go`, `repoinit/repoinit.go`, `signals/tree.go` | unit per consumer under a non-default state root; default-path tests assert `.atomic/...` |
| 4 | User-facing strings, embedded text, bundle CLAUDE.md | `claudeinstall/install.go` (ProfileNudge), `doctor/checks_profile.go` (ProfileRef + legacy WARN), `coldprompt/briefs/claude-merge.md`, `doctemplate/templates/session-report.md`, `CLAUDE.md`, `.claude/rules/authoring/axioms.md`, `main.go`/`cliusage.go` help text, `make render` + `make -C atomic bundle` | grep gate: `~/.claude/.atomic` absent outside legacy-migration code/tests and change logs; render+bundle parity; doctor legacy-ref WARN unit test |
| 5 | Doctor migrate row + spec/docs sweep | `doctor/checks_migrate.go` (+test), `docs/spec/atomic-state-and-config.md`, `docs/spec/atomic-doctor.md`, spec bodies (`atomic-binary.md`, `user-profile.md`, `uninstall.md`, `install-workflow.md`, `signals-router.md`, `atomic-update-doctor.md`), `README.md`, `docs/credits.md`, docs grep sweep (`docs/reference/`, `docs/guides/`) | doctor unit PASS/WARN paths; the feature's grep-gate success criterion satisfied repo-wide; `atomic validate` clean; spec change-log entries present |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Symlink compat path missed by a tool that stats before following | low | macOS/Linux only; `@-ref` reads are plain file opens, which follow symlinks |
| Cross-device rename (`~/.claude` on another mount) | low | EXDEV → recursive copy fallback; doctor flags the leftover legacy dir |
| Half-migrated state (both dirs real) | med | Prefer `~/.atomic`, never merge; doctor migrate WARN names the leftover |
| Tests that read the real `~/.claude` (known `internal/hooks` gap) interact with migration logic | med | Migration functions take `home` as a parameter; tests inject temp homes; never call `os.UserHomeDir` inside `MigrateUserState` |
| The cached state segment is a value tests need to vary | med | `SetStateDirForTest` seam resets the cache; helpers documented as process-stable |
| Import cycle pulling `internal/config` into codeintel | low | config imports only toml/doublestar/stdlib; verified no reverse dependency |
| `cliusage.go` drift vs new key | low | A1 lint (`atomic validate artifacts`) + `TestRootCmdExact*Verbs`-style surface tests |
| Old binaries (v5) run after migration write to the symlinked path | low | Symlink makes old-path writes land in `~/.atomic` — coherent by construction |


## Change log

### 2026-11-12 — Rename the state-segment test seam

**What changed:** The test seam `SetHarnessDirForTest` is `SetStateDirForTest` in the outline and the risk table.

**Why:** the seam overrides the state-root segment, and `harness.dir` is retired.

**Superseded:** the seam was named `SetHarnessDirForTest`.

### 2026-11-12 — Ignore the doc-surfaces cache

**What changed:** The state-root criterion lists `<dir>/project/doc-surfaces.md` among the paths `atomic repo init` ignores.

**Why:** `atomic docs scan` regenerates that cache on every run. Contract: `docs/spec/repo-init.md`.

**Superseded:** only `.scratchpad/`, `.atomic-index/`, and `worktrees/` were ignored under the state root.

### 2026-11-11 — Name the workflow-preferences file

**What changed:** The Goal's repo-local path list, the state-root success criterion, and the `harness.go` Outline entry name `<state-root>/workflow-preferences.md` and `WorkflowPrefsPath`, with the profile level in the `## Workflow preferences` block of `~/.atomic/profile.md`.

**Why:** `docs/spec/workflow-preferences.md` adds a repo-level preference file under the state root.


### 2026-11-11 — State root defaults to .atomic

**What changed:** The Goal, the state-root and default-behavior criteria, the `state.dir` criterion, the `harness.go`, `config.go`, and `statelocation.go` Outline entries, and Checkpoints 2 and 3 name built-in `.atomic` and the five-rung ladder with the populated-folder rung. A legacy `.claude` that is the only populated folder resolves in place until `atomic state migrate`. `state.dir` is a repository-relative path of safe segments. `atomic repo init` writes `<dir>/.gitignore` only inside the repository and ignores `.claude/worktrees/` when Claude is enrolled or `.claude` exists. The Goal's state-root list names `rules/wiki/` and says worktrees follow `config.WorktreeDir` instead of the ladder. **Correction:** the Outline names `WorktreeDir(root, harness, preference)` with its preference and harness behavior; no `WorktreesDir` helper exists in `config/harness.go`.

**Why:** `docs/spec/omp-plugin-compatibility.md` moves repository state to the `.atomic` default, with a resolution ladder and `atomic state migrate`.

**Superseded:** a ladder ending at built-in `.claude`, with every default repo-local path under `.claude/` and `state.dir` limited to one segment.

### 2026-11-11 — Reminders removed

**What changed:** The state-root criterion, Change tree, Outline, consumer list, and Checkpoint 3 drop the reminder package and `config.RemindersDir`.

**Why:** `docs/spec/workflow-preferences.md` drops the reminder feature.

**Removed:** `reminder/reminder.go` as a resolver consumer, `config.RemindersDir`, and the project-keyed `reminders/` path claim.

### 2026-11-10 — Harness variable, Pi root, and `harness.dir` retired

**What changed:** the body now describes repo-local state resolving through the harness-neutral ladder (`ATOMIC_STATE_DIR`, the per-clone selection record, user `state.dir`, built-in `.claude`) with no harness variable or fingerprint rung. `harness.dir` loads silently, is dropped on the next write, and `atomic config set harness.dir` refuses it. `ATOMIC_HARNESS` has no meaning, and `.pi` is no state candidate. The Goal, Non-goals, Success criteria, Change tree, Outline, Flows, Checkpoint 2, and Risks follow.

**Why:** orientation reads environment markers and `--harness` only, there are no Pi users, and a stored key or variable must not change where a repository's state lives. The install side moved too: a bare `atomic install` installs into every installed harness, and `atomic update` adopts a legacy Claude install once.

**Superseded:** `harness.dir` as the single repo-local state key, the five-rung `harnessDir` ladder with its `ATOMIC_HARNESS`, `PI_CODING_AGENT`, and `CLAUDECODE` rungs, and the `.pi` success criteria.


### 2026-08-20 — Reminders move off the harness-scoped path

**What changed:** The `harness.dir = .pi` success criterion no longer pins reminders under `.pi/.scratchpad/reminders`. They resolve under the project-keyed `~/.atomic/<project-key>/reminders/`, harness-independent — `harness.dir` no longer factors into the reminders path at all. The pre-relocation harness-scoped directory (`<harness.dir>/.scratchpad/reminders`) is read as a legacy fallback until a repo migrates.

**Why:** `docs/spec/serve-plans-page.md` relocates reminders (and session reports, and archived scratchpad bundles) to one project-keyed home shared across every worktree of a clone; this spec's success criterion pinned the pre-relocation path as current truth.

**Superseded:** "reminders live under `.pi/.scratchpad/reminders`" as an unqualified, current-truth statement of where reminders resolve under a non-default harness.

### 2026-07-16 — ATOMIC_HARNESS env + agent-fingerprint auto-detection

**What changed:** `harnessDir`'s resolution grows from two rungs (config, default) to five, env-first: (1) `ATOMIC_HARNESS` env — explicit harness name, leading `.` tolerated/normalized, invalid value falls through leniently; (2) `PI_CODING_AGENT == "true"` → `.pi`; (3) `CLAUDECODE == "1"` → `.claude`; (4) user config `harness.dir`; (5) built-in default. Env reads live inside the same once-per-process resolver (env is process-stable, so caching it alongside the config read is safe); `SetHarnessDirForTest` continues to override every rung.

**Why:** pi-agent mixed-machine need arrived (issue #150 follow-on) — a user running both Claude Code and a pi agent across machines wants the harness auto-detected from the launching agent's own env fingerprint, not hand-set per machine via `atomic config set harness.dir`. `PI_CODING_AGENT=true` / `CLAUDECODE=1` are user-verified fingerprints.

**Superseded:** the Non-goals line "No env-var overrides (`ATOMIC_STATE_DIR`, `ATOMIC_HARNESS_DIR`)" — the state-dir root (`~/.atomic`) remains a non-goal for env override, but `harness.dir` resolution now reads env by design.


### 2026-07-16 — Design-doc bodies scoped out of the grep gate

**What changed:** The grep-gate success criterion now enumerates its exclusions exhaustively; design-doc and research-note bodies of previously shipped features are excluded as point-in-time records, with each affected design doc carrying a one-line status banner pointing at this feature. Checkpoint 5 gains the four banner edits.

**Why:** CP5 review — four shipped features' design docs still name the old state dir, and the gate as previously written was unsatisfiable without rewriting them; rewriting point-in-time decision records would falsify the audit trail (the spec-currency body-is-truth contract binds specs, not designs), while a banner removes the misleading-reader risk.

**Superseded:** the gate excluded only "legacy-migration code, its tests, and spec/design change logs".


### 2026-07-16 — CP5 docs sweep made exhaustive

**What changed:** Checkpoint 5's file list now names the spec bodies still carrying `~/.claude/.atomic` as current truth (`atomic-binary`, `user-profile`, `uninstall`, `install-workflow`, `signals-router`, `atomic-update-doctor`) plus `README.md` and `docs/credits.md`; its Verifies column requires the feature's grep-gate criterion repo-wide.

**Why:** CP4 review — `README.md` was assigned to no checkpoint and CP5's original list was too narrow to ever satisfy the spec's own grep-gate success criterion; spec-currency makes body mentions binding.


### 2026-07-16 — Two missed repo-local consumers

**What changed:** Checkpoint 3 gains `codeintel/cli/code.go`'s `EnsureGitignore` (index ignore entry derives from the harness dir, not a `.claude/.atomic-index/` literal) and `internal/docs/docs.go` (doc-surfaces cache under `project/` derives like every other project-scoped consumer).

**Why:** CP3 review — under `harness.dir = .pi` the generated index dir was left un-ignored, and `atomic docs scan` kept writing `.claude/project/doc-surfaces.md`; both contradict the design's definition of repo-local state.


### 2026-07-16 — Load-path validation + harness-aware doctor display

**What changed:** `harnessDir`'s lenient posture now includes stored-value validation — a parsed but invalid `harness.dir` (e.g. `..`) falls back to `.claude` instead of reaching `filepath.Join`. `doctor/checks_repo_config.go` added to Checkpoint 3's consumer list: its Detail strings must derive from the harness-aware path, not a display literal.

**Why:** CP2 review — a hand-edited `..` bypassed Set-time validation on the load path and resolved repo-local paths outside the repo; the doctor Detail string would lie under a non-default harness dir.


### 2026-07-16 — Profile-refresh chain + hardened migration contract

**What changed:** Change tree/Outline/Checkpoint 1 gain `internal/profile/refresh.go` and `internal/hooks/hooks.go` (`refreshProfile`) — the profile-refresh call chain resolves `profile.md` from `home`, not `home/.claude`. `MigrateUserState` contract hardened: copy fallback stages into a sibling temp dir and renames into place (a partial copy never occupies `~/.atomic`); the compat symlink is ensured even when `~/.atomic` already exists (failed symlink retried on later runs; `~/.claude` never created). Checkpoint 1 verifies profile refresh through production wiring.

**Why:** CP1 review found `runProfile` and the session-start hook still writing `profile.md` to the legacy path (the chain was absent from the Change tree), a partial-copy state trusted as migrated, and a failed symlink permanently unretried.


### 2026-07-16 — Initial spec

**What changed:** New spec for issue #150: user state relocation `~/.claude/.atomic/` → `~/.atomic/` with automatic rename+symlink migration, and the `harness.dir` config key routing all repo-local CLI paths.

**Why:** Enable atomic under non-Claude agent harnesses (e.g. pi: `.pi/scratchpad` vs `.claude/scratchpad`); v6 breaking change on `next`.


## Implementation log


### v1 — 2026-07-16

Built by `/autopilot` across 5 checkpoints, 6 implement-review iterations, on branch `configurable-state-paths` (worktree off `next` @ 02efb13). Commits:

- `71a314b` — CP1: `Dir(home)` → `~/.atomic`; `MigrateUserState` (rename + compat symlink, staged-copy fallback via `~/.atomic.migrating`, symlink retry); two-root splits through `runMigrateInstall`, claudeinstall (install/uninstall/manifest/snapshot), doctor, signals, profile chain, hooks ride-along. `feat!:` — the breaking commit.
- `15f7b0d` — CP2: `harness.dir` schema key + `harness.go` resolver (8 repo-local helpers, `SetHarnessDirForTest` seam, load-path validation of stored values).
- `461ecd5` — CP3: 11 consumers threaded (reminder, followups, doctor followups/repo-config Detail, codeintel engine/realm/mcp/cli + `EnsureGitignore`, serve, repoinit, signals tree, internal/docs cache).
- `afa5dce` — CP4: user-state string sweep (bundle CLAUDE.md `@-refs`, coldprompt merge brief, doctemplate skeleton, retrospective-learning template, axioms rule); `checks_profile` legacy-ref WARN + two-root fix; render/bundle regenerated.
- `c50e527` — CP5: legacy-state-dir condition in the doctor migrate check (+ its latent config-root fix); 8 spec bodies + public docs swept; status banners on 4 superseded design docs; follow-up `doctor-spec-missing-migrate-row` closed.

**Unforeseens:** the profile-refresh chain (`runProfile`, session-start hook, `internal/profile`) was absent from the initial Change tree — caught by CP1 review writing `profile.md` to the legacy path. `internal/docs`' doc-surfaces cache and `EnsureGitignore`'s ignore literal were two more unlisted consumers caught by CP3 review. A stored-but-invalid `harness.dir` bypassed validation on the load path (CP2 review). The grep gate was unsatisfiable as first written — design-doc bodies of shipped features were scoped out as point-in-time records with status banners (CP5 review).

**Deferred:** nothing; scratchpad `FOLLOWUPS.md` ended empty. Known accepted gap: `docs/wiki/**` narrative self-heals via the signals refresh rather than a hand sweep.
