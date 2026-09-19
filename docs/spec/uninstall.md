# Uninstall workflow


## Goal

Users can cleanly reverse an Atomic install across every enrolled harness target, and a full removal leaves their user data intact. `atomic harness uninstall` is the primary surface — one target at a time, or every enrolled target with `--all`. The Claude-only pre-install snapshot route (`atomic claude uninstall`) remains available as the Claude target's snapshot-based recovery path.

The removal, retention, lock, and recovery contracts are shared with the multi-harness lifecycle — see `docs/spec/omp-plugin-compatibility.md`. This spec states only the uninstall-specific decision.


## Non-goals

- Binary self-removal (print instruction instead).
- Project-level artifact removal (`.claude/project/` signals, followups).
- Backwards-compat with installs predating the snapshot feature.
- Removing user data: `~/.atomic/config.toml`, `~/.atomic/profile.md`, `~/.atomic/wikis.md`, and backups survive a full uninstall.
- Removing operational state an unresolved journal still owns — it is retained until recovery completes.


## Success criteria

- [ ] `atomic harness uninstall <target-key>` removes only the unchanged resources owned for that target. A resource whose native bytes changed refuses the whole operation; a resource another enrolled consumer depends on is retained and reported.
- [ ] `atomic harness uninstall --all` removes every enrolled target, then the completed operational and adoption state.
- [ ] Full uninstall preserves `~/.atomic/config.toml`, `~/.atomic/profile.md`, `~/.atomic/wikis.md`, and backups, and retains unresolved journals plus the transaction backups, ledger rows, and state-location records they reference until recovery completes.
- [ ] `--dry-run` opens no lock, writes nothing, previews unresolved journals read-only, and reports `blocked_on_recovery` (exit 1) when a journal cannot resolve to one safe result.
- [ ] Every real mutation acquires one advisory lifecycle lock and reconciles unresolved journals oldest-first before planning.
- [ ] `atomic claude install` writes `~/.atomic/pre-install/` on first install containing every file it will touch (CLAUDE.md, settings.json, agents/, commands/, skills/, output-styles/, rules/) plus a `manifest.json` recording paths, SHA256s, and timestamps; while the directory exists a later install or update does NOT overwrite it.
- [ ] `atomic claude uninstall` outputs a structured LLM prompt for the Claude target, exits 1 with a clear error when `pre-install/manifest.json` is missing, and prints a human-readable hint when run outside a Claude session.


## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Pre-install snapshot on first install | `atomic/internal/claudeinstall/snapshot.go`, `install.go`, `config/paths.go`, tests | Test: first install creates `pre-install/` with manifest.json + file copies; second install skips; settings.json captured when present |
| 2 | `atomic claude uninstall` CLI subcommand | `atomic/cmd/atomic/main.go`, `atomic/internal/claudeinstall/uninstall.go`, tests | Test: outputs correct prompt given a manifest; exits 1 when no pre-install; TTY hint when interactive |
| 3 | Cross-reference wiring | `CLAUDE.md`, `README.md`, `docs/guides/install.md` | All surfaces reference the new subcommand |


## Design

### Target uninstall

`atomic harness uninstall <target-key>` removes one enrolled target's unchanged Atomic-owned resources. Ownership comes from the ledger, never from names or legacy config paths.

- A resource whose native bytes changed since Atomic wrote them refuses the whole operation — uninstall never overwrites a user edit.
- A resource another enrolled consumer depends on is retained and reported alongside what was removed.
- The plan is complete before anything is removed: every resource is observed, planned, and approved (one confirmation), then removed.

### Full uninstall

`atomic harness uninstall --all` removes every enrolled target, then removes the completed operational and adoption state through `installstate.Cleanup`.

- **Preserved:** `~/.atomic/config.toml`, `~/.atomic/profile.md`, `~/.atomic/wikis.md`, and backups under `~/.atomic/backups/` — user data, not Atomic-owned state.
- **Retained:** unresolved journals, and the transaction backups, ledger rows, and state-location records those journals reference, until recovery completes. `installstate.ComputeRetention` names what survives; a completed journal contributes nothing and its operational state is removable.
- A transaction directory with no journal at all is orphaned v2 evidence and is never auto-deleted.

The full retention criteria are owned by the multi-harness lifecycle spec — see `docs/spec/omp-plugin-compatibility.md`.

### Dry run and locking

- `--dry-run` opens no lock, writes nothing, and previews unresolved journals read-only through `installstate.SimulateRecoveries`. A journal that resolves to one safe result yields the advisory post-recovery plan, planned against the ledger a real recovery would leave behind. A journal that cannot resolve to one safe result reports `blocked_on_recovery` and exits 1 — no plan is offered.
- Every real mutation acquires one advisory lifecycle lock and reconciles unresolved journals oldest-first before planning. Uninstall is the one lifecycle operation that proceeds past a recovery conflict: recovery consumes what it can verify, and each removal independently refuses any resource whose bytes changed.

### Claude pre-install snapshot route

`atomic claude uninstall` is the Claude target's snapshot-based recovery path, independent of the ledger. It is CLI-only — no agent definition; the binary bakes the LLM prompt.

**Snapshot.** `atomic claude install` writes `~/.atomic/pre-install/` before its first `Apply()`: a copy of every file it will touch (`CLAUDE.md`, `settings.json`, `agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`) plus a `manifest.json` recording paths, SHA256s, and timestamps. Write-once — while the directory exists, a later install or update is a no-op.

**Plan.** `BuildUninstallPlan` reads the manifest and three-way compares each file against the pre-install SHA and the embedded SHA:

| Current bytes | Action |
|---------------|--------|
| unchanged since install (== pre-install SHA) | restore from snapshot |
| == embedded SHA | delete — Atomic wrote it, the user never touched it |
| neither | restore with merge — the user modified it post-install |

