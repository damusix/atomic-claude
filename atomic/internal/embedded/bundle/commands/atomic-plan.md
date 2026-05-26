---
description: Write a design doc (concepts, business rules, approaches) and a checkpoint-table spec (contract) for non-trivial work; inline spec only for trivial. Gauges triviality; loops spec authoring with subagents. Human-facing artifact, Mermaid diagrams allowed.
---

Plan before coding. Output is a spec — a checkpoint-table contract, not prose. Lives in `docs/` because it's human-facing — `.claude/.scratchpad/` is for LLM working memory, not user-invoked plans.

`/atomic-plan` is one command with one internal phase boundary. Lightweight tasks get an inline spec. Non-trivial tasks get a full design + spec-authoring loop with subagents. The LLM gauges triviality; the user confirms when uncertain.

<workflow>

## Phases

### Classify triviality

Gauge the task against this tier table. **High confidence required for trivial** — any uncertain signal pushes to borderline.

| Tier | Signals | Flow |
|------|---------|------|
| **Trivial** | One cohesive slice; ≤3 files; no architectural choice; no new package/module; no cross-cutting concern; no public-contract change; one obvious approach; no business-logic shape to think through. | Inline spec only. No design doc. No subagent loop. May also offer: "small enough to skip planning — implement directly?" |
| **Borderline** | 4-8 files; one subsystem but unfamiliar code; small architectural or business-rule choice; partial unknowns; one viable approach with caveats. | **Ask the user.** Surface the signals + your lean; let them pick `full / inline / abort`. |
| **Non-trivial** *(default)* | Cross-system; new package/module; multiple viable approaches; touches existing contracts; phrased as "design X" / "rebuild Y" / "refactor Z"; >8 files; doesn't fit one cohesion slice; non-obvious business rules or feature shape to work out. | Full flow: Ground → Diverge → Design → optional pressure-test → Spec loop. |

**Borderline confirmation format** — one block, not a question series:

```
This looks borderline. Signals I'm weighing:
- <N> files across <areas>
- <architectural decision or unknown>
- <familiarity / scope notes>

Lean: <full | inline>.
Override? [full | inline | abort]
```

**Trivial confirmation format** — one line, silent confidence, no question mark:

```
Trivial — <one-line shape>. Writing spec inline, skipping loop.
```

If the user disagrees they'll say so; otherwise proceed. Default bias is toward the loop — under-planning a non-trivial feature costs more than over-planning a trivial one.

### Clarify intent

One consolidated block covering: goal, success criteria, non-goals, constraints (perf, deadline, deps), affected systems. If the request already answered these, skip. One round max; after that, write with stated assumptions and let the user correct.

### Ground (non-trivial only)

Before drafting, dispatch `atomic-investigator` (haiku, read-only) when checkpoints will touch unfamiliar code. Brief it with the surface area to map; expect a `file:line — what` table back. This grounds checkpoint paths in reality instead of guessing.

Skip Ground for trivial — the file count is small enough to read directly.

Skip Ground when the surface area is already in your context from the current session.

### Diverge (non-trivial only)

Brainstorm 3-5 approaches. Capture in a table:

```markdown
## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | <name>   | <1-2 line shape> | low/med/high | <main risk> |
| B | …        | …      | …    | …    |
| C | …        | …      | …    | …    |
```

Then a `## Recommendation` with the chosen approach and rationale referencing evidence (file:line, signals snapshot, prior decisions). Hedged recommendations are a signal — surface `/pressure-test` at handoff.

**Optional `atomic-strategist` dispatch** (opus, read-only): when the tradeoff is genuinely hard, multiple approaches survive scrutiny, or blast radius spans ≥2 subsystems. Strategist returns a recommendation with explicit confidence + hidden assumptions named. Don't dispatch for clear-cut calls — opus is expensive.

### Write design (non-trivial only)

Design is the **conceptual workspace** — the place to think through feature shape, business rules, user-facing behavior, philosophy, and approaches *without* committing to technical implementation. Always write `docs/design/<topic>.md` for non-trivial work; the thinking has to happen somewhere durable so the spec can derive from it (and so future readers can see *why* the contract took its shape).

Design captures things the spec deliberately doesn't:

- What the feature *is* in user / domain terms — not the endpoint, the *behavior*.
- Business rules and invariants — what must always be true, regardless of implementation.
- Approaches considered and rejected, with reasoning. Evidence-backed (file:line, signals, prior decisions) where the evidence exists; honest about gaps where it doesn't.
- Open philosophical / product questions that the spec shouldn't try to answer.
- Diagrams of conceptual relationships (entities, states, flows) — not call graphs.

