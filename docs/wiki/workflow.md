---
type: Domain
description: Plan → gather-evidence → implement → review → ship → retrospective lifecycle; /autopilot runs it hands-off; /commit is the unified ship verb.
---

# workflow

## What it does

Plan → implement → ship lifecycle. Commands, agents, and skills that orchestrate feature work from spec to committed code. `/atomic-plan` produces specs; `/subagent-implementation` runs the implement→review loop with worktree isolation via the `worktree-setup` partial; `/commit` covers all ship escalation paths (push/PR/merge/squash) in one verb with automatic signals refresh and doc-impact checks.

## Artifacts

**Planning:**

- [`commands/gather-evidence.md`](../../commands/gather-evidence.md) — pre-design evidence gathering against primary sources (context7, official docs, source code, `atomic code explore`/`callers`/`impact`); returns `SUPPORTED / UNSUPPORTED / MIXED / INCONCLUSIVE` with cited evidence trail
- [`commands/atomic-plan.md`](../../commands/atomic-plan.md) — gauges triviality; trivial → inline spec; non-trivial → `docs/design/<topic>.md` + `docs/spec/<topic>.md` via subagent loop; optionally dispatches `atomic-investigator` and `atomic-strategist`; in the Diverge phase, when a design question is genuinely visual (layout, color, spacing, visual hierarchy, diagram shape), invokes `atomic-visual-options` skill just-in-time to render 2–4 variants as a throwaway HTML artifact; chosen codes are recorded in the design doc
- [`commands/pressure-test.md`](../../commands/pressure-test.md) — Socratic challenger session; questions only, no code, no agents; paired with `/atomic-plan` as pre-approval gate or surfaced by the stuck-fix escalation in `/subagent-implementation`
- [`commands/atomic-help.md`](../../commands/atomic-help.md) — routing assistant; reads intent, recommends one next action; never executes
- [`commands/atomic-setup.md`](../../commands/atomic-setup.md) — repo bootstrap; audits [`.gitignore`](../../.gitignore), [`docs/`](../../docs) layout, [`CLAUDE.md`](../../CLAUDE.md) presence; proposes only what is absent, never overwrites (axiom 3)
- [`commands/atomic-improve.md`](../../commands/atomic-improve.md) — session retrospective audit; mines session history for corrections, friction, and atomic-meta misbehavior

**Implementation loop:**

- [`commands/subagent-implementation.md`](../../commands/subagent-implementation.md) — orchestrates implement→review loop; dispatches `atomic-investigator` first for surface-area mapping; writes `BRIEF.md`, `STATE.md`, `FOLLOWUPS.md` to `.claude/.scratchpad/<date>-<topic>/`; Phase 1 captures `git rev-parse HEAD` before iteration 1 and records it in `STATE.md` as `Loop base SHA`; loops `atomic-implementer` + `atomic-reviewer` until `VERDICT: PASS`; commits per green iteration via `atomic-commit` skill; includes worktree-setup partial (interactive: asks user; hands-off: auto-creates); Phase 2 Step C carries stuck-fix escalation (2 consecutive `CHANGES_REQUESTED` on same root signal → surfaces `/pressure-test` + `atomic-strategist` RCA offer, user-gated); 6-iteration soft-stop is the outer bound; Phase 3 runs `atomic-verify`, surfaces `FOLLOWUPS.md` for user disposition, writes implementation log to spec, invokes `/documentation`, then runs a once-at-finalize range-scoped signals refresh (`atomic signals stale` exit 1 → dispatch `atomic-signals-inferrer` with `mode: silent`, `first_run: false`, `changed_range: <loop-base>..HEAD`; committed as `chore(signals): refresh after <topic>`)
- [`commands/autopilot.md`](../../commands/autopilot.md) — autonomous end-to-end delivery; takes `<task | issue#> [merge-verb]`; runs plan (no approval gate) → worktree-setup (auto-create, hands-off) → `/subagent-implementation` loop with overrides: every finding addressed in-iteration (`FOLLOWUPS.md` ends empty), `atomic-strategist` auto-dispatched on stuck (accepted autonomy cost), spec currency-clean before each dispatch; Phase 4 runs a range-scoped signals refresh (same staleness-gated `changed_range` pattern as `/subagent-implementation` finalize) before the Phase 5 ship gate; the ship verb's signals-gate then sees a fresh file and skips (documented no-op); Ship gate is the only human decision; `atomic code index` runs automatically on cold index (no prompt).
- [`commands/subagent-diagnose.md`](../../commands/subagent-diagnose.md) — failure-investigation orchestrator; `ci` mode seeds from failed GitHub Actions run; `bug` mode seeds from freeform symptom; same investigator + implementer + reviewer chain as `/subagent-implementation`
- [`commands/review-branch.md`](../../commands/review-branch.md) — dispatches `atomic-reviewer` once on `<base>..HEAD`; no loop
- [`commands/session-report.md`](../../commands/session-report.md) — writes timestamped why-context note to `.claude/.scratchpad/session-reports/<branch>/`; consumed by `/commit` before commit message synthesis, deleted after successful commit
- [`commands/_templates/implementer-prompt.md`](../../commands/_templates/implementer-prompt.md) — runtime prompt partial consumed by `/subagent-implementation` and `/subagent-diagnose`; placeholders: `{SCRATCH_PATH}`, `{SPEC_PATH}`, `{ITERATION_SCOPE}`, `{REVIEWER_FEEDBACK}`, `{BASE_SHA}`
- [`commands/_templates/reviewer-prompt.md`](../../commands/_templates/reviewer-prompt.md) — runtime prompt partial consumed by the same orchestrators; includes suppression-pattern check (Step 5)

