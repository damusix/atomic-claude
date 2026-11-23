<!--
Template for docs/design/<topic>.md — emitted by `atomic template design-doc`.
Write this design for the engineer approving the behavior and tradeoffs.
Fill every placeholder and remove the guidance comments. Keep the section order.
<topic> = short kebab-case (e.g. oauth-refresh). No date prefix — git log carries that.
-->
# <title>


## Problem


<user-facing pain or motivation>

<!-- Describe current behavior only as far as it explains the problem. -->


## Goals / Non-goals


- Goals: <...>
- Non-goals: <...>


## Proposed behavior


<Observable end result. Show what a user or caller does and what they get back.>

<!-- Use the project's language for short library or function calls that show
     the result. For a fix, show the same input before and after. For an endpoint,
     show the method, path, example request and response, and relevant errors.
     For a CLI, show the invocation and output. Omit implementation bodies. -->


## Business rules


- <What is allowed, required, or rejected? Include relevant permissions, invariants, and boundary cases.>


## Design


<The interfaces, logic, and data shapes that support the proposed behavior.>

<!-- Show the pieces an engineer needs to evaluate. Use a captioned Mermaid
     diagram for branches, handoffs, failure paths, or state changes. For a data
     change, use a small ERD of the affected entities and relationships; state
     constraints that affect behavior. Use a decision table for combinations
     of conditions. If these forms cannot explain complex logic clearly, use
     brief language-neutral pseudocode. Skip handlers, function bodies,
     migration code, and other implementation detail. -->


## Alternatives and tradeoffs


| Approach | Benefits | Costs or risks |
|----------|----------|----------------|
| <Viable option> | <...> | <...> |
| <Viable option> | <...> | <...> |

<Why choose the proposed design? Cite the evidence and name the tradeoff we accept.>


## Open decisions


- <What still needs a decision or evidence before implementation? Write “None” if resolved.>
