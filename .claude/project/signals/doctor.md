# doctor

## What it does

Integrity check suite (`atomic doctor`) and static validation (`atomic validate`). Runs 10 deterministic checks verifying install coherence, hooks, signals freshness, @-ref wiring, manifest parity, follow-ups, memory, binary version, config, and user profile wiring. Non-zero exit on FAIL for CI gating. Opt-in repair via `--fix`.

## Artifacts

No slash commands. `atomic doctor` and `atomic validate` are binary subcommands, not Claude Code commands. Entry points: [`atomic/cmd/atomic/main.go`](../../../atomic/cmd/atomic/main.go) (subcommand dispatch).

## CLI code

**Core doctor suite ([`atomic/internal/doctor/`](../../../atomic/internal/doctor) — 38 files):**

- `doctor.go` — orchestrator. Runs all 9 checks in index order, applies `--only` / `--skip` filters, collects results, returns exit code (0 = PASS/WARN/SKIP, 1 = FAIL, 2 = usage error).
- `flags.go` — CLI flag parsing for `atomic doctor [--fix] [--json] [--only] [--skip] [--stale-days] [--verbose]`.
- `format.go` — exports `FormatResultLine(r Result) string`. **Shared** by `FormatHuman` (full doctor output) and [`atomic/internal/updatedoctor/updatedoctor.go`](../../../atomic/internal/updatedoctor/updatedoctor.go) (post-update FAIL-only lines). Changing this function affects both surfaces.
- `fix.go` + `fix_impls.go` — per-category repair functions invoked by `atomic doctor --fix`. Interactively prompts via `stdin_prompter.go`.
- `stdin_prompter.go` — adapts `prompt.ErrAborted` → `DecisionAbort`, `prompt.ErrNonInteractive` → `DecisionSkip`.
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

`checks_profile.go` (category 10) checks three conditions: (1) `~/.claude/.atomic/profile.md` exists on disk and is readable; (2) `@~/.claude/.atomic/profile.md` appears in one of the candidate CLAUDE files; (3) the `<deterministic lastcheck=YYYY-MM-DD>` stamp in the file is within the last 30 days (`profileStaleDays = 30`). All legs return WARN (not FAIL). A v1-format file with no `lastcheck` attribute triggers WARN ("run `atomic profile refresh`"). `ProfileRef` const is exported for test use. `RunCheckProfileWith(claudeHome)` is the injectable seam. `config.ProfilePath` / `config.ProfileRelPath` derive the disk path.

`checks_hooks.go` (category 2) scope bug fixed: `checkHooks` passes `$HOME` to `RunCheckHooksWith` — not `~/.claude`. The prior bug passed `~/.claude` as scopeRoot, causing `hooks.IsInstalled` to look for `~/.claude/.claude/settings.json` (double [`.claude`](../..) segment). `RunCheckHooksWith(scopeRoot string)` is exported for tests; `checks_hooks_internal_test.go` holds internal package tests. `drifted=true` response from `hooks.IsInstalled` produces WARN "session-start hook uses legacy wrapper script — run `atomic hooks install` to migrate".

`checks_refs.go` (hash `477404b`) checks for `@.claude/project/signals.md` only. The prior bug (checking for `inferred-signals.md`) is resolved. Candidate files searched in order: [`claude.local.md`](../../../claude.local.md), [`CLAUDE.local.md`](../../../CLAUDE.local.md), [`CLAUDE.md`](../../../CLAUDE.md), [`claude.md`](../../../claude.md).

`checks_followups.go` — walks [`.claude/project/followups/`](../followups) via `followups.LoadEntriesWithErrors`. Byte-compares re-rendered INDEX against on-disk to detect drift. Two repair functions: `followupsRenderRepair` (re-renders INDEX), `followupsMigrateRepair` (runs migrate for legacy `followups.md`).

`checks_config.go` — imports [`atomic/internal/config`](../../../atomic/internal/config) directly. Validates config file structure, known key set, and value constraints.

**Post-update doctor adapter ([`atomic/internal/updatedoctor/`](../../../atomic/internal/updatedoctor)):**

- `updatedoctor.go` — called by `main.go:runUpdate` after binary swap. Calls `doctor.Run(Opts{Skip: []int{3, 8}})` — skips signals (index 3) and binary (index 8). Prints FAIL lines only (uses `format.FormatResultLine`). Recovers panics. Never changes update exit code.
- Controlled by `--no-doctor` flag (per-invocation) or `update.run_doctor = false` in config (durable).
- `RunDoctorFn` function type is the injectable test seam — production wires `doctor.Run`, tests inject stubs.

