---
type: Domain
description: Plan, implement, review, ship. The orchestrator commands, the five subagents they drive, and the discipline skills that fire along the way.
tags: [agents, artifacts, lifecycle]
---

# workflow


## What it does


A long task run in one context degrades. The model accumulates its own reasoning, stops seeing earlier choices as choices, and reviews its own work against the rationalizations that produced it.

This domain is the lifecycle a change moves through and the machinery that keeps that from happening: each step runs in a fresh context, and a gate sits between them. Orchestrator commands never write code themselves. Each dispatches fresh-context subagents, parses their verdicts, and commits between rounds. No Go package implements any of it; the whole domain is prompt artifacts under [`commands/`](../../commands), [`agents/`](../../agents), [`skills/`](../../skills), and [`templates/shared/`](../../templates/shared).


## How it works


Every arrow crosses a context boundary, which is what makes the gate between two steps worth anything.

```mermaid
flowchart LR
    A["/gather-evidence"] --> B["/pressure-test"]
    B --> C["/atomic-plan"]
    C --> D["/challenge-swarm"]
    D --> E["/subagent-implementation"]
    E --> F["/commit"]
    G["/autopilot"] -. drives C through F unattended .-> C
```

Which verb to reach for:

| Situation | Verb |
|---|---|
| A factual hunch the design would rest on | `/gather-evidence` |
| A decision you want challenged in dialogue | `/pressure-test` |
| Non-trivial work with no contract yet | `/atomic-plan` |
| A written design you want attacked from many angles at once | `/challenge-swarm` |
| An approved spec to build | `/subagent-implementation` |
| Known cause, one obvious fix, any file count | `/quick-fix` |
| Unknown cause: CI is red, or a bug reproduces | `/subagent-diagnose ci` / `/subagent-diagnose bug` |
| A branch you want reviewed once, no loop | `/review-branch` |
| Nothing to shipped, unattended | `/autopilot` |
| Ship it | `/commit [push \| pr \| merge \| squash \| squash merge]` |
| Lost | `/atomic-help` |

### The loop

The same engine drives `/subagent-implementation`, `/quick-fix`, `/subagent-diagnose`, and `/autopilot`. It cannot spin quietly: a blocking signal that survives two rounds routes to a human rather than to another iteration.

```mermaid
stateDiagram-v2
    [*] --> Investigate
    Investigate --> Implement
    Implement --> Review
    Review --> Commit: VERDICT PASS
    Review --> Implement: CHANGES_REQUESTED
    Review --> Stuck: same blocking signal, 2 rounds
    Stuck --> Implement: user picks continue
    Stuck --> [*]: user escalates or aborts
    Commit --> Implement: scope remains
    Commit --> Finalize: scope exhausted
    Finalize --> [*]
```

**The orchestrator never implements.** It writes the scratchpad, updates state, commits per `PASS`, and runs final verification. Implementer and reviewer are always separate agents in separate fresh contexts, never combined.

**Nothing the reviewer finds is dropped silently.** Non-blocking 🟡 risk, 🔵 nit, and ❓ question findings are harvested into `FOLLOWUPS.md` even on a `PASS`, and every entry gets an explicit disposition at finalize: `fix-now`, `defer`, `issue`, or `drop`. `/autopilot` is the exception, and inverts it: every finding is fixed in-iteration, so its `FOLLOWUPS.md` ends empty.

### Caps and stop conditions

| Orchestrator | Cap | Early stop |
|---|---|---|
| `/subagent-implementation` | 6 iterations, then ask | Same blocking signal across 2 consecutive `CHANGES_REQUESTED` rounds surfaces `/pressure-test` and `atomic-strategist` and waits for the user |
| `/quick-fix` | 3 iterations, then ask | Escape hatch on approach fork, fuzzy criteria, an unforeseen contract choice, a shifted root cause, or implementer `BLOCKED` / `NEEDS_CONTEXT` |
| `/subagent-diagnose` | `min(memory override, 5)` iterations | 3 consecutive iterations producing the same normalized top-level error |
| `/atomic-plan` spec loop | 5 iterations | none |
| `/autopilot` | Inherits the loop's 6 | Stuck auto-dispatches `atomic-strategist` instead of asking, because the strategist never writes |

**`/quick-fix` routes out on uncertainty, never on file count.** An unknown root cause goes to `/subagent-diagnose`; multiple viable approaches, fuzzy success criteria, or an architectural or contract choice goes to `/atomic-plan`; implementer `BLOCKED` or `NEEDS_CONTEXT` fires the hatch unconditionally. The surgical-versus-feature agent choice inside the loop is cohesion fit, not a scope cap.

