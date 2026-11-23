# /autopilot — autonomous feature delivery

## Goal

Take a task or GitHub issue through planning, implementation, and shipping without routine approvals.


## Success criteria


- [ ] For remote shipping, select a task branch before planning or implementation can commit. Never use the base branch as the PR head.
- [ ] Follow `/atomic-plan` and `/implement`. Make routine decisions autonomously, keeping any spec current and preserving independent review, verification, and audit.
- [ ] Consult `atomic-strategist` read-only when stuck. If the blocker remains, stop and report it without shipping.
- [ ] Follow explicit shipping instructions. Otherwise open or update a PR on the configured remote, or keep verified commits locally when no remote is available. Merge or rewrite published history only with explicit authorization.
- [ ] Report the shipping result and verification. Help and guides describe the same default.
- [ ] Resolve every reviewer and auditor finding before shipping. Run `/implement` finalization, including the end-result check and repo scan, before loading the `atomic-git-discipline` shipping reference.


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|---|---|---|
| 1 | Follow the plan and implementation workflows without duplicating them | `context/commands/autopilot.md`, help and user docs | Bundle contains the short prompt; no routine merge question |


## Change log

### 2026-11-22: Fix every finding before shipping


**What changed:** Use the implementation gates, resolve every finding, run the repo scan, and load shipping guidance on demand.

**Why:** Autonomous delivery should not leave known findings or repeat the shipping workflow in its prompt.


### 2026-09-06 — Compose from shared loop partials

**What changed:** The command is a policy table plus its own sections (scratch hygiene, resolve, plan, ship), composed with the `worktree-setup`, `implement-loop`, and `loop-finalize` partials. The five behaviors are policy table values (`Stuck: auto`, `Non-blockers: fix-all`, `Ship`) and constraints, stated once. The task scratchpad is retained at the end; only `tmp/trash/` is deleted.

**Why:** `docs/design/implement-loop-consolidation.md`: the body referenced `/subagent-implementation` as the loop without loading it, and restated the five behaviors three times.

**Superseded:** "run the loop exactly as `/subagent-implementation` defines it" with three locally stated overrides; Phase 6 scratchpad cleanup.

### 2026-06-07 — Scratch hygiene: quarantine instead of delete

**What changed:** Added a `scratch_hygiene` discipline to the command body. During an autopilot run, the orchestrator and its subagents never `rm` and never chain shell commands (`&&`, `;`) — both trigger Bash permission prompts that stall an unattended run. Experiments, probes, and one-off scripts are quarantined under `tmp/trash/` (created once in Phase 2, gitignored) by `mv`, not deleted. Every builder/surgeon dispatch brief carries the same instruction. Phase 6 (renamed "Summary and cleanup") performs one `rm -rf tmp/trash` + scratchpad removal at the very end — the single deletion where a permission prompt is expected; if the user is away to grant it, both are left in place (gitignored, never shipped).

**Why:** observed in practice — autopilot experiments looking for evidence ran `rm` and chained commands mid-run, each firing a permission prompt that blocked the hands-off flow waiting on a human. Deferring all deletion to one end-of-run cleanup keeps the run unattended and collapses N scattered prompts into one expected prompt at the finish.

**Note on rule 4:** the end cleanup is a Bash permission grant, not an `AskUserQuestion`, so the ship gate remains the only decision prompt.

### 2026-11-22: Ship without a separate command


**What changed:** `/autopilot` runs the selected shipping path directly through the shared partials.

**Why:** Interactive Git actions use plain language, so the bundle does not need a separate `/commit` command.

**Superseded:** Delegation to `/commit` for shipping.

### 2026-11-22: Make routine decisions autonomously


**What changed:** Follow the plan and implementation workflows, consult a read-only strategist when stuck, and ship to a PR by default.

**Why:** The same gates apply without a separate copy of the workflow or an end-of-run merge question.

**Superseded:** The composed lifecycle prompt and the mandatory shipping choice.
