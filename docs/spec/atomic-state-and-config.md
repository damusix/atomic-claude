# atomic state directory + config


## Goal


Consolidate atomic-owned state under `~/.claude/.atomic/` and ship a TOML-backed config (`atomic config get|set|unset|list|path`) whose resolved values reach every Claude session via a single `@-ref` from the bundled `CLAUDE.md` to `~/.claude/.atomic/config.resolved.md`. Universal delivery: works with or without Claude Code hooks installed.


## Non-goals


- Project-local `.claude/.atomic/` overrides. Deferred until a concrete case appears.
- Migrating legacy paths (`~/.claude/.atomic-backups/`, `~/.claude/CLAUDE.md.atomic-proposed`). Old paths orphaned; user cleans up.
- Moving `<bin-dir>/.atomic.new` (selfupdate staged binary). Cross-filesystem `os.Rename` constraint.
- Bundling `config.toml`. User state, never bundled.
- Hook-based config delivery. Hook stays out of the config path entirely.
- Template engine for `CLAUDE.md` or any artifact.
- Sentinel block / managed-region parser inside `CLAUDE.md`.
- Lockfile for concurrent `atomic config set` writes. `os.Rename` atomic-write is sufficient.


## Success criteria


- [ ] `atomic config get|set|unset|list|path` work end-to-end against the schema below.
- [ ] `atomic config set <key> <value>` rejects unknown keys and unknown values with a typo-suggesting error; valid sets atomically write `config.toml` and re-render `config.resolved.md`.
- [ ] Fresh install creates `~/.claude/.atomic/config.resolved.md` (empty), and bundled `CLAUDE.md` carries the `@~/.claude/.atomic/config.resolved.md` line.
- [ ] On fresh install, SHA-compare of installed vs bundled `CLAUDE.md` matches (no divergence, no `.atomic-proposed` written).
- [ ] `claudeinstall` writes backups to `~/.claude/.atomic/backups/<ts>/` and proposed merges to `~/.claude/.atomic/proposed/CLAUDE.md`.
- [ ] `atomic doctor` includes a new `config` check (TOML parses, no unknown keys, `config.resolved.md` matches render of TOML).
- [ ] `atomic doctor --fix` re-renders `config.resolved.md` when drift detected.
- [ ] `atomic-claude-merger` agent and `/atomic-claude-merge` command reference the new proposed path.
- [ ] Axiom 2 amended in `.claude/docs/axioms.md` with the shell-settable carve-out.


## Layout (target end state)


```
~/.claude/.atomic/
├── config.toml              # user-written, atomic config set rewrites
├── config.resolved.md       # rendered from TOML + defaults; @-ref'd from CLAUDE.md
├── backups/<ts>/<relpath>   # claudeinstall pre-write backups
├── proposed/
│   └── CLAUDE.md            # claudeinstall divergence merge target
├── cache/                   # reserved (selfupdate version-check, future)
└── state.json               # reserved (last-update-check, future)
```


## Schema (v1 — start narrow)


```toml
[output]
intensity = "full"          # lite | full | ultra
```


Only `output.intensity` ships in v1. `forge.*`, `cleanup.*`, `update.*` added per concrete steering need in follow-up specs (rollout step 5). Each schema addition: schema entry → renderer entry → one steering site reading it → change-log entry on this spec.


## Precedence (highest wins)


| # | Source | Role |
|---|--------|------|
| 1 | Built-in defaults (Go constants) | Fallback |
| 2 | `~/.claude/.atomic/config.toml` | **Durable floor** (set from shell) |
| 3 | Per-conversation memory | **Per-conversation nudge**, scoped to session/task |
| 4 | Command-line flag | One-shot override |


Memory entries overriding config must be scoped ("for this session", "for this task"), never "remember forever" — stale memory must not silently outlive `atomic config set`.


## Validation policy


- **Write (`set`)**: strict. Reject unknown keys, reject values outside the allowed enum/range, suggest a near-match key on typo (Levenshtein ≤ 2).
- **Read (load)**: lenient. Unknown keys ignored with a single WARN log line. Allows newer-config / older-binary forward-compat.


## Checkpoints


