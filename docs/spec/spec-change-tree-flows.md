# Specs carry a change tree, a hollow outline, and implementation flows

## Goal

Every `docs/spec/<topic>.md` carries three required body sections —
`## Change tree` (an indented file tree with A/M/D markers, sketch-level),
`## Outline` (a hollow outline of the named pieces of the work: per file,
`name — responsibility` lines, no signatures or bodies), and `## Flows`
(numbered actor → step sequences for the behavior being built) — so a human
approving a spec can see blast radius, shape, and behavior before code
exists, and the reviewer can walk the delivered work against the outline
afterwards. The requirement is enforced where specs are born (`/atomic-plan`
template + spec loop), where they are gated (`atomic-reviewer` spec-mode),
where they are touched (`rules/specs/spec-currency.md` auto-load), and where
the work is delivered (`atomic-reviewer` code-mode outline pass).

## Non-goals

- No change to spec-currency amendment lifecycle rules (add/change-supersede/
  remove/correct/rename) — this adds required content, not new lifecycle
  rules.
- No HTML authoring or rendering of design docs — that half of issue #114 is
  research only; findings live in `docs/research/design-docs-html.md`.
- No general browser rendering of wiki/repo docs (issue #51, `atomic serve`).
- No changes to `/subagent-implementation` or `/autopilot` templates.
- No retroactive backfill of pre-existing specs: an unrelated line-level
  amendment to an old spec does not trigger the requirement; backfill
  happens only when a scope-changing amendment rewrites the body anyway.
- No deterministic outline checker — delivered-vs-outline verification is an
  `atomic-reviewer` code-mode pass, not an `atomic` verb or a code-intel
  index diff.

## Success criteria

- [ ] `templates/commands/atomic-plan.md`'s spec-structure code fence shows
      `## Change tree`, `## Outline`, and `## Flows` sections, each with
      inline format guidance (markers `A`/`M`/`D`; per-file
      `name — responsibility` pieces with members nesting one level under
      their parent and no deeper; numbered actor → step sequences), placed
      between `## Approach` and `## Checkpoints`.
- [ ] The spec-structure fence documents the `None — <reason>` escape for
      `## Outline` (change has no nameable pieces) and `## Flows` (change
      ships no runtime behavior) — presence of each section is what the
      reviewer checks, not omission.
- [ ] The spec-structure template in `templates/commands/atomic-plan.md` is
      shared by the trivial and non-trivial spec paths and does not exempt
      trivial specs — a trivial inline spec includes `## Change tree`,
      `## Outline`, and `## Flows` (or their `None — <reason>` escapes) the
      same as a non-trivial spec.
- [ ] The "Spec loop" reviewer-criteria list in `templates/commands/atomic-plan.md`
      names all three sections (bullets asking whether Change tree/Outline/
      Flows are present and non-vague).
- [ ] The "`atomic-reviewer` spec-mode brief" bullet list in
      `templates/commands/atomic-plan.md` names all three sections as verdict
      criteria.
- [ ] `templates/agents/atomic-reviewer.md`'s spec-mode `<workflow>` gains a
      required-sections pass (a numbered step) that checks presence AND
      quality of `## Change tree`, `## Outline`, and `## Flows` — missing or
      vague sections (unmarked tree, signature-bearing or responsibility-free
      outline, vague flows) are findings under `Spec quality`.
- [ ] `templates/agents/atomic-reviewer.md`'s code-mode `<workflow>` gains an
      outline pass (a numbered step): when the spec carries `## Outline`, the
      delivered diff is walked against the outlined pieces for the
      iteration's checkpoint — an outlined piece absent with no explanation
      in the implementer's report is a `🟡 risk` finding under
      `Spec compliance`; renames/splits the report accounts for and pieces
      delivered beyond the outline are not findings unless a success
      criterion or the over-engineering rule breaks.
- [ ] `rules/specs/spec-currency.md`'s "Required content" clause states
      every spec body carries `## Change tree` + `## Outline` + `## Flows`,
      that amendments keep all three current via the existing body-is-truth
      rule, and that the requirement applies forward only: pre-existing
      specs are not backfilled, an unrelated line-level amendment does not
      trigger backfill, and backfill happens only when a scope-changing
      amendment rewrites the body anyway. The "## Amendment rules" section's
      five bullet rules (Adding/Changing/Removing/Correction/Renaming) are
      unchanged in substance — no new lifecycle rule added.
- [ ] `templates/commands/atomic-plan.md`'s "Spec voice" section states the
      voice reconciliation: the change tree is a sketch of the intended
      surface (same altitude as the checkpoint table's `Files/areas`
      column), the outline is hollow (names + one-line responsibilities,
      never signatures, bodies, or algorithms), success criteria remain the
      only binding contract, the implementer may deviate from either without
      amendment unless the deviation breaks a success criterion, and outline
      deviations surface in the reviewer's code-mode pass rather than as
      silent drift.
- [ ] `docs/reference/workflow.md` carries one sentence noting specs carry a
      change tree + hollow outline + flows so the human can inspect blast
      radius, shape, and behavior before approving, and that the reviewer
      walks delivered work against the outline.
- [ ] `docs/research/design-docs-html.md` exists, cites file:line evidence
      (`atomic serve`'s render path, the `atomic-visual-options` skill
      pattern), states an explicit recommendation, and states that markdown
      stays the machine-readable source subagents read (HTML is not a
      subagent-facing format).
- [ ] `make render && git diff --exit-code` and
      `make -C atomic bundle && git diff --exit-code` both run clean —
      `commands/atomic-plan.md`, `agents/atomic-reviewer.md`, and
      `atomic/internal/embedded/**` reflect the template edits.
- [ ] `grep -c '^## Change tree' docs/spec/spec-change-tree-flows.md`,
      `grep -c '^## Outline' docs/spec/spec-change-tree-flows.md`, and
      `grep -c '^## Flows' docs/spec/spec-change-tree-flows.md` each return
      `1` — this spec dogfoods all three sections it introduces.

## Approach

Two required body sections threaded through the spec template, the
`atomic-reviewer` spec-mode pass, and the auto-loading currency rule — see
[docs/design/spec-change-tree-flows.md](../design/spec-change-tree-flows.md)
§ Recommendation (Approach A).

## Change tree

    templates/
    ├── commands/atomic-plan.md ....... M  (template + criteria gain Change tree/Outline/Flows)
    └── agents/atomic-reviewer.md ..... M  (spec-mode required-sections pass + code-mode outline pass)
    rules/specs/spec-currency.md ...... M  (new "Required content" clause)
    commands/atomic-plan.md ........... M  (rendered output, `make render`)
    agents/atomic-reviewer.md ......... M  (rendered output, `make render`)
    atomic/internal/embedded/
    ├── bundle/** ..................... M  (regenerated, `make -C atomic bundle`)
    └── manifest.go ................... M  (regenerated, `make -C atomic bundle`)
    docs/reference/workflow.md ........ M  (one-sentence note on tree + outline + flows)
    docs/research/design-docs-html.md . A  (research note, issue #114 part 1)

## Outline

    templates/commands/atomic-plan.md
      spec-structure fence — Change tree + Outline + Flows blocks with format guidance
      spec-loop criteria — reviewer gap bullets naming all three sections
      spec-mode brief — verdict criteria naming all three sections
      Spec voice — sketch-not-contract reconciliation covering tree and outline

    templates/agents/atomic-reviewer.md
      spec-mode required-sections pass — presence + hollowness of all three sections
      code-mode outline pass — delivered diff walked against the outlined pieces

    rules/specs/spec-currency.md
      Required content — three required sections, forward-only, kept current on amendment

    docs/reference/workflow.md
      plan-stage note — one sentence on tree + outline + flows

    docs/research/design-docs-html.md
      research note — issue #114 part 1 findings and recommendation

## Flows

    Flow: spec authoring
    1. user invokes /atomic-plan for non-trivial work
    2. atomic-implementer (mode: feature) drafts docs/spec/<topic>.md,
       including a Change tree and a Flows section
    3. reviewer gates the draft — see Flow: spec review gate
    4. human reads the tree + flows and approves before /subagent-implementation

    Flow: spec review gate
    1. atomic-reviewer (spec-mode) runs the required-sections pass over the
       draft spec
    2. a section is missing, or present but vague (no A/M/D markers, a
       signature-bearing or responsibility-free outline, no numbered
       actor -> step sequence) -> finding recorded under Spec quality
    3. reviewer emits VERDICT: CHANGES_REQUESTED; spec loop iterates

    Flow: delivery verification
    1. /subagent-implementation dispatches atomic-reviewer (code-mode) on an
       iteration's diff
    2. reviewer walks the spec's ## Outline pieces for the iteration's
       checkpoint against the delivered diff
    3. an outlined piece missing with no explanation in the implementer's
       report -> 🟡 finding under Spec compliance; accounted-for renames or
       splits and extra pieces pass

    Flow: spec amendment
    1. an editor touches a file under docs/spec/** or docs/design/**
    2. rules/specs/spec-currency.md auto-loads (path-scoped rule)
    3. a scope-changing amendment rewrites Change tree, Outline, and Flows to
       the new current truth, per the existing body-is-truth rule; the change
       is logged in ## Change log

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Thread change-tree + flows contract through template, reviewer, and currency rule: spec-structure fence gains both sections + format guidance + `None — <reason>` escape; spec-loop criteria + spec-mode brief name both; spec-voice section gains the sketch-not-contract reconciliation; `atomic-reviewer` spec-mode workflow gains a required-sections pass; `spec-currency.md` gains the required-content clause; `docs/reference/workflow.md` gets the one-sentence note; regenerate render + bundle | `templates/commands/atomic-plan.md`, `templates/agents/atomic-reviewer.md`, `rules/specs/spec-currency.md`, `docs/reference/workflow.md`, regenerated `commands/atomic-plan.md`, `agents/atomic-reviewer.md`, `atomic/internal/embedded/**` | atomic-implementer (mode: feature) | ~4 source + regen | `make render && git diff --exit-code` and `make -C atomic bundle && git diff --exit-code` both clean; `grep -n '## Change tree\|## Flows' commands/atomic-plan.md` and `grep -n '## Change tree\|## Flows' rules/specs/spec-currency.md` both non-empty; `atomic-reviewer.md` spec-mode workflow step names both sections |
| 2 | Write research note on HTML design docs (issue #114 part 1) | `docs/research/design-docs-html.md` (A) | atomic-implementer (mode: surgical) | 1 | Note exists with file:line-cited evidence (`atomic serve` render path, `atomic-visual-options` pattern), an explicit recommendation, and states markdown stays the machine-readable source subagents read |
| 3 | Add hollow-outline contract: spec-structure fence gains `## Outline` + format guidance + `None — <reason>` escape; spec-loop criteria + spec-mode brief name it; spec-voice reconciliation covers it; `atomic-reviewer` spec-mode required-sections pass checks it and code-mode gains the outline pass; `spec-currency.md` Required content names three sections; `docs/reference/workflow.md` note extended; regenerate render + bundle | `templates/commands/atomic-plan.md`, `templates/agents/atomic-reviewer.md`, `rules/specs/spec-currency.md`, `docs/reference/workflow.md`, regenerated `commands/atomic-plan.md`, `agents/atomic-reviewer.md`, `atomic/internal/embedded/**` | atomic-implementer (mode: feature) | ~4 source + regen | render + bundle drift gates clean; `grep -n '## Outline' commands/atomic-plan.md rules/specs/spec-currency.md` non-empty; `agents/atomic-reviewer.md` code-mode workflow names the outline pass |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Change trees or outlines start dictating signatures/bodies/algorithms instead of staying sketch-level (over-prescription creep) | med | Voice reconciliation text in `templates/commands/atomic-plan.md` states both are sketches, not contracts; `atomic-reviewer`'s spec-mode required-sections pass flags signature-bearing trees and outlines |
| Template/bundle drift if `make render` / `make -C atomic bundle` is forgotten after editing templates | med | Pre-commit hook chains render→bundle on staged template edits; CI runs both drift gates independently |
| Spec bloat on trivial inline specs from adding two more required sections | low | Trivial change tree is 1-3 lines by design (no exemption); trivial Flows is one short numbered list or the `None — <reason>` escape |

## Change log

### 2026-07-04 — Outline unit semantics: nesting + mixed changes

**What changed:** Outline format guidance clarified across the template fence, `spec-currency.md`, the reviewer's spec-mode pass, and the design doc: a mixed code+docs change uses each file's natural unit side by side (symbols for code files, sections for doc files); members nest one level under their parent piece — a type's methods, a section's subsections — and no deeper, since what happens inside a member is implementation. Over-nested outlines are now a spec-mode finding.

**Why:** The initial guidance showed only flat piece lists and never said whether a class's methods belong in the outline or how a mixed change is written.

### 2026-07-04 — Add ## Outline (hollow work skeleton)

**What changed:** Spec contract extended from two required sections to three: `## Outline` — per file, the named pieces of the work as `name — responsibility` lines, hollow (no signatures, bodies, or algorithms) — sits between `## Change tree` and `## Flows`. Threaded through the same enforcement points (template fence, spec-loop criteria, spec-mode brief, spec-mode required-sections pass, spec-currency Required content) plus a new one: `atomic-reviewer` code-mode gains an outline pass that walks each iteration's delivered diff against the outlined pieces. `docs/reference/workflow.md` note extended; render + bundle regenerated. This spec's own body gained its `## Outline` (dogfood) and checkpoint 3.

**Why:** User feedback on the shipped contract — the tree shows where and the flows show behavior, but nothing showed the shape of the work or gave the reviewer a per-piece checklist to verify delivery against.

**Superseded:** Two-section contract (`## Change tree` + `## Flows`) with no delivered-work verification pass.

### 2026-07-03 — Implemented

**What changed:** All checkpoints delivered on branch `feat/spec-change-tree-flows`. CP1 (`a6a2263`): contract threaded through `templates/commands/atomic-plan.md`, `templates/agents/atomic-reviewer.md` (spec-mode step 6, steps renumbered 1-10), `rules/specs/spec-currency.md` (`## Required content`), `docs/reference/workflow.md`; rendered outputs + embedded bundle regenerated. CP2 (`6d77bf9`): `docs/research/design-docs-html.md`. Verified: render + bundle parity gates, `go test ./...`, `go vet`, `atomic validate` (branch-built binary), `/atomic-help` MISSING-scan — all green.

**Why:** Implementation log; issue #114.
