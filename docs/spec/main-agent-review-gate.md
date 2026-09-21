# Main-agent review gate

## Goal

Code the main agent writes outside a loop command meets one independent reader before it is called done. The gate lives in `context/CLAUDE.md`, which loads every session, and takes the form of a required line in the completion claim — `review: <verdict> (<agent> on <model>)` — so a skipped gate is a visibly incomplete answer rather than a silent omission. Both ad-hoc exits, `atomic-verify` before "ready" and the ship verbs' `review-gate` before the commit, dispatch `atomic-reviewer` in code mode with an explicit model override, so the gate runs on the session's model rather than the agent's frontmatter default.

## Non-goals

- No hooks of any kind. No `Stop` hook, no `PostToolUse` hook, no session-state file, nothing that blocks a turn from ending.
- No Go changes. No new binary verb, no doctor check, no install step.
- No new path-scoped rule under `context/rules/`.
- No change to `atomic-auditor`. Its contract stays the finished whole, once, after a loop goes green.
- No change to any agent's `model:` frontmatter. The pins stay as defaults; the ad-hoc gate overrides at dispatch.
- No change to the loop gates: `/implement`, `/subagent-implementation`, `/quick-fix`, `/autopilot`, and `/subagent-diagnose` keep `atomic-reviewer` per checkpoint or iteration.
- No gate on documentation-only changes.
- No new artifact of any kind: every change is to a file that already exists, plus the design and this spec.

## Success criteria

- [ ] `context/CLAUDE.md` carries a `<completion_claim>` block inside `<atomic>`, adjacent to `<quality_gates>`, that states the required line verbatim — `review: <PASS|CHANGES_REQUESTED> (<agent> on <model>)` — names `atomic-reviewer` as what the gate dispatches and the session's model as what it runs on, names the five loop commands whose work is exempt because a reviewer already read it, and exempts documentation-only changes. Under 15 lines.
- [ ] The `<completion_claim>` block states the requirement as a property of the reply ("a completion claim without this line is incomplete"), not as an instruction to run a review. No sentence in it is an imperative telling the model to review its work.
- [ ] `context/CLAUDE.md`'s `## Workflow` **Implement** bullet names the `review:` line as the ad-hoc exit gate instead of a bare "Ad-hoc edits get their review gate at the exits" clause.
- [ ] `context/skills/atomic-verify/SKILL.md`'s `## Verification discipline` list no longer carries the long "When you wrote the code yourself, outside …" bullet. It is deleted, not reworded: the surface never loaded at claim time, and `context/CLAUDE.md` now states the contract.
- [ ] `context/skills/atomic-verify/SKILL.md`'s claim table row for `code I wrote myself is ready` dispatches `atomic-reviewer` in code mode on the working diff with an explicit model override, and states the `review:` line, in one line consistent with the `context/CLAUDE.md` block.
- [ ] `context/skills/atomic-verify/SKILL.md`'s `description` names `atomic-reviewer` as the agent it dispatches, so the dispatch is inspectable from the always-loaded skill roster.
- [ ] `context/_partials/review-gate.md` dispatches `atomic-reviewer` (`subagent_type: "atomic-reviewer"`) in code mode on the staged diff with an explicit model override, and its report step names the `review:` line. It passes no `surfaces:` key.
- [ ] `context/_partials/review-gate.md`'s already-reviewed guard gains a bullet: skip when the claim gate already reviewed this same tree and nothing has changed since.
- [ ] Both ad-hoc dispatch sites resolve the diff scope rather than leaving it to the agent: working → `git diff HEAD` plus `git ls-files --others --exclude-standard` with each untracked file read in full; staged → `git diff --cached`.
- [ ] `context/agents/atomic-reviewer.md`'s `description` names the ad-hoc gate at both exits and states that callers override the model at dispatch. Its `model: claude-sonnet-5` frontmatter is unchanged, and its loop, `/implement`-checkpoint, and spec-mode uses are unchanged.
- [ ] `context/agents/atomic-auditor.md` describes one contract: the finished whole, once, after a loop goes green. It documents no ad-hoc mode, no `diff:` key, and no conditional on `scratch:`, and `docs/reference/agents.md`'s row for it says the same.
- [ ] `docs/wiki/workflow.md` describes the current contract: the `<completion_claim>` block in `context/CLAUDE.md` as the ad-hoc rule's home, the `review:` line, and all four `review-gate` skip conditions.
- [ ] `context/commands/atomic-help.md`'s `review` topic row, `agents` topic row, and tour Stage 2 name `atomic-reviewer` as the ad-hoc gate at both exits and the `review:` line, and no row claims `atomic-auditor` gates a single ad-hoc diff.
- [ ] The `/atomic-help` verification loop from the root `CLAUDE.md` prints zero `MISSING:` lines.
- [ ] `docs/reference/agents.md`'s dispatch-topology mermaid block and its `accDescr` show both ad-hoc exits reaching `atomic-reviewer`; its `atomic-auditor` row describes the whole-delivery-once contract only; its `atomic-reviewer` row names the ad-hoc gate at both exits.
- [ ] `docs/reference/commands.md`'s `/commit` row names `atomic-reviewer` as the ad-hoc gate. Its `/implement` row is unchanged.
- [ ] `docs/reference/skills.md`'s intro paragraph and `atomic-verify` row name `atomic-reviewer` and the `review:` line, and carry no claim that an unpinned model is why the agent was chosen.
- [ ] `docs/reference/workflow.md`'s review-gate paragraph, its `✓†` footnote, the **Independent review** bullet under `## Why custom ship commands?`, and the `### Editing directly, outside any verb` subsection under `## 2` all name `atomic-reviewer` and the `review:` line.
- [ ] `grep -rn "audit:" context/ docs/reference/ context/commands/atomic-help.md` returns no hit describing the ad-hoc gate.
- [ ] `make -C atomic bundle` succeeds, and `atomic validate config` / `validate artifacts` / `validate spec` each report 0 FAIL.
- [ ] No file under `atomic/` other than the gitignored bundle mirror is modified; `git diff --name-only origin/next..HEAD` lists no `.go` file.