**Ship verbs:**

- [`commands/commit.md`](../../commands/commit.md) — unified ship verb; reads `$ARGUMENTS` for escalation tokens (`push`, `pr`, `merge`, `squash`, `squash merge`); with no token commits then prompts interactively via `AskUserQuestion`; delegates message format to `atomic-commit` skill; runs doc-impact check then signals-gate refresh — signals-gate first checks staged set: if empty (post-merge/squash) falls through to staleness check; if non-empty and every staged path is documentation (under [`docs/`](../../docs), or top-level `README*`/`CHANGELOG*`/`CONTRIBUTING*` etc.) skips refresh entirely; artifact `.md` files under [`agents/`](../../agents)/[`commands/`](../../commands)/[`skills/`](../../skills)/[`rules/`](../../rules)/[`output-styles/`](../../output-styles) and [`CLAUDE.md`](../../CLAUDE.md) count as source, not docs; push path uses `git push -u origin <branch>` or `git push`; PR path creates via `gh pr create`; merge path prefers remote path (`gh pr merge`) when PR is open; squash path collapses via `git reset --soft`; detects worktrees on merge/squash and asks to delete
- [`commands/undo-commit.md`](../../commands/undo-commit.md) — soft-resets last commit; refuses on merge commits, initial commit, already-pushed HEAD

**Agents dispatched by workflow commands:**

- [`agents/atomic-implementer.md`](../../agents/atomic-implementer.md) — dual-mode implementation agent; `feature` mode: cohesion-bounded (one logical slice, any file count; bounces cross-cutting or ambiguous scope); `surgical` mode: hard cap of 2 files (bounces anything larger); both modes write TDD (failing test first, confirm fails, implement, confirm green); both report `## Did / ## Tests / ## Signals / ## Failed` block; mode declared by orchestrator in dispatch prompt
- [`agents/atomic-reviewer.md`](../../agents/atomic-reviewer.md) — diff reviewer; verifies TDD signals were run; emits `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`; includes suppression-pattern check
- [`agents/atomic-investigator.md`](../../agents/atomic-investigator.md) — read-only code locator; returns `file:line — what` tables; dispatched first in `/subagent-implementation` Phase 0 to scope the surface area before any implementation; Haiku-backed
- [`agents/atomic-strategist.md`](../../agents/atomic-strategist.md) — Opus-powered, read-only; "is this the right approach?" reasoning; dispatched when stuck-fix escalation fires (user-gated in `/subagent-implementation`; auto-dispatched in `/autopilot`)

**Skills auto-fired during workflow:**

- [`skills/atomic-tdd/`](../../skills/atomic-tdd) — fires on test/implementation work; TDD discipline
- [`skills/atomic-verify/`](../../skills/atomic-verify) — fires on "done", "fixed", "passing", "ready to merge" claims; invoked by `/subagent-implementation` Phase 3 orchestrator and by `/commit` merge preflight
- [`skills/atomic-commit/`](../../skills/atomic-commit) — fires on commit message synthesis; invoked by all ship paths in `/commit` and by `/subagent-implementation` Step D per green iteration
- [`skills/atomic-review/`](../../skills/atomic-review) — fires on review requests; provides PR title/body tone guidance invoked by the PR path in `/commit`
- [`skills/atomic-debug/`](../../skills/atomic-debug) — fires on debugging/failure-investigation language; complements `/subagent-diagnose`
- [`skills/atomic-visual-options/`](../../skills/atomic-visual-options) — fires on visual comparison requests ("show me a few options", "mock up some variants", "let me see this side by side", "compare these layouts"); renders 2–4 side-by-side variants per decision dimension as a single throwaway self-contained HTML file; user picks by typing terminal codes (e.g. `A2 B3`); also invoked just-in-time by `/atomic-plan` when a design question passes the see-it-over-read-it gate (layout, color, spacing, visual hierarchy, diagram shape — not conceptual or text decisions)

