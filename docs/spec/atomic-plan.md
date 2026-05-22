# /atomic-plan

## Goal

`/atomic-plan` produces planning artifacts for an upcoming feature: a design doc (concepts, business rules, approaches) and a checkpoint-table spec (contract). Triviality is LLM-gauged. Non-trivial work loops the spec authoring via subagents until the reviewer signs off; trivial work writes the spec inline.

## Non-goals

- Implementing code. `/atomic-plan` writes plans, never modifies source files outside `docs/design/` and `docs/spec/`.
- Replacing `/subagent-implementation`. The spec produced here is the input to that command, not a substitute.
- Auto-firing. `/atomic-plan` is explicit-only (slash command), never triggered by natural language.
- Generic brainstorming. The command targets work that will be implemented; it is not a free-form ideation tool.

## Success criteria

- [ ] When invoked on trivial work, `/atomic-plan` writes `docs/spec/<topic>.md` inline (no design doc, no subagent loop) and prints the handoff line in one user turn after clarify.
- [ ] When invoked on non-trivial work, `/atomic-plan` writes both `docs/design/<topic>.md` and `docs/spec/<topic>.md`, and the spec passed through at least one `atomic-reviewer` spec-mode pass.
- [ ] Borderline cases prompt the user with `full | inline | abort` before proceeding — never silently picks.
- [ ] Trivial confirmation is a single line; no question mark; the user can override by replying within the same turn.
- [ ] Every checkpoint in the produced spec has `Files/areas`, `Agent`, `Est. files`, and `Verifies` columns populated.
- [ ] Every checkpoint maps to one cohesive `/subagent-implementation` iteration. No row that obviously needs splitting (>~10 files) escapes the reviewer.
- [ ] The spec ships with `## Change log` empty on first write. Drafting/refinement turns do not log.
- [ ] Pressure-test prompt surfaces only when one of the conditions in the "Pressure-test surface" section holds; otherwise only `/subagent-implementation` is offered.
- [ ] The `atomic-investigator` dispatch is skipped when the surface area is already known from the current session, even on non-trivial work.
- [ ] The `atomic-strategist` dispatch is optional — only used when the tradeoff is genuinely hard or blast radius spans ≥2 subsystems.
- [ ] The spec loop terminates on `VERDICT: PASS` or the configurable iteration cap (default 5).

## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | Single explicit-only command, internal phase boundary | One `/atomic-plan` verb. Inside: classify → ground → diverge → write → loop → handoff. LLM gauges triviality; user confirms when borderline. | low | command does double duty (read+write artifact AND orchestrator); may grow large |
| B | Two commands: `/atomic-plan` writes design; `/spec-from-design <path>` runs the loop | Cleaner separation; each verb has one responsibility. | medium | user has to chain two commands; conceptual overhead |
| C | Always-loop, no triviality gauge | Every plan runs the full subagent loop. | high | overkill for trivial work, opus + sonnet token spend, latency |
| D | Always-inline, no loop | Old behavior; one-shot write, user reviews manually. | low | non-trivial specs ship under-baked; no alignment check against design |

## Recommendation

**Approach A.** Single command, internal phases, LLM-gauged triviality with confidence threshold. Rationale:

- Matches the existing cohesion of the system — users learn one verb per workflow step (`/atomic-plan` → `/subagent-implementation` → ship).
- Triviality gauge means we don't pay subagent cost on trivial work (one-endpoint adds, single-column migrations).
- The internal phase boundary is documented in the command file; readers can follow it without learning a second command.
- Cost: the command file grows. Mitigated by the spec/design voice rules (table-first, terse). Acceptable trade.

Evidence: existing `/subagent-implementation` and `/subagent-diagnose` already use the same orchestrator-in-one-command pattern. Cohesion with that pattern outweighs the "command does too much" concern.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Redesign `commands/atomic-plan.md` with phase-based flow, triviality tiers, spec loop description | `commands/atomic-plan.md` | atomic-surgeon | 1 | command reads top-to-bottom with the new flow; no contradictions with `CLAUDE.md` |
| 2 | Update workflow blurbs and command-table descriptions across `CLAUDE.md`, `README.md`, `docs/reference/commands.md` | `CLAUDE.md`, `README.md`, `docs/reference/commands.md` | atomic-surgeon | 3 | grep `atomic-plan` shows consistent descriptions referencing triviality + spec loop |
| 3 | Extend `atomic-reviewer` agent with spec-mode branch | `agents/atomic-reviewer.md` | atomic-surgeon | 1 | agent file has Modes table, separate workflow + output sections, brief-driven mode selection |
| 4 | Write `docs/spec/atomic-plan.md` capturing the canonical contract | `docs/spec/atomic-plan.md` | atomic-builder | 1 | spec exists, follows the spec/design voice, has `## Change log` empty |
| 5 | Regenerate embedded bundle | `atomic/internal/embedded/bundle/**`, `atomic/internal/embedded/manifest.go` | atomic-surgeon | ~3 | `make -C atomic bundle && git diff --exit-code` clean |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Triviality misclassification — LLM gauges "trivial" when scope is actually borderline, user gets under-planned spec | medium | Confidence rule: trivial requires high confidence on every signal; any uncertain signal pushes to borderline. Borderline always asks. |
| Spec loop never terminates (reviewer keeps finding nits) | low | Hard cap at 5 iterations (configurable via user memory). At cap, finalize with FOLLOWUPS and surface to user. |
| `atomic-strategist` over-dispatched, opus token spend balloons | medium | Description explicitly limits dispatch to hard tradeoffs / cross-subsystem scope. Reviewer doesn't gate strategist usage; orchestrator decides. |
| `atomic-reviewer` spec-mode confused with code-mode (wrong workflow runs) | low | Mode declared in brief from orchestrator; agent defaults to code-mode if unspecified. Mode-mismatched output is obvious (signals block in spec-mode = misroute). |
| Design doc treated as optional when it shouldn't be | low | Documented in command file: non-trivial work always writes design. Reviewer spec-mode pass checks design coverage. |
| Over-prescription in spec slips past reviewer | medium | Reviewer spec-mode workflow has dedicated voice pass with examples of forbidden patterns (function signatures, variable names, pseudocode). |
| Pressure-test handoff fires too often, becomes noise | low | Conditional surface with explicit triggers (open questions, hedged recommendation, cross-system scope, strategist dispatched). Documented in command. |

## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->