## Approach

Relocate the gate from an unloaded skill body to the always-loaded `context/CLAUDE.md`, restated as a required output line rather than a behavior instruction, and point both ad-hoc exits at `atomic-reviewer` with a per-dispatch model override — see [docs/design/main-agent-review-gate.md](../design/main-agent-review-gate.md) § Chosen.

## Change tree

```
context/
├── CLAUDE.md ......................... M  (new <completion_claim> block; Workflow bullet)
├── skills/atomic-verify/SKILL.md ..... M  (description clause; table row; delete stale bullet)
├── _partials/review-gate.md .......... M  (dispatch reviewer; new guard; review line)
├── agents/atomic-reviewer.md ......... M  (description names the ad-hoc gate at both exits)
└── commands/atomic-help.md ........... M  (review + agents topic rows; tour Stage 2)
docs/
├── design/main-agent-review-gate.md .. A  (design record)
├── spec/main-agent-review-gate.md .... A  (this file)
├── wiki/workflow.md .................. M  (domain page: rule's home, review line, skip conditions)
└── reference/
    ├── agents.md ..................... M  (topology diagram; auditor + reviewer rows)
    ├── commands.md ................... M  (/commit row)
    ├── skills.md ..................... M  (intro + atomic-verify row)
    └── workflow.md ................... M  (ship-commands bullet; gate paragraph; footnote; §2 subsection)
```

## Outline

