# atomic doctor


## Goal


Single deterministic Go CLI subcommand that verifies install + project-state coherence for atomic-claude. Reports drift (install mismatch, missing hooks, stale signals, unwired `@-refs`, manifest divergence, malformed followups, orphan memory refs, outdated binary) as an indexed PASS/WARN/FAIL checklist. Non-zero exit on FAIL for CI gating. Opt-in repair via `--fix`.


Design source: `docs/design/atomic-doctor.md`.


## Non-goals


- Linting artifact **content** (delegated to `atomic validate`, separate spec).
- Running tests, builds, or invoking Claude.
- Network calls beyond the existing `atomic update --check` path.
- Multi-project rollup. One repo per invocation.
- Auto-running on `atomic update` / `atomic claude install`. Doctor stays explicit-only in v1.


## Success criteria


- [ ] `atomic doctor` exits 0 when every check is PASS or WARN; exits 1 on any FAIL; exits 2 on internal error.
- [ ] Output is reproducible (no timestamps in the diff; no random ordering).
- [ ] `--json` emits a stable schema (versioned via `schema_version`) for CI consumers.
- [ ] `--fix` prompts per item (axiom 3); no batched silent mutations.
- [ ] `--only <cat>` / `--skip <cat>` accept category indices or canonical short names.
- [ ] Missing `~/.claude/` short-circuits to exit 0 with one informational line; no FAIL cascade.
- [ ] Repo-dev-only checks (manifest parity) skip silently when not in the atomic-claude repo.
- [ ] All checks deterministic — no LLM judgment, pure Go.
- [ ] `go test ./atomic/internal/doctor/...` covers each check + each repair with table-driven cases.


## Surface


```
atomic doctor [--fix] [--json] [--only <cat[,cat...]>] [--skip <cat[,cat...]>] [--stale-days N] [--verbose]
```


| Flag | Effect |
|------|--------|
| `--fix` | Per-item confirm prompt before applying any repair. Implies interactive. |
| `--json` | Emit machine-readable result to stdout. Suppresses human output. |
| `--only` | Comma-separated category indices (`1,3`) or names (`install,signals`). |
| `--skip` | Same syntax. Skip listed categories. |
| `--stale-days` | Override stale-signals threshold (positive int). Default 7. |
| `--verbose` | Print per-file detail for `install integrity` and `manifest parity`. |


`--fix` + `--json` is a usage error (exit 2).


## Check categories


Indexed. Numbers are stable; **never renumber**. New checks append.


| # | Name (canonical) | Checks | Fail severity |
|---|------------------|--------|---------------|
| 1 | `install`        | `~/.claude/{agents,commands,skills,output-styles,rules}/` exist; per-file SHA256 matches embedded bundle manifest. | WARN drift / FAIL missing |
| 2 | `hooks`          | `~/.claude/settings.json` contains the session-start hook payload that `atomic hooks session-start` would emit. | WARN |
| 3 | `signals`        | `.claude/project/deterministic-signals.md` exists; `atomic signals stale --threshold <days>` exits 0. Threshold defaults to 7; overridable via `--stale-days`. | WARN |
| 4 | `refs`           | `@`-refs to both signals files present in one of `claude.local.md` / `CLAUDE.local.md` / `CLAUDE.md` / `claude.md` (skill search order). | FAIL |
| 5 | `manifest`       | Repo-dev only (heuristic: `atomic/internal/bundlemirror/` exists). Committed `manifest.go` SHA matches what `go generate ./...` would produce — without writing. | FAIL |
| 6 | `followups`      | If `.claude/project/followups.md` exists, every `### F-<id>` entry has an `Origin:` line and a severity bucket. | WARN |
| 7 | `memory`         | `~/.claude/projects/<project>/memory/MEMORY.md` link targets all resolve (file exists in same dir). | WARN |
| 8 | `binary`         | `atomic update --check` succeeds without performing update. | WARN |
| 9 | `config`         | `~/.claude/.atomic/config.toml` parses + validates; `~/.claude/.atomic/config.resolved.md` matches render of TOML (byte-stable). Parse error → FAIL; invalid enum value → FAIL; unknown keys → WARN; drifted/missing resolved.md → WARN. | WARN by default; FAIL for parse error or invalid value |


Category short-names are stable: editing/removing one is a spec amendment (`Removed:` log entry).


## Stale-signals threshold


