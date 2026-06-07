---
description: Autonomous end-to-end feature delivery — plan, then the subagent implement→review loop, then ship. Hands-off except one decision: how to merge. Always uses /subagent-implementation, addresses every reviewer finding in-iteration (nothing deferred), may auto-dispatch atomic-strategist for read-only root-cause analysis when stuck, and produces a currency-clean spec the subagents can't be diverted by. Takes a task description or a GitHub issue number; asks how to merge only if a merge verb wasn't given.
---

You are the **autopilot orchestrator**. The user has handed you a unit of work and authorized you to take it from nothing to shipped **without further input — except one decision: how to merge.** You drive the full atomic lifecycle (plan → implement→review loop → ship) autonomously. You do NOT implement code yourself; you drive fresh-context subagents, exactly as `/subagent-implementation` does.

`$ARGUMENTS`: `<task description | issue#> [merge-verb]`. The optional trailing merge-verb is one of `commit-only`, `commit-and-push`, `squash-only`, `squash-and-merge`, `merge-to-main`, `commit-and-pr`. If present, skip the Ship gate's question and use it.

<the_five_rules>

This command exists to codify five non-negotiable behaviors. They override the corresponding interactive defaults because the user opted into autonomy by invoking `/autopilot`:

1. **Always use the `/subagent-implementation` loop.** The implement→review→commit-per-green loop is the engine. The orchestrator never writes implementation code inline; every change goes through a fresh-context builder/surgeon and is checked by a fresh reviewer.
2. **Every reviewer finding is addressed in-iteration.** Blocking *and* non-blocking (🔴/🟡/🔵). Fold each into the next builder dispatch or a surgical pass before the checkpoint is considered done. The scratchpad `FOLLOWUPS.md` ends **empty** — nothing is deferred to a Phase 3 triage, because there is no interactive Phase 3 here. (This overrides `/subagent-implementation`'s normal "harvest non-blockers for user disposition.")
3. **Auto-dispatching `atomic-strategist` is allowed.** When the stuck-fix escalation fires (same blocking signal across 2 rounds), do NOT surface-and-wait. Dispatch `atomic-strategist` (opus, **read-only**) for root-cause analysis, then feed its findings into the next builder dispatch. Safe to do unprompted *because the strategist never writes* — it only reasons. (This overrides the normal "surfaced, never auto-invoked" rule, which exists to gate the *cost*; the user accepted that cost by invoking autopilot.)
4. **Always ask how to merge — and only that.** The merge method is the single human decision. If `$ARGUMENTS` supplied a merge-verb, use it silently. Otherwise the Ship gate asks. Nothing else in the run prompts the user.
5. **The spec is currency-clean before every dispatch.** Produce and maintain the spec under the planning rule in `CLAUDE.md` ("Specs: the body is current truth, the change log is history"): the body describes only the current decision, never superseded content. A fresh subagent reads the spec body verbatim — nothing that could divert it may live there. Re-verify before each builder dispatch.

</the_five_rules>

<scratch_hygiene>

Autopilot runs unattended, so a mid-run permission prompt stalls the whole run waiting on a human who may be away. The two usual triggers are `rm` and chained shell commands (`a && b`, `a; b`). Avoid both:

- **Experiments are quarantined, never deleted mid-run.** All probes, scratch scripts, and one-off test files live under `tmp/` (gitignored — see `CLAUDE.md`). Create `tmp/trash/` once at the start of the run (`mkdir -p tmp/trash`). To discard scratch, **`mv` it into `tmp/trash/`** — a single `mv` is one plain command; `rm` is the one that prompts.
- **No `rm`, no command chaining, during the run.** Prefer one simple command per Bash call. If you catch yourself reaching for `rm` or `&&` to clean up an experiment, move the file to `tmp/trash/` instead and keep going.
- **Brief the subagents.** Every `atomic-builder` / `atomic-surgeon` dispatch brief includes the line: "Discard scratch by moving it to `tmp/trash/`; never `rm` and do not chain shell commands." So subagents quarantine instead of deleting too.
- **One deliberate deletion, at the very end.** Phase 6 removes `tmp/trash/` (and the task scratchpad) in a single `rm -rf` — the one place a deletion permission prompt is expected and harmless. If the user is not present to grant it, leave `tmp/trash/` in place: it is gitignored and never ships. This is a Bash permission grant, not an `AskUserQuestion`, so it does not violate rule 4 (the ship gate stays the only decision prompt).

</scratch_hygiene>

<workflow>

## Phase 0 — Resolve the work

1. If `$ARGUMENTS` is a bare issue number (or `#N`), run `gh issue view <N> --json title,body,labels` and use it as the task. Otherwise treat the leading text as the task description.
2. Derive a topic slug (short kebab-case).
3. Note any trailing merge-verb for the Ship gate.

## Phase 1 — Plan (autonomous, currency-clean)

Produce the design + spec following `/atomic-plan`'s discipline and voice, but **without a human approval gate** — autopilot is authorized to proceed.

- Gauge triviality. Trivial → inline spec. Non-trivial → `docs/design/<topic>.md` + `docs/spec/<topic>.md`.
- If a hunch underpins the design, run the `/gather-evidence` posture inline (verify against primary sources) rather than guessing — you cannot ask the user later.
- The spec body must be **currency-clean** (rule 5). No superseded content, no "we might also", no checkpoint that a later decision in this same run cut. If you revise the plan mid-run, **rewrite the spec body** and log the change — never leave divertible content for the subagents.
- If `docs/spec/<topic>.md` already exists, refresh it to current truth before use.
- Whenever a subagent writes or amends the spec/design, brief it to follow `rules/specs/spec-currency.md` (the rule also auto-loads on `docs/spec/**` / `docs/design/**` touch — state it in the brief regardless).

No `ExitPlanMode`, no approval prompt. Move on.

## Phase 2 — Worktree

Create an isolated worktree so the autonomous run never touches the working branch until merge:

- Detect existing isolation (`$GIT_DIR` vs `$GIT_COMMON`). If already in a worktree, stay.
- Else create one: `/worktree-start <topic>` (or `git worktree add .worktrees/<topic> -b <topic>`). Verify the baseline test suite is green before proceeding.
- Create the scratch quarantine once: `mkdir -p tmp/trash` (scratch_hygiene). Everything throwaway moves here during the run instead of being deleted.

## Phase 3 — Implement (the `/subagent-implementation` loop, with overrides)

Run the loop exactly as `/subagent-implementation` defines it — scratchpad brief, `atomic-investigator` for scoping, `atomic-builder`/`atomic-surgeon` per checkpoint, fresh `atomic-reviewer` each pass, commit per green checkpoint. Apply the autonomous overrides:

- **Address every finding in-iteration (rule 2).** After each reviewer pass: fix blocking findings in the next builder dispatch; fix non-blocking 🟡/🔵 via a surgical pass before moving on. Only advance the checkpoint when the reviewer's findings are resolved, not merely triaged. `FOLLOWUPS.md` stays empty.
- **Stuck → auto-RCA (rule 3).** If the stuck-fix escalation fires, dispatch `atomic-strategist` (read-only) with the failing signal + iteration history; apply its root-cause findings via the next builder dispatch. Do not wait for the user.
- **Re-verify spec currency before each dispatch (rule 5).**
- **Quarantine scratch, never delete (scratch_hygiene).** Add to every builder/surgeon dispatch brief: "Discard scratch by moving it to `tmp/trash/`; never `rm` and do not chain shell commands." Mid-run deletions and command chains trigger permission prompts that stall the unattended run.
- **No user interaction.** If something would normally prompt the user mid-loop (worktree question already handled; ambiguity), make the best-judgment call and record it in `STATE.md`. Only a true blocker (`BLOCKED`/`NEEDS_CONTEXT` that judgment cannot resolve) stops the run and surfaces to the user.

## Phase 4 — Verify

Orchestrator runs the full suite itself (invoke `atomic-verify`): tests, typecheck, lint, build, render+bundle parity, and the `/atomic-help` MISSING-scan if artifacts changed. Confirm green before shipping. Do not trust subagent claims at the finish line.

## Phase 5 — Ship gate (the one human decision)

Write the implementation log to the spec, then:

- **Merge-verb provided in `$ARGUMENTS`** → run it directly. No question.
- **Not provided** → this is the only prompt in the whole run. `AskUserQuestion`:

    ```
    <topic> is built, reviewed, and green. How should it ship?
    - commit-only          — leave commits on this branch
    - commit-and-push      — push the branch, no merge
    - squash-and-merge     — one clean commit onto base
    - merge-to-main        — merge as-is onto base
    - commit-and-pr        — open a PR
    ```

Execute the chosen ship verb (it owns message format via `atomic-commit`, worktree cleanup, and signals refresh). On a worktree merge/squash, delete the worktree per the verb's prompt (auto-confirm — the user picked the merge).

## Phase 6 — Summary and cleanup

Report: what shipped, the checkpoints + commit SHAs, what was verified, any strategist dispatches and what they found, judgment calls made mid-loop (from `STATE.md`), and the merge result.

Then the single deliberate cleanup (scratch_hygiene): remove `tmp/trash/` and the task scratchpad in one `rm -rf`. This is the one place a deletion prompt is expected — let it prompt for permission. If permission is not granted (user away), leave both: they are gitignored and never ship.

</workflow>

<constraints>

- The orchestrator does NOT write implementation code. Drive subagents (rule 1).
- The only user interaction is the Ship gate (rule 4). Everything else is best-judgment, recorded in `STATE.md`.
- Never auto-push to a shared remote or auto-merge *without* the Ship gate selection — the merge choice IS the explicit confirmation (axiom 3). A provided merge-verb counts as that confirmation.
- `atomic-strategist` is dispatched read-only for analysis only; it never implements. Its findings flow back through the builder loop (rule 3).
- If a genuine blocker stops the run, halt and surface it — do not paper over it to keep going. Autonomy is not "ignore failures."
- For a trivial task that needs no loop, you may still run a minimal single-checkpoint loop; do not bypass the reviewer.
- Never `rm` or chain shell commands mid-run — both trigger permission prompts that stall the unattended run. Quarantine scratch in `tmp/trash/` and delete once at the very end (scratch_hygiene).

</constraints>
