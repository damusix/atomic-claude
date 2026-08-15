<!--
Template for docs/spec/<topic>.md — emitted by `atomic template spec`.
The implementation contract written by /atomic-plan
(inline for trivial work, via the spec loop for non-trivial). Copy this body, fill every
<angle-bracket> placeholder, and delete the guidance comments as you fill — EXCEPT the
one under ## Change log, which stays until the first post-approval amendment.
The body is forward-only current truth: no decision history, no prior-version references,
no rejected-fork enumeration (see rules/specs/spec-currency.md).
Diagrams: a subagent builds from this contract and a human approves it, and both follow a
drawn flow faster than a paragraph. Use as many Mermaid blocks as the contract has shapes —
state machines for lifecycles, ERDs for data models, sequence or flowchart for behavior —
each next to what it explains, with a one-sentence caption above. No quota in either
direction: none for a one-file change, several for a multi-flow design. A diagram earns its
place by making the contract unambiguous, never by filling the template. Keep them
contract-level: pseudocode and implementation sketches belong in the design doc.
-->
# <title>


## Goal


<1-2 sentences. What done looks like.>


## Non-goals


- <thing explicitly out of scope>


## Success criteria


- [ ] <verifiable check>
- [ ] <verifiable check>


## Approach

<!-- Design doc exists (always, for non-trivial work): name the chosen approach in ONE line
     and link the design. Do NOT copy the Approaches table or name the rejected forks —
     that deliberation lives only in the design.
     No design doc (trivial inline spec): replace this section with the full `## Approaches`
     table + `## Recommendation` instead, since there's no design to hold them. -->

<chosen approach, one line> — see `docs/design/<topic>.md`.


## Change tree

<!-- Indented file tree of what this spec touches, inside a fenced code block —
     markdown collapses the alignment otherwise. One line per node.
     Markers: A created, M modified, D removed. Optional parenthetical for
     symbols touched — sketch-level, same altitude as the Checkpoints table's
     Files/areas column, never a signature contract. Example:

     src/auth/
     ├── session.ts ........... M  (SessionStore: rotation support)
     ├── session.test.ts ...... M  (tests for rotation)
     └── rotate.ts ............ A  (new: rotation policy)
     docs/guides/sessions.md .. M  (rotation section) -->

<tree>


## Outline

<!-- Hollow outline of the work, inside a fenced code block so the nesting
     survives: per file, the named pieces to be created or
     reshaped — functions/types for code, sections/blocks for markdown
     artifacts; a mixed change uses each file's natural unit. One line per
     piece: `name — responsibility`. Members nest one level under their
     parent piece — a type's methods, a section's subsections — and no
     deeper: what happens inside a member is implementation. Hollow means
     empty inside: no signatures, no bodies, no algorithms. A change with no
     nameable pieces writes `None — <reason>` instead of omitting the
     section. The reviewer walks this outline against the delivered work, so
     every piece here is a promise the diff should keep (or visibly deviate
     from). Example:

     src/auth/session.ts
       SessionStore — session persistence
         rotate — swap current token, retire previous
         prune  — drop retired tokens past grace period
       isExpired — token age check against policy

     src/auth/session.test.ts
       rotation survives restart — retired token honored during grace

     docs/guides/sessions.md
       Token rotation — behavior + grace-period semantics -->

<outline, or `None — <reason>`>


## Flows

<!-- One actor -> step sequence per behavior being implemented, as numbered
     steps or as a Mermaid `sequenceDiagram` / `flowchart` — whichever reads
     clearer for that flow. A multi-actor exchange or a branching path is
     usually clearer drawn; a short linear sequence is usually clearer
     numbered. Draw one per behavior; several flows means several diagrams.
     A change that ships no runtime behavior (pure docs/config) writes
     `None — <reason>` instead of omitting the section — presence is what
     the reviewer checks, not omission. Example:

     Flow: session rotation
     1. client presents a token older than the rotation interval
     2. middleware calls SessionStore.rotate -> new token issued, old retired
     3. response carries the new token; the old one is honored until the
        grace period ends -->

<flows, or `None — <reason>`>


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | <action>   | <paths>     | atomic-implementer (mode: feature) | ~4 | <test or signal> |
| 2 | <action>   | <paths>     | atomic-implementer (mode: surgical) | 1-2 | <test or signal> |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| <r>  | high/med/low | <plan> |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->
