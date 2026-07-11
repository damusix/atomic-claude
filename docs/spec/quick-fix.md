# /quick-fix command


## Goal


A `/quick-fix <task>` slash command that runs the subagent implement→review loop without the planning phase — for fixes with one obvious approach and clear success criteria, regardless of how many files the change spreads across.


## Non-goals


- No new agents, skills, or prompt templates. Reuses `atomic-implementer`, `atomic-reviewer`, `atomic-investigator`, and `commands/_templates/implementer-prompt.md` / `reviewer-prompt.md` unchanged.
- No spec or design authoring. `/quick-fix` never writes `docs/spec/` or `docs/design/`.
- No file-count cap anywhere in the command. Scope is cohesion-bounded (axiom 1) — a mechanical fix threading one param through DTO → validator → controller → use case → repo → client is one slice, however many files.
- No worktree gate.
- No finalize ceremony: no implementation log, no `/documentation` dispatch, no signals refresh (the ship verbs' `signals-gate` covers ad-hoc commits).
- Not for unknown root causes — that is `/subagent-diagnose` (or the `atomic-debug` skill) territory. `/quick-fix` assumes the cause is known and the fix shape is obvious.
- `/autopilot` unchanged — it continues to route through `/subagent-implementation` only.


## Success criteria


- [ ] `/quick-fix <task>` runs the implement→review loop with the same scratchpad trio (`BRIEF.md`, `STATE.md`, `FOLLOWUPS.md`) and the same `_templates` prompts as `/subagent-implementation`, with `{SPEC_PATH}` substituted as `"no spec — inline brief in BRIEF.md"`. No spec file is required or created.
- [ ] The command's fit gate and mid-loop escape hatch trigger on uncertainty signals only (multiple viable approaches, fuzzy success criteria, architectural/contract choice, root-cause shift) — never on file count. The command text contains no numeric file threshold as a scope gate or exit condition; the surgical-vs-feature mode-selection heuristic (mirroring `/subagent-implementation` Step A) is agent choice, not a scope cap.
- [ ] On escape, the command stops, names the signal that fired, and prints a handoff to `/subagent-implementation` (noting its inline path may still apply) or `/atomic-plan`, retaining the scratchpad for the handoff.
- [ ] Iteration cap is 3; at cap without PASS, the user is asked (continue / escalate / stop) via `AskUserQuestion`.
- [ ] Each green iteration commits via the `atomic-git-discipline` skill; the final gate invokes the `atomic-verify` skill (orchestrator re-runs signals itself).
- [ ] Open `FOLLOWUPS.md` findings are surfaced at the end with the same four dispositions as `/subagent-implementation` (fix-now / defer / issue / drop).
- [ ] `make render` produces `commands/quick-fix.md` from the template; `make -C atomic bundle` regenerated in the same commit; the help-router verification loop reports zero `MISSING:` lines.
- [ ] `/quick-fix` appears in the `/atomic-help` topic tables + tour Stage 2, `CLAUDE.md` Workflow step 2, `README.md`, and `docs/reference/commands.md`.


## Approaches


| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | New command, thin orchestrator over existing primitives | Same scratchpad + `_templates` prompts + agents as `/subagent-implementation`, minus spec gate, worktree gate, finalize ceremony | low | symmetry drift between the two command files on shared concerns |
| B | "Lite" mode flag inside `/subagent-implementation` | One file, mode branch | low | bloats every invocation's prompt; muddies the spec gate; two flows in one contract |
| C | Do nothing — rely on the existing inline path | `/subagent-implementation` already proceeds inline for <30-min obvious work | zero | inline path still drags the full finalize ceremony; poor discoverability for the "just fix it" intent |


## Recommendation


A. The primitives already support spec-less runs (both `_templates` prompts treat `{SPEC_PATH}` as optional); what a quick fix needs removed is the gates and the finalize ritual, which live in the orchestrating command — so a second thin command is the surgical cut. B was rejected as prompt bloat, C as leaving the ceremony in place.


## Change tree


    templates/commands/
    └── quick-fix.md ............... A  (command source: fit gate, loop, escape hatch, finalize)
    commands/
    └── quick-fix.md ............... A  (rendered output — make render)
    templates/commands/
    └── atomic-help.md ............. M  (lifecycle topic row + tour Stage 2 + command count)
    commands/
    └── atomic-help.md ............. M  (rendered)
    CLAUDE.md ...................... M  (Workflow step 2: quick-fix as the no-plan implement path)
    README.md ...................... M  (commands table row)
    docs/reference/commands.md ..... M  (row)
    docs/reference/workflow.md ..... M  (implement-stage mention)
    atomic/internal/embedded/ ...... M  (bundle regen — same commit as source artifacts)


## Outline


    templates/commands/quick-fix.md
      frontmatter description — trigger surface: straightforward fix, known cause, skip planning
      Fit gate — hold/exit signal table (uncertainty-based; explicitly cohesion-bounded, no file counts)
      Phase 0 — optional atomic-investigator dispatch; skip when the task names exact files
      Code-intel — warm index → atomic code sync; cold → proceed degraded (no index build; speed is the point)
      Scratchpad — same trio, seeded via atomic template brief|state|followups; loop base SHA recorded in STATE.md
      Loop — implementer (surgical ≤2 mechanical files, feature otherwise) → reviewer → triage → commit per green; cap 3
      Escape hatch — mid-loop exit conditions + handoff text naming /subagent-implementation and /atomic-plan
      Finalize — atomic-verify gate, FOLLOWUPS dispositions, scratchpad deletion, report; ship left to /commit

    templates/commands/atomic-help.md
      lifecycle topic row — /quick-fix one-liner
      tour Stage 2 — implement verbs gain quick-fix; command count bump

    CLAUDE.md
      Workflow step 2 — one-line clause: /quick-fix for obvious fixes, no spec

    README.md / docs/reference/commands.md / docs/reference/workflow.md
      command rows — one-line description each


## Flows


Flow: quick fix, clean pass

1. user runs `/quick-fix <task>`
2. orchestrator gauges fit against the hold/exit table; on exit signal, prints the handoff and stops before any dispatch
3. optional investigator maps the surface (skipped when files are named)
4. scratchpad trio written; `BRIEF.md` carries the inline brief (task, success criteria, scope)
5. implementer dispatched with `{SPEC_PATH}` = `"no spec — inline brief in BRIEF.md"`
6. reviewer verifies signals + brief compliance → `VERDICT: PASS`
7. orchestrator commits via atomic-git-discipline, runs atomic-verify
8. FOLLOWUPS surfaced for disposition; scratchpad deleted; report printed; user ships via `/commit`

Flow: escape hatch fires mid-loop

1. implementer reports `BLOCKED`/`NEEDS_CONTEXT`, or a reviewer round reveals an exit signal (approach fork, criteria dispute, contract choice, shifted root cause)
2. orchestrator stops the loop and names the signal
3. handoff printed: `/subagent-implementation <task>` (inline path may still apply) or `/atomic-plan <task>` if genuinely non-trivial
4. scratchpad retained — `BRIEF.md`/`STATE.md` carry the context into the next verb

Flow: iteration cap

1. 3 iterations without PASS
2. `AskUserQuestion`: continue / escalate to `/subagent-implementation` / stop


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Author command template, render, bundle | `templates/commands/quick-fix.md`, `commands/quick-fix.md`, `atomic/internal/embedded/` | atomic-implementer (mode: feature) | ~3 + bundle | `make render` exits 0 (orphan rule satisfied); `make -C atomic bundle && git diff --exit-code` after regen |
| 2 | Wire discovery surfaces, re-render, re-bundle | `templates/commands/atomic-help.md`, `commands/atomic-help.md`, `CLAUDE.md`, `README.md`, `docs/reference/commands.md`, `docs/reference/workflow.md`, `atomic/internal/embedded/` | atomic-implementer (mode: feature) | ~7 + bundle | help-router `MISSING:` loop prints nothing; grep for `/quick-fix` hits every wired surface |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Symmetry drift: a later change to `_templates` prompts or scratchpad shape updates `/subagent-implementation` but not `/quick-fix` | med | Both command files carry a one-line cross-reference naming the other as a co-consumer of `_templates` and the scratchpad contract |
| Users reach for `/quick-fix` on non-trivial work | med | Fit gate up front + mid-loop escape hatch; handoff text names the right verb, and the reviewer's fresh-context pass is a natural tripwire |
| A future `_templates` edit makes `{SPEC_PATH}` mandatory, breaking the spec-less path | low | Success criterion pins the substitution value; both templates currently state "skip if file doesn't exist" |
| Escape-hatch judgment is fuzzy in practice (orchestrator grinds instead of exiting) | med | Exit signals enumerated concretely in the command text; `BLOCKED`/`NEEDS_CONTEXT` from the implementer force the stop unconditionally |


## Change log

### 2026-07-11 — Correction: file-threshold criterion scoped to gates

**What changed:** The "no numeric file threshold" success criterion now applies to scope gates and exit conditions only; the surgical-vs-feature mode-selection heuristic is explicitly exempt.

**Why:** As written, the criterion contradicted the spec's own Outline ("surgical ≤2 mechanical files"), which mirrors `/subagent-implementation` Step A's agent-choice heuristic. Mode selection is not a scope cap.


## Implementation log

### shipped — 2026-07-11

Built across 2 iterations of /subagent-implementation. Commits (chronological):

- `d9ca1ac` — spec correction: file-threshold criterion scoped to gates (currency-gate fix pre-iteration)
- `6922690` — CP-1 `templates/commands/quick-fix.md` authored + render + bundle
- `afe9630` — CP-2 discovery wiring: atomic-help row/tour/count, CLAUDE.md Workflow step 2, README, docs/reference/commands.md, docs/reference/workflow.md

**Out-of-scope work performed during this build:**

- `chore: gitignore .claude/.atomic-index/` (`d460136`) — appended by `atomic repo init` during loop setup; committed separately for provenance.

**Unforeseens — surprises that emerged during implementation:**

- Iter 2 reviewer caught a tour Stage 1 count double-bump (~23→~24 when actual count was 23; quick-fix.md was already in the ~23 baseline after CP-1). Verified and corrected pre-commit (F-2, closed iter 2).

**Deferred items still open:**

- none — F-1 (frontmatter description 516 chars vs sibling 267–344) dropped by user decision: single coherent line, richer trigger surface; symmetry not worth a polish pass.

**Squashed into a single commit on feat/quick-fix — 2026-07-11.** Per-iteration SHAs above are historical (unreachable after the branch squash).
