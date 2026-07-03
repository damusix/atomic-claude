# Specs carry a change tree and implementation flows

## Goal

Every `docs/spec/<topic>.md` carries two new required body sections —
`## Change tree` (an indented file tree with A/M/D markers, sketch-level) and
`## Flows` (numbered actor → step sequences for the behavior being built) —
so a human approving a spec can see blast radius and behavior before code
exists. The requirement is enforced where specs are born (`/atomic-plan`
template + spec loop), where they are gated (`atomic-reviewer` spec-mode),
and where they are touched (`rules/specs/spec-currency.md` auto-load).

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

## Success criteria

- [ ] `templates/commands/atomic-plan.md`'s spec-structure code fence shows
      `## Change tree` and `## Flows` sections, each with inline format
      guidance (markers `A`/`M`/`D`; numbered actor → step sequences), placed
      between `## Approach` and `## Checkpoints`.
- [ ] The spec-structure fence documents the `None — <reason>` escape for
      `## Flows` on specs whose change ships no runtime behavior — presence
      of the section is what the reviewer checks, not omission.
- [ ] The spec-structure template in `templates/commands/atomic-plan.md` is
      shared by the trivial and non-trivial spec paths and does not exempt
      trivial specs — a trivial inline spec includes both `## Change tree`
      and `## Flows` (or the `None — <reason>` escape) the same as a
      non-trivial spec.
- [ ] The "Spec loop" reviewer-criteria list in `templates/commands/atomic-plan.md`
      names both sections (e.g. a bullet asking whether Change tree/Flows are
      present and non-vague).
- [ ] The "`atomic-reviewer` spec-mode brief" bullet list in
      `templates/commands/atomic-plan.md` names both sections as verdict
      criteria.
- [ ] `templates/agents/atomic-reviewer.md`'s spec-mode `<workflow>` gains a
      required-sections pass (a numbered step) that checks presence AND
      quality of `## Change tree` and `## Flows` — missing or vague sections
      are findings under `Spec quality`.
- [ ] `rules/specs/spec-currency.md` gains a "Required content" clause
      stating every spec body carries `## Change tree` + `## Flows`, that
      amendments keep them current via the existing body-is-truth rule, and
      that the requirement applies forward only: pre-existing specs are not
      backfilled, an unrelated line-level amendment does not trigger
      backfill, and backfill happens only when a scope-changing amendment
      rewrites the body anyway. The "## Amendment rules" section's five
      bullet rules (Adding/Changing/Removing/Correction/Renaming) are
      unchanged in substance — no new lifecycle rule added.
- [ ] `templates/commands/atomic-plan.md`'s "Spec voice" section states the
      voice reconciliation: the change tree is a sketch of the intended
      surface (same altitude as the checkpoint table's `Files/areas`
      column), success criteria remain the only binding contract, and the
      implementer may deviate from the tree without amendment unless the
      deviation breaks a success criterion.
- [ ] `docs/reference/workflow.md` carries one sentence noting specs carry a
      change tree + flows so the human can inspect blast radius and behavior
      before approving.
- [ ] `docs/research/design-docs-html.md` exists, cites file:line evidence
      (`atomic serve`'s render path, the `atomic-visual-options` skill
      pattern), states an explicit recommendation, and states that markdown
      stays the machine-readable source subagents read (HTML is not a
      subagent-facing format).
- [ ] `make render && git diff --exit-code` and
      `make -C atomic bundle && git diff --exit-code` both run clean —
      `commands/atomic-plan.md`, `agents/atomic-reviewer.md`, and
      `atomic/internal/embedded/**` reflect the template edits.
- [ ] `grep -c '^## Change tree' docs/spec/spec-change-tree-flows.md` and
      `grep -c '^## Flows' docs/spec/spec-change-tree-flows.md` each return
      `1` — this spec dogfoods both sections it introduces.

## Approach

Two required body sections threaded through the spec template, the
`atomic-reviewer` spec-mode pass, and the auto-loading currency rule — see
[docs/design/spec-change-tree-flows.md](../design/spec-change-tree-flows.md)
§ Recommendation (Approach A).

