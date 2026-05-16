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
```

For shape/architecture work include a Mermaid diagram (flowchart / ERD / sequence / state) under the goal. Caption it with one sentence above so non-rendering readers (grep, raw view) still get it.

### 4. Confirm + handoff

Print the file path. Summarize in 3-5 lines. Ask: "Proceed, or revise?"

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
