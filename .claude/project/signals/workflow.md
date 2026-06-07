# workflow

## What it does

Plan → implement → ship lifecycle. Commands, agents, and skills that orchestrate feature work from spec to committed code. `/atomic-plan` produces specs; `/subagent-implementation` runs the implement→review loop; ship verbs commit/push/PR/merge with automatic signals refresh and doc-impact checks.

## Artifacts

**Planning:**

- [`commands/gather-evidence.md`](../../../commands/gather-evidence.md) — `/gather-evidence [<hypothesis> | @<path>]` pre-design evidence gathering. Chases a hunch through primary sources (context7, official docs, source code, ast-grep, run-it experiments) before any spec is written. Returns `SUPPORTED / UNSUPPORTED / MIXED / INCONCLUSIVE` with cited evidence trail. Sits before `/pressure-test` in the hunch → plan pipeline.
- [`commands/atomic-plan.md`](../../../commands/atomic-plan.md) — `/atomic-plan` gauges triviality. Trivial → inline spec. Non-trivial → design doc + spec via subagent loop. Optionally invokes `atomic-investigator` and `atomic-strategist`.
- [`commands/pressure-test.md`](../../../commands/pressure-test.md) — `/pressure-test` Socratic challenger session. Questions only, no code, no agents. Pairs with `/atomic-plan` as pre-approval gate.
- [`commands/autopilot.md`](../../../commands/autopilot.md) — `/autopilot <task|issue#> [merge-verb]` autonomous end-to-end delivery. Drives plan→implement→review→ship without pausing except for the merge-method question. Key behaviors: always uses the subagent loop, addresses all reviewer findings in-iteration (scratchpad `FOLLOWUPS.md` ends empty), auto-dispatches `atomic-strategist` for RCA when stuck (accepted cost of autonomy), spec is currency-clean before every builder dispatch. **Scratch hygiene**: experiments quarantined under `tmp/trash/` (never `rm` mid-run); subagent briefs include the quarantine instruction; one deliberate `rm -rf tmp/trash/` at the very end.
- [`commands/atomic-help.md`](../../../commands/atomic-help.md) — `/atomic-help` routing assistant. Reads git state, classifies intent, recommends one next action. Never executes.
- [`commands/atomic-setup.md`](../../../commands/atomic-setup.md) — `/atomic-setup` repo bootstrap. Audits [`.gitignore`](../../../.gitignore), [`docs/`](../../../docs) layout, [`CLAUDE.md`](../../../CLAUDE.md) presence. Checks for `@.claude/project/signals.md` @-ref (not `deterministic-signals.md`). Creates `.signalsignore` and `signals-steering.md` scaffolds if missing. Proposes only what's absent — never overwrites.

**Implementation loop:**

- [`commands/subagent-implementation.md`](../../../commands/subagent-implementation.md) — `/subagent-implementation` reads spec, runs implement→review loop, commits per green iteration. Uses scratchpad at `.claude/.scratchpad/<date>-<desc>/` (BRIEF.md, STATE.md, FOLLOWUPS.md). Phase 2 / Step C carries a **stuck-fix escalation** default: after 2 consecutive `CHANGES_REQUESTED` rounds on the same blocking signal (same root failure, not verbatim match), surfaces a copyable `/pressure-test @<spec>` line + offer to dispatch `atomic-strategist` for RCA — surfaced, never auto-invoked (axiom 3). Separate 6-iteration soft-stop remains the outer bound.
- [`commands/subagent-diagnose.md`](../../../commands/subagent-diagnose.md) — `/subagent-diagnose <ci|bug>` failure-investigation orchestrator. `ci` mode seeds from failed GitHub Actions run; `bug` mode seeds from freeform symptom. Same scratchpad + investigator + builder/surgeon + reviewer chain. Hard bail at 5 iterations (user-memory-configurable) or 3 consecutive same-failure iterations. On same-failure bail, surfaces the same stuck-fix escalation block (`/pressure-test` + `atomic-strategist` RCA options, never auto-dispatched).
- [`commands/_templates/implementer-prompt.md`](../../../commands/_templates/implementer-prompt.md) — runtime prompt partial for the implementer turn. Consumed by both `/subagent-implementation` and `/subagent-diagnose`.
- [`commands/_templates/reviewer-prompt.md`](../../../commands/_templates/reviewer-prompt.md) — runtime prompt partial for the reviewer turn. Same consumers. Step 5 adds a **suppression-pattern check**: flags error-catching constructs (try/catch, `.catch(() => {})`, null-guards) added solely to silence a failure without investigation (no new logging, no test exercising the failure path). Severity 🟡 by default; 🔴 when same suppression appears on the same error across 2+ iterations. Judgment call — legitimate defensive code is not flagged.

