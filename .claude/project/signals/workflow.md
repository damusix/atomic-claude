# workflow

Plan → implement → ship lifecycle. The commands, agents, and skills that orchestrate feature work from spec to committed code.

## Artifacts

**Planning:**

- `commands/atomic-plan.md` — `/atomic-plan` gauges triviality. Trivial → inline spec. Non-trivial → design doc + spec via subagent loop. Optionally invokes `atomic-investigator` and `atomic-strategist`.
- `commands/pressure-test.md` — `/pressure-test` Socratic challenger session. Questions only, no code, no agents. Pairs with `/atomic-plan` as pre-approval gate.
- `commands/atomic-help.md` — `/atomic-help` routing assistant. Reads git state, classifies intent, recommends one next action. Never executes.

**Implementation loop:**

- `commands/subagent-implementation.md` — `/subagent-implementation` reads spec, runs implement→review loop, commits per green iteration. Uses scratchpad at `.claude/.scratchpad/<date>-<desc>/` (BRIEF.md, STATE.md, FOLLOWUPS.md).
- `commands/subagent-diagnose.md` — `/subagent-diagnose <ci|bug>` failure-investigation orchestrator. `ci` mode seeds from failed GitHub Actions run; `bug` mode seeds from freeform symptom. Same scratchpad + investigator + builder/surgeon + reviewer chain. Hard bail at 5 iterations (user-memory-configurable) or 3 consecutive same-failure iterations.
- `commands/_templates/implementer-prompt.md` — runtime prompt partial for the implementer turn. Consumed by both `/subagent-implementation` and `/subagent-diagnose`.
- `commands/_templates/reviewer-prompt.md` — runtime prompt partial for the reviewer turn. Same consumers.

**Ship verbs:**

- `commands/commit-only.md` — stage + commit, nothing else.
- `commands/commit-and-push.md` — commit + push (no PR).
- `commands/commit-and-pr.md` — commit + push + open PR.
- `commands/commit-and-merge.md` — commit pending + merge.
- `commands/commit-and-squash.md` — commit pending + squash branch history.
- `commands/push-only.md` — push existing commits (no commit, no PR).
- `commands/pr-only.md` — open PR for existing commits.
- `commands/merge-to-main.md` — merge branch into base, no squash.
- `commands/squash-only.md` — squash branch commits into one.
- `commands/squash-and-merge.md` — squash + merge to base in one shot.
- `commands/undo-commit.md` — `/undo-commit` soft-resets last commit. Refuses on merge commits, initial commit, already-pushed HEAD.
- `commands/worktree-start.md` — `/worktree-start <branch>` creates isolated `.worktrees/<branch>/`.
- `commands/review-branch.md` — `/review-branch` dispatches `atomic-reviewer` once on `<base>..HEAD`.
- `commands/session-report.md` — `/session-report` writes timestamped note to `.claude/.scratchpad/session-reports/<branch>/`. Ship verbs read all branch reports before commit-message synthesis, delete after successful commit.
- `commands/report-issue.md` — `/report-issue` opens GitHub issue against user's current repo.
- `commands/report-issue-with-atomic.md` — `/report-issue-with-atomic` opens GitHub issue against `damusix/atomic-claude` specifically.

**Agents dispatched by workflow commands:**

- `agents/atomic-builder.md` — feature-checkpoint builder. Cohesion-bounded (one logical slice, any file count). Writes TDD: failing test first, then implementation.
- `agents/atomic-surgeon.md` — surgical 1-2 file edits. Hard refuses 3+ file scope.
- `agents/atomic-investigator.md` — code locator (haiku). Returns `file:line — what` tables. Read-only.
- `agents/atomic-reviewer.md` — diff reviewer. Re-runs typecheck/tests, emits `## Spec compliance` + `## Code quality`, ends with `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.
- `agents/atomic-strategist.md` — heavyweight reasoning (opus). Read-only. "Is this the right approach?" not "Is this code correct?".

**Skills auto-fired during workflow:**

- `skills/atomic-tdd/SKILL.md` — fires on test/implementation work. Enforces TDD discipline.
- `skills/atomic-verify/SKILL.md` — fires on "done", "fixed", "passing", "ready to merge" claims. Runs verification before asserting completion.
- `skills/atomic-commit/SKILL.md` — fires on commit message synthesis. Enforced by all ship verbs.
- `skills/atomic-review/SKILL.md` — fires on review requests.
- `skills/atomic-debug/SKILL.md` — fires on debugging phrases.

## CLI code

None. The workflow domain is purely Claude Code artifacts — commands, agents, skills. No Go packages implement workflow logic directly (the `atomic` binary has no "implement" or "plan" subcommands).

## Docs

- `docs/spec/atomic-plan.md` — `/atomic-plan` behavior contract.
- `docs/spec/session-report.md` — `/session-report` + ship verb integration contract. Ship verbs read `.claude/.scratchpad/session-reports/<branch>/*.md` chronologically, pass to `atomic-commit` as supplemental why-context, delete after successful commit. Exempt verbs: `/pr-only`, `/push-only`, `/merge-to-main`.
- `docs/spec/subagent-diagnose.md` — `/subagent-diagnose` orchestrator contract. Scratchpad layout, investigator → builder/surgeon → reviewer chain, iteration bail conditions.
- `docs/design/diagnose-orchestrators.md` — design rationale for the diagnose orchestrator approach.
- `docs/reference/workflow.md` — canonical lifecycle reference (plan → implement → ship).
- `docs/reference/commands.md` — command roster reference.
- `docs/reference/skills.md` — skill roster reference.
- `docs/reference/agents.md` — agent roster reference.

## Coupling

- **→ bundle**: all ship verb commands are rendered from `templates/commands/` + `templates/shared/` partials. Ship verb changes require editing template sources, running `make render`, then `make bundle`. The `commands/_templates/` prompt partials ship in the bundle and are consumed at runtime.
- **→ bundle**: all workflow agents (`atomic-builder`, `atomic-surgeon`, etc.) ship in the bundle via `agents/atomic-*.md` bundlespec rule. Renaming or adding an agent requires `make bundle`.
- **→ bundle**: all workflow skills ship in the bundle via `skills/atomic-*/` bundlespec rule. Adding a new skill directory requires `make bundle`.
- **→ signals**: ship verbs invoke the `atomic-signals` skill after staged changes touch source files. This is a cross-cutting wiring rule — all ship verbs must fire signals refresh on source-tree changes.
- **→ config**: `/subagent-implementation` Phase 3 `defer` block shells out to `atomic followups add`. Follow-up schema changes (config domain) affect what the defer block produces.
- **→ docs-meta**: ship verbs trigger `atomic-documentation` on staged diffs. If the documentation skill's output contract changes, ship verb templates must be updated.
