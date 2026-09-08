# Output style seed


## Goal


Make Atomic the effective output style in every project that has not chosen otherwise, by seeding `outputStyle: "Atomic"` into the user-level Claude Code settings file when that key is absent, and by reporting through `atomic doctor` what is set where.


## Approach


Seed the user-level key from install, update, and session start; never overwrite, never write a project file. Design: [`docs/design/output-style-seed.md`](../design/output-style-seed.md).


## Non-goals


- No write to any project-scoped `.claude/settings.json` or `.claude/settings.local.json`. This binds `--target` too: an install pointed anywhere but the user-level `~/.claude` seeds nothing at all, rather than following the target into a repo.
- No overwrite of an existing `outputStyle` value, whatever it is.
- No "seeded once" marker. The session-start trigger is ungated by decision; a deleted key is re-seeded on the next session.
- No attempt to compute Claude Code's effective precedence. Doctor reports per-file contents only.
- No row in `docs/reference/atomic-toml.md`; that page owns the repo-scoped config and this flag is user-scoped.


## Success criteria


1. With `outputStyle` absent from `<scopeRoot>/.claude/settings.json`, each of `atomic claude install`, `atomic claude update`, and `atomic hooks session-start` writes `"Atomic"` exactly once, and a second run is a no-op.
2. With `outputStyle` already set to any value, no trigger modifies it.
3. With the bundle's `output-styles/atomic.md` absent from the install target, no trigger writes the key.
4. `atomic config set output_style.seed false` suppresses every trigger, and `atomic config list` shows the resolved value.
5. `writeSettingsHujson` never leaves a partial file observable: a concurrent reader sees either the old bytes or the new ones.
6. A symlinked `settings.json` is still a symlink after any write.
7. A read-only `settings.json` is not modified, and the failure does not abort an install or a session start.
8. A malformed `settings.json` produces a warning from install and silence from the hook; neither aborts.
9. `atomic hooks session-start` never returns a non-zero exit or a blocking error because of the seed.
10. `atomic hooks uninstall` removes `outputStyle` only when its value is exactly `"Atomic"` **and** the style file is already absent. With the style still installed it leaves the key alone, so disabling just the hook does not cost the user their output style.
11. `atomic doctor` reports the user-level key, flags a project-level value as a possible override, and warns when the key names a style that is not installed. It never flags the user-level file as an override of itself: when the repo root's `.claude` resolves to the install target, as it does running from `$HOME`, the project scan is skipped.
12. `atomic doctor --fix` seeds the user level only, and does nothing when `seed = false`.
13. `atomic claude install --dry-run` lists the seed in its plan and writes nothing.


## Change tree


```
atomic/
  internal/
    hooks/
      outputstyle.go            A  SeedOutputStyle, ReadOutputStyle,
                                   RemoveOutputStyleIfAtomic, style-file guard
      outputstyle_test.go       A  seed/no-overwrite/guard/flag/uninstall cases
      hooks.go                  M  SessionStart + SessionStartText call the seed;
                                   Uninstall removes the key
      hooks_hujson.go           M  writeSettingsHujson becomes atomic;
                                   resolveSettingsTarget added
      hooks_hujson_test.go      A  atomic-write, symlink, read-only, parity cases
      hooks_test.go             M  atomic-write, symlink, read-only cases
    config/
      config.go                 M  outputStyleSection, Config.OutputStyle,
                                   Default, Load presence, knownKeys, Set, Unset
      config_test.go            M  default/explicit-false/set/unset cases
      render.go                 M  Resolved exposes output_style.seed
    claudeinstall/
      install.go                M  installOrUpdate seeds after Apply, user-level
                                   target only; dry-run plan line; announcement
      install_test.go           M  seed-on-install, dry-run, guard cases
    doctor/
      checks_output_style.go    A  category 14 check
      checks_output_style_test.go  A
      doctor.go                 M  register category 14
      fix.go                    M  Repairer.OutputStyleFn, repairPlan case
      fix_impls.go              M  defaultOutputStyleRepair
      fix_impls_internal_test.go   A  repair no-write and write cases
      fix_test.go               M  repair-plan fixability cases
      doctor_test.go            M  registry count, names, sandboxed HOME
      checks_hooks_test.go      M  two-value Install/Uninstall signatures
      checks_hooks_internal_test.go  M  same
  cmd/atomic/
    cmd_claude.go               M  printPostInstallHint no longer tells the user
                                   to activate the style by hand
    cmd_hooks.go                M  install/uninstall report a skipped write
install.sh                      M  no longer promises an activation step
docs/
  spec/output-style-seed.md     A  this file
  spec/atomic-doctor.md         M  category 14 catalog and repair rows
  reference/output-style.md     M  activation section rewritten
  guides/install.md             M  activate becomes verify
  guides/getting-started.md     M  step 1 becomes a check, not an action
  index.md                      M  landing page drops manual activation
  public/img/output-style-*.png D  screenshots of the removed manual flow
  spec/atomic-state-and-config.md  M  output_style.seed in the key schema
context/
  commands/atomic-help.md       M  style row, doctor row, stage 4 tour line
```


