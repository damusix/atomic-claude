---
type: Domain
description: 13-check integrity suite + static validation (A1 artifact CLI-flag lint) + CLI surface table + post-update auto-fire + user profile + code-index health + migration-drift nudge + repo-scoped ignore-config validation.
---

# doctor

## What it does

Integrity check suite (`atomic doctor`) and static validation (`atomic validate`). Runs 13 deterministic checks verifying install coherence, hooks, signals freshness, @-ref wiring, manifest parity, follow-ups, memory, binary version, config, user profile wiring, code-index freshness, migration drift, and repo-scoped ignore-config validity. Non-zero exit on FAIL for CI gating. Opt-in repair via `--fix`.

## Artifacts

No slash commands. `atomic doctor` and `atomic validate` are binary subcommands, not Claude Code commands. Entry points: [`atomic/cmd/atomic/main.go`](../../atomic/cmd/atomic/main.go) (subcommand dispatch).

## CLI code

**Core doctor suite ([`atomic/internal/doctor/`](../../atomic/internal/doctor) — 49 files):**

- `doctor.go` — orchestrator. Runs all 13 checks in index order, applies `--only` / `--skip` filters, collects results, returns exit code (0 = PASS/WARN/SKIP, 1 = FAIL, 2 = usage error). `Opts.RepoRoot` is resolved once per `RunWith` call via lazy-fill — when empty, `gitToplevelFn` is called exactly once and the result is stored in `opts.RepoRoot`; all check functions read `opts.RepoRoot` rather than spawning their own git subprocesses. Tested in `gitcallcount_internal_test.go`.
- `flags.go` — CLI flag parsing for `atomic doctor [--fix] [--json] [--only] [--skip] [--stale-days] [--verbose]`.
- `format.go` — exports `FormatResultLine(r Result) string`. **Shared** by `FormatHuman` (full doctor output) and [`atomic/internal/updatedoctor/updatedoctor.go`](../../atomic/internal/updatedoctor/updatedoctor.go) (post-update FAIL-only lines). Changing this function affects both surfaces.
- `fix.go` — `Repairer` struct with injectable function fields (`ManifestBundleFn`, `ManifestRenderFn`, `RepoRootFn`, etc.). `DefaultRepairer()` wires production implementations. `Repair` is now a method on `Repairer`; the package-level `Repair` func is a thin wrapper calling `DefaultRepairer().Repair(...)`. `RepairSummary` has `Applied`, `Skipped`, `NonFixable` fields — `FixApplied` and `FixSummary` fields removed.
- `fix_impls.go` — per-category repair implementations. `applyManifestRepairWithGuard` and folloup/refs repair functions stream make/command output directly to `out io.Writer` via `cmd.Stdout = out; cmd.Stderr = out` — no output buffering.
- `stdin_prompter.go` — `stdinPrompter` struct implementing `Prompter` interface. Adapts `prompt.ErrAborted` → `DecisionAbort`, `prompt.ErrNonInteractive` → `DecisionSkip`. `NewStdinPrompter(r, w)` is the constructor; tested in `stdin_prompter_internal_test.go`.
- `exit.go` — exit code constants and determination logic.
- `shortcircuit.go` — early-exit conditions (e.g., not in a git repo).
- `repodev.go` — dev-mode detection (running inside this repo itself).
- `inode_unix.go` / `inode_windows.go` — platform-specific inode comparison for symlink detection.

**Check implementations (stable index order — never renumber):**

| Index | Name | File | Default severity |
|-------|------|------|-----------------|
| 1 | install | `checks_install.go` | WARN |
| 2 | hooks | `checks_hooks.go` | WARN |
| 3 | signals | `checks_signals.go` | WARN |
| 4 | refs | `checks_refs.go` | FAIL |
| 5 | manifest | `checks_manifest.go` | FAIL |
| 6 | followups | `checks_followups.go` | WARN |
| 7 | memory | `checks_memory.go` | WARN |
| 8 | binary | `checks_binary.go` | WARN |
| 9 | config | `checks_config.go` | WARN |
| 10 | profile | `checks_profile.go` | WARN |
| 11 | code-index | `checks_code_index.go` | WARN |
| 12 | migrate | `checks_migrate.go` | WARN |
| 13 | repo-config | `checks_repo_config.go` | WARN |

