# atomic state directory + config


## Goal


Consolidate atomic-owned state under `~/.atomic/` and ship a TOML-backed config (`atomic config get|set|unset|list|path`). `config.toml` is the single source of truth; `atomic config list` renders resolved values on demand. Config is consumed by the `atomic` binary, not injected into Claude sessions.

This spec also owns the harness-neutral repository-state contract: the `state.dir` config key, the resolution ladder, the per-clone selection record, and the authoritative realm registry at `~/.atomic/wikis.md`. Under Milestone A that contract composes with the multi-harness architecture — see `docs/spec/omp-plugin-compatibility.md`.


## Non-goals


- Project-local config overrides (a per-repo `config.toml` shadowing `~/.atomic/config.toml`). Deferred until a concrete case appears — distinct from `state.dir` (below), which names a repo-local *state-directory*, not a config-override mechanism, and is itself user-level only (no per-repo override).
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
- [ ] `state.dir` accepts a repository-relative path of one or more safe segments (each not empty, `.`, or `..`), such as `.config/atomic`; `harness.dir` is retired: a stored copy loads silently, supplies no evidence, and is dropped on the next config write, and `atomic config set` refuses it as an unknown key.
- [ ] Repository-state resolution walks process-only `ATOMIC_STATE_DIR` → `~/.atomic/<project-key>/state-location.json` → a populated state folder → user `state.dir` → built-in `.atomic`, never consulting a harness variable or fingerprint; no environment variable names a harness for state purposes.
- [ ] `atomic state migrate` moves Atomic's state out of legacy `.claude` into the selection record's root, else `state.dir`, else `.atomic`, and never reads `ATOMIC_STATE_DIR`; `atomic repo init` and `atomic update` run it once.
- [ ] `atomic state adopt [--dir <path>] [--clear] [--dry-run] [--json]` adopts one unambiguous populated state root in place without moving files; two or more populated roots refuse; an empty repository creates no candidate and writes nothing; the selection is persisted and fsynced before resolution switches.
- [ ] `~/.atomic/wikis.md` is the authoritative realm registry; while `wikis.md` is absent the authority is seeded once from the harness-installed `<wikis>` block, and a later divergence is a conflict, never auto-imported.
- [ ] Axiom 2 amended in `.claude/rules/authoring/axioms.md` with the shell-settable carve-out.


## Layout (target end state)


```
~/.atomic/
├── config.toml              # user-written, atomic config set rewrites
├── profile.md               # user profile; its ## Workflow preferences block is the profile level
├── wikis.md                 # authoritative realm registry; a harness <wikis> block is a derived projection
├── backups/<ts>/<relpath>   # claudeinstall pre-write backups
├── proposed/
│   └── CLAUDE.md            # claudeinstall divergence merge target
├── state.json               # machine-managed selfupdate state; atomic temp+rename; never hand-edited
└── <project-key>/           # one entry per clone (main checkout root, flattened); see docs/spec/serve-plans-page.md
    ├── state-location.json  # persisted repository-state selection (schema v1); shared by every worktree
    ├── reports/<branch>/    # /session-report output, resolved via `atomic where --json` .reports
    └── archive/<slug>/<created>/  # retired atomic scratchpad bundles
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

[state]
dir = ".atomic"             # repository-relative path of safe segments; repo-local state root
```

Current keys: `output.signals.max_depth`, `update.run_doctor`, `update.check`, `update.stage`, `update.channel`, `state.dir`, `output_style.seed`, `steering.claude`. Further keys (`forge.*`, `cleanup.*`, …) are added per concrete steering need in follow-up specs. Each schema addition: schema entry → renderer entry → one steering site reading it → change-log entry on this spec.

`[bus.remotes.<name>]` tables (`host`, `key`, optional `ca`) are modeled on `Config` but are not `atomic config set` keys: `atomic bus remote add|remove` writes them and `internal/bus/remote` reads them through `config.Load`. `bus` is an opaque section, so arbitrary remote names load without unknown-key warnings. Modeling it is what keeps every other config write from deleting it, since `WritePersist` serializes `Config` alone. Contract: [`atomic-bus-network.md`](./atomic-bus-network.md).