| # | Checkpoint | Files / areas | Verifies |
|---|------------|---------------|----------|
| 1 | New package `atomic/internal/config/` with TOML load (lenient), schema validate (strict), get/set/unset, atomic write via `os.Rename` from tmp | `atomic/internal/config/*.go` | unit: round-trip set→load→get; unknown key rejected on set; unknown key ignored on load with WARN |
| 2 | Renderer: `config.resolved.md` generated from resolved values (TOML + defaults) | `atomic/internal/config/render.go` | unit: deterministic output (byte-stable for same input); empty TOML renders empty-but-present file with header |
| 3 | CLI wiring: `atomic config get|set|unset|list|path`, including `list --json` | `atomic/cmd/atomic/main.go`, `atomic/internal/config/cli.go` | integration: each subcommand exit codes + output match contract; typo suggestion fires on near-match |
| 4 | Bundle source `CLAUDE.md` adds line `@~/.claude/.atomic/config.resolved.md` and a one-paragraph mention of the `.atomic/` namespace | `CLAUDE.md` (repo root), bundle regen via `make -C atomic bundle` | CI "Verify bundle is committed" passes; `manifest.go` reflects new CLAUDE.md hash |
| 5 | `claudeinstall` writes backups to `.atomic/backups/<ts>/` and proposed merges to `.atomic/proposed/CLAUDE.md`; pre-creates empty `~/.claude/.atomic/config.resolved.md` on first install | `atomic/internal/claudeinstall/install.go` (lines 81, 132, 275-276 + pre-create step) | unit: fresh install creates `.atomic/config.resolved.md`; backup written to new path; divergent CLAUDE.md proposed at new path |
| 6 | Update cross-references to the proposed path | `agents/atomic-claude-merger.md`, `commands/atomic-claude-merge.md` | grep: no remaining `CLAUDE.md.atomic-proposed` string in agents/ or commands/ |
| 7 | New `doctor` check category `config`: TOML present + parses, no unknown keys, `config.resolved.md` matches render of TOML; `--fix` re-renders on drift | `atomic/internal/doctor/checks_config.go`, `checks_config_test.go`, dispatch wiring | unit: PASS/WARN/FAIL paths; integration: `--fix` re-renders and check goes PASS |
| 8 | `doctor` install-integrity scans `.atomic/` paths (no legacy path scan) | `atomic/internal/doctor/checks_install.go` | unit: install check passes with new paths populated, regardless of legacy-path presence |
| 9 | Amend `docs/spec/atomic-doctor.md`: add category #9 entry + change-log entry per spec-amendment rule | `docs/spec/atomic-doctor.md` | spec body lists check #9; change log has dated entry referencing this spec |
| 10 | Amend `.claude/docs/axioms.md` axiom 2 with shell-settable carve-out | `.claude/docs/axioms.md` | grep: carve-out paragraph present under axiom 2 |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `@-ref` resolves to missing file on fresh install | med | CP5 pre-creates an empty `config.resolved.md`; CP4 ships the `@-ref` in source so it's present even before any `set` |
| Schema additions in newer binaries break older binaries that read the same `config.toml` | med | Lenient read (unknown keys ignored with WARN); strict only on write |
| `config.resolved.md` drifts from `config.toml` (manual edits to TOML, crashed `set`, etc.) | med | `doctor` config check compares; `--fix` re-renders |
| Old `~/.claude/.atomic-backups/` and `CLAUDE.md.atomic-proposed` pile up indefinitely | low | Accepted. Documented in non-goals; user cleans up |
| `CLAUDE.md` source edit forgotten when regenerating bundle | low | Existing `.githooks/pre-commit` regens; CI `git diff --exit-code` enforces |
| Renderer non-determinism (map iteration order, timestamp leakage) | med | Sort keys before emit; no timestamps in `config.resolved.md` body; byte-stable test |
| Memory entries overriding config silently outlive `atomic config set` | med | Axiom-2 carve-out documents the scoping rule; rely on user discipline |


## Open questions


- Should the config support per-project overrides via `.claude/.atomic/config.toml`? Deferred. Non-goal for v1. Revisit when a concrete case appears (e.g. a steering site whose value genuinely varies per project, not per user).


## Change log


<!-- Populated on first amendment after spec approval. -->
