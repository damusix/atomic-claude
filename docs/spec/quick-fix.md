# /quick-fix command


## Goal


A `/quick-fix <task>` slash command that runs the subagent implement→review loop without the planning phase — for fixes with one obvious approach and clear success criteria, regardless of how many files the change spreads across.


## Non-goals


- No new agents, skills, or prompt templates. Reuses `atomic-implementer`, `atomic-reviewer`, `atomic-investigator`, and the shared `atomic prompt implementer` / `atomic prompt reviewer` briefs unchanged.
- No spec or design authoring. `/quick-fix` never writes `docs/spec/` or `docs/design/`.
- No file-count cap anywhere in the command. Scope is cohesion-bounded (axiom 1) — a mechanical fix threading one param through DTO → validator → controller → use case → repo → client is one slice, however many files.
- No worktree gate.
- No finalize ceremony: no implementation log, no `/documentation` dispatch, no signals refresh (the ship verbs' `signals-gate` covers ad-hoc commits).
- Not for unknown root causes — that is `/subagent-diagnose` (or the `atomic-debug` skill) territory. `/quick-fix` assumes the cause is known and the fix shape is obvious.
- `/autopilot` unchanged — it continues to route through `/subagent-implementation` only.


## Success criteria


- [ ] `/quick-fix <task>` runs the implement→review loop with the same scratchpad trio (`BRIEF.md`, `STATE.md`, `FOLLOWUPS.md`) and the same `atomic prompt implementer` / `atomic prompt reviewer` briefs as `/subagent-implementation`, with `{SPEC_PATH}` substituted as `"no spec — inline brief in BRIEF.md"`. No spec file is required or created.
- [ ] The hand-off table (shared `handoff` partial) fires at entry and mid-loop on uncertainty signals only (multiple viable approaches, open success criteria, architectural/contract choice, root-cause shift) — never on file count. No numeric file threshold gates scope or exits anywhere in the composed command; the surgical-vs-feature mode choice in the shared `implement-loop` partial is agent selection, not a scope cap.
- [ ] On a hand-off, the command stops, names the signal that fired, prints the handoff verb, and retains the scratchpad.
- [ ] Two consecutive `CHANGES_REQUESTED` rounds on the same blocking signal surface the stuck choice (continue / `/pressure-test` / `atomic-strategist`), the same check the other implementation verbs run. There is no iteration cap.
- [ ] Each green iteration commits via the `atomic-git-discipline` skill; the final gate invokes the `atomic-verify` skill (orchestrator re-runs signals itself).
- [ ] Open `FOLLOWUPS.md` findings are surfaced at the end with the same four dispositions as `/subagent-implementation` (fix-now / defer / issue / drop).
- [ ] `make -C atomic bundle` resolves the command's `handoff`, `implement-loop`, and `loop-finalize` directives; the help-router verification loop reports zero `MISSING:` lines.
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


    context/commands/
    ├── quick-fix.md ............... A  (policy table + Surface; composes handoff, implement-loop, loop-finalize)
    └── atomic-help.md ............. M  (lifecycle topic row + tour Stage 2 + command count)
    context/CLAUDE.md .............. M  (Workflow step 2: quick-fix as the no-plan implement path)
    README.md ...................... M  (commands table row)
    docs/reference/commands.md ..... M  (row)
    docs/reference/workflow.md ..... M  (implement-stage mention)
    atomic/internal/embedded/ ...... M  (bundle regen — same commit as source artifacts)


## Outline


    context/commands/quick-fix.md
      frontmatter description — trigger surface: straightforward fix, known cause, skip planning
      Policy table — Writer atomic-implementer; Entry hand-off table; Worktree none; Stuck ask; Non-blockers ledger; Finalize verify, audit, follow-ups, report; Scratchpad purpose fix
      handoff partial — routing table, checked at entry and mid-loop
      Surface — optional atomic-investigator dispatch; skip when the task names exact files
      implement-loop partial — index (sync or build), scratchpad trio, implementer (surgical or feature) → reviewer → triage → commit per green; stuck check
      loop-finalize partial — verify, audit, follow-ups, report; docs, log, and signals skipped by policy; ship left to /commit

    context/commands/atomic-help.md
      lifecycle topic row — /quick-fix one-liner
      tour Stage 2 — implement verbs gain quick-fix; command count bump

    CLAUDE.md
      Workflow step 2 — one-line clause: /quick-fix for obvious fixes, no spec

    README.md / docs/reference/commands.md / docs/reference/workflow.md
      command rows — one-line description each


## Flows


Flow: quick fix, clean pass

1. user runs `/quick-fix <task>`
2. orchestrator checks the hand-off table; on a match, prints the handoff and stops before any dispatch
3. optional investigator maps the surface (skipped when files are named)
4. scratchpad trio written; `BRIEF.md` carries the inline brief (task, success criteria, scope)
5. implementer dispatched with `{SPEC_PATH}` = `"no spec — inline brief in BRIEF.md"`
6. reviewer verifies signals + brief compliance → `VERDICT: PASS`
7. orchestrator commits via atomic-git-discipline, runs atomic-verify, dispatches atomic-auditor once
8. FOLLOWUPS surfaced for disposition; bundle retained in place; report printed; user ships via `/commit`

Flow: hand-off fires mid-loop

1. implementer reports `BLOCKED`/`NEEDS_CONTEXT`, or a reviewer round reveals a hand-off signal (approach fork, criteria dispute, contract choice, shifted root cause)
2. orchestrator stops the loop and names the signal
3. handoff printed: `/subagent-diagnose <task>` or `/atomic-plan <task>`, or the implementer's report surfaced to the user
4. scratchpad retained — `BRIEF.md`/`STATE.md` carry the context into the next verb

Flow: stuck

1. two consecutive `CHANGES_REQUESTED` rounds on the same blocking signal
2. `AskUserQuestion`: continue / `/pressure-test` / dispatch `atomic-strategist`


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Author the command and bundle | `context/commands/quick-fix.md` | atomic-implementer (mode: feature) | 1 | `make -C atomic bundle` exits 0 with every partial directive resolved |
| 2 | Wire discovery surfaces | `context/commands/atomic-help.md`, `context/CLAUDE.md`, `README.md`, `docs/reference/commands.md`, `docs/reference/workflow.md` | atomic-implementer (mode: feature) | ~5 | help-router `MISSING:` loop prints nothing; grep for `/quick-fix` hits every wired surface |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Symmetry drift between `/quick-fix` and `/subagent-implementation` | low | Both compose the same `implement-loop` and `loop-finalize` partials; a shape change reaches both at build time |
| Users reach for `/quick-fix` on non-trivial work | med | Hand-off table at entry and mid-loop names the right verb, and the reviewer's fresh-context pass is a natural tripwire |
| A future `_templates` edit makes `{SPEC_PATH}` mandatory, breaking the spec-less path | low | Success criterion pins the substitution value; both templates currently state "skip if file doesn't exist" |
| Hand-off judgment is fuzzy in practice (orchestrator grinds instead of exiting) | med | Signals enumerated concretely in the shared `handoff` table; `BLOCKED`/`NEEDS_CONTEXT` from the implementer force the stop unconditionally |


## Change log

### 2026-09-06 — Compose from shared loop partials; same-signal stuck check replaces the cap

**What changed:** The command is a policy table plus a Surface section, composed with the `handoff`, `implement-loop`, and `loop-finalize` partials. The fit gate and escape hatch are the shared hand-off table. Stuck handling is the same-signal check the other implementation verbs run. A cold code-intel index is built, not degraded around. The Outline, Flows, and Risks describe the composed command.

**Why:** `docs/design/implement-loop-consolidation.md`: the four implementation commands restated one loop; the cap, the degrade, and the scratchpad deletion (already removed from this body on 2026-08-20 but still present in the command text) differed from the siblings without a policy reason.

**Superseded:** a command-local fit gate and escape hatch; the 3-iteration cap with its continue / escalate / stop question; cold index → proceed degraded; the co-consumer cross-reference as the drift mitigation.

### 2026-08-10 — Correction: prompt source moved to the binary

**What changed:** Non-goals and the first Success criterion cited `commands/_templates/implementer-prompt.md` / `reviewer-prompt.md`. Those files were migrated into the `coldprompt` package and are now served by `atomic prompt implementer` / `atomic prompt reviewer` — both references updated to match.

**Why:** The prompt-templates-to-binary migration (see `docs/spec/document-templates.md` change log) removed `commands/_templates/` entirely; this spec's body still cited the old path.

**Superseded:** the file-path references to `commands/_templates/implementer-prompt.md` / `reviewer-prompt.md`.

### 2026-08-20 — Drop scratchpad deletion at finalize

**What changed:** The Outline's Finalize step and the clean-pass Flow no longer delete the scratchpad bundle. `/quick-fix` already retained its bundle in practice; this removes the stale "scratchpad deletion" / "scratchpad deleted" language so the body agrees with that behavior.

**Why:** `docs/spec/serve-plans-page.md` establishes that no command retires a bundle at close-out — retirement is `atomic scratchpad archive`, run by `/git-cleanup` or a ship-verb worktree cleanup. `/quick-fix` needed no behavior change, only the stale wording removed.

**Superseded:** "Finalize — atomic-verify gate, FOLLOWUPS dispositions, scratchpad deletion, report"; "FOLLOWUPS surfaced for disposition; scratchpad deleted; report printed".

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