`update.check` and `update.stage` (bool, default `true`) gate the two halves of the detached background-update child described in [`selfupdate-state.md`](./selfupdate-state.md): `update.check` enables the hourly GitHub lookup that any invoked verb may spawn; `update.stage` enables that child's once-per-version download-and-checksum-verify into `~/.cache/atomic/staged/`. Both are user-level only — no repo-scoped equivalent.

`update.channel` (string, default `stable`; valid `stable`, `prerelease`) selects the release channel every update path reads: the background check, the banner, `atomic update`, and doctor's binary check. Stored empty means unset and resolves to `stable`. `atomic update --pre` (or `--channel`) overrides it for one invocation and never writes it back, so a pinned machine can still take a single update from the other channel. A stored value outside the enum fails `Validate`; the update paths fall back to `stable` rather than blocking on it. User-level only — no repo-scoped equivalent.


`state.dir` (string, default `.atomic`) names the repository-state directory every repo-scoped `atomic` verb resolves against (`<resolved-root>/.scratchpad`, `project`, `.atomic-index`, `atomic.toml`, `rules/wiki`, `workflow-preferences.md`), keeping those paths out of any harness's own directory (e.g. `atomic config set state.dir .config/atomic`). Linked worktrees follow `config.WorktreeDir`: a `worktrees` preference path wins and `off` disables them; otherwise Claude keeps them in `.claude/worktrees` and other harnesses in `<resolved-root>/worktrees`. It is unrelated to the `~/.atomic` user-state root above: `~/.atomic` is fixed and not configurable (see Non-goals). Validation: **write (`set`)** accepts a repository-relative path of one or more safe segments and rejects an empty segment, `.`, `..`, or an absolute path; the resolver refuses those shapes for a recorded value too. **Read (load)** goes one step further than the generic unknown-key leniency described below: an unset `state.dir` falls through the resolution ladder rather than erroring, and an invalid configured value degrades to the next rung instead of reaching `filepath.Join` unguarded in every repo-local path helper.

`harness.dir` is **retired**. A stored copy loads silently, changes no resolution, and is dropped on the next config write; `atomic config set` refuses it as an unknown key, and the resolved config no longer lists it. The same holds for a stored `pi` table.


## Precedence (highest wins)


| # | Source | Role |
|---|--------|------|
| 1 | Built-in defaults (Go constants) | Fallback |
| 2 | `~/.atomic/config.toml` | **Durable floor** (set from shell) |
| 3 | Per-conversation memory | **Per-conversation nudge**, scoped to session/task |
| 4 | Command-line flag | One-shot override |


Memory entries overriding config must be scoped ("for this session", "for this task"), never "remember forever" — stale memory must not silently outlive `atomic config set`.


## Validation policy


- **Write (`set`)**: strict. Reject unknown keys, reject values outside the allowed enum/range, suggest a near-match key on typo (Levenshtein ≤ 2). `set` and `unset` refuse a `<harness>.agents.*` key (`claude`, `omp`, `codex`) with an error naming `atomic config agents --harness <harness>`, the only writer of agent overrides.
- **Environment**: `config.ClaudeConfigDir` resolves a relative `CLAUDE_CONFIG_DIR` against the process's current directory and returns the absolute path, so every caller in one process names the same root.
- **Read (load)**: lenient. Unknown keys ignored with a single WARN log line. Allows newer-config / older-binary forward-compat.


## Repository-state resolution


The repository-state directory is harness-neutral, so one repo resolves the same root whatever harness runs it. `config.ResolveStateLocation` walks the ladder most specific first:

