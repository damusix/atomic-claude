# /atomic-plan

## Goal

`/atomic-plan` produces a design and an implementation spec for non-trivial work, or an inline spec for trivial work. The plan defines the result to prove, not only the checks for its intermediate slices.

## Non-goals

- Implementing the plan or replacing `/implement`.
- Automatically running challenge methods without the user's choice.
- Generic brainstorming without an intended implementation.

## Success criteria

- [ ] Trivial work has one obvious approach, at most three files, and no new module or change to architecture, business rules, or a public contract. The planner states why and writes a spec inline.
- [ ] Borderline work asks the user to choose full, inline, or abort unless Autopilot can decide within authorized scope. Non-trivial work receives a design and a spec.
- [ ] For unfamiliar code not already mapped in the session, a read-only scout on an inexpensive model returns a `file:line` map. A strategist is optional for hard tradeoffs or cross-subsystem impact.
- [ ] The planner uses `atomic template design-doc` and `atomic template spec` for the applicable artifacts and stops if a required template fails.
- [ ] The design shows the observable end result, business rules, affected interfaces and data shapes, tradeoffs, and open decisions. Use short native-syntax examples for consumption and language-neutral pseudocode only when a diagram or table does not explain the logic. Omit implementation bodies.
- [ ] Each spec states the observable end result, non-goals, and end-to-end proof separately from checkpoint checks. The proof names setup, action, expected observation, and required access or dependencies. An unavailable proof has an explicit limitation and owner.
- [ ] Each checkpoint is a cohesive builder dispatch with a named verification. The checkpoint table follows the spec template.
- [ ] The planner chooses at most one evidence or challenge method using `atomic-planning`. The user decides whether to run a challenge unless Autopilot is acting within authorized scope.
- [ ] Automated challenge runs on request or under Autopilot. A separate respondent answers evidence-backed objections. The planner leaves decisions outside authorized scope to the user.
- [ ] The planner drafts and revises the full spec. An independent `atomic-reviewer` checks the design, template, evidence, end-result contract, and checkpoints. Stop at PASS or the iteration cap and report unresolved items.
- [ ] Keep the full spec at `SPEC.md` in the task scratchpad. Do not commit it or add a change log. Link to design flows instead of copying them. Keep approved decisions in the design and leave historical tracked specs in place.
- [ ] The handoff names the spec. It offers `/implement` only when no blocking uncertainty remains; otherwise it recommends one method.

## Approach

Keep `/atomic-plan` as the planning entry point. The `atomic-planning` skill owns method selection and on-demand challenge references; the direct commands select their respective methods.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Define the end-result contract and preserve the full spec review gate | `context/commands/atomic-plan.md` | The command distinguishes end-to-end proof from checkpoint checks and retains the review loop. |
| 2 | Consolidate methods and keep direct commands | `context/skills/atomic-planning/`, `context/commands/` | Each command loads its method; automated challenge runs only on request or under Autopilot. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Intermediate checks pass while the outcome is not demonstrated | medium | Require separate end-to-end proof with observable expected results. |
| Automated agents settle a human decision | medium | Escalate user-facing decisions and blocking disputes to the user. |
| Challenge substitutes for spec review | low | Retain the full spec-mode review gate independently of challenge selection. |

## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->

### 2026-11-23: Keep the spec only while the task needs it

**What changed:** Scratchpad specs include end-to-end proof and point to design flows. Discard only the task-generated spec after acceptance.

**Why:** The design retains decisions; the spec is a working contract.

### 2026-11-23: Show the design's end result

**What changed:** The design template adds proposed behavior, business rules, and design shapes. Consumer examples use native syntax; internal logic uses diagrams, tables, or language-neutral pseudocode.

**Why:** Engineers need to approve what the design does without reading production code.

### 2026-11-22: Separate spec writing from its review

**What changed:** The planner writes the spec. The reviewer stays independent. Autopilot owns routine route and challenge choices within authorized scope.

**Why:** The implementer is for code checkpoints. Unattended planning should not stop for decisions it can make.

### 2026-05-23 — Conform checkpoints table to canonical 4-column header

**Correction:** Checkpoints table previously had 6 columns (`# | Checkpoint | Files/areas | Agent | Est. files | Verifies`); `atomic validate spec` rule S5 requires the exact 4-column header `| # | Checkpoint | Files/areas | Verifies |`. CI run #102 failed on this. Agent + est-files info folded into the Checkpoint cell as parenthetical (e.g. `(atomic-surgeon, ~1 file)`); no contract loss.

**Why:** Spec lint gates merge.

### 2026-11-22: Use an inexpensive scout


**What changed:** Use any inexpensive model for read-only scouting and omit `atomic code` guidance.

**Why:** The planner needs a source map, and models were not using the suggested commands.

**Superseded:** The planner named `atomic-investigator` and suggested `atomic code explore`.

### 2026-11-22: Consolidate challenge methods and define end-result proof

**What changed:** The planning skill owns on-demand challenge methods. The planner recommends one method for the uncertainty, permits automated challenge only with user authorization, and requires an end-result contract distinct from checkpoint checks.

**Why:** Plans need a demonstrable outcome and a focused challenge without making automated debate or multiple methods the default.

**Superseded:** The planner's conditional offer of both pressure-test and challenge-swarm.
