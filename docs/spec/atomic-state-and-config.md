# atomic state directory + config


## Goal


Consolidate atomic-owned state under `~/.atomic/` and ship a TOML-backed config (`atomic config get|set|unset|list|path`). `config.toml` is the single source of truth; `atomic config list` renders resolved values on demand. Config is consumed by the `atomic` binary, not injected into Claude sessions.


## Non-goals


- Project-local config overrides (a per-repo `config.toml` shadowing `~/.atomic/config.toml`). Deferred until a concrete case appears — distinct from `harness.dir` (below), which names a repo-local *state-directory*, not a config-override mechanism, and is itself user-level only (no per-repo override).
- Migrating legacy paths (`~/.claude/.atomic-backups/`, `~/.claude/CLAUDE.md.atomic-proposed`). Old paths orphaned; user cleans up.
- Moving `<bin-dir>/.atomic.new` (selfupdate staged binary). Cross-filesystem `os.Rename` constraint.
- Bundling `config.toml`. User state, never bundled.
- Hook-based config delivery. Hook stays out of the config path entirely.
- Template engine for `CLAUDE.md` or any artifact.
- Sentinel block / managed-region parser inside `CLAUDE.md`.
- Lockfile for concurrent `atomic config set` writes. `os.Rename` atomic-write is sufficient.


## Success criteria


- [ ] `atomic config get|set|unset|list|path` work end-to-end against the schema below.
- [ ] `atomic config set <key> <value>` rejects unknown keys and unknown values with a typo-suggesting error; valid sets atomically write `config.toml`.
- [ ] On fresh install, SHA-compare of installed vs bundled `CLAUDE.md` matches (no divergence, no `.atomic-proposed` written).
- [ ] `claudeinstall` writes backups to `~/.atomic/backups/<ts>/` and proposed merges to `~/.atomic/proposed/CLAUDE.md`.
- [ ] `atomic doctor` includes a `config` check (TOML parses, no unknown keys, values validate).
- [ ] `atomic-claude-merger` agent and `/atomic-claude-merge` command reference the new proposed path.
- [ ] Axiom 2 amended in `.claude/rules/authoring/axioms.md` with the shell-settable carve-out.


## Layout (target end state)


```
~/.atomic/
├── config.toml              # user-written, atomic config set rewrites
├── backups/<ts>/<relpath>   # claudeinstall pre-write backups
├── proposed/
│   └── CLAUDE.md            # claudeinstall divergence merge target
└── state.json               # machine-managed selfupdate state; atomic temp+rename; never hand-edited
```

Selfupdate stages a downloaded, checksum-verified release archive at a fixed XDG-style path, `~/.cache/atomic/staged/` (hardcoded; not resolved via `os.UserCacheDir()`). The directory is disposable, safe to delete anytime. `state.json`'s `staged` field is the sole authority on what is currently staged, not the file's mere presence on disk.

## State file schema (`~/.atomic/state.json`)

