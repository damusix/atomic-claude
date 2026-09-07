# /implement: main-agent implementation with a per-checkpoint reviewer gate


## Goal


An `/implement` command runs the implementation phase in the main agent rather than
through fresh-context subagents, for work whose context is already in the
conversation. It keeps the checkpoint discipline and commit-per-green rhythm of
`/subagent-implementation`, dispatches `atomic-reviewer` after every checkpoint to
supply the independent review the main agent cannot give itself, and finalizes the
way the subagent loop does: docs, one `atomic-auditor` pass, a range-scoped signals refresh.


## Non-goals


- Replacing `/subagent-implementation`. When the context is not already loaded, the
  subagent loop is the cheaper way to buy it and stays the default.
- Replacing `/quick-fix`. That verb skips planning for a known-cause fix and still
  delegates the writing; this one keeps the checkpoint structure and does the writing here.
- Removing the ship-verb review gate. `/commit`'s `review-gate` still covers ad-hoc
  edits made outside any command.
- A new reviewer prompt or scratchpad shape. Both come from the binary, unchanged.
- Autonomy. Every gate that asks the user in `/subagent-implementation` still asks here.


## Success criteria


- [ ] `/implement` with no arguments works — the task comes from the conversation, not from `$ARGUMENTS`.
- [ ] The hand-off table (shared `handoff` partial) plus the command's own Entry row exits to `/subagent-implementation` when the context is not already loaded or the work would not fit in it, to `/subagent-diagnose` on an unknown root cause, and to `/atomic-plan` on multiple viable approaches, open criteria, or an implied contract choice.
- [ ] Checkpoints are declared and stated to the user before any code is written; a spec's checkpoint table is used when one exists.
- [ ] `atomic-reviewer` is dispatched once per checkpoint, never batched to the end, and never replaced by the main agent's own suite run.
- [ ] A checkpoint commits only on `VERDICT: PASS`; 🔴 findings and readability 🟡 findings are fixed before the commit rather than deferred to `FOLLOWUPS.md`.
- [ ] Two consecutive `CHANGES_REQUESTED` rounds on the same blocking signal surface the stuck choice (continue / `/pressure-test` / `atomic-strategist`) and wait for the user.
- [ ] The worktree gate runs only when the working tree is clean, and states the skip in one line when it is dirty.
- [ ] The scratchpad trio (`BRIEF.md`, `STATE.md`, `FOLLOWUPS.md`) is written from `atomic template` verbs, and `BRIEF.md` is refreshed per checkpoint so the reviewer prompt's Step 1 read resolves.
- [ ] `STATE.md` records the loop base SHA before the first checkpoint entry, and a commit SHA per green checkpoint.
- [ ] `atomic-auditor` is dispatched exactly once, with `range: <loop-base>..HEAD`, `state:`, `scratch:`, and the spec or brief path.
- [ ] `atomic-verify`'s reviewer-dispatch exclusion list and `atomic-reviewer`'s own description both name `/implement`.
- [ ] The signals refresh runs after the audit, gated on `atomic signals stale` exit 1, scoped to `<loop-base>..HEAD`.
- [ ] The command never pushes, merges, or opens a PR.
- [ ] `/commit`'s `review-gate` skips a change produced by `/implement`, since every checkpoint already met a reviewer.
- [ ] `/implement` appears in `/atomic-help`'s lifecycle topic table and in tour stage 2.
- [ ] `/implement` appears in `docs/reference/commands.md`, `docs/reference/workflow.md`, `docs/reference/agents.md`'s dispatch tree, and the README command list.
- [ ] `make -C atomic bundle` succeeds with the command present, resolving its `handoff`, `worktree-setup`, `implement-loop`, and `loop-finalize` directives.


## Approach