`~/.atomic/profile.md` is user data with no pre-install counterpart; the plan never restores or deletes it. A missing manifest exits 1 with "no pre-install snapshot found".

**Prompt.** The CLI prints a structured markdown prompt to stdout: show the plan, take one confirmation, then restore / merge / delete as listed and print the binary removal instruction. When stdout is a TTY the CLI prints a human-readable hint above the prompt.

**`--target`.** `atomic claude uninstall [--target <dir>]`. The default root resolves through the Claude adapter and honors `CLAUDE_CONFIG_DIR`; an explicit path goes through claudeinstall's tilde handling.


## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| A target resource was edited after Atomic wrote it | Medium | The whole operation refuses; the user resolves the divergence before retrying |
| A shared resource is still used by another enrolled target | Low | Retained and reported; removed only once no enrolled consumer depends on it |
| A crash leaves an unresolved journal | Low | The next mutation recovers oldest-first; unresolved work and its backups are retained, never silently dropped |
| `settings.json` has complex nested structure that the LLM route misreads | Medium | Show a unified diff to the user; require explicit confirm before writing |
| User runs uninstall, regrets it, wants to re-install | Low | The binary still exists and `atomic install` works fresh; preserved config, profile, and wikis survive |


## Implementation log

### v1.6.0 — 2026-05-24

Built across 4 iterations of /subagent-implementation. Commits (chronological):

- `c4a8740` — CP-1 Pre-install snapshot (snapshot.go + paths.go + install wiring + 4 tests)
- `bd77e8e` — CP-2 `atomic claude uninstall` CLI subcommand (uninstall.go + main.go wiring + 9 tests + three-way merge detection fix)
- `495cbff` — CP-3 Cross-reference wiring (CLAUDE.md, README.md, docs/guides/install.md)
- `dc00d13` — Polish pass (6 follow-up fixes: test coverage gaps, misleading comments, documentation)

**Out-of-scope work performed during this build:**
- README before/after example, start-here gradient, merge callout, credits fix, repo topics — done before implementation started as part of the review/polish that led to this feature.

**Unforeseens:**
- Reviewer caught that merge detection needed three-way comparison (current vs pre-install vs embedded), not two-way. Spec was correct; initial implementation underspecified the logic. Fixed in iteration 3.

**Deferred items still open:**
- None — all 6 follow-ups fixed in polish pass.

**Squashed to `7a68621` — 2026-05-24.** Per-iteration SHAs above are historical (unreachable from any branch).


## Change log

### 2026-09-19 — generic multi-target uninstall is the primary surface

**What changed:** The body now leads with `atomic harness uninstall <target-key> | --all`. A target uninstall removes only unchanged owned resources, refuses the whole operation when a resource's native bytes changed, and retains a resource another enrolled consumer depends on. `--all` removes every enrolled target, then completed operational and adoption state; full uninstall preserves `~/.atomic/config.toml`, `profile.md`, `wikis.md`, and backups, and retains unresolved journals plus the transaction backups, ledger rows, and state-location records they reference until recovery completes. `--dry-run` opens no lock, writes nothing, previews journals read-only, and reports `blocked_on_recovery`; every real mutation takes one advisory lifecycle lock and recovers journals oldest-first first. The pre-install snapshot and the `atomic claude uninstall` LLM prompt are kept as the Claude target's route, with its write-once snapshot and `--target`/`CLAUDE_CONFIG_DIR` resolution stated from code.

**Why:** Milestone A (`docs/spec/omp-plugin-compatibility.md`) makes uninstall a ledger-managed multi-harness lifecycle operation with a preserve-data and recovery-retention contract, not only the Claude-only snapshot recovery this body previously described.

**Superseded:** Prior body described only the Claude-only pre-install snapshot + `atomic claude uninstall` LLM-prompt path, whose uninstall removed `~/.atomic/` wholesale. Whole-root removal no longer holds — full uninstall preserves config, profile, wikis, and backups. The `Pure-CLI uninstall without LLM mediation` non-goal is dropped (the generic path is pure CLI) and the stale `.atomic-uninstall-backup` risk mitigation is removed — neither appears in the implementation.

### 2026-07-16 — User state root relocated to ~/.atomic

**What changed:** Every body mention of `~/.claude/.atomic/...` (pre-install snapshot location, manifest read path, restore/merge instructions, final `rm -rf` target) now reads `~/.atomic/...`.

**Why:** `docs/spec/configurable-state-paths.md` (issue #150) relocates the user state root; `BuildUninstallPlan` already resolves the removal target via `config.Dir(home)`, which now returns `~/.atomic`. The compat symlink migration leaves at `~/.claude/.atomic` is intentionally not removed by uninstall (per that spec's non-goals) — it is left orphaned, same as the other legacy paths this spec already documents leaving behind.

**Superseded:** Prior body named `~/.claude/.atomic/` as the pre-install snapshot root and removal target throughout.

### 2026-05-28 — preserve user profile on uninstall

**What changed:** `BuildUninstallPlan` now explicitly excludes `~/.claude/.atomic/profile.md` from the Delete list. Profile is user-data generated by `ensureProfileStub` during install; there is no pre-install counterpart and uninstall must not delete it.

**Why:** `docs/spec/user-profile.md` introduces a new global profile file at `~/.claude/.atomic/profile.md`. The file accumulates user-provided facts across sessions and projects. Treating it like other atomic-owned files (deleting on uninstall) would destroy user data. The defensive guard covers the edge case where future manifest changes accidentally include the file.

**Superseded:** prior contract treated all atomic-touched files uniformly via pre-install three-way detection — no explicit per-file preservation list.
