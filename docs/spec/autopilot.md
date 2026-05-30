# /autopilot — autonomous feature delivery

## Goal

A command that takes a unit of work (task description or GitHub issue number)
from nothing to shipped with **one** human decision — how to merge. It codifies
the autonomous lifecycle: plan → the `/subagent-implementation` loop → ship.

## Non-goals

- Replacing the interactive lifecycle. `/atomic-plan` and `/subagent-implementation` stay for when the user wants approval gates and per-finding control.
- Auto-pushing to a shared remote or auto-merging without the user's merge selection (axiom 3 — the merge choice is the explicit confirm).
- A `--auto` flag bolted onto `/subagent-implementation` — `/autopilot` also owns the planning and ship phases, so it is its own command.

## The five codified behaviors

These are the contract; they override the interactive defaults because invoking
`/autopilot` is the user's opt-in to autonomy.

1. **Always uses the `/subagent-implementation` loop.** No inline implementation by the orchestrator.
2. **Every reviewer finding addressed in-iteration.** Blocking and non-blocking. The scratchpad `FOLLOWUPS.md` ends empty — nothing deferred to a Phase 3 triage (there is no interactive triage here).
3. **Auto-dispatching `atomic-strategist` is allowed.** On stuck-fix escalation, dispatch the strategist (read-only RCA) instead of surfacing-and-waiting; feed findings back through the builder loop. Safe because the strategist never writes.
4. **Always asks how to merge — and only that.** The single interactive gate. Skipped if a merge-verb was passed in `$ARGUMENTS`.
5. **Currency-clean spec before every dispatch.** Per the `CLAUDE.md` planning rule — the spec body is current truth, nothing that could divert a fresh subagent.

## Success criteria

- [ ] `templates/commands/autopilot.md` exists and renders to `commands/autopilot.md`; frontmatter `description` names the five behaviors and the input shape (`<task | issue#> [merge-verb]`).
- [ ] Phases: resolve input (issue# → `gh issue view`) → plan (autonomous, no approval gate, currency-clean spec) → worktree → `/subagent-implementation` loop with the three overrides → verify → ship gate → summary.
- [ ] The body states each of the five behaviors and *why each override is safe* (strategist read-only; merge gate = the axiom-3 confirm).
- [ ] The ship gate uses `AskUserQuestion` with the ship-verb options, and is skipped when a merge-verb is supplied in `$ARGUMENTS`.
- [ ] A genuine blocker halts the run and surfaces — autonomy is not "ignore failures".
- [ ] Registered on every discovery surface: `CLAUDE.md` Workflow section, `/atomic-help` (topic row + tour), `docs/reference/commands.md`. Cross-references `/subagent-implementation`, `atomic-strategist`, and the ship verbs.
- [ ] `make render` + `make bundle` parity clean; `/atomic-help` MISSING-scan returns zero; signals refreshed.

## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | Dedicated `/autopilot` command that drives `/atomic-plan`-style planning + the `/subagent-implementation` loop + a ship gate, with the autonomous overrides as its policy | one command body, composes existing engines | low-med | must keep the override semantics consistent with the loop it wraps |
| B | A `--auto` flag on `/subagent-implementation` | reuse one command | low | doesn't cover planning or ship; the user asked for a command; flag-mode bloats the loop command |
| C | Reimplement plan+loop+ship from scratch | self-contained | high | duplicates `/subagent-implementation` + `/atomic-plan`; drift |

## Recommendation

**A.** `/autopilot` is a thin orchestrator that composes the existing engines
(`/atomic-plan` planning discipline, the `/subagent-implementation` loop, the
ship verbs) and layers the five autonomous behaviors as policy. It owns the whole
lifecycle (planning and ship included), which a flag on the loop command (B)
cannot. Evidence: this exact flow was executed by hand for issue #29
(worktree → spec → 2-checkpoint loop with all findings folded in-iteration →
ship) — `/autopilot` codifies it.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Author `templates/commands/autopilot.md` (phases + five behaviors + safety rationale + ship gate) | `templates/commands/autopilot.md` | atomic-builder | 1 | Renders; body covers every success-criterion bullet about behavior |
| 2 | Discovery wiring: `CLAUDE.md` Workflow, `/atomic-help` topic + tour, `docs/reference/commands.md`, cross-refs | `CLAUDE.md`, `templates/commands/atomic-help.md`, `docs/reference/commands.md` | atomic-builder | ~3 | `make render` clean; `/atomic-help` MISSING-scan zero; cross-ref grep |
| 3 | Render + bundle + signals refresh | `commands/`, `atomic/internal/embedded/**`, `.claude/project/signals*` | atomic-surgeon | ~few | `make bundle` parity clean; `atomic doctor` no new WARN/FAIL |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Autonomy hides a failure (run "succeeds" while skipping a broken step) | med | Phase 4 runs the full suite via `atomic-verify`; a genuine blocker halts and surfaces; constraint "autonomy is not ignore failures" |
| Strategist auto-dispatch surprises a cost-sensitive user | low | Read-only + documented in the description; the user opted in by invoking `/autopilot`; a future `--no-strategist` is a clean follow-up if needed |
| Override semantics drift from the loop they wrap | med | The body references `/subagent-implementation` as the engine rather than restating it; only the three overrides are stated locally |
| Currency-clean rule not actually enforced mid-run | med | Rule 5 + the loop's own currency gate (added with #29's sibling planning work) both require re-verifying the spec body before each dispatch |

## Change log

<!-- Empty. First entry on the first amendment after this spec ships. -->