**Ship verbs:**

- [`commands/commit-only.md`](../../../commands/commit-only.md) — stage + commit, nothing else.
- [`commands/commit-and-push.md`](../../../commands/commit-and-push.md) — commit + push (no PR).
- [`commands/commit-and-pr.md`](../../../commands/commit-and-pr.md) — commit + push + open PR.
- [`commands/commit-and-merge.md`](../../../commands/commit-and-merge.md) — commit pending + merge.
- [`commands/commit-and-squash.md`](../../../commands/commit-and-squash.md) — commit pending + squash branch history.
- [`commands/push-only.md`](../../../commands/push-only.md) — push existing commits (no commit, no PR).
- [`commands/pr-only.md`](../../../commands/pr-only.md) — open PR for existing commits.
- [`commands/merge-to-main.md`](../../../commands/merge-to-main.md) — merge branch into base, no squash.
- [`commands/squash-only.md`](../../../commands/squash-only.md) — squash branch commits into one.
- [`commands/squash-and-merge.md`](../../../commands/squash-and-merge.md) — squash + merge to base in one shot.
- [`commands/undo-commit.md`](../../../commands/undo-commit.md) — `/undo-commit` soft-resets last commit. Refuses on merge commits, initial commit, already-pushed HEAD.
- [`commands/worktree-start.md`](../../../commands/worktree-start.md) — `/worktree-start <branch>` creates isolated `.worktrees/<branch>/`.
- [`commands/review-branch.md`](../../../commands/review-branch.md) — `/review-branch` dispatches `atomic-reviewer` once on `<base>..HEAD`.
- [`commands/session-report.md`](../../../commands/session-report.md) — `/session-report` writes timestamped note to `.claude/.scratchpad/session-reports/<branch>/`. Ship verbs read all branch reports before commit-message synthesis, delete after successful commit.
- [`commands/report-issue.md`](../../../commands/report-issue.md) — `/report-issue` opens GitHub issue against user's current repo.
- [`commands/report-issue-with-atomic.md`](../../../commands/report-issue-with-atomic.md) — `/report-issue-with-atomic` opens GitHub issue against `damusix/atomic-claude` specifically.
- [`commands/atomic-improve.md`](../../../commands/atomic-improve.md) — `/atomic-improve [$ARGUMENTS]` session retrospective audit. Mines `.jsonl` session history and current conversation for corrections, friction, and atomic-meta misbehavior. Cross-references installed artifacts via `atomic doctor --json` and `atomic validate --json`. Presents up to 15 indexed findings across 13 priority tiers (drifted > re-surface > atomic-meta > targeted > critical > promotion > content placement > improvement > technique > maintenance > reinforcement > new skill > user coaching). Persists run logs to `~/.claude/.atomic/improve-runs/<ts>.json` and learnings to `~/.claude/.atomic/improve-learnings.md`. Never auto-commits — suggests `/commit-only` at the end.

**Agents dispatched by workflow commands:**