`checks_code_index.go` (category 11) checks code-index freshness via `RunCheckCodeIndexWith(root, staleDays)`. No DB → `PASS` informational ("code index not initialized (optional; run 'atomic code index' to enable)"). DB present + mtime ≥ `staleDays` → `WARN` ("run 'atomic code sync'"). DB present + fresh → `PASS`. Never produces `FAIL`. Uses `engine.IndexPath(root)` to locate the DB; stats the file rather than opening it (avoids spinning up the wazero pool for a health check).

`checks_profile.go` (category 10) checks four conditions: (1) `~/.atomic/profile.md` exists on disk and is readable; (2) `@~/.atomic/profile.md` (`ProfileRef` const) appears in one of the candidate CLAUDE files; (3) the `<deterministic lastcheck=YYYY-MM-DD>` stamp in the file is within the last 30 days (`profileStaleDays = 30`); (4) — new, issue #150 — none of the candidate files still carries the legacy `@~/.claude/.atomic/profile.md` ref (`legacyProfileRef` const, a v5-bundle install that hasn't run `atomic claude install` since the v6 relocation). All legs return WARN (not FAIL); leg 4's detail names `atomic claude install` as the fix. A v1-format file with no `lastcheck` attribute triggers WARN ("run `atomic profile refresh`"). `RunCheckProfileWith(home)` is the injectable seam (searches candidates under `filepath.Join(home, ".claude")` — the Claude integration target, D4 — while resolving `profile.md` itself under `home`, the `~/.atomic` root). `config.ProfilePath` / `config.ProfileRelPath` derive the disk path.

`checks_hooks.go` (category 2) scope bug fixed: `checkHooks` passes `$HOME` to `RunCheckHooksWith` — not `~/.claude`. The prior bug passed `~/.claude` as scopeRoot, causing `hooks.IsInstalled` to look for `~/.claude/.claude/settings.json` (double [`.claude`](../..) segment). `RunCheckHooksWith(scopeRoot string)` is exported for tests; `checks_hooks_internal_test.go` holds internal package tests. `drifted=true` response from `hooks.IsInstalled` produces WARN "session-start hook uses legacy wrapper script — run `atomic hooks install` to migrate".

`checks_refs.go` checks for `@docs/wiki/index.md` only (updated for [`docs/wiki/`](.) relocation per wiki-storage-relocation CP2). Candidate files searched in order: [`claude.local.md`](../../claude.local.md), [`CLAUDE.local.md`](../../CLAUDE.local.md), [`CLAUDE.md`](../../CLAUDE.md), [`claude.md`](../../claude.md).

`checks_followups.go` — walks the folder returned by `config.FollowupsDir(root)` (harness.go — default [`.claude/project/followups/`](../followups)) via `followups.LoadEntriesWithErrors`. Byte-compares re-rendered INDEX against on-disk to detect drift. Two repair functions: `followupsRenderRepair` (re-renders INDEX), `followupsMigrateRepair` (runs migrate for legacy `followups.md`).

`checks_config.go` — imports [`atomic/internal/config`](../../atomic/internal/config) directly. Validates config file structure, known key set (now including `harness.dir`), and value constraints — including `[agents.<name>]`: an invalid `effort` value FAILs via `config.Validate`; `model` is lenient and never fails the check (`checks_config_test.go` asserts effort-fail / lenient-model-pass; the prior invalid-tier test was retired alongside the tier allowlist).

`checks_install_test.go` (category 1, `install`) gained `TestCheckInstall_agentOverrideDrift_detectAndRepair`: a regression test, not a new check. It installs cleanly (PASS), then writes a `[agents.atomic-implementer]` effort override to `~/.atomic/config.toml` with the on-disk agent file left un-patched — `RunCheckInstall` reports WARN with a `"drifted: agents/atomic-implementer.md"` finding (via the existing `claudeinstall.Diff`/`readPatchedEmbedded` comparison, bundle domain) — then re-running `claudeinstall.Install` (the same repair `atomic doctor --fix` performs for this category) returns the check to PASS. Locks the config↔installed-agent drift behavior that CP2's `Diff` override-patching already produced as a side effect.

