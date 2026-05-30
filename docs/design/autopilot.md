# /autopilot — autonomous feature delivery

## Problem

The atomic lifecycle (`/atomic-plan` → `/subagent-implementation` → a ship verb)
is deliberately interactive: plans get human approval, reviewer findings get
per-item disposition, the merge method is chosen explicitly. That control is
right when the user wants it. But for well-scoped work the user trusts the
system to handle, the interactivity is friction — they want to hand over a task
or an issue number and get back a shipped, reviewed result with **one** decision:
how to merge.

This was executed by hand for issue #29 — worktree → spec → a 2-checkpoint
implement→review loop with every reviewer finding folded in-iteration →
fast-forward merge. `/autopilot` codifies exactly that flow.

## Goals / Non-goals

- **Goals**
  - One command, hands-off, from task/issue to shipped, with a single human gate (merge method).
  - Reuse the existing engines (planning discipline, the `/subagent-implementation` loop, the ship verbs) — codify *policy*, not new machinery.
  - Make the autonomous overrides explicit and *safe by construction*.

- **Non-goals**
  - Replacing the interactive lifecycle — it stays for when the user wants approval gates and per-finding control.
  - Auto-pushing or auto-merging without the merge selection (axiom 3).

## Why the autonomous overrides are safe

`/autopilot` overrides three interactive defaults. Each override is safe because
the thing the default was protecting is preserved another way:

| Interactive default | `/autopilot` override | Why it's still safe |
|---------------------|------------------------|---------------------|
| Reviewer non-blockers → harvested to `FOLLOWUPS.md` for user disposition at Phase 3 | Fixed in-iteration; ledger ends empty | Nothing is *dropped* — findings are *resolved*, which is strictly stronger than deferring them |
| Stuck-fix escalation is surfaced, never auto-invoked | Auto-dispatch `atomic-strategist` for RCA | The default gates *cost* (opus); the user accepted that cost by invoking `/autopilot`. The strategist is **read-only** — it cannot do harm, only reason |
| Merge method chosen explicitly per ship | Still chosen explicitly — the one gate | Unchanged. Axiom 3's destructive-op confirm is preserved as the single interaction |

Planning's human-approval gate is dropped, but the planning *rule* (currency-clean
spec body — nothing that could divert a fresh subagent) is enforced harder,
because there is no human to catch a divertible spec mid-run.

## Approaches

| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Dedicated `/autopilot` command composing the existing engines + autonomous policy | owns the whole lifecycle; no new machinery; matches the executed reference | must keep override semantics aligned with the wrapped loop |
| B | `--auto` flag on `/subagent-implementation` | reuses one command | doesn't cover planning or ship; bloats the loop command; user asked for a command |
| C | Reimplement plan+loop+ship standalone | self-contained | duplicates two commands; drift |

## Recommendation

**A** — a thin orchestrator that composes `/atomic-plan`'s discipline, the
`/subagent-implementation` loop, and the ship verbs, layering the five behaviors
as policy. See `docs/spec/autopilot.md` for the contract.

## Open questions

- A `--no-strategist` opt-out for cost-sensitive runs — deferred until someone wants it (axiom 2: don't add the knob before the need is real).
