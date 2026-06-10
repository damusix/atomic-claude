# Spec: `atomic validate` in the verify gate

Wire `atomic validate` into the pre-ship verify gate so structurally-invalid specs are caught locally, not only in CI. The `atomic-verify` skill is the source of behavior; both orchestrators that delegate to it name the check explicitly.

## Context

PR #38 shipped a spec that failed `atomic validate spec` rule S5 in CI but passed every local check, because the verify gate never runs `atomic validate`. This spec closes that gap. The secondary cause from the originating followup (the `/atomic-plan` template's 6-column checkpoint table) is already resolved by commit `0d24f22`, which relaxed S5 to allow extra columns — so no template change is in scope. See `docs/design/verify-gate-validate.md`.

## Behavior contract

### `skills/atomic-verify/SKILL.md`

Add `atomic validate` as a **conditional** check on the verify gate:

- Fires only when the change under verification touched `docs/spec/**`, `docs/design/**`, or bundled artifacts (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`).
- When it fires: run `atomic validate spec` (when a spec changed) and `atomic validate config` (cross-reference integrity). A FAIL is a gate failure, same standing as a failing test.
- **Graceful degradation:** if the `atomic` binary is not on PATH, or there are no matching files, skip silently — never fail the gate on absence. The skill ships to user repos that may have installed the artifacts but not the binary.
- The skill otherwise stays generic. The conditional never fires for plain user work (no spec/artifact touched), so the existing tests/build/lint/typecheck behavior is unchanged for the common case.

Add a row to the "Claim → required check" table and a short conditional note in the verification-discipline section. Frame positively ("when a spec or bundled artifact changed, run `atomic validate`").

### `templates/commands/autopilot.md` — Phase 4 Verify

Add `atomic validate` to the inline enumeration of checks the orchestrator runs (currently "tests, typecheck, lint, build, render+bundle parity, and the `/atomic-help` MISSING-scan if artifacts changed"). Keep the existing delegation to `atomic-verify`; the inline list documents what the gate now includes.

### `templates/commands/subagent-implementation.md` — Phase 3 Finalize

Same addition to the finalize gate (step 1, which invokes `atomic-verify`): name `atomic validate` alongside the test/typecheck/lint/build suite.

## Constraints

- No change to `atomic validate` Go code, rules, or flags. Interface used as-is: `atomic validate spec [paths...]`, `atomic validate config [paths...]`; `--json`, `--suggest`.
- No `/atomic-plan` template change (out of scope — S5 already passes the 6-column header).
- Bundle sources change → `make render` then `make -C atomic bundle` must run, and the regenerated `commands/` + `atomic/internal/embedded/` outputs committed in the same change.
- This is a behavior change to existing artifacts, not a new artifact. The `/atomic-help` router needs no new row (no new verb/agent/skill), but verify the existing autopilot / subagent-implementation descriptions still read true.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Add conditional `atomic validate` to the verify gate: `atomic-verify` skill + autopilot Phase 4 + subagent-implementation Phase 3 finalize; degrade gracefully when binary/specs absent. Then `make render` + `make -C atomic bundle`. | `skills/atomic-verify/SKILL.md`, `templates/commands/autopilot.md`, `templates/commands/subagent-implementation.md`, `commands/autopilot.md`, `commands/subagent-implementation.md`, `atomic/internal/embedded/**` | `atomic validate spec docs/spec/verify-gate-validate.md` passes; rendered `commands/autopilot.md` + `commands/subagent-implementation.md` contain `atomic validate`; `make render` + `make -C atomic bundle` leave no git diff; `go test ./...` green |

## Success criteria

- The verify gate (skill + both orchestrators) runs `atomic validate` when a spec or bundled artifact changed.
- The skill degrades silently when the `atomic` binary is absent — no hard failure in a binary-less user repo.
- Render + bundle parity holds; CI drift gates stay green.
- This spec itself passes `atomic validate spec`.

## Change log

- 2026-06-10 — Initial spec. Scope narrowed from the originating followup: cause 2 (reconcile `/atomic-plan` 6-column checkpoint table to 4 columns) dropped as moot — commit `0d24f22` relaxed S5 to allow extra columns, verified empirically (6-column header passes with 0 FAIL). Only cause 1 (the root fix: `atomic validate` in the verify gate) is in scope.