- [`agents/atomic-builder.md`](../../../agents/atomic-builder.md) — feature-checkpoint builder. Cohesion-bounded (one logical slice, any file count). Writes TDD: failing test first, then implementation.
- [`agents/atomic-surgeon.md`](../../../agents/atomic-surgeon.md) — surgical 1-2 file edits. Hard refuses 3+ file scope.
- [`agents/atomic-investigator.md`](../../../agents/atomic-investigator.md) — code locator (haiku). Returns `file:line — what` tables. Read-only.
- [`agents/atomic-reviewer.md`](../../../agents/atomic-reviewer.md) — diff reviewer. Re-runs typecheck/tests, emits `## Spec compliance` + `## Code quality` (includes suppression-pattern findings from Step 5), ends with `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- [`agents/atomic-strategist.md`](../../../agents/atomic-strategist.md) — heavyweight reasoning (opus). Read-only. "Is this the right approach?" not "Is this code correct?".

**Skills auto-fired during workflow:**

- [`skills/atomic-tdd/SKILL.md`](../../../skills/atomic-tdd/SKILL.md) — fires on test/implementation work. Enforces TDD discipline.
- [`skills/atomic-verify/SKILL.md`](../../../skills/atomic-verify/SKILL.md) — fires on "done", "fixed", "passing", "ready to merge" claims. Runs verification before asserting completion.
- [`skills/atomic-commit/SKILL.md`](../../../skills/atomic-commit/SKILL.md) — fires on commit message synthesis. Enforced by all ship verbs.
- [`skills/atomic-review/SKILL.md`](../../../skills/atomic-review/SKILL.md) — fires on review requests.
- [`skills/atomic-debug/SKILL.md`](../../../skills/atomic-debug/SKILL.md) — fires on debugging phrases.

## CLI code

None. The workflow domain is purely Claude Code artifacts — commands, agents, skills. No Go packages implement workflow logic directly (the [`atomic`](../../../atomic) binary has no "implement" or "plan" subcommands).

## Docs

- [`docs/spec/atomic-plan.md`](../../../docs/spec/atomic-plan.md) — `/atomic-plan` behavior contract.
- [`docs/spec/session-report.md`](../../../docs/spec/session-report.md) — `/session-report` + ship verb integration contract. Ship verbs read `.claude/.scratchpad/session-reports/<branch>/*.md` chronologically, pass to `atomic-commit` as supplemental why-context, delete after successful commit. Exempt verbs: `/pr-only`, `/push-only`, `/merge-to-main`.
- [`docs/spec/subagent-diagnose.md`](../../../docs/spec/subagent-diagnose.md) — `/subagent-diagnose` orchestrator contract. Scratchpad layout, investigator → builder/surgeon → reviewer chain, iteration bail conditions.
- [`docs/spec/stuck-fix-escalation.md`](../../../docs/spec/stuck-fix-escalation.md) — spec for stuck-fix escalator + suppression-pattern awareness. Defines the 2-round threshold, surfaced-never-auto-invoked rule, suppression-pattern severity tiers, and `/subagent-diagnose` bail enrichment. Closes GitHub issue #29.
- [`docs/design/stuck-fix-escalation.md`](../../../docs/design/stuck-fix-escalation.md) — design rationale: approach A (escalator in orchestrator + suppression flag in reviewer) selected over a new skill (axiom 2) or verbatim port of the diagnose detector.
- [`docs/design/diagnose-orchestrators.md`](../../../docs/design/diagnose-orchestrators.md) — design rationale for the diagnose orchestrator approach.
- [`docs/reference/workflow.md`](../../../docs/reference/workflow.md) — canonical lifecycle reference (plan → implement → ship). Updated to note stuck-fix escalation as a loop default.
- [`docs/reference/commands.md`](../../../docs/reference/commands.md) — command roster reference.
- [`docs/reference/skills.md`](../../../docs/reference/skills.md) — skill roster reference.
- [`docs/reference/agents.md`](../../../docs/reference/agents.md) — agent roster reference.

## Coupling

- **→ bundle**: all ship verb commands are rendered from [`templates/commands/`](../../../templates/commands) + [`templates/shared/`](../../../templates/shared) partials. Ship verb changes require editing template sources, running `make render`, then `make bundle`. The [`commands/_templates/`](../../../commands/_templates) prompt partials ship in the bundle and are consumed at runtime.
- **→ bundle**: all workflow agents (`atomic-builder`, `atomic-surgeon`, etc.) ship in the bundle via `agents/atomic-*.md` bundlespec rule. Renaming or adding an agent requires `make bundle`.
- **→ bundle**: all workflow skills ship in the bundle via `skills/atomic-*/` bundlespec rule. Adding a new skill directory requires `make bundle`.
- **→ signals**: ship verbs dispatch `atomic-signals-inferrer` in silent mode (via signals-gate partial) after staged changes touch source files. This is a cross-cutting wiring rule — all ship verbs must fire signals refresh on source-tree changes.
- **→ config**: `/subagent-implementation` Phase 3 `defer` block shells out to `atomic followups add`. Follow-up schema changes (config domain) affect what the defer block produces.
- **→ docs-meta**: ship verbs trigger `atomic-documentation` on staged diffs. If the documentation skill's output contract changes, ship verb templates must be updated.