The design doc persists by default. Whether it gets cited later is downstream — not a gate. A design doc that captured one feature's thinking still pays for itself by anchoring the spec authoring loop and giving future contributors the "why".

`<topic>` = short kebab-case (e.g. `oauth-refresh`, `user-search-perf`). No date prefix — `git log` carries that.

**Design structure:**

```markdown
# <title>

## Problem

<user-facing pain or motivation>

## Goals / Non-goals

- Goals: …
- Non-goals: …

## Approaches

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | … | … | … |
| B | … | … | … |

## Recommendation

<chosen option + why, with evidence>

## Open questions

- <q>
```

Include a Mermaid diagram (flowchart / ERD / sequence / state) under Problem or Recommendation when the work involves architecture, conceptual relationships, or state transitions. One-sentence caption above so non-rendering readers still get it.

### Write spec

Always produce `docs/spec/<topic>.md`. For trivial, write it inline. For non-trivial, **enter the spec loop** below.

**Spec structure:**

```markdown
# <title>

## Goal

<1-2 sentences. What done looks like.>

## Non-goals

- <thing explicitly out of scope>

## Success criteria

- [ ] <verifiable check>
- [ ] <verifiable check>

## Approaches *(non-trivial; copy from design if a design doc exists)*

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | … | … | … | … |

## Recommendation

<chosen approach + why, with evidence>

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | <action>   | <paths>     | atomic-builder | ~4 | <test or signal> |
| 2 | <action>   | <paths>     | atomic-surgeon | 1-2 | <test or signal> |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| <r>  | high/med/low | <plan> |

## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->
```

The `## Change log` section ships **empty** on creation. Drafting and refinement turns before approval are not amendments — the spec is being born. The first real entry happens later, when an *approved* spec is changed. See "Spec files are append-mostly" in `CLAUDE.md`.

### Spec loop (non-trivial only)

Mirrors `/subagent-implementation`'s implement→review pattern, but for spec authoring instead of code.

```
Iter 1: atomic-builder reads design (if exists) + Approaches → drafts spec.
Iter 2: atomic-reviewer (spec-mode) reads design + spec → reports gaps:
        - Design intent not covered in spec?
        - Success criteria untestable / missing / vague?
        - Checkpoints not cohesive slices?
        - Spec over-prescribes implementation details?
        - Contradictions between design and spec?
        - Approaches table or Recommendation missing evidence?
Iter 3+: builder applies feedback; reviewer re-checks.
Terminate on VERDICT: PASS or hard-cap (5 iters; configurable via memory).
```

Use `.claude/.scratchpad/<YYYY-MM-DD>-spec-<topic>/` with `BRIEF.md` + `STATE.md` + `FOLLOWUPS.md`. Deleted on PASS. Reuses the same scratchpad shape as `/subagent-implementation` so contributors don't need a second mental model.

**`atomic-reviewer` spec-mode brief.** When dispatched from this command, brief the reviewer for *alignment review* — not diff review. It is checking the spec against the design (and against repo evidence), not against code. Spec-mode verdict criteria:

- Design intent fully covered.
- Success criteria verifiable and falsifiable.
- Checkpoints are cohesive slices, not line-by-line code.
- Voice is evidence-backed, not prescriptive.
- No contradictions between design and spec.
- Risks table honestly enumerates likelihood + mitigation.

### Handoff

Print the spec path. Summarize in 3-5 lines.

**Pressure-test surface** — conditional, not always. Surface `/pressure-test` only when any of:

- `## Open questions` is non-empty.
- Recommendation row is hedged ("probably A, but B if X").
- User's clarify answers contained hedges ("maybe", "not sure", "could go either way").
- Cross-system blast radius (≥2 subsystems or ≥3 affected areas).
- `atomic-strategist` was dispatched (the question was hard enough to need opus — hard enough to pressure-test).

When triggered, print the reason and the copyable command:

```
This spec has <open questions / hedged recommendation / cross-system scope / strategist-reviewed>.
Pressure-test before implementing:

    /pressure-test @docs/spec/<topic>.md

Or proceed:

    /subagent-implementation @docs/spec/<topic>.md
```

When not triggered, just print the proceed line:

```
    /subagent-implementation @docs/spec/<topic>.md
```

Both lines are copy-paste runnable.

</workflow>

## Amending an existing spec

