---
description: Write a checkpoint-table plan to docs/design/ (brainstorm/rationale) or docs/spec/ (implementation contract). Human-facing artifact, Mermaid diagrams allowed.
---

Plan before coding. Output is a checkpoint table, not prose. Lives in `docs/` because it's human-facing — `.claude/.scratchpad/` is for LLM working memory, not user-invoked plans.

## Four steps

### 1. Classify

Pick one — ask the user if ambiguous:

| Type | Path | Audience | Use when |
|------|------|----------|----------|
| **design** | `docs/design/<topic>.md` | Humans deciding *what* to build | Brainstorming, exploring alternatives, weighing trade-offs |
| **spec** | `docs/spec/<topic>.md` | Future implementers (human or agent) | Implementation contract for an agreed feature |

If the work starts as design and graduates to spec, write two files — design first, spec second. Don't try to make one file serve both audiences.

`<topic>` = short kebab-case (e.g. `oauth-refresh`, `user-search-perf`). No date prefix — `git log` carries that.

### 2. Clarify intent (one round, consolidated)

Ask one consolidated block covering: goal, success criteria, non-goals, constraints (perf, deadline, deps), affected systems. If the answers are in the request, skip and proceed.

### 3. Write the plan

Create `docs/design/` or `docs/spec/` if missing. Then write the file.

**Spec / design voice — table-first, terse, brevity-dominant.** These files are re-read often by humans and agents and live or die by token cost. Use tables, Mermaid diagrams, and short bullet lists as the primary form. Prose only where a contract genuinely needs sentences (Goal, Problem statement, Rationale, Recommendation). When prose is required, keep it tight: active voice, no marketing jargon, no em dashes, no throat-clearing, no AI-tell phrases. **Do NOT invoke the `atomic-prose` skill here** — that skill is for enduring narrative docs (README, guides) where some narrative carries value; specs and design pay token cost on every read and stay terse.

**Design** structure:

```markdown
# <title>

## Problem

<the user-facing pain or motivation>

## Goals / Non-goals

- Goals: …
- Non-goals: …

## Alternatives considered

| Option | Pros | Cons |
|--------|------|------|
| A | … | … |
| B | … | … |

## Recommendation

<chosen option + why>

## Open questions

- <q>
```

**Spec** structure:

```markdown
# <title>

## Goal

<1-2 sentences. What done looks like.>

## Non-goals

- <thing explicitly out of scope>

## Success criteria

- [ ] <verifiable check>
- [ ] <verifiable check>

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | <action> | <paths> | <test or signal> |
| 2 | <action> | <paths> | <test or signal> |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| <r>  | high/med/low | <plan> |

## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->
```


The `## Change log` section ships **empty** on creation. While the user is still drafting and refining the spec (revise loops in step 4 before they say "proceed"), do not add log entries — those are not amendments, they are the spec being born. The first real entry happens later, when an *approved* spec is changed. See "Spec files are append-mostly" in `CLAUDE.md` for amend / change / remove / correct / rename rules.

For shape/architecture work include a Mermaid diagram (flowchart / ERD / sequence / state) under the goal. Caption it with one sentence above so non-rendering readers (grep, raw view) still get it.

### 4. Confirm + handoff

Print the file path. Summarize in 3-5 lines. Ask: "Proceed, or revise?"

**Amending an existing spec.** If `docs/spec/<topic>.md` already exists *and was previously approved* (committed, or the user has moved past the initial planning round), do not silently overwrite. Apply the append-mostly rule from `CLAUDE.md`: edit the body as needed AND add a dated entry to `## Change log` capturing what changed and why. If the file lacks a `## Change log` section (legacy), add one before amending.

**Refinement vs. amendment.** Revisions during the initial planning conversation (before the user has said "proceed" the first time) are *drafting*, not amendments — keep iterating on the body, leave `## Change log` empty. Once the spec is approved and implementation has started (or the file is committed), every later edit is an amendment and must log.

On revise → edit the file, re-summarize. On proceed → done. Implementation happens via `/subagent-implementation` which can read the spec as its source of truth (it will still maintain its own working copy in `.claude/.scratchpad/`).

## Relationship to scratchpad

`docs/spec/` and `docs/design/` are durable, curated, human-facing.
`.claude/.scratchpad/<date>-<desc>/` is ephemeral working memory for `/subagent-implementation` — it points at the spec via `BRIEF.md` (current iteration scope + reviewer feedback) and logs progress in `STATE.md`. The spec stays canonical; scratchpad is throwaway.

## Rules

- Plan is a table, not an essay. If a section runs >5 bullets, split into checkpoints.
- Every checkpoint has a `Verifies` column. If you can't write what proves the step done, the step is too vague.
- No speculative scope ("we might also want to..."). Out-of-scope items go in Non-goals.
- No code in this command. Plan only.
- One round of clarifying questions max. After that, write the plan with stated assumptions and let the user correct.
- Skip planning entirely if the task is <30 min of obvious work — tell the user and proceed to implement directly.
- Promoted files are curated: no TODOs, no open questions, no "tbd". If unresolved items exist, leave them in `## Open questions` and flag them in the summary.