| # | Rung | Notes |
|---|------|-------|
| 1 | process-only `ATOMIC_STATE_DIR` | never rewrites the persisted selection |
| 2 | `~/.atomic/<project-key>/state-location.json` | persisted per-clone selection, shared by every worktree |
| 3 | a populated state folder | the destination (`state.dir`, else `.atomic`) wins when it holds state, with a one-time warning naming any other populated folder as leftover; otherwise a single other populated folder, such as legacy `.claude`, answers in place silently; two or more give an ambiguity error |
| 4 | user `state.dir` | repository-relative path of safe segments, set once for the user |
| 5 | built-in `.atomic` | fallback |

No rung consults a harness variable or fingerprint, and no environment variable names a harness for state purposes. Candidate state folders are the configured `state.dir`, `.atomic`, and legacy `.claude`; no other directory is a candidate. A folder counts as populated when it holds `atomic.toml`, `.atomic-index`, `.scratchpad`, `project`, or `rules/wiki`. `atomic where --json` prints the resolved root as `state_root`; code reads it through `config.RepositoryStateDir` (cached, warns) or `config.QuietStateDir` (uncached, silent, for repositories a command only visits); `config.QuietStateRoots` is the reader's form, returning every populated candidate root when the ladder refuses. Writing or withdrawing a selection record drops the cached roots, so the same process resolves the new choice.

Workflow preferences are not config keys. The repo level is `<state-root>/workflow-preferences.md`, the realm level is the same file under the realm root's state root, and the profile level is the `## Workflow preferences` block in `~/.atomic/profile.md`; `atomic workflow set` writes them and `atomic workflow resolve` prints the resolved block. The file does not count toward a populated state folder. Contract: `docs/spec/workflow-preferences.md`.

`atomic state migrate` moves Atomic's state, `workflow-preferences.md` included, out of legacy `.claude` into the migration destination: the selection record's root, else `state.dir`, else `.atomic`. It never reads `ATOMIC_STATE_DIR`, stages tracked renames, and deletes `.claude/.atomic-index/` instead of moving it, unless an MCP daemon is live there, which keeps the old index and is reported; the next `atomic code index` rebuilds in the state root. `atomic repo init` and `atomic update` run the migration once.

The install side follows the same neutrality. A bare `atomic install` installs into every installed harness (Claude when its root exists, OMP for the `OMP_PROFILE` profile else the default, Codex only when `CODEX_HOME` names an existing directory, Claude when none) and prints a `detected:` line; `--harness` narrows it. `atomic update` adopts an unenrolled legacy Claude root once, when `~/.atomic/config.toml` carries the legacy `[install]` record without `adopted = true`, and stops naming the file on user content it will not overwrite; the install guide (`docs/guides/install.md`) lists the blockers.

A relative rung value must be a repository-relative path of safe segments (`ValidateStateDirPath` rejects an empty segment, `.`, and `..`); a selection record may hold an absolute directory, which is used as-is. `<project-key>` is the clone's main checkout root flattened to one path segment, so every worktree of one clone shares the selection and the project-keyed home (`reports/<branch>/`, `archive/<slug>/<created>/`, and `state-location.json` itself).


### `atomic state adopt`


`atomic state adopt [--dir <path>] [--clear] [--dry-run] [--json]` selects the repository's state root:

- **no flags** — discovery scans the plausible repository-local directory names for Atomic state signatures; an empty or harness-only directory is not a candidate. Zero candidates write nothing and report the ladder's current resolution; one is adopted in place without moving files; two or more refuse, telling the user to re-run with `--dir`.
- **`--dir`** — persists the given repository-relative or absolute path as the selection.
- **`--clear`** — withdraws the persisted selection, so the ladder decides again; `--dir` and `--clear` are mutually exclusive.
- **`--dry-run`** — makes no changes; **`--json`** emits the report.

`PersistStateSelection` writes and fsyncs the record bytes and its parent directory before resolution switches, so a crash cannot leave resolution pointing at a root the record does not name. A failed migration withdraws the record only while its bytes still match what was written — a later state write makes that a conflict, never a discarded choice. An empty repository creates no candidate and no adoption record until a stateful operation needs one. The retired harness variable blocks adoption.