### Finalize and ship

Finalize order is fixed, and each step depends on the one before it:

```
atomic-verify → FOLLOWUPS triage → implementation log → /documentation → atomic-auditor → signals refresh → delete scratchpad
```

Docs are written before the signals refresh so a new page exists when the scan runs. `doc-impact` runs before `signals-gate` inside `/commit` for the same reason.


## Where it lives


### Commands

| Path | Role |
|---|---|
| [`commands/gather-evidence.md`](../../commands/gather-evidence.md) | Chases a hypothesis through primary sources; returns `SUPPORTED` / `UNSUPPORTED` / `MIXED` / `INCONCLUSIVE` with a cited trail. Tier rule: community-level-only evidence caps the verdict at `MIXED`. |
| [`commands/pressure-test.md`](../../commands/pressure-test.md) | Socratic challenger session. Questions only, no code, no agents, no artifacts. |
| [`commands/atomic-plan.md`](../../commands/atomic-plan.md) | Triviality gate (trivial / borderline / non-trivial), then design doc plus spec. Non-trivial runs a spec-authoring subagent loop capped at 5 iterations. |
| [`commands/challenge-swarm.md`](../../commands/challenge-swarm.md) | Dispatches 4 to 6 isolated expert lenses at one written artifact, merges their findings into a contradiction map. Report-only, never edits the target. |
| [`commands/subagent-implementation.md`](../../commands/subagent-implementation.md) | The full loop: investigator, spec gate, worktree gate, implement to review to commit, then the finalize ceremony. |
| [`commands/quick-fix.md`](../../commands/quick-fix.md) | The same loop minus the spec gate, worktree gate, and finalize ceremony. Fit gate at entry, escape hatch mid-loop. |
| [`commands/subagent-diagnose.md`](../../commands/subagent-diagnose.md) | Failure-driven loop. `ci` mode seeds from a failed GitHub Actions run, `bug` mode from a freeform symptom. |
| [`commands/autopilot.md`](../../commands/autopilot.md) | Runs plan to loop to ship unattended. The merge method is the only human decision. |
| [`commands/review-branch.md`](../../commands/review-branch.md) | One `atomic-reviewer` pass over `<base>..HEAD`. Pre-flight before `/commit pr` or `/commit merge`. |
| [`commands/commit.md`](../../commands/commit.md) | The single ship verb. Escalation tokens `push`, `pr`, `merge`, `squash`, `squash merge`; no token commits then prompts. |
| [`commands/undo-commit.md`](../../commands/undo-commit.md) | Soft-resets the last commit. Refuses on merge commits, the initial commit, and an already-pushed HEAD. |
| [`commands/session-report.md`](../../commands/session-report.md) | Writes branch-scoped why-context to `.claude/.scratchpad/session-reports/<branch>/`. Read by `/commit`, deleted after a successful commit. |
| [`commands/setup-wiki.md`](../../commands/setup-wiki.md) | Repo bootstrap. Audits [`.gitignore`](../../.gitignore), [`docs/`](..) layout, and [`CLAUDE.md`](../../CLAUDE.md) presence; proposes only what is absent. |
| [`commands/retrospective-learning.md`](../../commands/retrospective-learning.md) | Mines session history for friction and corrections, walks findings one at a time. |
| [`commands/report-issue.md`](../../commands/report-issue.md) | Opens a GitHub issue against the current repo via `gh`. |
| [`commands/report-issue-with-atomic.md`](../../commands/report-issue-with-atomic.md) | Same flow, target hardcoded to `damusix/atomic-claude`, never inferred from `gh repo view` or cwd. |
| [`commands/atomic-help.md`](../../commands/atomic-help.md) | Routes a lost user to one next action. Bare, topic keyword, freeform intent, or `tour`. |
| [`commands/_templates/implementer-prompt.md`](../../commands/_templates/implementer-prompt.md) | Runtime prompt consumed by all three loop orchestrators. Placeholders: `{SCRATCH_PATH}`, `{SPEC_PATH}`, `{ITERATION_SCOPE}`, `{REVIEWER_FEEDBACK}`, `{BASE_SHA}`. |
| [`commands/_templates/reviewer-prompt.md`](../../commands/_templates/reviewer-prompt.md) | Same, for the reviewer. Placeholders: `{SCRATCH_PATH}`, `{SPEC_PATH}`, `{BASE_SHA}`, `{HEAD_SHA}`. |

