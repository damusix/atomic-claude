# Workflow

Atomic Claude follows a lifecycle: set up the repo, plan the work, implement it, fix what breaks, ship it, and learn from the session.


## 0. Set up your repo

Before your first session in a new project, two commands teach Claude what it is looking at:

```
/atomic-setup
/refresh-signals
```

`/atomic-setup` audits your repo for missing conventions (`.gitignore` entries, `docs/` layout, starter `CLAUDE.md`) and proposes only what is missing. `/refresh-signals` scans the project and generates the [signals files](/reference/signals-workflow) that give Claude a map of your framework, build commands, and project structure.

You only need to do this once per repo. Signals refresh automatically after that — ship commands re-scan whenever source files change.

For deeper structural queries, run `atomic code index` to build a symbol graph of the project. Once indexed, you can ask `atomic code explore "<question>"` for a one-shot context digest, and the implementation agents query the graph for callers and blast radius instead of grepping. This is also a one-time setup step. `atomic code sync` keeps the index current, and the ship commands and `/refresh-signals` run it for you. See the [code-intel reference](/reference/code-intel).

If you work across several repos in one realm — a folder of services, a set of libraries, your client projects — a wiki gives Claude a map of how they relate, one level up from per-repo signals. Set one up with `/refresh-wiki`; see [wiki workflow](/reference/wiki-workflow).


## 1. Plan

```
/atomic-plan
```

You and Claude produce a spec together. For small tasks, this is an inline checkpoint table in `docs/spec/`. For larger work, Claude writes a design doc first (`docs/design/`) and then derives the spec from it. Nothing gets implemented until you approve the plan.

If the plan rests on an unverified hunch, `/atomic-plan` will suggest `/gather-evidence` before continuing — you decide whether to gather first or proceed at risk.


### Verify hunches (optional)

```
/gather-evidence "<hypothesis>"
```

When the work ahead rests on a factual hunch ("library X supports Y", "our codebase already has a Z pattern", "approach A is faster than B"), `/gather-evidence` chases the claim through primary sources before any spec is written. It pulls from context7, official docs, source code, ast-grep, and run-it experiments — citing every piece of evidence with its source tier. Hearsay from blogs or forums cannot produce a `SUPPORTED` verdict.

Returns one of `SUPPORTED`, `UNSUPPORTED`, `MIXED`, or `INCONCLUSIVE` with a clear recommendation: proceed to `/atomic-plan`, abandon, refine the hypothesis, or dig deeper. Skip this step when the work is grounded in code you've already read — but reach for it the moment you catch yourself assuming.


## 2. Implement

```
/subagent-implementation
```

Claude reads the approved spec and runs an autonomous implement-then-review loop. A builder agent writes code (failing test first), a reviewer agent checks it, and each passing checkpoint gets committed automatically. Non-blocking findings (risks, nits, questions) accumulate in a ledger that you review at the end — nothing gets silently dropped. When the loop gets stuck — the same failure surviving two rounds of fixes, or the reviewer flagging error-swallowing patches that dodge the bug instead of fixing it — it stops grinding and surfaces a root-cause path: a pressure-test prompt or a read-only strategist analysis you can run, rather than piling on more suppression.

If the project is indexed, the loop uses the code-intel graph throughout. It indexes the project at the start of the task, the investigator leads with `atomic code explore` to scope each surface, the reviewer checks blast radius with `atomic code impact`, and the orchestrator runs `atomic code sync` after each committed checkpoint so the graph reflects the latest code. When no index is present the agents fall back to plain search, so the loop runs either way.


### Hands-off: /autopilot

```
/autopilot <task | issue#> [merge-verb]
```

When you trust the system to drive, `/autopilot` runs the whole lifecycle — plan, the implement-then-review loop, and ship — from a task description or a GitHub issue number, with one decision left to you: how to merge. It always uses the same subagent loop, but with three autonomous defaults. Every reviewer finding is fixed as it goes rather than deferred. When the loop gets stuck, it dispatches the read-only strategist for root-cause analysis on its own instead of waiting for you. And it keeps the spec current the whole way, so a fresh subagent never reads stale scope. The only decision is the merge method at the end — pass a merge token (`/autopilot 29 "squash merge"`) to skip even that. It also keeps experiments in a gitignored scratch folder rather than deleting them mid-run, so it never stops to ask permission for a stray `rm`; it clears that folder once when the run finishes. Reach for the interactive verbs above when you want approval gates; reach for this when you don't.


