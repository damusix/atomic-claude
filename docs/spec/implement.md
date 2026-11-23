# /implement


## Goal


Implement a task or approved spec with one independently reviewed loop, whether the main agent writes the code or delegates a checkpoint.


## Non-goals


- Diagnose unknown causes inside `/implement`.
- Replace `/autopilot` or the ship commands.


## Success criteria


- [ ] Accept a task, a spec reference, or the task already in the conversation without a mode flag.
- [ ] Stop before editing on unknown causes and direct the user to `atomic-debug`. Direct unresolved scope or contract decisions to `/atomic-plan`.
- [ ] Use the spec's checkpoints when available. Otherwise declare cohesive checkpoints before editing; a straightforward known-cause fix uses one checkpoint without a spec or worktree.
- [ ] Write directly when the context and scope fit. Otherwise delegate to `atomic-implementer`. Keep a scratchpad for delegated or multi-checkpoint work.
- [ ] Review the task-only checkpoint patch. Fix avoidable duplication, library drift, speculative abstractions, and other blockers before committing on `PASS`. Outside Autopilot, defer only explicitly advisory nits and pass them to the final auditor.
- [ ] Verify the outcome and use the scratch scan and task diff to update affected docs. Audit the whole delivery once, including uncommitted docs and remaining polish. Fix every remaining finding and review and verify the fixes without repeating the audit. Reconcile docs and rescan if scope changes. Keep the baseline unchanged until acceptance. Do not ship without a request.
- [ ] Keep a task-generated scratchpad spec accessible through verification and audit. Discard it after acceptance, keeping the design and other bundle files. Preserve incomplete specs and externally supplied or tracked specs.
- [ ] Route live help, planner handoffs, and examples to `/implement`. No separate quick-fix, delegated implementation, or diagnosis command remains in the bundle.


## Approach


Use one command that selects the writer and requires the same review gate for either writer. See `docs/design/implement-loop-consolidation.md` for the review loop design.


## Change tree


```
context/
  CLAUDE.md  M
  commands/
    implement.md  M
    subagent-implementation.md  D
    subagent-diagnose.md  D
    quick-fix.md  D
    atomic-plan.md  M
    atomic-help.md  M
  _partials/
    handoff.md  D
    review-gate.md  M
docs/
  spec/implement.md  M
  reference/  M
README.md  M
```


## Outline


```
context/commands/implement.md
  Starting point — check readiness and select checkpoints
  Implement and review — select a writer, verify, and apply the review gate
  Finish — verify the end result, scan and update affected docs, audit once, then accept the reviewed baseline
```


## Flows


**Flow: reviewed checkpoint**

1. User provides a task or spec, or refers to the current conversation.
2. Main agent declares checkpoints and implements or delegates each one.
3. Independent reviewer checks the task-only patch. The writer fixes blockers and returns for review, or the checkpoint commits that patch on `PASS`.
4. Main agent verifies and audits the delivery.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|---|---|---|---|---|
| 1 | Consolidate implementation routes and preserve independent review | `context/commands/implement.md`, command handoffs and review gate | atomic-implementer (mode: feature) | ~8 | Config and target artifact validation; former command files absent |
| 2 | Update discovery and reference material | Help, README, reference docs, and demos | atomic-implementer (mode: feature) | ~8 | Live examples use `/implement`; diagnosis guidance points to `atomic-debug` |


## Risks


| Risk | Likelihood | Mitigation |
|---|---|---|
| Review includes unrelated work or misses new files | Medium | Pass `diff: working` and checkpoint paths; commit only the reviewed changes. |
| An unconfirmed diagnosis leads to a speculative fix | Medium | Stop and direct the user to `atomic-debug`. |
| A caller invokes a retired command | Medium | Update live handoffs and examples. |


## Change log

### 2026-11-23: Gate reuse and close out polish

**What changed:** Checkpoint reviews block on avoidable duplication and library drift. Harmless polish can wait for the final audit outside Autopilot. Review and verify audit fixes without repeating the audit.

**Why:** Each change should build on the repo's conventions, and local polish should not interrupt checkpoint work that can safely proceed.

### 2026-11-23: Retire task-generated specs after acceptance

**What changed:** Read the supplied scratchpad spec and design through review, then discard only the task's disposable spec.

**Why:** The design keeps the decisions. An incomplete run still needs its contract to resume.

### 2026-11-23: Use the scan to update documentation

**What changed:** Diff a scratch scan before updating docs. Accept it only after review.

**Why:** Advancing the baseline first hides documentation drift.

### 2026-11-22: Review task-owned patches

**What changed:** Checkpoint review and commit share a patch. The final audit also sees uncommitted and new task files. Only a non-autonomous run can defer an advisory nit.

**Why:** Path filters miss unrelated hunks in shared files, and a commit range misses edits that have not been committed.

### 2026-11-22: Show checkpoint review and finalization as diagrams


**What changed:** The command shows both loops directly. Finalization checks the end result, audits, verifies fixes, and scans the final source tree without writing wiki prose.

**Why:** The two diagrams show when a change can commit and when the task can ship without requiring a reader to follow shared partials.

### 2026-09-06 — Compose from shared loop partials; auditor is the final gate

**What changed:** The command is a policy table plus its Checkpoints section, composed with the `handoff`, `worktree-setup`, `implement-loop`, and `loop-finalize` partials. The fit gate and escape hatch are the shared hand-off table plus one Entry row. The final gate is `atomic-auditor`, always; the `$ARGUMENTS` agent token is gone.

**Why:** `docs/design/implement-loop-consolidation.md`: the four implementation commands restated one loop, and the three-way final-gate pick added a prompt and a parsing caveat the other verbs did not have.

**Superseded:** a user-selected final gate (`auditor` / `strategist` / `reviewer`) chosen by a trailing `$ARGUMENTS` token or an `AskUserQuestion`; a command-local fit gate and escape hatch.

### 2026-11-22: Consolidate implementation and keep diagnosis separate


**What changed:** `/implement` selects a direct or delegated writer and applies the same checkpoint review gate to both. Unknown causes go to `atomic-debug`.

**Why:** The writer choice does not need a separate command, and diagnosis belongs before implementation.

**Superseded:** Main-agent-only `/implement` and handoffs to `/subagent-implementation` or `/subagent-diagnose`.

### 2026-11-22: Fold quick fixes into implementation


**What changed:** Straightforward known-cause fixes use one `/implement` checkpoint without a spec or worktree. Independent review and verification still apply.

**Why:** A small fix does not need a separate command.

**Superseded:** The separate `/quick-fix` command.