Default: 7 days. Overridable per-invocation via `--stale-days N`. No persistent configuration — axiom 2 (memory > config) does not apply: doctor is a deterministic Go CLI with no LLM in the loop, so memory is unreadable from its perspective. A real config file would be parsed-markdown-cosplay; a flag is honest.


## Repo-dev detection


A run counts as "in the atomic-claude repo" iff `atomic/internal/bundlemirror/mirror.go` exists relative to the git toplevel of the cwd. When false, category 5 is skipped (status `SKIP`, no PASS/WARN/FAIL counted).


## Output format (human)


```
atomic doctor — integrity check  (project: <name>)

[1] PASS  install                  36/36 files match bundle
[2] WARN  hooks                    session-start hook missing
[3] PASS  signals                  last scan 3d ago (threshold 7d)
[4] FAIL  refs                     @-refs not present in CLAUDE.md, claude.local.md, CLAUDE.local.md, or claude.md
[5] SKIP  manifest                 not in atomic-claude repo
[6] PASS  followups                no .claude/project/followups.md
[7] PASS  memory                   8/8 refs resolve
[8] PASS  binary                   v0.4.2 (latest)

5 PASS, 1 WARN, 1 FAIL, 1 SKIP. exit 1.

To repair: atomic doctor --fix
```


Counters exclude SKIP. Exit code is determined by FAIL count only.


## Output format (`--json`)


```json
{
  "schema_version": 1,
  "project": "claude-code-setup",
  "results": [
    {"index": 1, "name": "install", "severity": "PASS", "detail": "36/36 files match bundle"},
    {"index": 2, "name": "hooks",   "severity": "WARN", "detail": "session-start hook missing"}
  ],
  "summary": {"pass": 5, "warn": 1, "fail": 1, "skip": 1, "exit": 1}
}
```


Bumping `schema_version` is a spec amendment.


## Missing `~/.claude/`


Short-circuit before running any check:


```
atomic-claude not installed; run `atomic claude install`.
```


Exit 0. No other output. `--json` form:


```json
{"schema_version": 1, "installed": false, "message": "atomic-claude not installed; run `atomic claude install`.", "summary": {"exit": 0}}
```


## Repair mode (`--fix`)


Per-item confirm (axiom 3). Each repair idempotent. Print every shell command before running it.


| # | Category | Repair action |
|---|----------|---------------|
| 1 | `install`   | `atomic claude install --merge` (re-uses existing merge-required guard for `CLAUDE.md`). |
| 2 | `hooks`     | `atomic hooks install`. |
| 3 | `signals`   | **Cannot auto-fix.** Print: `run /refresh-signals from Claude Code to refresh signals.` |
| 4 | `refs`      | Ask user which file to patch (numbered list per axiom 4 if >1 candidate); append `@`-ref block. |
| 5 | `manifest`  | `make -C atomic bundle` (regenerates); refuses outside the atomic-claude repo. |
| 6 | `followups` | **Cannot auto-fix.** Print malformed entries with line numbers; refuse to edit (content authorship is human). |
| 7 | `memory`    | **Cannot auto-fix.** Print orphan refs; refuse to delete (user-authored). |
| 8 | `binary`    | Print: `atomic update` to update. |


Skill-required and content-authored repairs degrade to printed instructions. This is the acceptable boundary: the CLI cannot dispatch a Claude skill, and cannot rewrite human authorship.


## Exit codes