### Agents

| Path | Role |
|---|---|
| [`agents/atomic-implementer.md`](../../agents/atomic-implementer.md) | Writes the code. `mode: feature` is cohesion-bounded, any file count; `mode: surgical` hard-caps at 2 non-test files and bounces anything larger. Both write TDD and report `## Did` / `## Tests` / `## Signals` / `## Failed`. |
| [`agents/atomic-reviewer.md`](../../agents/atomic-reviewer.md) | Gates each iteration. Code-mode diffs against the spec and verifies TDD signals actually ran; spec-mode reviews a draft spec against its design. Ends with `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`, no third option. |
| [`agents/atomic-auditor.md`](../../agents/atomic-auditor.md) | Gates the whole task once, after the loop goes green. Four passes: cumulative spec compliance, cross-iteration coherence, commit soundness, documentation adherence. Read-only, fresh context. |
| [`agents/atomic-investigator.md`](../../agents/atomic-investigator.md) | Read-only locator. Returns a `file:line — what` table, no prose. Haiku-backed at `effort: low`, so it is cheap enough to dispatch by default. |
| [`agents/atomic-strategist.md`](../../agents/atomic-strategist.md) | Read-only "is this the right approach?" reasoning at `effort: xhigh`. Dispatched only when the loop is stuck. |

### Skills

| Path | Role |
|---|---|
| [`skills/atomic-tdd/`](../../skills/atomic-tdd) | Failing test before production code. Owns writing or changing code once the cause is known. |
| [`skills/atomic-verify/`](../../skills/atomic-verify) | No completion claim without a verification command run in this turn. Invoked explicitly at every finalize. |
| [`skills/atomic-git-discipline/`](../../skills/atomic-git-discipline) | Conventional Commits messages and PR bodies. Every ship path delegates message format here. |
| [`skills/atomic-review/`](../../skills/atomic-review) | One-line-per-finding review comments. Supplies PR title and body tone on the `/commit pr` path. |
| [`skills/atomic-debug/`](../../skills/atomic-debug) | Hypothesis-driven diagnosis of an unknown root cause. Complements `/subagent-diagnose`. |
| [`skills/atomic-visual-options/`](../../skills/atomic-visual-options) | Renders 2 to 4 variants per decision dimension as a throwaway HTML file; the user picks by typing codes. Invoked by `/atomic-plan` when a design question is genuinely visual. |

### Shared partials

Composed at render time. The full pool and its inventory live in [`docs/wiki/bundle.md`](bundle.md); these are the ones that carry workflow behavior.

| Path | Role |
|---|---|
| [`templates/shared/worktree-setup.md`](../../templates/shared/worktree-setup.md) | The whole worktree gate: isolation detection, branch resolution, spec carry-forward, `git worktree add`, `EnterWorktree`, setup and baseline test detection. Composed into `subagent-implementation` and `autopilot`. |
| [`templates/shared/commit-flow.md`](../../templates/shared/commit-flow.md) | Stage, doc-impact, signals-gate, commit, session-report cleanup. |
| [`templates/shared/push-flow.md`](../../templates/shared/push-flow.md), `pr-flow.md`, `merge-flow.md`, `squash-flow.md` | The four escalation paths `/commit` routes into. |
| [`templates/shared/doc-impact.md`](../../templates/shared/doc-impact.md) | Matches the staged diff against the `## Documentation surfaces` table, walks each match with Yes / Later / Remind / Skip. |
| [`templates/shared/signals-gate.md`](../../templates/shared/signals-gate.md) | Docs-only guard, `atomic signals stale`, silent `atomic-wiki-inferrer` dispatch, `atomic wiki mark-dirty`. |
| [`templates/shared/worktree-cleanup-prompt.md`](../../templates/shared/worktree-cleanup-prompt.md) | Offers to remove a linked worktree after a merge. Composed into `merge-flow` only. |
| [`templates/shared/git-safety.md`](../../templates/shared/git-safety.md) | Explicit staging, one git command per Bash call, never `--amend` after a hook failure, no force-push on base. |
| [`templates/shared/report-issue-privacy.md`](../../templates/shared/report-issue-privacy.md) | PII and secret redaction plus a preview-and-confirm gate, composed into both issue commands. |
| [`templates/shared/agent-yagni.md`](../../templates/shared/agent-yagni.md) | The 7-rung simplicity ladder, composed into `atomic-implementer`, `atomic-reviewer`, and `atomic-strategist`. |
| [`templates/shared/agent-implementer-workflow.md`](../../templates/shared/agent-implementer-workflow.md) | The entire `<workflow>` block for `atomic-implementer`; itself composes `agent-search-tooling`, `agent-tdd-signals`, `agent-code-intel`, `agent-where`. |

