# Specs carry a change tree and implementation flows

## Problem


`docs/spec/<topic>.md` is the contract a human approves before Claude implements — but it is optimized for the machine consumer (fresh-context subagents read the body verbatim). A human reviewing it gets checkpoint rows and success criteria, and has to reverse-engineer from prose *which files and symbols are about to change* and *how the implemented behavior will flow*. The approve-then-implement loop is only as safe as the human's ability to read what they are approving (issue #114, part 2).


## Goals / Non-goals


- Goals:
  - Every spec makes the blast radius inspectable at a glance: a tree of files/symbols to be created, modified, or removed.
  - Every spec makes the behavior traceable before code exists: the flows being implemented as actor → step sequences.
  - The requirement is enforced where specs are born (`/atomic-plan` spec template + spec loop) and where they are gated (`atomic-reviewer` spec-mode) and where they are touched (`rules/specs/spec-currency.md` auto-load).
- Non-goals:
  - No change to spec-currency amendment lifecycle rules — this adds required content, not new lifecycle rules (issue #114 scope note).
  - No HTML authoring/rendering of design docs — that half of #114 is research; findings live in `docs/research/design-docs-html.md`.
  - No general browser rendering of wiki/repo docs (#51, `atomic serve`).


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Two required body sections (`## Change tree`, `## Flows`) in the spec structure; template + currency rule + reviewer pass all name them | One artifact, no drift; auto-loading rule enforces on every touch; reviewer gates presence and quality | Slightly longer specs; needs voice reconciliation with the anti-over-prescription rule |
| B | Sidecar artifact (`docs/spec/<topic>.tree.md`) generated per spec | Keeps spec body unchanged | Second file to keep current; drifts from the body; subagents and humans read two places |
| C | Reviewer-only convention (spec-mode flags missing tree/flows, template unchanged) | Smallest diff | Invisible contract — authors discover the requirement only on review failure; violates "wire both directions" |


## Recommendation


A. The spec body is already the single contract read by both audiences; the missing content belongs in it, not beside it. The path-scoped `rules/specs/spec-currency.md` already auto-loads on every `docs/spec/**` touch (main agent and subagents), so a required-content clause there makes the requirement self-enforcing without new mechanism.


## Design decisions


### Section shape


- `## Change tree` — an indented tree of files, annotated per node with one of three markers and, where meaningful, the symbols touched:

    ```
    atomic/internal/wiki/
    ├── bucket.go ............ M  (PromoteBucket: new rotate step)
    ├── bucket_test.go ....... M  (tests for rotation)
    └── manifest.go .......... A  (new: manifest read/write)
    docs/reference/wiki-workflow.md  M  (bucket section)
    ```

    Markers: `A` created, `M` modified, `D` removed. Symbol notes in parentheses, sketch-level.

- `## Flows` — one numbered actor → step sequence per behavior being implemented:

    ```
    Flow: bucket promote
    1. user runs `atomic wiki bucket promote <name>`
    2. CLI resolves bucket manifest → rotates baseline → previous
    3. CLI writes new baseline, prints summary
    ```

    A spec whose change ships no runtime behavior (pure docs/config) writes `None — <reason>` under `## Flows` instead of omitting the section; presence is what the reviewer checks.

### Placement


Both sections sit between `## Approach` and `## Checkpoints`: the reader goes chosen-approach → what changes → how it behaves → how the work is sliced.

### Voice reconciliation


The spec voice rule forbids over-prescription (exact signatures, variable names, algorithms). The change tree does not conflict: it is a **sketch of the intended surface**, same altitude as the checkpoint table's `Files/areas` column, not a signature contract. Success criteria remain the only binding contract; the implementer may deviate from the tree (add a helper file, split a symbol) without amendment, unless the deviation breaks a success criterion. Amendments that change scope rewrite the tree — that is the existing body-is-current-truth rule applied to a new section, not a new lifecycle rule.

### Trivial specs


"Every spec" includes trivial inline specs. A trivial change tree is 1-3 lines and costs nothing; it is exactly the at-a-glance summary the approving human wants. No exemption.

### Retroactivity


The requirement applies forward, not retroactively. Pre-existing specs are not required to backfill the sections, and an unrelated line-level amendment to an old spec does not trigger backfill. Backfill happens when a scope-changing amendment rewrites the body anyway — at that point the tree and flows describe the amended scope. The currency-rule clause must encode this so the auto-loading rule never makes an old spec unsatisfiable on touch.


## Open questions


(none)
