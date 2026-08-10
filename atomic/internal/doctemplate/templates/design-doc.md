<!--
Template for docs/design/<topic>.md — emitted by `atomic template design-doc`.
The conceptual workspace written by /atomic-plan
for non-trivial work. Copy this body, fill every <angle-bracket> placeholder, and delete
the guidance comments (including this one) as you fill. Do not improvise, reorder, or
rename sections; add sections only when the design genuinely needs them.
<topic> = short kebab-case (e.g. oauth-refresh). No date prefix — git log carries that.
-->
# <title>


## Problem


<user-facing pain or motivation>

<!-- Diagrams: use as many Mermaid blocks (flowchart / ERD / sequence / state) as the
     complexity warrants — here, under Approaches, or under Recommendation, each placed
     next to what it explains. No quota in either direction: none for a simple change,
     several for a multi-flow or multi-entity design. One-sentence caption above each
     block so non-rendering readers still get it.
     Pseudocode: a fenced pseudocode block is welcome wherever it is the clearest
     statement of logic — an algorithm, a decision rule, a matching order.
     Language-agnostic preferred (no syntax details to parse); a real language
     (TypeScript, SQL) is fine when it genuinely conveys better, kept illustrative —
     no real imports or library calls. Pseudocode communicates the rule, not the
     implementation.
     Usage sample: when the work ships a surface others call — a library, an API,
     a CLI — sketch both sides: the implementation shape AND how the consumer uses
     it (the public call site, the command invocation, the request/response). -->


## Goals / Non-goals


- Goals: <...>
- Non-goals: <...>


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | <name> | <...> | <...> |
| B | <name> | <...> | <...> |


## Recommendation


<chosen option + why, with evidence — file:line, signals snapshot, prior decisions>


## Open questions


- <q>
