# Spec: /challenge-swarm


## Goal


One explicit command that subjects a written design, spec, or plan to 4-6 isolated expert lenses running as parallel subagents, then reports a contradiction map — where the lenses conflict, where they independently agree, and what they all assumed without checking. It is the post-design, pre-implementation gate in the lifecycle: `/atomic-plan` → `/challenge-swarm` → `/subagent-implementation`.


## Approach


Adapted from Stanford's STORM: perspective-diverse agents grounded in a corpus (here, the codebase), kept isolated so that their independent agreements and disagreements are both informative. Role files carry the specialization, so lens agents run on a mid-tier model.


## Non-goals


- No auto-fire. Explicit invocation only — the command spawns 4-6 subagents, which must never be a surprise. It is a command, not a skill (axiom 5).
- No `/autopilot` integration. Human-invoked gate, like `/pressure-test`.
- No new agent type. Lenses run as `general-purpose` subagents; the role file is the specialization.
- No durable artifacts. The report lives in the conversation; the workspace is gitignored scratchpad, deleted at close-out.
- No modification of the target document or any code.
- No conflict resolution on the design owner's behalf, except where a contested claim is objectively checkable by tool call.


## Success criteria


- `/challenge-swarm @<path.md>` reads the target, orients in the code it touches, selects 4-6 fitting lenses, and prints the roster with a one-line reason per lens before dispatch.
- Workspace created at `.claude/.scratchpad/<yyyy-mm-dd>-challenge-swarm-<slug>/` containing `lens-instructions.md` (verbatim from the command's canonical block), one `lenses/<lens>.md` role file per selected lens, and a `findings/` directory.
- All lens subagents dispatched in a single message (parallel); the dispatch prompt is pointer-only and identical across lenses except the role-file path and findings path.
- No findings file is read until every lens has reported its one-line reply.
- Report carries six sections in order: Verdict, Conflicts, Reinforced findings, Single-lens findings, Unexamined assumptions, Missing lens. Findings are severity-ordered and each carries evidence.
- Contested claims that are objectively checkable are resolved by tool call (`atomic code` verbs, `sg`/grep, or a `tmp/` probe) before appearing in the map.
- Close-out is a numbered typed-selection list (axiom 4); the workspace is deleted only on the explicit "done" pick.
- Empty or ambiguous `$ARGUMENTS` resolves via numbered selection over design/spec files changed on the current branch; paths outside the git toplevel are rejected.
- If subagents are unavailable, the command degrades to sequential self-run with the same workspace and the same no-reread-until-aggregation rule.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|-----------|-------------|----------|
| CP1 | Command template + cross-artifact wiring + reference docs + render/bundle in one commit | `templates/commands/challenge-swarm.md` (+ rendered `commands/challenge-swarm.md`); cross-references in `templates/commands/atomic-plan.md`, `pressure-test.md`, `gather-evidence.md`, `atomic-help.md`; `CLAUDE.md`; `docs/reference/commands.md`, `docs/reference/workflow.md`; `docs/credits.md`; embedded bundle | `make render` and `make -C atomic bundle` leave a clean diff; help-router verification loop reports no `MISSING:` line for `/challenge-swarm` |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Lenses converge despite isolation (shared project instructions load into every subagent) | low | Role files dominate the lens's attention; behavioral rule forbids passing any lens's findings into another lens's prompt or role file |
| Cost surprise — 4-6 subagents per run | low | Explicit-only invocation; roster printed before dispatch; lens agents run mid-tier |
| Findings filler buries real signal | medium | Lens instructions cap findings at 3-7 with mandatory evidence; aggregation cuts evidence-free and no-stake findings; report must stay shorter than the design |
| Aggregator resolves conflicts it should surface | medium | Behavioral rule: surface the trade-off decision unless one side's evidence is decisively stronger |


## Change tree


```
templates/commands/challenge-swarm.md   A  command source (self-contained, no partials)
commands/challenge-swarm.md             A  rendered output (make render)
templates/commands/atomic-plan.md       M  handoff: challenge surface offers /pressure-test + /challenge-swarm
templates/commands/pressure-test.md     M  next-step bullet routes written-artifact challenges here
templates/commands/gather-evidence.md   M  workflow-position pipeline gains /challenge-swarm
templates/commands/atomic-help.md       M  lifecycle rows (plan, pressure-test, challenge-swarm) + tour stages 1-2
commands/*.md                           M  re-rendered outputs for the four edited templates
CLAUDE.md                               M  Workflow step 1 gains the post-design gate line
docs/reference/commands.md              M  Planning table row
docs/reference/workflow.md              M  "Challenge the written design (optional)" subsection
docs/credits.md                         M  STORM prior-art section
docs/spec/challenge-swarm.md            A  this contract
atomic/internal/embedded/**             M  bundle regen (make -C atomic bundle)
```


## Outline


- `templates/commands/challenge-swarm.md`
    - frontmatter — description (trigger surface) + argument-hint
    - Workflow position — pipeline diagram placing the verb between plan and implement
    - Parse arguments — @path / .md / topic-glob / branch-changed-candidates precedence + path safety
    - Step 1 Read, then select lenses — seven-lens table, bespoke-lens rule, skip-empty rule, roster print
    - Step 2 Workspace, then dispatch — workspace tree, pointer dispatch prompt, verbatim lens-instructions block, role-file shape, sequential fallback
    - Step 3 Build the contradiction map — conflicts / reinforced / unexamined-assumptions buckets, checkable-claim resolution
    - Step 4 Report — six-section format, severity ordering, brevity rule
    - Close-out — numbered offers (rerun lens / add lens / file follow-ups / fold into design / done)
    - Behavioral rules — report-only, isolation, evidence-per-finding, filler-dies, verify-before-asserting, roster-is-judgment
    - What this command does not do — durable artifacts, target mutation, auto-fire, autopilot, pressure-test overlap
    - When to suggest the next step — routing to /atomic-plan, /gather-evidence, /subagent-implementation
- `templates/commands/atomic-plan.md`
    - Challenge surface block — dual copyable challenge commands, unchanged trigger conditions
- `templates/commands/pressure-test.md`
    - When-to-suggest bullet — written-artifact / circling-session route to /challenge-swarm
- `templates/commands/gather-evidence.md`
    - Workflow position diagram — pipeline includes /challenge-swarm
- `templates/commands/atomic-help.md`
    - Lifecycle topic rows — challenge-swarm row, plan and pressure-test rows name the complement
    - Tour Stage 1 — command count
    - Tour Stage 2 — challenge-gates line
- `CLAUDE.md` — Workflow step 1 sentence naming the post-design gate
- `docs/reference/commands.md` — Planning table row
- `docs/reference/workflow.md` — Challenge the written design subsection
- `docs/credits.md` — STORM section
- `docs/spec/challenge-swarm.md` — this file


## Flows


1. **Happy path.** User → `/challenge-swarm @docs/spec/x.md` → command reads target end-to-end and orients in touched code → prints lens roster with reasons → writes workspace (`lens-instructions.md`, role files, `findings/`) → dispatches one `general-purpose` subagent per lens in a single message → each lens reads instructions, role file, target, and listed code paths → writes `findings/<lens>.md` → replies one line → command waits for all replies → loads every findings file → resolves objectively checkable disputes via tool calls → prints the six-section report → prints numbered close-out offers → user picks → command acts on the pick.
2. **No or ambiguous target.** User → `/challenge-swarm` (or a topic phrase matching several files) → command lists candidate design/spec files (branch-changed, newest first) as a numbered list → user types a selection → flow 1 from the read step.
3. **Rerun a lens.** User picks close-out option 1 (optionally editing `lenses/<name>.md` first) → command re-dispatches only that lens with the same pointer prompt → rebuilds the contradiction map from all findings files → re-prints the report and offers.
4. **Subagents unavailable.** Command runs each lens itself, sequentially, writing that lens's findings file before starting the next and rereading nothing until aggregation → flow 1 from the aggregation step.


## Change log


### 2026-07-04 — Initial contract

**What changed:** New command `/challenge-swarm` — multi-lens design challenger with isolated parallel subagents and a contradiction-map report, wired as the post-design gate across `/atomic-plan`, `/pressure-test`, `/gather-evidence`, `/atomic-help`, `CLAUDE.md`, and the reference docs.

**Why:** The lifecycle had single-voice challenge gates only (`/pressure-test` dialogue, `atomic-reviewer` spec-mode alignment, `atomic-strategist` single heavyweight opinion). Nothing attacked a written design from multiple independent perspectives, and nothing surfaced the *disagreements between* perspectives — the highest-value signal for the trade-off decisions a design still owes.


### 2026-07-05 — Correction: Checkpoints table columns

**What changed:** The Checkpoints table used `# | Checkpoint | Proof`; reshaped to the required `# | Checkpoint | Files/areas | Verifies` — `Proof` folded into `Verifies`, `Files/areas` populated from the checkpoint's actual wiring. The single checkpoint's scope is unchanged.

**Why:** `atomic validate spec` rule S5 requires the `# | Checkpoint | Files/areas | … | Verifies` column contract as an exact ordered subsequence; the `Proof` shorthand failed the gate.

**Correction:** Found by `atomic validate spec` reporting S5 FAIL at the Checkpoints table.
