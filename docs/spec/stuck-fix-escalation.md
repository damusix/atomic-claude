# Stuck-fix escalation + suppression awareness

## Goal

Wire two defaults into the implement→review loop so it stops digging when stuck
instead of accumulating suppression debt: (1) a **stuck-fix escalator** that
surfaces a runnable `/pressure-test` or `atomic-strategist` RCA step after
repeated failure on the same signal, and (2) **suppression-pattern awareness** in
the reviewer that flags error-catching-without-investigation. Both surfaced,
never auto-invoked. Closes GitHub issue #29.

## Non-goals

- Hard-coded line-level lint for suppression patterns (false-positive-prone).
- Auto-dispatching `atomic-strategist` without user opt-in (axiom 3).
- Replacing the reviewer's `CHANGES_REQUESTED` loop — this complements it.
- A new skill/command artifact — the loop is the home (axiom 2).

## Success criteria

- [ ] `/subagent-implementation` Step C (triage) tracks the failing signal across iterations and, after **2 consecutive `CHANGES_REQUESTED` rounds on the same checkpoint with an unchanged failing signal** (or the same finding repeating twice), surfaces a **stuck-fix escalation** block: a copyable `/pressure-test @<spec>` line AND an offer to dispatch `atomic-strategist` (opus, read-only) for cross-cutting RCA. It is a loop **default**, not opt-in.
- [ ] The escalation is **surfaced, never auto-invoked** — the orchestrator prints it and waits; it does not auto-dispatch the strategist (axiom 3). The spec/STATE wording states this explicitly.
- [ ] `atomic-reviewer` emits a **suppression-pattern finding** when a diff adds error-catching constructs (try/catch, `?.`/null-guards added solely to dodge an error, `.catch(() => …)` swallows, empty catch, broad `except`) **without** accompanying investigation (no new logging/instrumentation, no new test exercising the failure, no evidence the root cause was examined). Severity 🟡 by default; 🔴 when it is the Nth such suppression on the same error across iterations.
- [ ] The shared `reviewer-prompt` template carries the suppression check so dispatched reviewers apply it.
- [ ] `/subagent-diagnose`'s existing same-failure bail gains the same escalation surface: on bail it offers `/pressure-test` + `atomic-strategist` RCA, rather than only stopping.
- [ ] Both behaviors are documented as loop defaults wherever the loop's contract is described; no contradiction introduced with the existing iteration-cap / repeat-finding prose (reconcile, don't duplicate).
- [ ] `make render` + `make bundle` parity clean; `/atomic-help` MISSING-scan zero; `go test ./...`, `go vet`, `gofmt -l` clean; `atomic doctor` no new WARN/FAIL.

## Approaches

(From `docs/design/stuck-fix-escalation.md`.)

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | Escalator in orchestrator triage + suppression flag in reviewer | each detection where its evidence lives | low-med | two files change in concert |
| B | New `atomic-stuck` skill | a skill watches stuck language | med | wrong home; axiom 2 |
| C | Port diagnose detector verbatim | reuse error-string bail | low | string-compare not edit-shape; no RCA escalation |

## Recommendation

**A.** The orchestrator owns the escalation (it alone sees `STATE.md` iteration
history); the reviewer owns the suppression-shape judgment (it alone reads the
diff). Defaults ride the existing loop with no new artifact. Evidence:
`subagent-diagnose.md` already has the same-failure bail to extend;
`agents/atomic-strategist.md` already names "stuck or repeatedly failing review"
as its dispatch trigger — this wires the caller that was missing.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Stuck-fix escalator: add a "Stuck-fix escalation" trigger to `/subagent-implementation` Step C (track failing signal across iterations; after 2 same-signal `CHANGES_REQUESTED` rounds → surface copyable `/pressure-test` + `atomic-strategist` RCA offer, never auto-dispatch). Reconcile with the existing 6-iter soft-stop + repeat-finding prose. Extend `/subagent-diagnose` same-failure bail to surface the same RCA options. — atomic-builder, ~2 files | `templates/commands/subagent-implementation.md`, `templates/commands/subagent-diagnose.md` | The trigger, threshold, surfaced-not-auto-invoked rule, and both runnable options are present; no contradiction with existing cap/repeat-finding text; diagnose bail offers RCA |
| 2 | Suppression-pattern awareness: add a suppression finding rule to `atomic-reviewer` (flag error-catching-without-investigation; 🟡 default, 🔴 on repeat) and to the shared `reviewer-prompt` template so dispatched reviewers apply it — atomic-builder, ~2 files | `templates/agents/atomic-reviewer.md`, `commands/_templates/reviewer-prompt.md` (or its template source) | Reviewer + reviewer-prompt both describe the suppression check with severity rule; consistent with existing severity tiers |
| 3 | Regenerate render + bundle; wire discovery if the loop contract changed (CLAUDE.md note / `/atomic-help` if needed); verify — atomic-surgeon | `commands/`, `agents/`, `atomic/internal/embedded/**`, `CLAUDE.md`/`templates/commands/atomic-help.md` if needed | `make render`+`make bundle` parity clean; `/atomic-help` MISSING-scan zero; `go test ./...` + `atomic doctor` no new WARN/FAIL |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Suppression rule false-positives on legitimate defensive code | med | Surfaced as 🟡 finding (not a block); rule keys on *catch-without-investigation across iterations*, not any single guard; reviewer uses judgment, not regex |
| Escalation fires too eagerly, nags | low | One ignorable surfaced block; threshold = 2 same-signal rounds; never auto-dispatches |
| New text contradicts existing iteration-cap / repeat-finding prose | med | CP1 explicitly reconciles rather than appends; reviewer checks for contradiction |
| Editing the loop's own orchestration files breaks the phase-gate / cross-ref coherence just established | med | Builders edit templates only; render/bundle parity gate; reviewer checks the named-gate + Phase-3 references survive |

## Implementation log

### v1 — 2026-05-30

Built in a worktree (`stuck-fix-escalation`) across 2 checkpoints of
/subagent-implementation, each builder→reviewer with findings folded in-iteration.

- CP1 — stuck-fix escalator in `/subagent-implementation` Step C (track failing signal across iterations; after 2 same-signal `CHANGES_REQUESTED` rounds → surface copyable `/pressure-test` + `atomic-strategist` RCA offer; surfaced, never auto-invoked; reconciled with the old 6-iter soft-stop + repeat-finding prose). `/subagent-diagnose` same-failure bail enriched to offer the same RCA options.
- CP2 — suppression-pattern rule in `atomic-reviewer` + the shared `reviewer-prompt` (flag error-catching-without-investigation; 🟡 default, 🔴 on a 2+ suppression of the same error, aligned to the orchestrator threshold; judgment not regex).
- CP3 — `docs/reference/workflow.md` notes the new default; render/bundle + verification.

**Out-of-scope work:** none.

**Unforeseens:** the pre-commit hook auto-ran render+bundle on the CP1/CP2 commits (templates + agent sources), so the rendered `commands/`/`agents/` and embedded bundle stayed in sync throughout.

**Deferred items still open:** none — every reviewer finding (4 in CP1, 3 in CP2) was folded in-iteration via surgical passes.

## Change log

<!-- Empty. First entry on the first amendment after this spec ships. -->