## Outline


```
atomic/internal/hooks/outputstyle.go
  OutputStyleName                  — the value written, "Atomic"
  OutputStyleRelPath               — "output-styles/atomic.md", the guard target
  SeedOutputStyle                  — write the key when absent; honors the flag
                                     and the guard; reports whether it wrote
  ReadOutputStyle                  — read the key from one settings file
  RemoveOutputStyleIfAtomic        — delete the key only when it equals "Atomic"
                                     and the style file is already gone
  StyleInstalled                   — guard: does the target carry the style file
  SeedEnabled                      — read output_style.seed from user config
  setOutputStyleMember             — set or append the top-level key on the AST
  removeOutputStyleMember          — drop the top-level key from the AST

atomic/internal/hooks/hooks.go
  SessionStart                     — also fires the seed, silently
  SessionStartText                 — same
  seedOutputStyleSilent            — swallow every error, mirror refreshProfile
  Install                          — reshaped: reports a skipped write
  Uninstall                        — removes the key when it is "Atomic" and the
                                     style file is already gone; reports a
                                     skipped write
  migrateLegacy                    — reshaped: reports a skipped write

atomic/internal/hooks/hooks_hujson.go
  writeSettingsHujson              — temp file plus rename, not truncate-in-place
  resolveSettingsTarget            — EvalSymlinks, then probe writability
  registerInSettings               — reshaped: returns skipped alongside err
  unregisterFromSettings           — same

atomic/internal/hooks/hooks.go (paths)
  SettingsPath                     — scope root to its settings.json, exported
  SameDir                          — symlink-resolving directory comparison,
                                     shared by claudeinstall and doctor

atomic/internal/config/config.go
  outputStyleSection               — the [output_style] table
  outputStyleSeedDefault           — built-in default, true
  Config.OutputStyle               — the new field
  Default                          — seeds the default
  Load                             — explicit-presence backfill, as run_doctor
  knownKeys                        — adds output_style.seed
  Set                              — parses true/false
  Unset                            — reverts to seedDefault

atomic/internal/config/render.go
  Resolved                         — emits output_style.seed

atomic/internal/claudeinstall/install.go
  installOrUpdate                  — seeds after Apply; plan line on dry run
  seedOutputStyleForInstall        — user-level target only; announce, warn on error
  isUserLevelTarget                — the Non-goal 1 guard, via hooks.SameDir
  manifestHasOutputStyle           — dry-run guard, manifest rather than disk
  planOutputStyleSeed              — report the intended seed, write nothing

atomic/internal/doctor/checks_output_style.go
  checkOutputStyle                 — category 14 entry point
  RunCheckOutputStyleWith          — same, with roots injected for tests
  projectOutputStyleOverrides      — list project-level values, never a precedence

atomic/internal/doctor/doctor.go
  categories                       — appends {Index: 14, Name: "output-style"}

atomic/internal/doctor/fix.go
  Repairer.OutputStyleFn           — the repair seam
  repairPlan                       — fixable only when seeding can actually help

atomic/internal/doctor/fix_impls.go
  defaultOutputStyleRepair         — seeds the user level; errNonFixable on no-write
```


## Flows


**Seed on install or update**

1. `installOrUpdate` applies the bundle, then calls `seedOutputStyleForInstall` with `targetDir`, `home`, and the manifest.
2. `targetDir` is compared against the user-level `~/.claude`. Anything else — a `--target` install into a repo or elsewhere — returns immediately, writing nothing and planning nothing. This is what keeps the seed out of a project's committed settings file.
3. The manifest is searched for an artifact whose target is `output-styles/atomic.md`. Absent → return, no plan line.
4. `output_style.seed` is read from `~/.atomic/config.toml`. False → return.
5. `~/.claude/settings.json` is read. A parse failure warns and returns; an artifact install never aborts over it.
6. `outputStyle` present → return. Absent → write `"Atomic"` and print one line naming `atomic config set output_style.seed false` as the opt-out.
7. Under `--dry-run`, steps 5 and 6 report the intended write and change nothing.

**Seed on session start**

1. `SessionStart` calls `seedOutputStyleSilent` alongside `refreshProfile`.
2. Home resolves to `~`; the target is `~/.claude`. An unresolvable home returns.
3. `~/.claude/output-styles/atomic.md` missing → return. This is what makes a `--target` install no-op here.
4. `output_style.seed` false → return.
5. `~/.claude/settings.json` is read. Malformed, unreadable, read-only, or `outputStyle` already present → return.
6. Otherwise write `"Atomic"`. Every error is swallowed; the hook payload is unaffected either way.

