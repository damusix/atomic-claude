# Spec: /challenge-swarm


## Goal


Find disagreements and gaps in a written design, spec, or plan before it is implemented. Use this method when independent perspectives would resolve the main uncertainty.

## Success criteria


- Select independent lenses whose stakes are evident in the artifact. Keep their findings separate until synthesis.
- Verify checkable disputes against source. Report conflicts, reinforced findings, single-lens findings, shared assumptions, and missing perspectives.
- Do not edit the target or decide human tradeoffs. When independent agents are unavailable, call out that limitation before offering sequential review.
- On an authorized automated run, a separate respondent answers the challenger with evidence. Report decisions that still need the user.

## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|---|---|---|
| 1 | Route challenge work through the planning skill | `context/commands/challenge-swarm.md`, `context/skills/atomic-planning/references/swarm.md`, `context/skills/atomic-planning/references/automated.md` | Prompt preserves independent findings and has an explicit automated route. |


## Risks


| Risk | Likelihood | Mitigation |
|---|---|---|
| Repeated findings are mistaken for independent agreement. | Medium | Keep lens results separate until synthesis. |
| The aggregator decides a user tradeoff. | Medium | Verify facts and name the decision without making it. |


## Change log


### 2026-11-22: Move the method into the planning skill


**What changed:** `/challenge-swarm` loads the independent-lens reference. The planner selects it when a written artifact needs that kind of review. An authorized automated run uses separate challenger and respondent roles.

**Why:** The planner should pick the method that resolves the uncertainty without repeating the method in multiple commands.

**Superseded:** A fixed lifecycle gate and an unauthorised sequential fallback.



### 2026-07-04 — Initial contract

**What changed:** New command `/challenge-swarm` — multi-lens design challenger with isolated parallel subagents and a contradiction-map report, wired as the post-design gate across `/atomic-plan`, `/pressure-test`, `/gather-evidence`, `/atomic-help`, `CLAUDE.md`, and the reference docs.

**Why:** The lifecycle had single-voice challenge gates only (`/pressure-test` dialogue, `atomic-reviewer` spec-mode alignment, `atomic-strategist` single heavyweight opinion). Nothing attacked a written design from multiple independent perspectives, and nothing surfaced the *disagreements between* perspectives — the highest-value signal for the trade-off decisions a design still owes.


### 2026-07-05 — Correction: Checkpoints table columns

**What changed:** The Checkpoints table used `# | Checkpoint | Proof`; reshaped to the required `# | Checkpoint | Files/areas | Verifies` — `Proof` folded into `Verifies`, `Files/areas` populated from the checkpoint's actual wiring. The single checkpoint's scope is unchanged.

**Why:** `atomic validate spec` rule S5 requires the `# | Checkpoint | Files/areas | … | Verifies` column contract as an exact ordered subsequence; the `Proof` shorthand failed the gate.

**Correction:** Found by `atomic validate spec` reporting S5 FAIL at the Checkpoints table.


### 2026-07-10 — Lens dispatches pin `model: sonnet`

**What changed:** Every lens dispatch (including close-out lens reruns) passes `model: sonnet`; only an explicit user request for a different tier this run overrides the pin. Approach, Success criteria, and the cost-surprise risk mitigation now state the pin; the command's Step 2, close-out, and behavioral rule 7 enforce it.

**Why:** Issue #141 — the command's "mid-tier is sufficient" wording was a soft hint with a fall-through to the session model, so on a premium session (Opus, Fable) all 4-6 parallel lenses ran on the expensive session model.

**Superseded:** Prior contract said lens agents "run on a mid-tier model" without a mechanism; the command allowed inheriting the session model when a tier override was unavailable.


### 2026-08-20 — Workspace is a slug-keyed bundle, never deleted by this command

**What changed:** The workspace moves from a dated `.claude/.scratchpad/<yyyy-mm-dd>-challenge-swarm-<slug>/` directory this command creates and deletes itself to a bundle created via `atomic scratchpad new <slug> --purpose review`, using the same slug the design/spec-authoring plan used so the challenge and the plan share one bundle. `--purpose review` seeds `lenses/` and `findings/`; the command still writes its own `lens-instructions.md` and per-lens role files into that bundle. Close-out's "done" pick ends the session without deleting or archiving anything — this command never retires a bundle.

**Why:** `docs/spec/serve-plans-page.md` retires per-command scratchpad-path construction in favor of `atomic scratchpad new`/`path`, and retires close-out deletion in favor of `atomic scratchpad archive` run by `/git-cleanup` or a ship-verb worktree cleanup.

**Superseded:** a dated, command-owned workspace directory; "the workspace is gitignored scratchpad, deleted at close-out"; "the workspace is deleted only on the explicit 'done' pick".