## Realm wiki registry


`~/.atomic/wikis.md` is the authoritative realm registry: one writer, and every save rewrites only the `<wikis>` block while preserving every other byte. A `<wikis>` block installed in a harness's global file is a derived projection: registration adds the authority entries it lacks and keeps every other byte, and a later divergence is a conflict that stays in place and is never imported automatically. `SeedFromProjection` seeds the authority once from the installed block's absolute entries while `wikis.md` is absent, so a realm that only ever lived in the projection survives the switch; a relative entry is never seeded.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | New package `atomic/internal/config/` with TOML load (lenient), schema validate (strict), get/set/unset, atomic write via `os.Rename` from tmp | `atomic/internal/config/*.go` | unit: round-trip set→load→get; unknown key rejected on set; unknown key ignored on load with WARN |
> Checkpoints 2, 4, 5, and 7 below describe the `config.resolved.md` renderer, its `@-ref`, its install-time stub, and the doctor drift check. All four were removed on 2026-08-14 — see the change log. The rows stay as the build record; do not treat them as work to do.

| 2 | Renderer: `config.resolved.md` generated from resolved values (TOML + defaults) | `atomic/internal/config/render.go` | unit: deterministic output (byte-stable for same input); empty TOML renders empty-but-present file with header |
| 3 | CLI wiring: `atomic config get|set|unset|list|path`, including `list --json` | `atomic/cmd/atomic/main.go`, `atomic/internal/config/cli.go` | integration: each subcommand exit codes + output match contract; typo suggestion fires on near-match |
| 4 | Bundle source `CLAUDE.md` adds line `@~/.atomic/config.resolved.md` and a one-paragraph mention of the `.atomic/` namespace | `CLAUDE.md` (repo root), bundle regen via `make -C atomic bundle` | CI "Verify bundle is committed" passes; `manifest.go` reflects new CLAUDE.md hash |
| 5 | `claudeinstall` writes backups to `.atomic/backups/<ts>/` and proposed merges to `.atomic/proposed/CLAUDE.md`; pre-creates empty `~/.atomic/config.resolved.md` on first install | `atomic/internal/claudeinstall/install.go` (lines 81, 132, 275-276 + pre-create step) | unit: fresh install creates `.atomic/config.resolved.md`; backup written to new path; divergent CLAUDE.md proposed at new path |
> Checkpoint 6 named cross-references in a command/agent pair that was later removed. Its row is historical — see the change log. Do not treat it as work to do.

| 6 | (historical) Update cross-references to the proposed path — the named command/agent pair was removed | `agents/`, `commands/` | Retired 2026-09-19; see the change log. The row stays as the build record; do not treat it as work to do. |
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


- Should the config support per-project overrides (a repo-local `config.toml` shadowing the user config)? Deferred. Non-goal for v1 — `state.dir` (below) covers the one concrete per-project need that has appeared so far (naming the repo-local state-directory), but it is a single user-level key, not a per-project override mechanism. Revisit if a steering value genuinely needs to vary per project, not per user.


## Change log

### 2026-11-12 — `steering.claude` key

**What changed:** "Current keys" lists `steering.claude` (`loader`, `agents-md`, or `claude-md`; unset is `loader`), which a repository or realm `<state-root>/atomic.toml` `[steering] claude` overrides.

**Why:** The key shipped without a body entry. Contract: `docs/spec/omp-plugin-compatibility.md`; reference: `docs/reference/atomic-toml.md`.
### 2026-11-12 — Agent-override keys, a relative CLAUDE_CONFIG_DIR, cache drops

**What changed:** The Validation policy names the `config set|unset` refusal for `<harness>.agents.*` keys, which points at `atomic config agents --harness <harness>`, and states that a relative `CLAUDE_CONFIG_DIR` resolves to an absolute path against the process's current directory. `## Repository-state resolution` names `config.QuietStateRoots` and states that a selection-record write or withdrawal drops the cached roots.

