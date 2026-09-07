# /autopilot — autonomous feature delivery

## Goal

A command that takes a unit of work (task description or GitHub issue number)
from nothing to shipped with **one** human decision — how to merge. It codifies
the autonomous lifecycle: plan → the shared implement→review loop → ship.

## Non-goals

- Replacing the interactive lifecycle. `/atomic-plan` and `/subagent-implementation` stay for when the user wants approval gates and per-finding control.
- Auto-pushing to a shared remote or auto-merging without the user's merge selection (axiom 3 — the merge choice is the explicit confirm).
- A `--auto` flag bolted onto `/subagent-implementation` — `/autopilot` also owns the planning and ship phases, so it is its own command.

## The five codified behaviors

These are the contract; they override the interactive defaults because invoking
`/autopilot` is the user's opt-in to autonomy.

1. **Always uses the shared implement→review loop** (the `implement-loop` partial, the same text `/subagent-implementation` composes). No inline implementation by the orchestrator.
2. **Every reviewer finding addressed in-iteration.** Blocking and non-blocking. The scratchpad `FOLLOWUPS.md` ends empty; the finalize has no follow-ups step.
3. **Auto-dispatching `atomic-strategist` is allowed.** On stuck-fix escalation, dispatch the strategist (read-only RCA) instead of surfacing-and-waiting; feed findings back through the builder loop. Safe because the strategist never writes.
4. **Always asks how to merge — and only that.** The single interactive gate. Skipped if a merge-verb was passed in `$ARGUMENTS`.
5. **Currency-clean spec before every dispatch.** Per the `CLAUDE.md` planning rule — the spec body is current truth, nothing that could divert a fresh subagent.

## Success criteria

- [ ] `context/commands/autopilot.md` composes the `worktree-setup`, `implement-loop`, and `loop-finalize` partials; its frontmatter `description` names the input shape (`<task | issue#> [merge-verb]`) and the single merge question.
- [ ] Sections: policy table (Worktree `auto`, Stuck `auto`, Non-blockers `fix-all`, Finalize verify, docs, audit, log, signals, report; Ship) → scratch hygiene → resolve input (issue# → `gh issue view`) → plan (autonomous, no approval gate, currency-clean spec) → worktree → loop → finalize → ship gate → report.
- [ ] The five behaviors are stated once, as policy table values and constraints; the strategist runs read-only and the merge gate is the one confirmation.
- [ ] The ship gate uses `AskUserQuestion` with the ship-verb options, and is skipped when a merge-verb is supplied in `$ARGUMENTS`.
- [ ] A genuine blocker halts the run and surfaces — autonomy is not "ignore failures".
- [ ] Scratch hygiene: the body forbids `rm` and chained shell commands mid-run (both trigger permission prompts that stall an unattended run); experiments are quarantined to `tmp/trash/` (gitignored) and the dispatch briefs carry the same instruction; the Ship section does one `rm -rf tmp/trash` at the end (the single expected deletion prompt), degrading to "leave it" if permission is not granted; the task scratchpad is retained for `/git-cleanup`.
- [ ] Registered on every discovery surface: `CLAUDE.md` Workflow section, `/atomic-help` (topic row + tour), `docs/reference/commands.md`. Cross-references `/subagent-implementation`, `atomic-strategist`, and the ship verbs.
- [ ] `make -C atomic bundle` resolves every partial directive; `/atomic-help` MISSING-scan returns zero.

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

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Author `context/commands/autopilot.md` (policy table, scratch hygiene, resolve, plan, ship; loop and finalize from the shared partials) — atomic-implementer, ~1 file | `context/commands/autopilot.md` | Bundles; body covers every success-criterion bullet about behavior |
| 2 | Discovery wiring: `CLAUDE.md` Workflow, `/atomic-help` topic + tour, `docs/reference/commands.md`, cross-refs — atomic-implementer, ~3 files | `context/CLAUDE.md`, `context/commands/atomic-help.md`, `docs/reference/commands.md` | `make -C atomic bundle` exit 0; `/atomic-help` MISSING-scan zero; cross-ref grep |
| 3 | Bundle + signals refresh — atomic-implementer (mode: surgical) | `docs/wiki/` | `make -C atomic bundle` exit 0; `atomic doctor` no new WARN/FAIL |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Autonomy hides a failure (run "succeeds" while skipping a broken step) | med | The finalize verify step runs the full suite via `atomic-verify`; a genuine blocker halts and surfaces; constraint "autonomy is not ignore failures" |
| Strategist auto-dispatch surprises a cost-sensitive user | low | Read-only + documented in the description; the user opted in by invoking `/autopilot`; a future `--no-strategist` is a clean follow-up if needed |
| Override semantics drift from the loop they wrap | low | The body composes the same `implement-loop` partial the other verbs use; the overrides are policy table values the partial branches on |
| Currency-clean rule not actually enforced mid-run | med | The Plan section's currency sentence and the loop's spec check both require re-verifying the spec body before each dispatch |

## Change log

### 2026-09-06 — Compose from shared loop partials

**What changed:** The command is a policy table plus its own sections (scratch hygiene, resolve, plan, ship), composed with the `worktree-setup`, `implement-loop`, and `loop-finalize` partials. The five behaviors are policy table values (`Stuck: auto`, `Non-blockers: fix-all`, `Ship`) and constraints, stated once. The task scratchpad is retained at the end; only `tmp/trash/` is deleted.

**Why:** `docs/design/implement-loop-consolidation.md`: the body referenced `/subagent-implementation` as the loop without loading it, and restated the five behaviors three times.

**Superseded:** "run the loop exactly as `/subagent-implementation` defines it" with three locally stated overrides; Phase 6 scratchpad cleanup.

### 2026-06-07 — Scratch hygiene: quarantine instead of delete

**What changed:** Added a `scratch_hygiene` discipline to the command body. During an autopilot run, the orchestrator and its subagents never `rm` and never chain shell commands (`&&`, `;`) — both trigger Bash permission prompts that stall an unattended run. Experiments, probes, and one-off scripts are quarantined under `tmp/trash/` (created once in Phase 2, gitignored) by `mv`, not deleted. Every builder/surgeon dispatch brief carries the same instruction. Phase 6 (renamed "Summary and cleanup") performs one `rm -rf tmp/trash` + scratchpad removal at the very end — the single deletion where a permission prompt is expected; if the user is away to grant it, both are left in place (gitignored, never shipped).

**Why:** observed in practice — autopilot experiments looking for evidence ran `rm` and chained commands mid-run, each firing a permission prompt that blocked the hands-off flow waiting on a human. Deferring all deletion to one end-of-run cleanup keeps the run unattended and collapses N scattered prompts into one expected prompt at the finish.

**Note on rule 4:** the end cleanup is a Bash permission grant, not an `AskUserQuestion`, so the ship gate remains the only decision prompt.