| Code | Meaning |
|------|---------|
| 0 | All PASS or only WARN/SKIP. Also: `~/.claude/` missing (short-circuit). |
| 1 | One or more FAIL. |
| 2 | Doctor itself errored (cannot read state, conflicting flags, missing required dependency). |


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Scaffold `atomic/internal/doctor/` package: types (`Check`, `Result`, `Severity`), category registry, CLI flag parsing. | `atomic/internal/doctor/doctor.go`, `_test.go`; `atomic/cmd/atomic/main.go` wiring | `go test ./internal/doctor` covers registry + flag parsing; `atomic doctor --help` lists categories. |
| 2 | Extract shared `manifestcheck` substrate (for parity check; reused by future `atomic validate`). | `atomic/internal/manifestcheck/manifestcheck.go`, `_test.go` | Unit test: synthetic bundle, mutate one file → parity fails; revert → passes. |
| 3 | Implement checks 1 (install), 5 (manifest), 8 (binary). All are SHA / file / network-light; closest to existing code. | `atomic/internal/doctor/checks_install.go`, `checks_manifest.go`, `checks_binary.go`, `_test.go` | Table-driven tests per check; happy + drift + missing-dir + skip cases. |
| 4 | Implement checks 2 (hooks), 4 (refs). Depend on `atomic/internal/hooks` and signals-skill search order. | `atomic/internal/doctor/checks_hooks.go`, `checks_refs.go`, `_test.go` | Tests cover all 4 ref-file candidates; hook payload diff. |
| 5 | Implement checks 3 (signals), 6 (followups), 7 (memory). Threshold from flag (default 7). | `atomic/internal/doctor/checks_signals.go`, `checks_followups.go`, `checks_memory.go`, `_test.go` | Tests cover default + `--stale-days` override; malformed followup entry; orphan memory link. |
| 6 | Output formatters: human + JSON. Filter (`--only`/`--skip`). Counters/exit-code logic. Missing-`~/.claude/` short-circuit. | `atomic/internal/doctor/format.go`, `_test.go` | Golden-file tests for both formats; exit-code matrix. |
| 7 | Repair mode (`--fix`). Per-item `AskUserQuestion`-style prompts; print-before-run; idempotency. | `atomic/internal/doctor/fix.go`, `_test.go` | Tests with stubbed prompter (yes / no / abort). |
| 8 | Wire into surfaces: `CLAUDE.md` "Other commands"/Atomic-binary line, `claude.local.md` mirror, `README.md` table, signals refresh. Bundle regen. | `CLAUDE.md`, `CLAUDE.md`, `README.md`, `atomic/internal/embedded/bundle/**`, `manifest.go` | `git diff` shows all four surfaces updated; `make -C atomic bundle` no-op; `make -C atomic test` green. |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Install-integrity SHA check slow on large bundles | low | Bundle is small (47 items today); cache nothing, just measure. Re-evaluate if >500ms. |
| `manifest` check produces false-positives outside the atomic repo | med | Skip via repo-dev heuristic; document the heuristic; expose `--force-manifest` only if real demand emerges (out of scope for v1). |
| Hardcoded 7-day threshold wrong for some workflows | low | `--stale-days N` overrides per-invocation; document default in `atomic doctor --help`. No persisted config (deliberate). |
| `--fix` for `refs` could clobber user-authored CLAUDE.md content | med | Append only at EOF (not mid-file). If file is missing trailing newline, add one. Never modify existing `@`-refs. |
| Bundle parity check (`make -C atomic bundle`) writes files even in dry-run | high | Use `manifestcheck` against an in-memory generated manifest; do NOT shell out to `go generate` for the check itself. Only `--fix` shells out. |
| Categories drift between code and spec | med | Keep `atomic/internal/doctor/categories.go` as the single registry; spec table mirrors it; CI lint (future) flags drift. |
| Doctor invoked in CI without `--json` clutters logs | low | Document `--json` in `atomic doctor --help`; add example to README install/eval section. |


## Cross-references


- Design: `docs/design/atomic-doctor.md`.
- Implementation home: `atomic/internal/doctor/` (new package).
- Shared substrate: `atomic/internal/manifestcheck/` (new package; consumed by future `atomic validate`).
- Axioms: 3 (destructive ops explicit confirm — `--fix`), 4 (plain-text indexed selection — multi-candidate ref patch). Axiom 2 (memory > config) deliberately N/A — see "Stale-signals threshold" rationale.
- Surfaces to update on landing (per `claude.local.md` checklist): `CLAUDE.md`, `CLAUDE.md`, `README.md` (commands or "atomic binary" table), bundle regen.


## Change log


<!-- empty on creation; entries appended on amendment after approval -->

### 2026-05-23 — Signals check gains router validation

**What changed:** The `signals` check (category 3) now validates the signals router in addition to existing freshness checks. New validations: `signals.md` present, `signals.md` `@-ref`'d in a CLAUDE.md-family file, all domain files referenced in the router's Domains table exist on disk, no orphan domain files under `signals/`. Missing router emits WARN (not FAIL) to allow the transition period where repos still have the flat `inferred-signals.md`. Check remains anchored to cwd via `repoctx.Toplevel()` — no worktree cross-comparison.

**Why:** Signals router spec (`docs/spec/signals-router.md`) replaces the flat `inferred-signals.md` with a router + domain files architecture. The doctor check must validate the new file layout to catch broken refs, orphan files, and missing `@-ref` wiring.


### 2026-05-22 — Post-update auto-fire (from atomic-update-doctor)