```
context/CLAUDE.md
  <completion_claim> — the required review line, its exemptions, what produces the verdict
  ## Workflow / Implement bullet — ad-hoc exits named by the line they emit

context/skills/atomic-verify/SKILL.md
  description — names atomic-reviewer as the dispatched agent
  Claim → required check — the `code I wrote myself is ready` row
  Verification discipline — the superseded ad-hoc bullet removed

context/_partials/review-gate.md
  Already-reviewed guard — new bullet for a tree the claim gate already read
  Dispatch — atomic-reviewer, code mode, staged diff, model override
  Report — the review line beside the totals

context/agents/atomic-reviewer.md
  description — the ad-hoc gate at both exits, model overridden at dispatch

context/commands/atomic-help.md
  review topic row — both ad-hoc exits and their agent
  agents topic row — reviewer's third dispatch, auditor unchanged
  tour Stage 2 — the ad-hoc gate beside the loop gates

docs/reference/agents.md
  dispatch-topology diagram — ad-hoc edges into the reviewer, accDescr updated
  atomic-auditor row — whole-delivery-once only
  atomic-reviewer row — ad-hoc gate at both exits

docs/reference/commands.md
  /commit row — ad-hoc gate agent

docs/reference/skills.md
  intro paragraph — the second opinion outside the loop
  atomic-verify row — the agent and the line

docs/reference/workflow.md
  §2 / Editing directly, outside any verb — the ad-hoc path, where the contract is introduced
  Why custom ship commands? / Independent review bullet — points at it
  review-gate paragraph — points at it
  ✓† footnote — the skip conditions only

docs/wiki/workflow.md
  ad-hoc section — the rule's home, the review line, the four skip conditions
```

## Flows

**Flow: ad-hoc edit reaches a completion claim**

1. User asks for a change in conversation; no lifecycle command runs.
2. Main agent edits code directly and runs the project's verification.
3. Main agent begins the completion claim. `context/CLAUDE.md` is in context, as it is every session, and states that the claim is incomplete without a `review:` line.
4. Main agent dispatches `atomic-reviewer` in code mode with an explicit model override to the session's model, on `git diff HEAD` plus the untracked files `git ls-files --others --exclude-standard` lists, each read in full.
5. Reviewer runs its code-mode passes, re-runs the project's signals, runs `atomic code comments` over the same diff, and returns `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED` with findings.
6. Main agent acts on the verdict: 🔴 fixed before the claim, 🟡 fixed or justified in one line, 🔵 mentioned.
7. Completion claim includes `review: <verdict> (<agent> on <model>)`.
8. A fix after `CHANGES_REQUESTED` changes the tree, so step 4 repeats before the next claim.

**Flow: ship verb gate**

1. User runs `/commit` or an escalation.
2. `review-gate` checks its guards: work from a loop command, a change this gate already read, a tree the claim gate already reviewed with no edits since, or an all-documentation staged set → skip.
3. Otherwise dispatch `atomic-reviewer` in code mode on `git diff --cached`, with an explicit model override.
4. Act on the verdict, then report the totals line and `review: <verdict> (<agent> on <model>)` before the commit message is written.

**Flow: exempt paths**