### Docs

| Path | Role |
|---|---|
| [`docs/reference/workflow.md`](../reference/workflow.md) | Human-facing lifecycle reference. |
| [`docs/reference/commands.md`](../reference/commands.md), `agents.md`, `skills.md` | Roster tables. |
| [`docs/spec/atomic-plan.md`](../spec/atomic-plan.md) | Contract for the triviality gate, design and spec split, and the spec-authoring loop. |
| [`docs/spec/spec-change-tree-flows.md`](../spec/spec-change-tree-flows.md) | Why every spec body carries `## Change tree`, `## Outline`, and `## Flows`. |
| [`docs/spec/subagent-diagnose.md`](../spec/subagent-diagnose.md), [`docs/design/diagnose-orchestrators.md`](../design/diagnose-orchestrators.md) | Contract and rationale for the two diagnose modes. |
| [`docs/spec/quick-fix.md`](../spec/quick-fix.md) | Fit-gate and escape-hatch signals, the no-file-threshold constraint, prompt-reuse contract, 3-iteration cap. |
| [`docs/spec/autopilot.md`](../spec/autopilot.md) | The five autonomous overrides. |
| [`docs/spec/atomic-auditor.md`](../spec/atomic-auditor.md) | The four audit passes and the once-per-task dispatch rule. |
| [`docs/spec/stuck-fix-escalation.md`](../spec/stuck-fix-escalation.md), [`docs/design/stuck-fix-escalation.md`](../design/stuck-fix-escalation.md) | Stuck-fix escalation and reviewer suppression-pattern awareness. |
| [`docs/spec/signals-refresh-timing.md`](../spec/signals-refresh-timing.md), [`docs/design/signals-refresh-timing.md`](../design/signals-refresh-timing.md) | When signals refresh fires in the loop, in autopilot, and in the ship verbs. |
| [`docs/spec/challenge-swarm.md`](../spec/challenge-swarm.md) | Lens roster, workspace layout, isolated dispatch, contradiction-map aggregation. |
| [`docs/spec/document-templates.md`](../spec/document-templates.md), [`docs/design/document-templates.md`](../design/document-templates.md) | The `atomic template <name>` verb these commands seed from. |
| [`docs/spec/session-report.md`](../spec/session-report.md), [`docs/spec/setup-wiki.md`](../spec/setup-wiki.md) | Contracts for those two commands. |
| [`docs/spec/comment-discipline.md`](../spec/comment-discipline.md) | Comment rules the implementer and reviewer both enforce. |
| [`docs/spec/visual-options.md`](../spec/visual-options.md), [`docs/design/visual-options.md`](../design/visual-options.md) | Contract and rationale for the visual-options skill. |


## Constraints


**The spec body is read verbatim by subagents.** `/subagent-implementation`'s currency gate exists because the `BRIEF.md` points fresh agents straight at `docs/spec/<topic>.md`. If a decision in the conversation superseded any part of that body, rewrite the body before dispatching. The test: could a fresh subagent reading only the spec body build something a later decision already cut? Papering over it in the brief does not work.

**Every spec body carries `## Change tree`, `## Outline`, and `## Flows`.** The change tree marks files `A` / `M` / `D`; the outline names the pieces per file as `name — responsibility`, one level of nesting, no signatures; flows are numbered actor-to-step sequences. `## Outline` is what `atomic-reviewer` walks the delivered diff against in its outline pass. An empty section is written `None — <reason>`, never omitted.

**The auditor is dispatched exactly once per task.** A `CHANGES_REQUESTED` verdict earns one more implementer and reviewer iteration, then the run continues regardless of what a second audit would say. Re-auditing turns finalize into an unbounded loop, which never terminates under `/autopilot`.

**Worktree cleanup fires on `merge` and `squash merge`, not on plain `squash`.** `worktree-cleanup-prompt` is composed into `merge-flow` only, so a branch squashed without merging leaves its worktree in place.

