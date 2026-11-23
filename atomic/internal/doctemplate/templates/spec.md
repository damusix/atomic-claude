<!--
Template for a disposable implementation spec, emitted by `atomic template spec`.
For non-trivial work, fill SPEC.md in the task scratchpad. For trivial work, write
the spec inline. Fill the placeholders and remove all guidance comments. Keep
decisions and their history in the design; keep only the current contract here.
-->
# <title>


## Goal


<1-2 sentences. What done looks like.>


## Non-goals


- <thing explicitly out of scope>


## Success criteria


- [ ] <verifiable check>
- [ ] <verifiable check>


## End-to-end proof


- Setup: <Starting state, data, access, or dependencies needed.>
- Action: <Run the complete user or system journey.>
- Expected result: <The observation that proves the intended outcome.>

<!-- This proves the finished result, separately from checkpoint checks.
     If you cannot run it here, name the limitation and who can run it. -->


## Approach

<!-- Design doc exists (always, for non-trivial work): name the chosen approach in ONE line
     and link the design. Do NOT copy its alternatives or name the rejected forks —
     that deliberation lives only in the design.
     No design doc (trivial inline spec): replace this section with the full `## Approaches`
     table + `## Recommendation` instead, since there's no design to hold them. -->

<chosen approach, one line> — see `docs/design/<topic>.md`.


## Change tree

<!-- Indented file tree of what this spec touches, inside a code fence — the
     glyphs and dot leaders only line up preformatted. One line per node.
     Markers: A created, M modified, D removed. Optional parenthetical for
     symbols touched — sketch-level, same altitude as the Checkpoints table's
     Files/areas column, never a signature contract. Example:

     ```
     src/auth/
     ├── session.ts ........... M  (SessionStore: rotation support)
     ├── session.test.ts ...... M  (tests for rotation)
     └── rotate.ts ............ A  (new: rotation policy)
     docs/guides/sessions.md .. M  (rotation section)
     ``` -->

```
<tree>
```


## Outline

<!-- Hollow outline of the work, inside a code fence — the nesting is carried
     by indentation, which only survives preformatted. Per file, the named
     pieces to be created or reshaped — functions/types for code, sections
     or blocks for markdown artifacts; a mixed change uses each file's own
     natural unit. One line per piece: `name — responsibility`. Members nest
     one level under their parent piece — a type's methods, a section's
     subsections — and no deeper: what happens inside a member is
     implementation. Hollow means empty inside: no signatures, no bodies, no
     algorithms. A change with no nameable pieces writes a bare
     `None — <reason>`, no fence, instead of omitting the
     section. The reviewer walks this outline against the delivered work, so
     every piece here is a promise the diff should keep (or visibly deviate
     from). Example:

     ```
     src/auth/session.ts
       SessionStore — session persistence
         rotate — swap current token, retire previous
         prune  — drop retired tokens past grace period
       isExpired — token age check against policy

     src/auth/session.test.ts
       rotation survives restart — retired token honored during grace

     docs/guides/sessions.md
       Token rotation — behavior + grace-period semantics
     ``` -->

```
<outline — or drop the fence and write `None — <reason>`>
```


## Flows

<!-- Link to the relevant design sections for flows they already describe.
     Write a short, numbered actor → action sequence only for a flow not
     covered there. If nothing has a flow, write `None — <reason>`. -->

<Design flow links or uncovered flows>


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | <action>   | <paths>     | atomic-implementer (mode: feature) | ~4 | <test or signal> |
| 2 | <action>   | <paths>     | atomic-implementer (mode: surgical) | 1-2 | <test or signal> |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| <r>  | high/med/low | <plan> |