Unlike `config.toml` above, `state.json` is machine-managed: written only by `atomic` itself (the detached background-check child and `atomic update`'s lock/swap logic), atomically via temp+rename, and never hand-edited. It holds one top-level `update` block:

| Field | Type | Meaning |
|-------|------|---------|
| `last_check` | timestamp | when the periodic background lookup last ran |
| `updating` | bool | true while an `atomic update` swap (or background stage) holds the lock |
| `update_started_at` | timestamp | lock-acquisition time; a lock older than 10 minutes is considered abandoned |
| `updated_at` | timestamp | when the last successful swap completed |
| `last_notified` | timestamp | when the update-available banner last printed (rate-limited to once per 24h) |
| `latest_version` | string | most recent version seen by the background lookup |
| `stage_attempted_for` | string | version the once-per-version staging gate has already attempted |
| `last_result` | string | empty on success; the background lookup's or staging attempt's error otherwise |
| `staged.version`, `staged.path`, `staged.sha256` | string | the pre-verified release archive `atomic update` can swap from without re-downloading |

See [`selfupdate-state.md`](./selfupdate-state.md) for the full spawn cadence, lock-acquisition rules, and staged-swap flow that read and write this file.


## Schema (v1 — start narrow)


```toml
[output.signals]
max_depth = 3               # positive integer; bounded tree depth in `atomic signals scan`

[update]
run_doctor = true           # true | false; run doctor after `atomic update`
check = true                # true | false; enable the hourly detached background version check
stage = true                # true | false; enable once-per-version background staged download

[harness]
dir = ".claude"              # single non-empty path segment; repo-local state-directory name
```


Current keys: `output.signals.max_depth`, `update.run_doctor`, `update.check`, `update.stage`, `harness.dir`. Further keys (`forge.*`, `cleanup.*`, …) are added per concrete steering need in follow-up specs. Each schema addition: schema entry → renderer entry → one steering site reading it → change-log entry on this spec.

`update.check` and `update.stage` (bool, default `true`) gate the two halves of the detached background-update child described in [`selfupdate-state.md`](./selfupdate-state.md): `update.check` enables the hourly GitHub lookup that any invoked verb may spawn; `update.stage` enables that child's once-per-version download-and-checksum-verify into `~/.cache/atomic/staged/`. Both are user-level only — no repo-scoped equivalent.

`harness.dir` (string, default `.claude`) names the repo-local state-directory every repo-scoped `atomic` verb resolves against — `<repo>/<harness.dir>/.scratchpad`, `<repo>/<harness.dir>/project`, `<repo>/<harness.dir>/.atomic-index`, `<repo>/<harness.dir>/atomic.toml`, `<repo>/<harness.dir>/worktrees` — decoupling those paths from Claude Code's `.claude` convention (e.g. `atomic config set harness.dir .pi` for a `pi` harness). It is unrelated to the `~/.atomic` user-state root above: `~/.atomic` is fixed and not configurable (see Non-goals). Validation: **write (`set`)** rejects empty, `.`, `..`, and any value containing `/` — same shape as every other write-time rejection in this schema. **Read (load)** goes one step further than the generic unknown-key leniency described below: a stored value that fails that same shape check (e.g. hand-edited to `..`) is not merely warned about — the resolver silently falls back to the built-in default, because an unvalidated value would otherwise reach `filepath.Join` unguarded in every repo-local path helper.


## Precedence (highest wins)


| # | Source | Role |
|---|--------|------|
| 1 | Built-in defaults (Go constants) | Fallback |
| 2 | `~/.atomic/config.toml` | **Durable floor** (set from shell) |
| 3 | Per-conversation memory | **Per-conversation nudge**, scoped to session/task |
| 4 | Command-line flag | One-shot override |


Memory entries overriding config must be scoped ("for this session", "for this task"), never "remember forever" — stale memory must not silently outlive `atomic config set`.


## Validation policy


- **Write (`set`)**: strict. Reject unknown keys, reject values outside the allowed enum/range, suggest a near-match key on typo (Levenshtein ≤ 2).
- **Read (load)**: lenient. Unknown keys ignored with a single WARN log line. Allows newer-config / older-binary forward-compat.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | New package `atomic/internal/config/` with TOML load (lenient), schema validate (strict), get/set/unset, atomic write via `os.Rename` from tmp | `atomic/internal/config/*.go` | unit: round-trip set→load→get; unknown key rejected on set; unknown key ignored on load with WARN |
| 2 | Renderer: `config.resolved.md` generated from resolved values (TOML + defaults) | `atomic/internal/config/render.go` | unit: deterministic output (byte-stable for same input); empty TOML renders empty-but-present file with header |
| 3 | CLI wiring: `atomic config get|set|unset|list|path`, including `list --json` | `atomic/cmd/atomic/main.go`, `atomic/internal/config/cli.go` | integration: each subcommand exit codes + output match contract; typo suggestion fires on near-match |
| 4 | Bundle source `CLAUDE.md` adds line `@~/.atomic/config.resolved.md` and a one-paragraph mention of the `.atomic/` namespace | `CLAUDE.md` (repo root), bundle regen via `make -C atomic bundle` | CI "Verify bundle is committed" passes; `manifest.go` reflects new CLAUDE.md hash |
| 5 | `claudeinstall` writes backups to `.atomic/backups/<ts>/` and proposed merges to `.atomic/proposed/CLAUDE.md`; pre-creates empty `~/.atomic/config.resolved.md` on first install | `atomic/internal/claudeinstall/install.go` (lines 81, 132, 275-276 + pre-create step) | unit: fresh install creates `.atomic/config.resolved.md`; backup written to new path; divergent CLAUDE.md proposed at new path |
| 6 | Update cross-references to the proposed path | `agents/atomic-claude-merger.md`, `commands/atomic-claude-merge.md` | grep: no remaining `CLAUDE.md.atomic-proposed` string in agents/ or commands/ |
| 7 | New `doctor` check category `config`: TOML present + parses, no unknown keys, `config.resolved.md` matches render of TOML; `--fix` re-renders on drift | `atomic/internal/doctor/checks_config.go`, `checks_config_test.go`, dispatch wiring | unit: PASS/WARN/FAIL paths; integration: `--fix` re-renders and check goes PASS |
| 8 | `doctor` install-integrity scans `.atomic/` paths (no legacy path scan) | `atomic/internal/doctor/checks_install.go` | unit: install check passes with new paths populated, regardless of legacy-path presence |
| 9 | Amend `docs/spec/atomic-doctor.md`: add category #9 entry + change-log entry per spec-amendment rule | `docs/spec/atomic-doctor.md` | spec body lists check #9; change log has dated entry referencing this spec |
| 10 | Amend `.claude/rules/authoring/axioms.md` axiom 2 with shell-settable carve-out | `.claude/rules/authoring/axioms.md` | grep: carve-out paragraph present under axiom 2 |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Schema additions in newer binaries break older binaries that read the same `config.toml` | med | Lenient read (unknown keys ignored with WARN); strict only on write |
| Old `~/.claude/.atomic-backups/` and `CLAUDE.md.atomic-proposed` pile up indefinitely | low | Accepted. Documented in non-goals; user cleans up |
| `CLAUDE.md` source edit forgotten when regenerating bundle | low | Existing `.githooks/pre-commit` regens; CI `git diff --exit-code` enforces |
| Memory entries overriding config silently outlive `atomic config set` | med | Axiom-2 carve-out documents the scoping rule; rely on user discipline |


## Open questions


- Should the config support per-project overrides (a repo-local `config.toml` shadowing the user config)? Deferred. Non-goal for v1 — `harness.dir` (below) covers the one concrete per-project need that has appeared so far (naming the repo-local state-directory), but it is a single user-level key, not a per-project override mechanism. Revisit if a steering value genuinely needs to vary per project, not per user.


## Change log


### 2026-08-09 — Add state.json + update.check/update.stage config keys

**What changed:** The Layout tree drops the stale `cache/` reserved placeholder and `state.json`'s "reserved (future)" annotation, describing it instead as the machine-managed selfupdate state file it now is; a real staging path is documented separately: a fixed XDG-style path, `~/.cache/atomic/staged/` (hardcoded, not `os.UserCacheDir()`), disposable; `state.json`'s `staged` field is the authority, not the file's presence. A new `## State file schema` section documents `state.json`'s `update` block: `last_check`, `updating`, `update_started_at`, `updated_at`, `last_notified`, `latest_version`, `stage_attempted_for`, `last_result`, and `staged{version,path,sha256}`. Schema v1 gains two config keys: `update.check` and `update.stage` (bool, default `true` each), gating the hourly background lookup and the once-per-version background staging respectively.

| Key | Type | Default | Valid values |
|-----|------|---------|--------------|
| `update.check` | bool | `true` | `true`, `false` |
| `update.stage` | bool | `true` | `true`, `false` |

**Why:** `docs/spec/selfupdate-state.md` replaces the old in-process goroutine/cache-file version check with a detached child that reads and writes `~/.atomic/state.json`, and adds a once-per-version staged-download fast path for `atomic update`.

**Superseded:** The Layout tree previously listed `cache/  # reserved (selfupdate version-check, future)` and `state.json  # reserved (last-update-check, future)` as unused placeholders with no schema.

### 2026-07-16 — Add harness.dir config key

**What changed:** Schema gains a fourth key: `harness.dir` (string, default `.claude`). Names the repo-local state-directory every repo-scoped `atomic` verb resolves against (`.scratchpad`, `project`, `.atomic-index`, `atomic.toml`, `worktrees`), decoupling those paths from Claude Code's `.claude` convention. Write-time (`set`) validation: a single non-empty path segment, never `.` or `..`, never containing `/`. Read-time (`load`) goes beyond the generic unknown-key leniency: a stored value that fails that same shape check is not merely warned about — the resolver silently falls back to the default, since an unvalidated value would otherwise reach `filepath.Join` unguarded in every repo-local path helper.

| Key | Type | Default | Valid values |
|-----|------|---------|--------------|
| `harness.dir` | string | `.claude` | single non-empty path segment; not `.`, `..`, or containing `/` |

**Why:** `docs/spec/configurable-state-paths.md` (issue #150) decouples CLI-managed repo-local paths from Claude Code conventions so `atomic` can operate under other agent harnesses (e.g. `.pi/` instead of `.claude/`).

### 2026-07-16 — User state root relocated to ~/.atomic

**What changed:** Every body mention of the state root (`Goal`, `Layout`, `Success criteria`, `Precedence`, `Checkpoints`) now reads `~/.atomic/...` in place of `~/.claude/.atomic/...`. The `Non-goals` and `Open questions` entries that referenced a hypothetical project-local `.claude/.atomic/config.toml` override are reworded to drop the now-nonsensical path — the state root no longer nests under `.claude` at all — while keeping the non-goal itself (no project-local config override) unchanged.

**Why:** `docs/spec/configurable-state-paths.md` (issue #150) moves per-user state from `~/.claude/.atomic/` to `~/.atomic/`, with an automatic, idempotent migration (rename + compat symlink at the old path) so existing `@~/.claude/.atomic/...` refs keep resolving.

**Superseded:** Prior body named `~/.claude/.atomic/` as the state root throughout.

### 2026-06-07 — Remove output.intensity config key

**What changed:** Dropped `output.intensity` (the original v1 key) from the schema. The atomic output style is now single-mode; the `lite` / `full` / `ultra` intensity levels no longer exist. The schema is now `output.signals.max_depth` + `update.run_doctor`. The `Resolved()` zero-value-Config sentinel that backfills `update.run_doctor` — previously keyed off `cfg.Output.Intensity == ""` — now keys off `cfg.Output.Signals.MaxDepth <= 0` (a literal `&Config{}` has `MaxDepth == 0`; `Default()`/`Load()` always yield `MaxDepth > 0`).

**Why:** intensity added prompt bloat (the lite/full/ultra table loaded into every session) without earning its keep. One clarity-focused mode is simpler to reason about and to document.

**Superseded:** v1 originally shipped `output.intensity = "full"` (`lite | full | ultra`) as the first and only key; the render sentinel keyed the run_doctor default off the empty intensity string.

**Removed:** `output.intensity` key, `validIntensity` enum, intensity backfill in `Load`, and the intensity cases in `Validate`/`Set`/`Unset`. Closes follow-up `atomic-update-doctor-f-1` (the intensity-sentinel coupling it flagged is gone).


### 2026-05-23 — Add output.signals.max_depth config key

**What changed:** Schema gains a third key: `output.signals.max_depth` (int, default `3`). Controls the bounded tree depth in `atomic signals scan`. Shell-settable via `atomic config set output.signals.max_depth 5`. The signals scan reads this value when `MaxDepth` is not explicitly passed via `Options`.

| Key | Type | Default | Valid values |
|-----|------|---------|--------------|
| `output.signals.max_depth` | int | `3` | positive integer |

**Why:** Signals router spec (`docs/spec/signals-router.md`) requires configurable tree depth per axiom-2 carve-out for shell-settable defaults.

**Superseded:** Tree depth was hardcoded at `3` in `atomic/internal/signals/tree.go` with no config override.


### 2026-05-23 — Conform checkpoints table header

**Correction:** Checkpoints header was `| # | Checkpoint | Files / areas | Verifies |` (spaces around slash). `atomic validate spec` rule S5 requires exact `Files/areas`. CI run #102 failed on this. Header normalized; row content unchanged.

**Why:** Spec lint gates merge.


### 2026-05-22 — Add update.run_doctor config key

**What changed:** Schema v1 gains a second key: `update.run_doctor` (bool, default `true`). When `true`, `atomic update` invokes the doctor pass automatically after a successful binary swap. Setting it to `false` suppresses the pass permanently for that user. The `--no-doctor` flag overrides per-invocation regardless of config value. Precedence follows the existing order: flag > config > default.

| Key | Type | Default | Valid values |
|-----|------|---------|--------------|
| `update.run_doctor` | bool | `true` | `true`, `false` |

**Why:** Users who find the automatic post-update doctor pass noisy or who run doctor explicitly as part of a CI gate can disable it durably without passing `--no-doctor` on every invocation.


### 2026-08-14 — Drop config.resolved.md

**What changed:** `~/.atomic/config.resolved.md` is gone, along with the `@-ref` that pulled it into every Claude session. `config.Render`, `config.ResolvedPath`, `writeResolved`, and the install-time stub are deleted. `config.toml` is the only source of truth; `atomic config list` renders resolved values on demand. Doctor category 9 keeps validating the TOML and loses the drift half, which also removes its `--fix` repair, since neither remaining condition is auto-fixable.

**Why:** The file put a generated snapshot into every session's context for values only the `atomic` binary reads, and it drifted silently. A user's copy advertised four per-agent effort overrides while their `config.toml` had no `[claude.agents]` block at all, written by an older binary using pre-CP7 key names and never regenerated. A snapshot that can lie about config is worse than no snapshot.

**Removed:** the rendered markdown view, its `@-ref`, the drift check, and the drift repair.


## Implementation log


### v1 — 2026-05-21

Built across 5 implement-review iterations of `/subagent-implementation` plus a Phase 3 polish pass. Commits on `atomic-state-and-config` branch (chronological):

- `b6f1417` — CP-1 + CP-2: `atomic/internal/config/` package — TOML load (lenient) / validate (strict) / get / set / unset / atomic `WritePersist`, deterministic `Render` to markdown, path helpers (`Dir`, `TOMLPath`, `ResolvedPath`, `BackupDir`, `ProposedCLAUDEMD`). 15 unit tests.
- `6cbde38` — CP-3: `atomic config get|set|unset|list|path` CLI wired through `atomic/cmd/atomic/main.go`. Re-renders `config.resolved.md` after every `set`/`unset`. Near-match (Levenshtein-2) suggestions on unknown keys for all three of get/set/unset. 24 CLI tests.
- `0ed7004` — CP-4 + CP-5 + CP-6: bundled `CLAUDE.md` gains the `@~/.claude/.atomic/config.resolved.md` `@-ref` and a "Where things live" bullet. `claudeinstall` migrates backups to `.atomic/backups/<ts>/`, proposed merges to `.atomic/proposed/CLAUDE.md`, and idempotently pre-creates `config.resolved.md` on every Apply so the bundled ref always resolves. Cross-references in `agents/atomic-claude-merger.md` + `commands/atomic-claude-merge.md` updated. Bundle regenerated.
- `c5c34fc` — CP-7 + CP-8: new doctor check category #9 (`config`), with PASS / WARN / FAIL coverage and a `--fix` repair that re-renders on drift but refuses to write when validation fails (`bogus` never reaches `config.resolved.md`). `repairPlan` reports FAIL-severity config results as non-fixable. Install-integrity check confirmed to skip the `.atomic/` subtree. `CLAUDE.md` prose updated from "eight" to "nine" checks.
- `009baaa` — CP-9 + CP-10: `docs/spec/atomic-doctor.md` gains row 9 + change-log entry per the append-mostly rule. `.claude/rules/authoring/axioms.md` axiom 2 gains the shell-settable carve-out paragraph settled during pressure-test.
- `57ab0ae` — Phase 3 polish: closed F-1, F-2, F-3, F-4, F-5, F-8, F-9 (test gaps, hoisted package vars, dead-code removal, alphabetical usage printer, combined-WARN UX in doctor config check, `strings.Contains` cleanup). Added `claude.local.md` Platform support rule (macOS/Linux only).

**Out-of-scope work performed during this build:** none. Spec was tight; no schema additions, no expansion beyond CP-1..CP-10.

**Unforeseens — surprises that emerged during implementation:**

- Iter 2: builder shipped `Set` with Levenshtein suggestions but not `Get`/`Unset` — caught by reviewer; fixed in iter 2b.
- Iter 3: spec text used `@~/...` tilde-prefix `@-ref`, with no local precedent. Verified upstream (`https://code.claude.com/docs/en/memory`) before ship — tilde IS supported. Closed F-7.
- Iter 4: builder's first repair implementation reported FAIL as fixable AND wrote invalid values into `config.resolved.md`. Caught by reviewer; fixed in iter 4b by gating `fixable: r.Severity == WARN` and calling `config.Validate` inside the repair before writing.
- Iter 3: builder originally used `Resolved` map iteration with an unreachable empty-keys guard in `Render`; reviewer flagged as dead code, closed during the polish pass.

**Deferred items still open:**

- `F-1` accepted with a known limitation: `TestWritePersistAtomic` asserts no tempfile residue post-write, but the assertion would also pass for a direct-write regression (no tmp ever existed). Reviewer noted; the brief explicitly accepted that form.
- `F-6` dropped: pre-existing Windows path-extraction fragility in `install_test.go:534`. macOS/Linux are the only supported platforms going forward (recorded in `claude.local.md`).

No items deferred to project-level `.claude/project/followups.md`. No tracked issues filed.


**Squashed onto `main` as `5c9d61c` — 2026-05-21.** Per-iteration SHAs above are historical (unreachable post-squash).