### What the loop costs

The loop trades tokens for verification: every checkpoint is implemented by one agent and re-checked by another, and that second pass is not free. Implementation and review run on Sonnet subagents; log reading and CI watching run on Haiku, the cheapest tier. The overhead has not been measured precisely. As one anecdote, heavy daily use on the Claude Max 20x plan, often four or five instances at once, never hits the five-hour window limit and lands around half the weekly limit; the smaller Max plan may hit the window cap under the same load. If you are rate-limit sensitive, run the gated verbs stage by stage instead of `/autopilot`, and skip the loop entirely for small edits: a one-file fix does not need a builder and a reviewer.

## 3. Diagnose

```
/subagent-diagnose ci
/subagent-diagnose bug "description of what's broken"
```

When something breaks, this command runs the same loop as implementation but seeded from a failed CI run or a bug description. It investigates, proposes a fix, reviews its own fix, and commits when green.


## 4. Ship

One verb covers all ship paths:

```
/commit                   — stage + commit, then ask how far to ship
/commit push              — commit + push
/commit pr                — commit + push + PR
/commit merge             — commit + merge to base
/commit squash            — commit + squash branch
/commit squash merge      — commit + squash + merge to base
```

With no pending changes and commits already ahead of base, `/commit` skips straight to the ship step — so `/commit merge` on a clean branch just merges. All paths run tests on the merged/squashed result and prompt to clean up the worktree if you used one.


## 5. Track what's deferred

```
/remind-me 2h check the deploy
/follow-up review
```

Not everything gets resolved in the same session. Reminders are time-based nudges that surface at the specified moment (or at the start of your next session if you are away). Follow-ups are non-blocking findings from implementation — risks, nits, open questions — that you parked for later. `/follow-up review` walks you through stale entries and lets you close, extend, or promote each one.

Both mechanisms exist because shipping is not the end. The things you deferred during implementation should not silently rot.


## 6. Improve

```
/atomic-improve
```

After a long session or a run of friction, `/atomic-improve` looks back. It mines your session history and the current conversation for corrections, repeated requests, and places where Claude misbehaved, then cross-references those signals against your installed artifacts (commands, skills, agents, CLAUDE.md). It walks proposed improvements one at a time; you accept, modify, or skip each. A run log persists to `~/.claude/.atomic/improve-runs/`, so a later run can tell whether a past accept actually landed or quietly drifted back.

This is the stage that closes the loop. Shipping a feature teaches you something about how you and Claude work together, and `/atomic-improve` is where that lesson becomes a durable config change instead of a frustration you re-hit next week.


## Lost? Start with the router

```
/atomic-help
/atomic-help tour
```

`/atomic-help` reads your git state, works out where you are in the lifecycle, and recommends one next command. It routes; it never executes. `/atomic-help tour` runs a four-stage guided walkthrough of the whole system (surfaces, lifecycle, state files, maintenance), and a bare `/atomic-help` offers the tour automatically the first time you run it in a fresh repo.


## Why custom ship commands?

Claude Code already knows how to commit and push. The reason atomic-claude wraps those operations into its own commands is everything that happens around them:

- **Signals refresh** — when source files changed, the command re-scans the project so Claude's map stays current
- **Doc-impact check** — checks whether your change affects documentation and prompts you to update the relevant surfaces
- **Commit message discipline** — messages are generated by the `atomic-commit` skill in Conventional Commits format, drawn from the diff and any session reports
- **Verification gate** — merge commands run `atomic-verify` before touching the base branch, re-running tests on the merged tip

Documentation is almost always an afterthought. These commands make it part of the flow rather than something you remember to do later.


### What runs automatically

Every `/commit` invocation runs signals refresh and doc-impact checks as part of the commit flow — signals are regenerated, documentation surfaces are presented for review, and the commit message is synthesized from the diff. Escalation paths that touch the base branch (`merge`, `squash merge`) also run `atomic-verify` on the merged tip before finalizing.

| Path | Signals | Doc-impact | Commit msg | Verify |
|------|:-------:|:----------:|:----------:|:------:|
| commit (all paths) | ✓ | ✓ | ✓ | |
| merge / squash merge | ✓ | ✓ | ✓ | ✓ |

✓ = runs automatically.