**Atomic settings write**

1. `writeSettingsHujson` resolves the path through `filepath.EvalSymlinks`, so a dotfiles symlink is followed rather than replaced.
2. It probes writability with `os.OpenFile(resolved, O_WRONLY, 0)`; `EACCES` returns a skip, not a failure.
3. It writes a temp file in the same directory, then `os.Rename` over the resolved path.
4. A reader racing the write observes either the complete old file or the complete new one.

**Remove on uninstall**

1. `atomic hooks uninstall` unregisters the SessionStart command as it does today.
2. It removes `outputStyle` **only when the style file is already gone** — that is, when `<scopeRoot>/.claude/output-styles/atomic.md` does not exist. While the style is still installed, the key names something real and is left alone.
3. When the guard passes, the key is removed only if its value is exactly `"Atomic"`. Any other value, an absent key, or an absent file is a no-op.

The style-file guard is what keeps the two uninstall flows apart. `atomic hooks uninstall` is the documented way to disable just the session-start hook while keeping the artifact bundle; a user doing that keeps their output style, because the style they point at is still installed. Only after the bundle is removed does the key name a missing style, and then it is cleaned up.

**Doctor report**

1. Category 14 reads the user-level `outputStyle`.
2. Absent → WARN, repairable.
3. Present but naming a style with no installed file → WARN.
4. Present → PASS, reporting the value.
5. Either way, any `outputStyle` found in `<repoRoot>/.claude/settings.json` or `settings.local.json` is reported as a possible override. The scan is skipped when `<repoRoot>/.claude` and the install target resolve to the same directory, so running from `$HOME` does not report the user-level file as overriding itself. The comparison resolves symlinks, since a `$HOME` or temp dir that is itself a symlink is routine on macOS.
6. `--fix` seeds the user level only, and returns without action when `seed = false`.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Atomic settings write | `hooks/hooks_hujson.go`, `hooks/hooks_test.go` | temp+rename, `EvalSymlinks`, read-only probe, mode preserved; existing register/unregister tests still green; new symlink and read-only tests pass |
| 2 | `output_style.seed` user-config key | `config/config.go`, `config/render.go`, `config/config_test.go` | `Default`, `Load` presence backfill, `knownKeys`, `Set`, `Unset`, `Resolved`; `atomic config set/unset/list` round-trip |
| 3 | Seed, read, remove primitives | `hooks/outputstyle.go`, `hooks/outputstyle_test.go` | seeds when absent, never overwrites, honors style-file guard and flag, removes only an exact `"Atomic"` |
| 4 | Triggers wired | `claudeinstall/install.go`, `hooks/hooks.go` | `installOrUpdate` seeds and announces and honors `--dry-run`; `SessionStart` and `SessionStartText` seed silently; `hooks.Uninstall` removes |
| 5 | Doctor category 14 | `doctor/checks_output_style.go`, `doctor/doctor.go`, `doctor/fix_impls.go` | reports user and project values, warns on a missing style file, `--fix` seeds user level and no-ops when disabled |
| 6 | Docs and artifacts | `cmd/atomic/cmd_claude.go`, `docs/reference/output-style.md`, `docs/guides/install.md`, `docs/spec/atomic-state-and-config.md`, `context/commands/atomic-help.md` | the stale "explicit user opt-in" post-install hint is gone; `make -C atomic bundle` clean; `atomic validate spec` and `atomic validate config` pass |


## Risks


| Risk | Mitigation |
|------|-----------|
| The write wakes every running session's settings watcher and fires `ConfigChange` | Accepted and documented; the write happens at most once per machine per absent key |
| A user deletes the key expecting the stock style and it returns next session | Accepted by explicit decision; the opt-out is the config flag, and the docs must lead with it |
| `os.Rename` across a symlink severs a dotfiles repo | `EvalSymlinks` before the rename |
| A lost update against Claude Code's own `/config` write | Accepted; the window is microseconds and Claude Code writes this file only on a `/config` change |
| `"Default"` as an explicit opt-out value is unverified | Do not document that route unless a cheap check confirms it |
| Seeding a key that names an uninstalled style | Guard on the style file, per trigger, with the manifest standing in under `--dry-run` |


## Change log


### 2026-09-08 — Initial spec

**What changed:** First contract, derived from the approved design.

**Why:** Sessions in 113 of 118 local projects start on the default output style because `/config` writes `outputStyle` project-locally and the user-level key was never set.

### 2026-09-08 — Correction: AST helpers live with the domain logic

**What changed:** `setOutputStyleMember` and `removeOutputStyleMember` moved in the Outline from `hooks/hooks_hujson.go` to `hooks/outputstyle.go`. The change tree also now names `hooks/hooks_hujson_test.go`, which Checkpoint 1 created and the original tree omitted.

