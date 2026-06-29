# atomic-migrate framework (workstream C)


## Goal

A versioned, replayable migration framework (`atomic/internal/migrate/`) backed by install state in `config.toml [install]`, so breaking changes across atomic versions can be applied to any prior install without one-shot scripts.


## Non-goals

- The `docs/wiki/` target layout and the relocation transform are workstream B's contract; C only registers that transform as migration step v1.
- Agent model overrides (`config.toml [agents]`) are workstream F's contract; C defines the config schema v2 that F builds on.
- Cobra migration is workstream A.
- No new staleness mechanism — drift detection is workstream E.


## Success criteria

- [ ] `atomic/internal/migrate/` compiles with `Migration{TargetVersion string, Scope string, Up func(ctx) error}` type and an ordered registry slice.
- [ ] `parseSemver`/`compare` reused from `internal/selfupdate/semver.go` (or promoted to an exported helper in that package); no duplicate implementation.
- [ ] `internal/config` parses and writes `config.toml [install].version` and `[install.artifacts]` per-kind lists (`agents`, `commands`, `skills`, `output-styles`, `rules`).
- [ ] `checks_config.go` validates config schema v2 (the two new `[install]` keys alongside existing `output.signals.max_depth` and `update.run_doctor`); invalid values are reported as a doctor finding.
- [ ] `atomic claude install` writes `[install].version` (current binary version) and `[install.artifacts]` (what it copied) on completion.
- [ ] `atomic update` auto-runs install-scope migration steps after artifact refresh (in `main.go:runUpdate`), in semver order, before exiting.
- [ ] `atomic migrate` verb exists and is registered in `cliusage.go` with `--repo [path]` and `--realm [path]` flags; `atomic validate artifacts` passes (A1 linter).
- [ ] `atomic migrate` (no flags) runs install-scope steps against `~/.claude/`.
- [ ] `atomic migrate --repo [path]` reads the `<wiki-schema>N</wiki-schema>` block from that repo's `docs/wiki/index.md` as the version anchor and runs repo-scope steps.
- [ ] `atomic migrate --realm [path]` runs install-scope steps then prompts to migrate each atomic'd member repo (fan-out, one confirm per repo).
- [ ] First registered step (signals→`docs/wiki/` relocation) is a no-op when `docs/wiki/index.md` already exists; otherwise delegates to the workstream B transform.
- [ ] Pre-framework first run (no `[install]` table in config.toml) treats the recorded version as a floor (v0.0.0) and runs all steps; idempotency of each `Up` prevents damage.
- [ ] Prune: on install and on `atomic migrate`, diff `[install.artifacts].<kind>` against the current bundle; present a batched confirm listing all artifacts to remove; only removes entries that appear in `install.artifacts` (never user-added files).
- [ ] `claudeinstall/uninstall.go` consults `[install.artifacts]` to scope which agents/commands/skills/etc. to remove; does not remove files absent from `install.artifacts`.
- [ ] Doctor emits a nudge ("run `atomic migrate`") when binary version > `[install].version`.
- [ ] `/atomic-help` maintenance/binary topic and `docs/guides/install.md` mention `atomic migrate`.
- [ ] `make render` + `make -C atomic bundle` pass with no drift after all artifact edits.


## Approach

Ordered registry of idempotent `Migration` steps in a new `atomic/internal/migrate/` package, version-anchored by `config.toml [install]` (global) and `<wiki-schema>` in `docs/wiki/index.md` (per-repo); see `docs/design/signals-wiki-unification.md` ("Versioned migration framework (workstream C)") for the full design.


## Checkpoints

| # | Checkpoint | Files / areas | Agent | Est. files | Verifies |
|---|-----------|--------------|-------|-----------|---------|
| C1 | `migrate` package: `Migration` type, ordered registry, runner (read version → run steps > recorded → write version), semver reuse | `atomic/internal/migrate/` (new); `atomic/internal/selfupdate/semver.go` (export or add thin exported wrapper) | builder | 3–5 | runner applies only steps with `TargetVersion > recorded`; idempotent re-run changes nothing; semver compare matches `selfupdate` behavior |
| C2 | Config schema v2: `[install]` table in `internal/config`; `checks_config.go` validation | `atomic/internal/config/` (extend schema); `atomic/internal/doctor/checks_config.go` | builder | 3–5 | `config.toml` with `[install].version` and `[install.artifacts]` round-trips; invalid keys reported; missing `[install]` (pre-framework install) is valid (not an error) |
| C3 | Install-time manifest write + prune + `uninstall.go` scoping | `atomic/internal/claudeinstall/` (write manifest after copy; reuse `snapshot.go` file-walk helpers, keep distinct from uninstall snapshot); `atomic/internal/claudeinstall/uninstall.go` (consult `[install.artifacts]` to remove only atomic-installed files; batched confirm for prune) | builder | 4–6 | install writes `[install]`; prune batched-confirm lists only `install.artifacts` entries absent from current bundle; `uninstall` does not touch user-added files |
| C4 | `atomic migrate` verb + first step registration + `runUpdate` hook | `atomic/cmd/atomic/main.go` (new `migrate` case; `runUpdate` calls runner after artifact refresh); `atomic/internal/cliusage/cliusage.go` (register `migrate` + `--repo`/`--realm`); `atomic/internal/migrate/steps.go` (first step: no-op if `docs/wiki/index.md` exists, else invoke B's relocation); `<wiki-schema>` block read/write in `docs/wiki/index.md` for repo-scope anchor | builder | 5–8 | `atomic migrate` runs with no flags (install-scope); `--repo` reads `<wiki-schema>` correctly; `--realm` prompts fan-out; first step is a no-op when target already exists; `atomic validate artifacts` passes A1 lint |
| C5 | Doctor drift check + help + docs | `atomic/internal/doctor/` (new install-version check: binary version > `[install].version` → emit nudge); `templates/commands/atomic-help.md` (maintenance/binary topic row for `migrate`); `docs/guides/install.md` (migration-on-update paragraph); `make render` + `make bundle` | builder | 4–6 | doctor reports drift when version mismatch; `/atomic-help` lists `atomic migrate`; install guide describes auto-migration on update |


## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| `parseSemver` / `semver.compare` are unexported in `internal/selfupdate` — C1 cannot call them directly | High | Medium | Add a thin exported `CompareSemver(a, b string) int` in `selfupdate` and document it as the canonical compare; do not duplicate the logic |
| Pre-framework installs (no `[install]`) running prune see no `install.artifacts` and could no-op silently, masking drift | Medium | Low | Treat missing `[install]` as floor (v0.0.0 / empty artifacts list); prune produces an empty diff → no batched confirm → no removals; first-time migrate writes the manifest after running all steps |
| `migrate --realm` fan-out requires enumerating "atomic'd member repos" — no current registry of managed repos | Medium | Medium | Enumerate by presence of `docs/wiki/index.md` with a `<wiki-schema>` block; repos without the block are skipped with a warning; design doc defers this enumeration strategy to the C spec (this) |
| Prune's batched confirm removes an artifact the user hand-modified (a managed artifact they edited) | Low | Medium | Prune only removes entries in `install.artifacts`; warn in the confirm prompt that the file may have been modified; user can decline per-artifact if the list is scoped to individual files |
| First migration step depends on B's relocation transform existing at the time C ships | Medium | High | C and B must ship in order (B first); C's first step is a stub no-op when B's transform is unavailable, logging a skip instead of erroring |


## Change log

(none)
