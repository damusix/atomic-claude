# Quick fixes


## Goal


Use [`/implement`](implement.md) for a straightforward fix with a known cause. One checkpoint covers the fix, independent review, and verification. The bundle exposes no separate quick-fix command.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|---|---|---|---|---|
| 1 | Remove the separate command and route known-cause fixes to implementation | `context/commands/`, help and reference docs | atomic-implementer (mode: feature) | ~8 | The separate command is absent; help and examples point to `/implement` |


## Change log

### 2026-11-22: Retire the separate command


**What changed:** Known-cause fixes use `/implement` without a spec or worktree.

**Why:** The same checkpoint review gate covers both fixes and planned work.

**Removed:** `/quick-fix` and its command-specific setup.


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