**What changed:** Doctor now has a second invocation surface beyond the explicit `atomic doctor` command. After a successful binary swap, `atomic update` invokes the doctor pass automatically (controlled by `update.run_doctor` config key, default `true`; overridable per-invocation via `--no-doctor`). Only FAIL-severity lines are printed in this mode; a clean run produces no output. The check set, format, and exit semantics are identical to the explicit invocation.

**Why:** Post-update drift is the most common window for install/config coherence failures. Automatic post-update execution catches these without requiring the user to remember to run doctor manually after every binary swap.

**Cross-reference:** `docs/spec/atomic-update-doctor.md` — full spec for the `atomic update` post-update doctor integration, including flag surface, config key, FAIL filtering, and panic recovery contract.

### 2026-05-20 — Add config check category

**What changed:** New check category 9 (`config`) verifies `~/.claude/.atomic/config.toml` parses + validates and that `~/.claude/.atomic/config.resolved.md` matches the TOML render. `--fix` re-renders the resolved view on drift (WARN). Parse errors and invalid enum values are reported as non-fixable FAIL.

**Why:** Spec `docs/spec/atomic-state-and-config.md` introduces user-persistent config storage delivered to every session via an `@-ref` from `CLAUDE.md` to `~/.claude/.atomic/config.resolved.md`. The new check ensures coherence between the TOML source and the rendered view, and catches values that would otherwise be silently injected into Claude sessions.


## Implementation log


### v1 — 2026-05-17


Built across 11 iterations of `/subagent-implementation` (8 checkpoints + 1 spec promotion + 2 reviewer-driven fix iterations + 1 polish pass). Commits chronological:


- `ba5992f` — docs(spec): promote atomic-doctor design to spec
- `b0d8475` — feat(doctor): CP-1 scaffold (types, registry, flag parsing, CLI subcommand)
- `2248b68` — feat(manifestcheck): CP-2 in-memory bundle parity substrate
- `9eb1e4c` — feat(doctor): CP-3 checks 1/5/8 (install, manifest, binary)
- `05fbc8e` — feat(doctor): CP-4 checks 2/4 (hooks, refs)
- `b98b3f9` — feat(doctor): CP-5 checks 3/6/7 (signals, followups, memory)
- `1331e3d` — feat(doctor): CP-6 human + JSON formatters, exit codes, CLI wiring
- `28314cc` — feat(doctor): CP-7 --fix repair mode with axiom-3 per-item confirm
- `044fe00` — docs(doctor): CP-8 wire CLAUDE.md, README, atomic-binary spec, .gitignore
- `dbe2a53` — chore(doctor): close 6 FOLLOWUPS in polish pass


**Out-of-scope work performed during this build:**


- Added `hooks.IsInstalled` exported func (`atomic/internal/hooks/hooks.go`). Needed for check 2; could not be inferred from existing surface. Single new export, mirrors `Install` style.
- Added `bundlemirror.Enumerate` exported func (read-only walker). Needed by `manifestcheck.Compare` to reuse the inclusion rules without writing. Refactor preserves `bundlemirror.Run` behavior bit-for-bit (CI parity check passes).


**Unforeseens — surprises that emerged during implementation:**


- Spec initially proposed memory-configured stale-signals threshold (axiom 2). Caught during plan review — doctor is a deterministic Go CLI with no LLM in the loop, so memory is unreadable from its perspective. Replaced with `--stale-days` flag per-invocation override (default 7). Axiom 2 explicitly N/A.
- `TestRunReturnsAllResults` (added in CP-1 when every check was a SKIP stub) started making live GitHub Releases calls once check 8 was wired in CP-3. Caught at CP-3 review; fixed by filtering check 8 out of the integration-shape test (its dedicated tests use the `binaryLookupFn` seam).
- `appendRefsIfMissing` initially appended the full ref block even when one ref was already present, creating a semantic duplicate. Caught at CP-7 review; fixed to append only the missing line(s) in partial cases.


**Deferred items still open:**


- 4 entries promoted to `.claude/project/followups.md` as `atomic-doctor-F-1` through `atomic-doctor-F-4`: bundlemirror hidden contract, `gitToplevel` triple-call latency, repair-seam global mutators, manifest-repair output forwarding.
- 13 nits dropped at finalize (test-quality cosmetics, inherited spec format quirks). Audit trail in branch commit history and per-iteration `STATE.md` (deleted with the scratchpad).


**This branch (atomic-state-and-config) squashed onto `main` as `5c9d61c` — 2026-05-21.** Change log entry above amended via squash.