**YAGNI partial ([`templates/shared/agent-yagni.md`](../../templates/shared/agent-yagni.md)):**

New shared partial (20th in the shared pool) composing the Simplicity-first (YAGNI) 7-step ladder into `atomic-implementer`, `atomic-reviewer`, and `atomic-strategist`. The ladder is kept verbatim in sync with the same text in [`CLAUDE.md`](../../CLAUDE.md)'s Principles block — edit both together (CLAUDE.md is not rendered, so the duplication is manual). Ladder stops at first hit: (1) skip if not needed; (2) stdlib; (3) native platform feature; (4) installed dep; (5) existing codebase solution; (6) one-liner; (7) minimum code that fully solves the problem.

## CLI code

None. The workflow domain is purely Claude Code artifacts (commands, agents, skills, shared partials). No Go packages implement workflow logic.

## Docs

- [`docs/reference/workflow.md`](../../docs/reference/workflow.md) — canonical lifecycle reference (plan → implement → ship)
- [`docs/reference/commands.md`](../../docs/reference/commands.md) — command roster reference
- [`docs/reference/agents.md`](../../docs/reference/agents.md) — agent roster reference
- [`docs/reference/skills.md`](../../docs/reference/skills.md) — skill roster reference
- [`docs/spec/stuck-fix-escalation.md`](../../docs/spec/stuck-fix-escalation.md) — contract for stuck-fix escalation + suppression-pattern awareness in `/subagent-implementation` and `atomic-reviewer`
- [`docs/design/stuck-fix-escalation.md`](../../docs/design/stuck-fix-escalation.md) — design rationale for the escalation mechanic
- [`docs/spec/signals-refresh-timing.md`](../../docs/spec/signals-refresh-timing.md) — contracts when signals refresh fires in the implement loop (C3: `/subagent-implementation` finalize) and autopilot (C4: pre-ship), with `changed_range` scoping; the ship-verb gate (C2: docs-only guard) is defined here too
- [`docs/design/signals-refresh-timing.md`](../../docs/design/signals-refresh-timing.md) — design rationale: why `atomic signals stale` is the coordinator (no marker file needed); docs-only classification rules; SHA-range scoping approach (no Go change)

## Coupling

- → **bundle**: all commands under [`commands/`](../../commands) are rendered from [`templates/commands/`](../../templates/commands) + [`templates/shared/`](../../templates/shared) partials via `make render`; the worktree-setup partial at [`templates/shared/worktree-setup.md`](../../templates/shared/worktree-setup.md) is composed into `subagent-implementation` and `autopilot`; all agents under [`agents/`](../../agents) are rendered from [`templates/agents/`](../../templates/agents); [`commands/_templates/`](../../commands/_templates) partials ship in the embedded bundle (require `go:embed all:bundle`); any template change requires `make render` then `make -C atomic bundle`
- → **signals**: the implementation phase owns the refresh — `/subagent-implementation` finalize and `/autopilot` Phase 4 dispatch `atomic-signals-inferrer` in silent mode with `changed_range: <loop-base>..HEAD`, staleness-gated, committed as `chore(signals)`. `/commit` is the ad-hoc fallback via signals-gate partial: skips for docs-only staged sets; runs `atomic signals stale` on all other paths; exit 1 → dispatch; the gate returns exit 0 (skip) when the loop already refreshed. All ship escalation paths in `/commit` carry the same signals-gate block; adding new ship paths must wire the same gate. Docs-only classification: paths under [`docs/`](../../docs) or top-level conventional filenames (`README*`, `CHANGELOG*`, `CONTRIBUTING*`, `CODE_OF_CONDUCT*`, `SECURITY*`, `LICENSE*`) — any other path is source
- → **config**: `/subagent-implementation` Phase 3 defers follow-ups via `atomic followups add` (config domain); session-report scratchpad at `.claude/.scratchpad/session-reports/<branch>/` is consumed and deleted by `/commit`; worktrees at `.worktrees/<branch>/` are detected and cleaned up on merge/squash
- → **docs-meta**: `/subagent-implementation` Phase 3 invokes `/documentation`; `/commit` runs doc-impact check against `## Documentation surfaces` table before committing; new commands or agents in this domain require updates to [`docs/reference/workflow.md`](../../docs/reference/workflow.md) and the `/atomic-help` topic table