**The signals docs-only guard treats artifact markdown as source.** A commit touching only [`docs/`](..) or a top-level `README*` / `CHANGELOG*` / `CONTRIBUTING*` / `CODE_OF_CONDUCT*` / `SECURITY*` / `LICENSE*` skips the refresh. Anything under [`agents/`](../../agents), [`commands/`](../../commands), [`skills/`](../../skills), [`rules/`](../../rules), or [`output-styles/`](../../output-styles), plus [`CLAUDE.md`](../../CLAUDE.md), counts as source, because in a config repo those files are the product.

**Commands hard-stop rather than improvise a document skeleton.** When `atomic template <name>` is unavailable or errors, the command prints `document template unavailable (atomic template <name> failed) — install/update the atomic binary. cannot proceed.` and stops. A missing prompt template stops the same way: `implementer/reviewer prompt template not found at commands/_templates/<file>. cannot proceed.`

**Refusals to expect, quoted exactly:**

- No spec for non-trivial work: `Run /atomic-plan first. I need an approved spec at docs/spec/<topic>.md before launching the implementation loop.`
- Worktree branch collision: `branch <name> already exists. pick a different name or checkout existing.`
- Concurrent diagnose run: `scratchpad <path> already exists; rm -rf it or pick a different topic suffix.`
- Sandbox blocks worktree creation: `sandbox blocked worktree creation. working in place.` The run then continues in place rather than failing.

**`/autopilot` avoids `rm` and shell chaining mid-run.** Both trigger permission prompts that stall an unattended session. Scratch is quarantined into `tmp/trash/` and deleted once, at Phase 6.


## Coupling


**bundle** owns everything under [`commands/`](../../commands) and [`agents/`](../../agents). Both are generated: edit `templates/commands/<verb>.md` or `templates/agents/<name>.md`, never the output. Run `make render` then `make -C atomic bundle`, in that order, and commit both. [`commands/_templates/`](../../commands/_templates) ships inside the embedded bundle.

**signals** owns refresh timing. The short version for a session working here: the implementation phase refreshes once at finalize over `<loop-base>..HEAD`, and the ship verbs are the ad-hoc fallback that skips when the loop already refreshed. Full contract in [`docs/spec/signals-refresh-timing.md`](../spec/signals-refresh-timing.md).

**config** supplies four binary verbs these commands call:

| Verb | Called by | On failure |
|---|---|---|
| `atomic template <name>` | `/atomic-plan`, `/subagent-implementation`, `/subagent-diagnose`, `/quick-fix`, `/session-report` | Hard stop. Never improvise the skeleton from memory. |
| `atomic repo init` | Every command that creates a scratchpad or worktree | Best-effort, silent skip. |
| `atomic followups add` | Finalize, on a `defer` disposition | Surface the error, retry once, then ask the user to run it manually. |
| `atomic code index` / `sync` | Orchestrators only, at task start and after each green commit | Silent skip; subagents fall back to `sg` and `grep`. |

Config also owns the paths this domain writes to: `.claude/.scratchpad/<date>-<topic>/`, `.claude/.scratchpad/session-reports/<branch>/`, `.claude/project/followups/<id>.md`, and `.claude/worktrees/<branch>/`.

**docs-meta** owns `/documentation` and the `atomic-documentation` skill, which finalize invokes and which `doc-impact` calls per matched surface. Design docs and specs are written in the `atomic-writing` voice.

**doctor** owns `atomic validate spec` and `atomic validate config`, which finalize runs when the change touched `docs/spec/**`, `docs/design/**`, or a bundled artifact. It also owns `cliusage.go`, the source of truth for the [`atomic`](../../atomic) command surface, which is the reverse dependency worth knowing: every `atomic template <name>` variant a command cites must have a matching `cliusage.go` entry or `atomic validate artifacts` flags the citation as an unknown verb.

**code-intel** is queried, never driven, from inside the loop. The orchestrator owns index freshness; subagents never trigger indexing.

Contracts that change in lockstep:

- The two prompt templates in [`commands/_templates/`](../../commands/_templates) are consumed by three orchestrators. A placeholder change updates `subagent-implementation`, `quick-fix`, and `subagent-diagnose` together.
- The YAGNI ladder in [`templates/shared/agent-yagni.md`](../../templates/shared/agent-yagni.md) is kept verbatim identical to the ladder in [`CLAUDE.md`](../../CLAUDE.md)'s Principles block. [`CLAUDE.md`](../../CLAUDE.md) is not rendered, so that duplication is manual.
- The ship verbs must agree on message format, worktree detection, and the signals gate. Changing one path's behavior on a shared concern means changing all of them.
- A new command, agent, or skill needs a row in the `/atomic-help` topic table before it is done.