`checks_migrate.go` (category 12) combines two conditions into one Result (combined-detail style, as `checks_config.go` does): (a) version drift — reads `[install].version` from `~/.atomic/config.toml` and compares it against the running binary version via `selfupdate.CompareSemver`; a binary newer than the recorded install version → WARN nudging `atomic migrate`. No nudge (PASS-leg) when: `config.toml` is absent (not atomic-installed), `[install].version` is empty (pre-framework install), or the binary is not newer. The `"dev"` version string (default for local builds) is treated as `0.0.0` by `CompareSemver`, so dev builds never nudge. (b) — new, issue #150 — legacy state dir: `legacyStateDirCondition(home)` `Lstat`s `<home>/.claude/.atomic`; a real (not symlinked) directory there means the automatic migration hasn't completed → WARN naming the path, that migration runs automatically on any [`atomic`](../../atomic) verb invocation, and to check for a prior failure or a conflicting `~/.atomic`. Severity is the worst of the two conditions; detail concatenates whichever fired; neither firing → PASS. `RunCheckMigrateDriftWith(home, binaryVersion)` is the injectable test seam.

`checks_repo_config.go` (category 13) validates `config.RepoConfigPath(root)` (harness.go — default `<projectRoot>/.claude/atomic.toml`) via `config.LoadRepoConfig` + `config.NewIgnoreMatcher` (config domain); its Detail strings derive from that same harness-aware path (not a hardcoded display literal), so they stay correct under a non-default `harness.dir`. Absent file → PASS informational, mirroring category 11 (`code-index`)'s opt-in-absence contract. Parse errors, unknown keys, and invalid `[code] ignore` glob patterns each → WARN with detail. A valid file → PASS naming the active ignore-pattern count via `IgnoreMatcher.PatternCount()`. Never FAIL — a malformed repo config only degrades code-intel indexing to unfiltered, it never blocks the repo. `RunCheckRepoConfigWith(root)` is the injectable test seam.

**Post-update doctor adapter ([`atomic/internal/updatedoctor/`](../../atomic/internal/updatedoctor)):**

- `updatedoctor.go` — called by `main.go:runUpdate` after binary swap. Calls `doctor.Run(Opts{Skip: []int{3, 8}})` — skips signals (index 3) and binary (index 8). Prints FAIL lines only (uses `format.FormatResultLine`). Recovers panics. Never changes update exit code.
- Controlled by `--no-doctor` flag (per-invocation) or `update.run_doctor = false` in config (durable).
- `RunDoctorFn` function type is the injectable test seam — production wires `doctor.Run`, tests inject stubs.

**CLI surface table ([`atomic/internal/cliusage/`](../../atomic/internal/cliusage) — 2 files):**

- `cliusage.go` — defines the complete [`atomic`](../../atomic) command surface as structured data (`Command` type: verb-path tokens, args hint, accepted `--flags`, description). Exports `TopLevelVerbs()`, `Lookup(path)`, `RenderHelp(w)`. Two consumers: (1) `main.go` renders `--help` from it; (2) `validate artifacts` rule A1 checks artifact citations against it. Single source of truth for the command surface — callers never maintain parallel flag lists. The `update` verb entry has flags `--check`, `--channel`, `--no-doctor`, `--skip-claude-update`; description "Self-update the atomic binary, then refresh ~/.claude artifacts". Also carries 8 `template <name>` entries (`brief`, `design-doc`, `diagnose-context`, `followups`, `implementation-log`, `session-report`, `spec`, `state`) for the config-domain `atomic template` verb — each has `Flags: nil`, `Args: ""`, and description `"Emit the <name> document template"`.
- `cliusage_test.go` — golden test pinning `--help` output; validates all top-level verbs and flag sets.

**Validation suite ([`atomic/internal/validate/`](../../atomic/internal/validate) — 16 files):**

- `validate.go` — dispatch entry point. Modes: `spec`, `config`, `bundle`, `artifacts`. No-args = whole-repo run (all four modes).
- `spec.go` — checks S0/S1/S5/S6 spec markdown structure.
- `config.go` — checks C3/C5/C7/C9 cross-reference integrity in CLAUDE.md / commands / agents / skills. Rule C1 was retired — the "Subagents available for dispatch" section it validated was removed from [`CLAUDE.md`](../../CLAUDE.md); `RunConfigRules` now runs only C3/C5/C7/C9.
- `bundle.go` — bundle parity against embedded manifest.
- `artifacts.go` — rule A1: scans artifact corpus for [`atomic`](../../atomic) verb/flag citations in code spans and fenced blocks; validates each cited `--flag` against the `cliusage` surface table. Exported seam `ScanArtifactText(path, src)` accepts raw markdown for testability. Unresolved citations (unknown subcommand) emit nothing (false-negative over false-positive). Universal flags (`--help`, `-h`, `--version`, `-v`, `--repo`, `--no-update-check`) always pass.
- `artifacts_test.go` — tests `ScanArtifactText` for bad flags (FAIL), good citations, universal flags, arg-enum subcommands, and prose-only citations (no FAIL).
- `dispatch.go` — routes to per-mode validators (now includes `artifacts` mode).
- `finding.go` — finding type (FAIL/WARN/SKIP) and formatters.
- `output.go` — output formatting (human and JSON).