1. Work produced by `/implement`, `/subagent-implementation`, `/quick-fix`, `/autopilot`, or `/subagent-diagnose` → a reviewer read every checkpoint; no `review:` line, and the loop's own finalize audit stands.
2. A change whose every path is documentation → no `review:` line; the doc-impact step owns it.
3. A reply that reports progress without claiming completion → no line; the contract applies to completion claims.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Contract and both exits: `<completion_claim>` block and Workflow bullet in `context/CLAUDE.md`; delete the superseded `atomic-verify` bullet and rewrite its table row and description; repoint `review-gate` at `atomic-reviewer` with the new guard, the resolved staged diff, and the reported line; restore the ad-hoc clause to `atomic-reviewer`'s description | `context/CLAUDE.md`, `context/skills/atomic-verify/SKILL.md`, `context/_partials/review-gate.md`, `context/agents/atomic-reviewer.md` | atomic-implementer (mode: feature) | 4 | `grep -n 'completion_claim' context/CLAUDE.md` non-empty; `grep -c 'When you wrote the code yourself' context/skills/atomic-verify/SKILL.md` returns 0; `grep -n 'atomic-reviewer' context/_partials/review-gate.md` non-empty; `make -C atomic bundle` succeeds; `atomic validate config` and `atomic validate artifacts` report 0 FAIL |
| 2 | Discovery and documentation: `/atomic-help` `review` row, `agents` row, and tour Stage 2; `docs/reference/agents.md` topology diagram plus auditor and reviewer rows; `docs/reference/commands.md` `/commit` row; `docs/reference/skills.md` intro and `atomic-verify` row; `docs/reference/workflow.md` ship-commands bullet, `§2` subsection, gate paragraph, and `✓†` footnote | `context/commands/atomic-help.md`, `docs/reference/agents.md`, `docs/reference/commands.md`, `docs/reference/skills.md`, `docs/reference/workflow.md` | atomic-implementer (mode: feature) | 5 | The root `CLAUDE.md` `/atomic-help` verification loop prints zero `MISSING:` lines; `grep -rn 'audit:' context/ docs/reference/` shows no ad-hoc-gate claim; `make -C atomic bundle` succeeds |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| The `review:` line gets emitted without the dispatch actually running — the model writes the shape and invents a verdict | med | The line names the agent and the model, so a fabricated one is checkable against the transcript; `atomic-verify`'s iron rule (no claim without a fresh command in this turn) already covers invented evidence, and the dispatch is a visible tool call |
| The line fires on replies that are not completion claims, adding noise to every answer | med | The block scopes it to a claim about code the main agent wrote, exempts documentation-only changes and loop-produced work, and `atomic-verify`'s existing boundaries already separate progress reports from completion claims |
| Two reviews per change, one at each exit | med | The commit gate skips when the claim gate already read the same unchanged tree, so the normal path pays one review; the commit gate fires only when the claim gate did not run or the tree moved after it |
| A caller forgets the model override and the gate silently runs on the frontmatter default | med | The `review:` line names the model, so a downgrade is visible in the claim rather than silent; both dispatch sites state the override |
| `context/CLAUDE.md` grows toward the 200-line adherence ceiling Claude Code documents | med | Accepted, not mitigated away: the block adds 12 lines to a file that loads every session in every repo, and instruction text across `context/` is up on balance (+599 / −401 words). The 15-line cap on the block bounds it, and deleting the `atomic-verify` bullet keeps a second copy from drifting, but the cost is real and is the price of the rule being present when the claim is written |

## Change log

### 2026-09-21 — Caller vocabulary defined on the receiving agent

**What changed:** `context/agents/atomic-reviewer.md` now defines `diff: working`, `diff: staged`, and `intent:` in its code-mode diff step, including the untracked-file read. `context/skills/atomic-verify/SKILL.md` names the scope for the reviewer instead of instructing the caller to read untracked files itself, and the duplicated scope aside is gone from `context/_partials/review-gate.md`. `docs/wiki/workflow.md` returns to the change set, and the criteria that froze two files against `origin/next` are replaced by criteria on what those files say.

**Why:** Five surfaces passed caller keys that the receiving agent never defined, and its diff step read neither untracked files nor the new vocabulary — so a newly added file, the most common shape of an ad-hoc change, could pass the gate unread. The `context/CLAUDE.md` growth risk also claimed a net prose reduction that the delivery does not show; the measured figure replaces it.

### 2026-09-21 — Ad-hoc gate dispatches atomic-reviewer, not atomic-auditor

**What changed:** Both ad-hoc exits now dispatch `atomic-reviewer` in code mode with an explicit per-dispatch model override, and the required claim line is `review: <verdict> (<agent> on <model>)`. `atomic-auditor` and `docs/wiki/workflow.md` are restored to their `origin/next` state, and the `surfaces:` key is gone from the ship-verb dispatch.

**Why:** The earlier choice rested on `atomic-reviewer`'s `model: claude-sonnet-5` frontmatter making the gate Sonnet-only. The Agent tool's `model` parameter takes precedence over that pin, confirmed by dispatching the reviewer with `model: opus` and having it report `claude-opus-5[1m]`. With the premise gone, the reviewer is the better fit: the auditor's four passes degrade to substitutions or no-ops on a single pre-commit diff, at `effort: max`, on a trigger that fires on every completion claim — and its ad-hoc mode had to skip the `atomic code comments` counter that `atomic-reviewer` runs natively.

**Superseded:** `atomic-auditor` dispatched with `diff: working|staged` and `intent:`; the ad-hoc mode section, caller-context keys, and per-mode constraint scoping added to that agent; the `audit:` line; the `surfaces:` dispatch key.

### 2026-09-20 — Initial spec

**What changed:** First version.