**Why:** a refused override key gave a bare unknown-key error with no route to the writer, and a relative `CLAUDE_CONFIG_DIR` stayed relative, so a printed or recorded root did not say which directory it meant. A verb that persisted a selection and then built a path read the stale cached root.

**Superseded:** an override key failed as an unknown key; a relative `CLAUDE_CONFIG_DIR` was only cleaned; a selection written mid-process left the cached root in place.


### 2026-11-11 — Name the workflow-preferences locations

**What changed:** The Layout lists `~/.atomic/profile.md` with its `## Workflow preferences` block. The `state.dir` paragraph lists `workflow-preferences.md` among the repo-scoped paths, and `## Repository-state resolution` names the repo, realm, and profile locations, the `atomic workflow` verbs, that the file is not a populated-folder signature, and that `atomic state migrate` moves it.

**Why:** `docs/spec/workflow-preferences.md` adds a preference file under the state root and a block in the profile.


### 2026-11-11 — `<wikis>` projection is insert-only

**What changed:** The registry paragraph states that a projection only gains the authority entries it lacks, keeps every other byte, and leaves a divergent entry in place. Seeding takes absolute entries only.

**Why:** Audit rows P-15 and P-16 in `docs/research/harness-parity-audit.md`; `docs/spec/wiki.md` carries the placement and warning contract.

**Superseded:** The projection was rendered from the authority, and seeding resolved relative entries against the process cwd.

### 2026-11-11 — State root defaults to .atomic

**What changed:** The success criteria, the `[state]` schema example, the `state.dir` paragraph, and `## Repository-state resolution` name `.atomic` as the built-in default and the five-rung ladder with the populated-folder rung. `state.dir` is a repository-relative path of safe segments. The section names `atomic where --json` `state_root`, `config.RepositoryStateDir` and `config.QuietStateDir`, and `atomic state migrate` with its destination order. Worktrees follow `config.WorktreeDir`: the preference path or `off`, else `.claude/worktrees` on Claude and `<state-root>/worktrees` elsewhere. The migration paragraph names the live-daemon exception for the legacy index, and `atomic state adopt` takes `--dir <path>`.

**Why:** `docs/spec/omp-plugin-compatibility.md` moves repository state to the `.atomic` default, with a resolution ladder and `atomic state migrate`.

**Superseded:** a four-rung ladder ending at built-in `.claude`, with `state.dir` limited to one path segment and `.claude` as the only other candidate.

### 2026-11-11 — Reminders removed

**What changed:** The `~/.atomic/<project-key>/` layout and the project-keyed home list drop `reminders/`.

**Why:** `docs/spec/workflow-preferences.md` drops the reminder feature.

**Removed:** the `reminders/` directory under `<project-key>/`, resolved via `atomic where --json` `.reminders`. Leftover reminder files stay on disk, unread.

### 2026-11-10 — Retire the harness variable and key; name install and update behavior

**What changed:** the body drops `harness.dir` and `ATOMIC_HARNESS`. `harness.dir` is retired: a stored copy loads silently, supplies no migration evidence, and is dropped on the next write, and `atomic config set` refuses it. No environment variable names a harness for state purposes. Adoption and migration candidates are the configured `state.dir` segment and `.claude`. The resolution section also states that a bare `atomic install` installs into every installed harness (`--harness` narrows it) and that `atomic update` adopts an unenrolled legacy Claude root once.

**Why:** orientation reads environment markers and `--harness` only, there are no Pi users, and existing users must upgrade without a manual step.

**Superseded:** the legacy-evidence role of `harness.dir` while `state.dir` is unset, the `[harness]` schema block, and the refusal that a set `ATOMIC_HARNESS` produced.


### 2026-09-23 — Model [bus.remotes] on Config