## Change tree

    templates/
    ├── commands/atomic-plan.md ....... M  (template + criteria gain Change tree/Flows)
    └── agents/atomic-reviewer.md ..... M  (spec-mode gains required-sections pass)
    rules/specs/spec-currency.md ...... M  (new "Required content" clause)
    commands/atomic-plan.md ........... M  (rendered output, `make render`)
    agents/atomic-reviewer.md ......... M  (rendered output, `make render`)
    atomic/internal/embedded/
    ├── bundle/** ..................... M  (regenerated, `make -C atomic bundle`)
    └── manifest.go ................... M  (regenerated, `make -C atomic bundle`)
    docs/reference/workflow.md ........ M  (one-sentence note on change tree + flows)
    docs/research/design-docs-html.md . A  (research note, issue #114 part 1)

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
    2. a section is missing, or present but vague (no A/M/D markers, no
       numbered actor -> step sequence) -> finding recorded under Spec quality
    3. reviewer emits VERDICT: CHANGES_REQUESTED; spec loop iterates

    Flow: spec amendment
    1. an editor touches a file under docs/spec/** or docs/design/**
    2. rules/specs/spec-currency.md auto-loads (path-scoped rule)
    3. a scope-changing amendment rewrites Change tree and Flows to the new
       current truth, per the existing body-is-truth rule; the change is
       logged in ## Change log

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Thread change-tree + flows contract through template, reviewer, and currency rule: spec-structure fence gains both sections + format guidance + `None — <reason>` escape; spec-loop criteria + spec-mode brief name both; spec-voice section gains the sketch-not-contract reconciliation; `atomic-reviewer` spec-mode workflow gains a required-sections pass; `spec-currency.md` gains the required-content clause; `docs/reference/workflow.md` gets the one-sentence note; regenerate render + bundle | `templates/commands/atomic-plan.md`, `templates/agents/atomic-reviewer.md`, `rules/specs/spec-currency.md`, `docs/reference/workflow.md`, regenerated `commands/atomic-plan.md`, `agents/atomic-reviewer.md`, `atomic/internal/embedded/**` | atomic-implementer (mode: feature) | ~4 source + regen | `make render && git diff --exit-code` and `make -C atomic bundle && git diff --exit-code` both clean; `grep -n '## Change tree\|## Flows' commands/atomic-plan.md` and `grep -n '## Change tree\|## Flows' rules/specs/spec-currency.md` both non-empty; `atomic-reviewer.md` spec-mode workflow step names both sections |
| 2 | Write research note on HTML design docs (issue #114 part 1) | `docs/research/design-docs-html.md` (A) | atomic-implementer (mode: surgical) | 1 | Note exists with file:line-cited evidence (`atomic serve` render path, `atomic-visual-options` pattern), an explicit recommendation, and states markdown stays the machine-readable source subagents read |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Change trees start dictating signatures/algorithms instead of staying a file-level sketch (over-prescription creep) | med | Voice reconciliation text in `templates/commands/atomic-plan.md` states the tree is a sketch, not a contract; `atomic-reviewer`'s existing over-engineering/over-prescription pass covers it |
| Template/bundle drift if `make render` / `make -C atomic bundle` is forgotten after editing templates | med | Pre-commit hook chains render→bundle on staged template edits; CI runs both drift gates independently |
| Spec bloat on trivial inline specs from adding two more required sections | low | Trivial change tree is 1-3 lines by design (no exemption); trivial Flows is one short numbered list or the `None — <reason>` escape |

## Change log

### 2026-07-03 — Implemented

**What changed:** All checkpoints delivered on branch `feat/spec-change-tree-flows`. CP1 (`a6a2263`): contract threaded through `templates/commands/atomic-plan.md`, `templates/agents/atomic-reviewer.md` (spec-mode step 6, steps renumbered 1-10), `rules/specs/spec-currency.md` (`## Required content`), `docs/reference/workflow.md`; rendered outputs + embedded bundle regenerated. CP2 (`6d77bf9`): `docs/research/design-docs-html.md`. Verified: render + bundle parity gates, `go test ./...`, `go vet`, `atomic validate` (branch-built binary), `/atomic-help` MISSING-scan — all green.

**Why:** Implementation log; issue #114.
