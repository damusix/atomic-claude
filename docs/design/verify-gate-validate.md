# Design: `atomic validate` in the verify gate

## Problem

PR #38 shipped a structurally-invalid spec that passed every local verification and failed only in CI. `docs/spec/wiki.md` carried a 6-column `## Checkpoints` table where `atomic validate spec` rule S5 requires the canonical 4-column header. The repo's own validator was never in the pre-ship gate, so the break surfaced only after push.

The original root cause is unaddressed:

- **The verify gate never runs `atomic validate`.** The `atomic-verify` skill and both orchestrators (`/autopilot` Phase 4, `/subagent-implementation` Phase 3) check tests / build / lint / typecheck only. A structurally-invalid spec passes local verification and surfaces only in CI.

The proximate trigger for the original PR #38 break — `/atomic-plan` emitting a 6-column checkpoint table that S5 rejected — has since been resolved independently. Commit `0d24f22` (2026-06-08) relaxed S5 to match the required columns as an ordered subsequence, explicitly allowing extra columns (`Agent`, `Est. files`). Verified empirically: the current 6-column template header passes `atomic validate spec` with 0 FAIL. No template change is needed; see Non-goals.

## Decision

Wire `atomic validate` into the pre-ship verify gate, and reconcile the `/atomic-plan` template so template-authored specs pass S5 by default.

### Where `atomic validate` belongs

The `atomic-verify` skill is the general gate both orchestrators invoke. But the skill ships to user repos and is otherwise fully generic (tests / build / lint / typecheck / repro) — it carries zero atomic-system-specific content today. `atomic validate` is atomic-specific: it validates `docs/spec/**` structure, cross-reference integrity, and bundle parity, and it requires the `atomic` binary.

Resolution: add `atomic validate` to the skill as a **conditional** check, not an unconditional one. It runs only when the change touched `docs/spec/**`, `docs/design/**`, or bundled artifacts, and it degrades silently when the `atomic` binary is absent or no specs exist. This keeps the skill useful for plain user work (where the conditional never fires) while closing the gap for spec-bearing changes. Because both orchestrators already delegate to `atomic-verify`, they inherit the behavior; the inline enumerations in each command are updated to name `atomic validate` explicitly so the documented gate matches what runs.

### Which validators

- `atomic validate spec` — the rule that bit us (S5), plus S0/S1/S6. Runs when a spec changed.
- `atomic validate config` — cross-reference integrity (C1/C3/C5/C7/C9). Runs when a spec/design/artifact changed.
- `atomic validate bundle` — already covered by the existing render+bundle parity check in the orchestrators; not added to the generic skill (it is repo-build-specific, not user-relevant).

### Graceful degradation

`atomic validate` is gated behind binary presence. If `atomic` is not on PATH, or there are no matching files, the check is skipped without failing the gate. This is the axiom-2-consistent posture: the skill must not hard-fail in a repo that installed the artifacts but not the binary.

## Non-goals

- **No `/atomic-plan` template reconcile.** The followup proposed dropping the 6-column checkpoint table to 4 columns. S5 now allows extra columns as an ordered subsequence (commit `0d24f22`), so the 6-column header already passes. Removing the `Agent` / `Est. files` columns would strip useful planning context for zero validation benefit — out of scope.
- No new `atomic validate` rules or flags. The interface is used as-is.
- No change to `atomic validate` itself (Go code untouched).
- No change to the generic claim→check table semantics beyond the one conditional row.

## Verification

- `atomic validate spec docs/spec/verify-gate-validate.md` passes (this spec dogfoods the gate it adds).
- Rendered `commands/autopilot.md` and `commands/subagent-implementation.md` name `atomic validate` in their verify phases.
- `make render` + `make -C atomic bundle` clean (no drift).
- Full Go suite green.