**What changed:** `Config` gained a `Bus` section holding `[bus.remotes.<name>]`, and `bus` joined the opaque sections.

**Why:** `[bus.remotes]` lived in `config.toml` but not in `Config`, so `config set`, `config agents`, `claude install`, and `migrate` deleted it on every write.


### 2026-09-08 — Add output_style.seed config key

**What changed:** Schema gains `output_style.seed` (bool, default `true`), gating whether `atomic claude install`, `atomic claude update`, and the session-start hook seed `outputStyle: "Atomic"` into the user-level Claude Code settings file.

| Key | Type | Default | Valid values |
|-----|------|---------|--------------|
| `output_style.seed` | bool | `true` | `true`, `false` |

**Why:** `docs/spec/output-style-seed.md` — the seed needs a persisted opt-out, since the session-start trigger is deliberately ungated and re-seeds a deleted key on every session.


### 2026-09-02 — Add update.channel config key

**What changed:** Schema v1 gains `update.channel` (string, default `stable`, valid `stable` / `prerelease`), selecting the release channel for the background check, the update banner, `atomic update`, and doctor's binary check. Previously every one of those was pinned to `stable` in code, so `--channel prerelease` applied only to the one foreground invocation that carried it and a machine tracking pre-releases was still told it was on the latest stable release.

| Key | Type | Default | Valid values |
|-----|------|---------|--------------|
| `update.channel` | string | `stable` | `stable`, `prerelease` |

**Why:** `atomic update --pre` installs pre-releases cut from the `next` branch. Without a persisted channel the flag reaches only the foreground path, leaving the banner and doctor reporting against a channel the user is not on.


### 2026-08-20 — Layout gains the project-keyed state home

**What changed:** The Layout tree gains `~/.atomic/<project-key>/`, one entry per clone (keyed by the main checkout root, path-separator flattened), holding `reports/<branch>/`, `reminders/`, and `archive/<slug>/<created>/`. These paths are resolved by `atomic where --json` and the `atomic scratchpad` verb, not read or written directly against this layout tree.

**Why:** `docs/spec/serve-plans-page.md` relocates session reports, reminders, and archived scratchpad bundles from repo-local and harness-scoped paths to one project-keyed home under `~/.atomic/`, so every worktree of a clone agrees on the location; this spec's Layout section was the per-user state root reference and omitted the new home.

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


### 2026-09-19 — Supersede `harness.dir` with `state.dir`; add repository-state resolution

**What changed:** Schema v1 gains `state.dir` as the current repository-state key (string, one safe path segment, default `.claude`). `harness.dir` becomes a legacy input that supplies migration evidence only while `state.dir` is unset. The body gains a `## Repository-state resolution` section — the ladder process-only `ATOMIC_STATE_DIR` → `~/.atomic/<project-key>/state-location.json` → user `state.dir` (legacy `harness.dir` evidence) → built-in `.claude`, with no harness fingerprint, and the retired `ATOMIC_HARNESS` refusing resolution rather than acting as an alias — plus the `atomic state adopt [--dir <segment|absolute>] [--clear] [--dry-run] [--json]` contract and a `## Realm wiki registry` section making `~/.atomic/wikis.md` the authority and the harness `<wikis>` block a derived projection. The Layout tree gains `wikis.md` and `<project-key>/state-location.json`.

**Why:** Milestone A makes repository state and the realm registry harness-neutral — see `docs/spec/omp-plugin-compatibility.md`.

**Superseded:** `harness.dir` was the current repo-local state-directory key, the resolver consulted a harness fingerprint, `ATOMIC_HARNESS` selected the harness, and `~/.atomic/wikis.md` held no authority over a harness-installed `<wikis>` block.

**Removed:** the `atomic-claude-merger` agent and `/atomic-claude-merge` command success criterion; checkpoint 6's cross-reference row is retained as historical only. Both artifacts were removed by the earlier artifact-consolidation work; the cold-op replacement is `atomic prompt claude-merge`.


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