**Supporting packages used by doctor:**

- [`atomic/internal/manifestcheck/`](../../atomic/internal/manifestcheck) — called by `checks_manifest.go`. Imports `bundlespec` for inclusion predicates.
- [`atomic/internal/followups/`](../../atomic/internal/followups) — called by `checks_followups.go`. `LoadEntriesWithErrors` is the parse boundary; `RenderIndex` is used for drift comparison.
- [`atomic/internal/config/`](../../atomic/internal/config) — called by `checks_config.go`; also called directly by `checks_repo_config.go` (category 13) for the repo-scoped [`.claude/atomic.toml`](../../.claude/atomic.toml) schema.

## Docs

- [`docs/spec/atomic-doctor.md`](../../docs/spec/atomic-doctor.md) — canonical contract for all 13 check categories, fix functions, exit codes, `--fix` behavior. Master reference.
- [`docs/spec/atomic-validate.md`](../../docs/spec/atomic-validate.md) — `atomic validate` subcommand contract (S0/S1/S5/S6, C3/C5/C7/C9, A1 checks). C1 rule retired per 2026-06-24 spec amendment.
- [`docs/spec/validate-artifact-cli-flags.md`](../../docs/spec/validate-artifact-cli-flags.md) — A1 rule contract: `internal/cliusage` surface table, `validate artifacts` subcommand, scanner rules, known scope limits. Design: [`docs/design/validate-artifact-cli-flags.md`](../../docs/design/validate-artifact-cli-flags.md).
- [`docs/spec/verify-gate-validate.md`](../../docs/spec/verify-gate-validate.md) — `atomic validate` integration with the `atomic-verify` skill: when and how `/commit-only` and `/subagent-implementation` gate on validate output. Design: [`docs/design/verify-gate-validate.md`](../../docs/design/verify-gate-validate.md).
- [`docs/spec/atomic-update-doctor.md`](../../docs/spec/atomic-update-doctor.md) — post-update doctor auto-fire contract. Specifies skip indices `[3, 8]`, panic recovery, exit code preservation. "Artifact auto-refresh contract" section: refresh runs by default after binary swap (no detection gate); re-execs new binary as `<exe> claude update --no-update-check`; appends `--no-hooks` when session-start hook absent; `--skip-claude-update` opts out; failure warns, never blocks update.
- [`docs/design/atomic-doctor.md`](../../docs/design/atomic-doctor.md) — design rationale for the 9-check architecture.
- [`docs/design/atomic-validate.md`](../../docs/design/atomic-validate.md) — design rationale for the validate subcommand.
- [`docs/spec/user-profile.md`](../../docs/spec/user-profile.md) — contract for the user profile feature: schema, sections, `<stable>`/`<volatile>`/`<deterministic>` tag semantics, install-time stub generation.
- [`docs/design/user-profile.md`](../../docs/design/user-profile.md) — design rationale for user profile capture and stub rendering.
- [`docs/spec/document-templates.md`](../spec/document-templates.md) — config-domain doc-templates feature contract (the `atomic template <name>` verb, [`atomic/internal/doctemplate/`](../../atomic/internal/doctemplate)), cross-listed here because it required 8 new `cliusage.go` surface-table entries.
- [`docs/design/document-templates.md`](../design/document-templates.md) — design rationale for the doc-templates feature, cross-listed here for the same reason.
- [`docs/spec/configurable-state-paths.md`](../../docs/spec/configurable-state-paths.md) — config-domain spec (issue #150), cross-listed here for its Checkpoint 5: `checks_migrate.go` gains the legacy-state-dir leg, `checks_profile.go` gains the legacy-ref leg, and [`docs/spec/atomic-doctor.md`](../spec/atomic-doctor.md) body is amended (categories 9/10/12) to read `~/.atomic` in place of `~/.claude/.atomic`.

## Coupling

- **→ bundle**: `checks_manifest.go` uses [`atomic/internal/manifestcheck/`](../../atomic/internal/manifestcheck) which imports `bundlespec`. Changing bundle inclusion rules (bundle domain) affects which manifest check items pass/fail.
- **→ bundle**: `validate/artifacts.go` calls `bundlemirror.Enumerate(repoRoot)` to discover the artifact corpus for A1 scanning. Changes to bundle inclusion rules (bundle domain) change which files `validate artifacts` scans.
- **→ self (cliusage)**: `validate/artifacts.go` imports `cliusage.TopLevelVerbs()` and `cliusage.Lookup()`. Any change to the command surface table in `cliusage.go` (new verb, removed verb, flag added/removed) directly changes what A1 considers valid — the table and the binary's registered `flag.FlagSet` calls must stay in sync.
- **→ signals**: `checks_refs.go` reads candidate CLAUDE files for `@docs/wiki/index.md`. The `signalsRef` const is the single source of truth — changes to the expected @-ref path require updating this const and the signals domain's wiring convention simultaneously.
- **→ signals**: `checks_signals.go` verifies [`docs/wiki/scan.md`](scan.md) exists and is not stale. Staleness logic tracks the signals domain's scan output.
- **→ config**: `checks_config.go` imports [`atomic/internal/config`](../../atomic/internal/config) directly. Config schema changes (config domain) must be reflected in `checks_config.go` validation.
- **→ config**: `checks_repo_config.go` (category 13) imports [`atomic/internal/config`](../../atomic/internal/config)'s `LoadRepoConfig`/`NewIgnoreMatcher`/`RepoConfigPath` directly. Repo-scoped config schema changes (config domain) must be reflected here — same pattern as `checks_config.go`'s coupling to the user-scoped schema above.
- **→ code-intel**: category 13 (`repo-config`) mirrors category 11 (`code-index`)'s opt-in-absence contract — an absent [`.claude/atomic.toml`](../../.claude/atomic.toml) is normal because repo-scoped ignore filtering (code-intel domain) is itself opt-in.
- **→ config**: `updatedoctor` skip indices `[3, 8]` are hardcoded. Adding or renumbering doctor categories requires updating `updatedoctor.go` to match.
- **→ workflow**: `checks_followups.go` imports [`atomic/internal/followups`](../../atomic/internal/followups). Follow-up schema changes (config domain) affect what doctor accepts as valid.
- **→ docs-meta**: `format.FormatResultLine` is a shared output primitive. Changing it affects both `FormatHuman` (full doctor) and `updatedoctor` (post-update FAIL-only).
- **→ config**: `checks_profile.go` calls `config.ProfilePath` and `config.ProfileRelPath`. Adding new profile-related paths to [`atomic/internal/config/paths.go`](../../atomic/internal/config/paths.go) (config domain) requires checking whether `checkProfile` needs updating.
- **→ bundle**: [`atomic/internal/profile/`](../../atomic/internal/profile) is called by [`atomic/internal/claudeinstall/install.go`](../../atomic/internal/claudeinstall/install.go) at install time to generate the profile stub. Changes to `RenderStub` or `CaptureEnv` (profile package) affect what gets written to `~/.atomic/profile.md` on fresh install. `profile/refresh.go`'s `Refresh`/`RefreshIfStale` and the session-start ride-along in [`atomic/internal/hooks/hooks.go`](../../atomic/internal/hooks/hooks.go) (config domain) resolve the same path via `home`, not `home/.claude` (issue #150).
- **→ config**: `checks_migrate.go`'s new legacy-state-dir leg and `checks_profile.go`'s new legacy-ref leg (issue #150) both detect the pre-v6 `~/.claude/.atomic` path — [`atomic/internal/config/statemigrate.go`](../../atomic/internal/config/statemigrate.go) (config domain) is what performs the automatic migration these checks are verifying completed.
- **→ code-intel**: `checks_code_index.go` imports `engine.IndexPath` to locate the SQLite DB. If the engine's index path convention changes (code-intel domain), this check breaks silently — both must change together.
- **→ config**: the new `atomic template <name>` verb (config domain, [`atomic/internal/doctemplate/`](../../atomic/internal/doctemplate)) required 8 new `cliusage.go` entries. Adding or removing a template name in `doctemplate.Names()` without a matching `cliusage.go` entry desyncs the two — the same coordination hazard the `wiki` verb family already carries against `cliusage.go` (documented in the wiki domain file).
- **→ config, → bundle**: `checks_install_test.go`'s agent-override-drift regression test exercises config domain's `[agents.<name>]` schema ([`atomic/internal/config`](../../atomic/internal/config)) and bundle domain's `claudeinstall.Diff`/`Install` (this check imports [`atomic/internal/claudeinstall`](../../atomic/internal/claudeinstall) directly). A schema or patching change in either domain that breaks this drift-detect/repair contract surfaces here first.