A single-agent loop composed from the same partials as `/subagent-implementation`
(`handoff`, `worktree-setup`, `implement-loop`, `loop-finalize`) with one role
collapsed: the orchestrator and the implementer are the same context. Because that
removes the structural separation the subagent loop relies on, the reviewer dispatch
is an explicit, non-optional step after each checkpoint, stated in the loop partial's
`Writer: main agent` branch. The command's own text is its policy table and the
checkpoint declaration. Design: `docs/design/implement-loop-consolidation.md`.


## Change tree


```
context/
├── CLAUDE.md ....................... M  (workflow section: third implementation path)
├── _partials/
│   └── review-gate.md .............. M  (already-reviewed guard names /implement)
├── agents/
│   └── atomic-reviewer.md .......... M  (description names the per-checkpoint dispatch)
├── skills/
│   └── atomic-verify/SKILL.md ...... M  (gate exclusion list names /implement)
└── commands/
    ├── implement.md ................ A  (new; composes handoff, worktree-setup, implement-loop, loop-finalize)
    └── atomic-help.md .............. M  (lifecycle row, tour stage 2, review row)
docs/
├── spec/implement.md ............... A  (this file)
└── reference/
    ├── commands.md ................. M
    ├── workflow.md ................. M
    └── agents.md ................... M
README.md ........................... M
```


## Checkpoints


| # | Checkpoint | Files/areas | Est. files | Verifies |
|---|------------|-------------|------------|----------|
| 1 | Write the `/implement` command: policy table, Entry row, checkpoint declaration; the loop and finalize come from the shared partials | `context/commands/implement.md` | 1 | `make -C atomic bundle` resolves every partial directive |
| 2 | Wire the cross-artifact contracts: review-gate guard, global contract workflow entry, and the two agent-facing surfaces that decide whether a redundant reviewer fires — `atomic-verify`'s exclusion list and `atomic-reviewer`'s description | `context/_partials/review-gate.md`, `context/CLAUDE.md`, `context/skills/atomic-verify/SKILL.md`, `context/agents/atomic-reviewer.md` | 4 | bundle succeeds; no surface instructs a second reviewer pass on work `/implement` already gated |
| 3 | Register across discovery surfaces: help router row and tour stage, reference tables, README | `context/commands/atomic-help.md`, `docs/reference/{commands,workflow,agents}.md`, `README.md` | ~6 | the help-router verification loop in root `CLAUDE.md` prints zero `MISSING:` lines |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| The verb becomes the default because it feels faster, and work with no loaded context runs without the loop's benefits | high | The policy table's Entry row tests for loaded context specifically, and the description leads with the precondition rather than the speed |
| The main agent skips the reviewer dispatch, judging its own suite run sufficient | high | Non-optional in the workflow and restated as a rule with its why; "your own suite run is evidence, not review" |
| Implementing inline exhausts the context that made this the right verb, mid-task | med | The Checkpoints section's mid-loop hand-off fires on little remaining context, with `STATE.md` intact |
| Checkpoints get discovered while writing, so the reviewer fires once at the end | med | Declaration is a separate step before implementation and is stated to the user |
| Entering a worktree mid-flow strands uncommitted work in the source tree | med | The worktree gate runs only on a clean tree |
| `/commit` re-reviews work every checkpoint already reviewed | low | The `review-gate` already-reviewed guard names `/implement` alongside the other loops |


## Change log

### 2026-09-06 — Compose from shared loop partials; auditor is the final gate

**What changed:** The command is a policy table plus its Checkpoints section, composed with the `handoff`, `worktree-setup`, `implement-loop`, and `loop-finalize` partials. The fit gate and escape hatch are the shared hand-off table plus one Entry row. The final gate is `atomic-auditor`, always; the `$ARGUMENTS` agent token is gone.

**Why:** `docs/design/implement-loop-consolidation.md`: the four implementation commands restated one loop, and the three-way final-gate pick added a prompt and a parsing caveat the other verbs did not have.

**Superseded:** a user-selected final gate (`auditor` / `strategist` / `reviewer`) chosen by a trailing `$ARGUMENTS` token or an `AskUserQuestion`; a command-local fit gate and escape hatch.