**Validation suite ([`atomic/internal/validate/`](../../../atomic/internal/validate) — 14 files):**

- `validate.go` — dispatch entry point. Modes: `spec`, `config`, `bundle`. No-args = whole-repo run.
- `spec.go` — checks S0/S1/S5/S6 spec markdown structure.
- `config.go` — checks C1/C3/C5/C7/C9 cross-reference integrity in CLAUDE.md / commands / agents / skills.
- `bundle.go` — bundle parity against embedded manifest.
- `dispatch.go` — routes to per-mode validators.
- `finding.go` — finding type (FAIL/WARN/SKIP) and formatters.
- `output.go` — output formatting (human and JSON).

**Supporting packages used by doctor:**

- [`atomic/internal/manifestcheck/`](../../../atomic/internal/manifestcheck) — called by `checks_manifest.go`. Imports `bundlespec` for inclusion predicates.
- [`atomic/internal/followups/`](../../../atomic/internal/followups) — called by `checks_followups.go`. `LoadEntriesWithErrors` is the parse boundary; `RenderIndex` is used for drift comparison.
- [`atomic/internal/config/`](../../../atomic/internal/config) — called by `checks_config.go`.

## Docs

- [`docs/spec/atomic-doctor.md`](../../../docs/spec/atomic-doctor.md) — canonical contract for all 9 check categories, fix functions, exit codes, `--fix` behavior. Master reference.
- [`docs/spec/atomic-validate.md`](../../../docs/spec/atomic-validate.md) — `atomic validate` subcommand contract (S0/S1/S5/S6, C1/C3/C5/C7/C9 checks).
- [`docs/spec/atomic-update-doctor.md`](../../../docs/spec/atomic-update-doctor.md) — post-update doctor auto-fire contract. Specifies skip indices `[3, 8]`, panic recovery, exit code preservation.
- [`docs/design/atomic-doctor.md`](../../../docs/design/atomic-doctor.md) — design rationale for the 9-check architecture.
- [`docs/design/atomic-validate.md`](../../../docs/design/atomic-validate.md) — design rationale for the validate subcommand.
- [`docs/spec/user-profile.md`](../../../docs/spec/user-profile.md) — contract for the user profile feature: schema, sections, `<stable>`/`<volatile>`/`<deterministic>` tag semantics, install-time stub generation.
- [`docs/design/user-profile.md`](../../../docs/design/user-profile.md) — design rationale for user profile capture and stub rendering.

## Coupling

- **→ bundle**: `checks_manifest.go` uses [`atomic/internal/manifestcheck/`](../../../atomic/internal/manifestcheck) which imports `bundlespec`. Changing bundle inclusion rules (bundle domain) affects which manifest check items pass/fail.
- **→ signals**: `checks_refs.go` reads candidate CLAUDE files for `@.claude/project/signals.md`. The `signalsRef` const is the single source of truth — changes to the expected @-ref path require updating this const and the signals domain's wiring convention simultaneously.
- **→ signals**: `checks_signals.go` verifies `deterministic-signals.md` exists and is not stale. Staleness logic tracks the signals domain's scan output.
- **→ config**: `checks_config.go` imports [`atomic/internal/config`](../../../atomic/internal/config) directly. Config schema changes (config domain) must be reflected in `checks_config.go` validation.
- **→ config**: `updatedoctor` skip indices `[3, 8]` are hardcoded. Adding or renumbering doctor categories requires updating `updatedoctor.go` to match.
- **→ workflow**: `checks_followups.go` imports [`atomic/internal/followups`](../../../atomic/internal/followups). Follow-up schema changes (config domain) affect what doctor accepts as valid.
- **→ docs-meta**: `format.FormatResultLine` is a shared output primitive. Changing it affects both `FormatHuman` (full doctor) and `updatedoctor` (post-update FAIL-only).
- **→ config**: `checks_profile.go` calls `config.ProfilePath` and `config.ProfileRelPath`. Adding new profile-related paths to [`atomic/internal/config/paths.go`](../../../atomic/internal/config/paths.go) (config domain) requires checking whether `checkProfile` needs updating.
- **→ bundle**: [`atomic/internal/profile/`](../../../atomic/internal/profile) is called by [`atomic/internal/claudeinstall/install.go`](../../../atomic/internal/claudeinstall/install.go) at install time to generate the profile stub. Changes to `RenderStub` or `CaptureEnv` (profile package) affect what gets written to `~/.claude/.atomic/profile.md` on fresh install.