If `docs/spec/<topic>.md` already exists *and was previously approved* (committed, or the user has moved past the initial planning round), do not silently overwrite. Apply the append-mostly rule from `CLAUDE.md`: edit the body as needed AND add a dated entry to `## Change log` capturing what changed and why. If the file lacks a `## Change log` section, add one before amending.

**Refinement vs. amendment.** Revisions during the initial planning conversation (before the user has said "proceed" the first time) are *drafting*, not amendments — keep iterating, leave `## Change log` empty. Once the spec is approved and implementation has started (or the file is committed), every later edit is an amendment and must log.

## Relationship to scratchpad

`docs/spec/` and `docs/design/` are durable, curated, human-facing.
`.claude/.scratchpad/<date>-<desc>/` is ephemeral working memory for `/subagent-implementation` — it points at the spec via `BRIEF.md` (current iteration scope + reviewer feedback) and logs progress in `STATE.md`. The spec stays canonical; scratchpad is throwaway.

The spec loop also uses `.claude/.scratchpad/<date>-spec-<topic>/` during authoring. Same shape, deleted on PASS.

## Spec voice

**Evidence-backed, not prescriptive.** The spec captures what done looks like and where the evidence came from. It does not dictate implementation.

A spec SHOULD say:

- What done looks like — success criteria, verifiable and falsifiable.
- What we're not doing — non-goals, explicit scope boundary.
- The slices — checkpoints, cohesion units the implementer can dispatch as one builder pass.
- The evidence behind decisions — Approaches + Recommendation with file:line or signals-derived facts.

Implementation details belong in code, not specs:

- Exact function signatures — the implementer picks these.
- Specific variable names — the implementer picks these.
- Specific algorithms ("use `Array.reduce`") — the implementer decides how.
- Step-by-step pseudocode — write success criteria instead.

The implementer (builder / surgeon) is allowed to correct course on anything that doesn't break success criteria. **Success criteria are the contract; everything else is a sketch the implementer can adapt.** Deviations that would break a success criterion require spec amendment, not silent drift.

If the spec writer wants to dictate the code, they should have written the code. Over-prescription is the most common spec failure mode — the writer hasn't run the code, hits a real constraint at implementation time, and the spec becomes a liability (either ignored, or amended for every mechanical surprise).

## Checkpoint sizing

**A checkpoint is one cohesive slice of work that ends in a green test run.** Equivalent to one `/subagent-implementation` iteration — one builder dispatch, one reviewer pass, one commit if PASS.

- **Not a git commit.** Commits are the side-effect of green checkpoints. The checkpoint is the unit of *work*, not history.
- **Not a single file.** Cohesion-bounded (axiom 1): a NestJS endpoint = controller + service + DTO + entity + module wiring + tests = one checkpoint. Splitting cohesive work into per-file checkpoints kills the loop with overhead.
- **Not a whole feature.** "Build the auth system" is not a checkpoint — it's a spec. If the slice can't be implemented + reviewed in one builder pass, split it.

The `Agent` column hints at dispatch:

- `atomic-builder` — multi-file cohesive slice (the default).
- `atomic-surgeon` — 1-2 file surgical edit (typo, rename, single-function rewrite).

`Est. files` is a sanity check — if a row shows `~15 files`, it's probably two checkpoints.

## Voice — spec/design, not prose

**Spec / design voice is table-first, terse, brevity-dominant.** These files are re-read often by humans and agents and live or die by token cost. Use tables, Mermaid diagrams, and short bullet lists as the primary form. Prose only where a contract genuinely needs sentences (Goal, Problem statement, Rationale, Recommendation). When prose is required, keep it tight: active voice, no marketing jargon, no em dashes, no throat-clearing, no AI-tell phrases.

**Do NOT invoke the `atomic-prose` skill here.** That skill is for enduring narrative docs (README, guides) where some narrative carries value; specs and design pay token cost on every read and stay terse.

<constraints>

## Rules

- Spec is a table, not an essay. If a section runs >5 bullets, split into checkpoints.
- Every checkpoint has a `Verifies` column. If you can't write what proves the step done, the step is too vague.
- No speculative scope ("we might also want to..."). Out-of-scope items go in Non-goals.
- No code in this command. Plan only.
- One round of clarifying questions max. After that, write with stated assumptions and let the user correct.
- Skip planning entirely for *trivial* work — write the spec inline (or offer to implement directly if the user prefers).
- Promoted files are curated: no TODOs, no "tbd". Unresolved items go in `## Open questions` and get flagged in the handoff summary.

</constraints>