**Why:** Checkpoint 3's reviewer flagged the divergence between the delivered code and the Outline. The delivered placement is the better one — `hooks_hujson.go` holds generic JWCC plumbing, and these two helpers are output-style-specific — so the spec is corrected to match rather than the code moved to match the spec.

### 2026-09-08 — Add the stale post-install hint to Checkpoint 6

**What changed:** `atomic/cmd/atomic/cmd_claude.go` joins the change tree and Checkpoint 6. Its `printPostInstallHint` prints "open claude code and run /config → output style → Atomic (claude code requires explicit user opt-in for output styles)", and its doc comment repeats the claim.

**Why:** Found by hand-driving the built binary after Checkpoint 4, not by the test suite. The line is false the moment the seed fires — install now tells the user to perform by hand the step it just performed for them.

### 2026-09-08 — Correction: doctor's self-override skip, and one directory comparison

**What changed:** Success criterion 11 and the "Doctor report" flow now state that the project scan is skipped when the repo's `.claude` resolves to the install target, so running from `$HOME` cannot report the user-level file as its own override. `docs/spec/atomic-doctor.md`'s catalog row carries the same qualification. The Outline gains `hooks.SameDir`, `hooks.SettingsPath`, `isUserLevelTarget`, the reshaped `Install`/`Uninstall`/`migrateLegacy`/`registerInSettings`/`unregisterFromSettings`, and the corrected `outputStyleSeedDefault` name; the change tree gains the two `checks_hooks` test files.

**Why:** the second audit found the skip had shipped with no spec amendment, so a fresh-context subagent reading either body would have rebuilt the self-flag bug. It also found `isUserLevelTarget` and doctor's `sameDir` giving two answers to one question five files apart, with a verified false negative when `$HOME` sits under a symlink, which is routine on macOS. One `hooks.SameDir` now answers it for both, and `settingsPath`/`SettingsPath` collapsed to a single exported name.

### 2026-09-08 — Correction: the seed is user-level only, never a `--target` install

**What changed:** `seedOutputStyleForInstall` fires only when the install target is the user-level `~/.claude`. Non-goal 1, the Guard section, and the install flow are rewritten to say so.

**Superseded:** the seed previously derived its scope root from `filepath.Dir(targetDir)` and followed `--target` wherever it pointed.

**Correction:** found by the final audit, which reproduced it: `atomic claude install --target ./.claude`, the project-scoped install route documented in `docs/guides/install.md`, wrote `outputStyle` into the repo's **committed** `.claude/settings.json` and nothing at the user level. That is a direct violation of Non-goal 1, which this spec has carried since the first draft. The earlier "the path derives from the install target, so the seed follows it" reasoning was about `~/.claude` being relocatable; it did not consider that a target can be a repo.

### 2026-09-08 — Correction: outline and change tree realigned to the delivered work

**What changed:** `## Change tree` and `## Outline` rewritten to match what shipped. `styleInstalled`/`seedEnabled`/`outputStyleRelPath` are exported as `StyleInstalled`/`SeedEnabled`/`OutputStyleRelPath` and consumed by `doctor` and `claudeinstall`; the install seed sits after `Apply`, not after `populateProfile`; `manifestHasOutputStyle`, `planOutputStyleSeed`, `projectOutputStyleOverrides`, `Repairer.OutputStyleFn`, and `repairPlan` are named; `applyOutputStyleRepair` shipped as `defaultOutputStyleRepair`; `doctor/fix.go`, `docs/spec/atomic-doctor.md`, `cmd_hooks.go`, `install.sh`, `docs/guides/getting-started.md`, `docs/index.md`, and the deleted screenshots are added; the never-delivered `design/…  M implementation log appended` row is removed.

**Why:** the final audit found both sections had drifted across six checkpoints, having been amended only twice. A stale outline is what a later fresh-context subagent builds against, so it is corrected rather than left.

### 2026-09-08 — Correction: uninstall removal is guarded on the style file

**What changed:** `hooks.Uninstall` removes `outputStyle` only when the style file at `<scopeRoot>/.claude/output-styles/atomic.md` is already absent, in addition to the value being exactly `"Atomic"`. Success criterion 10, the Outline entry, and the "Remove on uninstall" flow are rewritten to match.

**Superseded:** the key was previously removed on every `atomic hooks uninstall` whose value was `"Atomic"`, with no style-file check.

**Why:** Checkpoint 4's reviewer caught that `hooks.Uninstall` is reached by `atomic hooks uninstall`, which never deletes the style file — it is the documented way to disable only the session-start hook (`docs/guides/install.md`). As originally specified, a user disabling the hook silently lost their output style even though the style remained installed. The original rationale, that the key must not name a deleted style, only applies once the bundle is actually gone, which is exactly what the new guard tests.
