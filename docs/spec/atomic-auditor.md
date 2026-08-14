# Spec: atomic-auditor

## Goal

A sixth subagent that gates the finished implementation once, in a context that never saw the loop produce it. It closes four holes that per-checkpoint review cannot reach: cumulative spec compliance, cross-iteration coherence, commit-message soundness, and documentation adherence.

## Non-goals

- Not a diff reviewer. `atomic-reviewer` keeps gating each iteration; the auditor never re-litigates a single checkpoint.
- Not an approach critic. Whether the design was right stays with `atomic-strategist`.
- No writes. It reports; the orchestrator dispatches a builder against its findings.
- No second audit pass. One dispatch per task, always.
- No new CLI verb, no Go change.

## Success criteria

- [ ] `templates/agents/atomic-auditor.md` renders to `agents/atomic-auditor.md` and lands in the embedded bundle.
- [ ] Frontmatter pins `model: claude-opus-5`, `effort: max`, read-only tools, and preloads `atomic-git-discipline`, `atomic-writing`, `atomic-verify`.
- [ ] Output ends with exactly one `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`, matching the contract the orchestrator already parses for `atomic-reviewer`.
- [ ] Emits all four section headers every run, including when empty, so the orchestrator can grep them.
- [ ] `/subagent-implementation` Phase 3 dispatches it after `/documentation` and before the signals refresh, capped at one dispatch.
- [ ] `/autopilot` Phase 4 dispatches it after documentation and before signals, capped at one dispatch.
- [ ] `/atomic-help` reports 6 subagents and names the auditor in both the topic row and the tour listing.
- [ ] `docs/reference/agents.md` carries an auditor row and the corrected model/effort defaults table.
- [ ] `make render` and `make -C atomic bundle` are clean; `atomic validate` reports zero FAIL.

## Approach

A read-only gating agent dispatched once at finalize, sharing `atomic-reviewer`'s `VERDICT:` contract so the orchestrator branches identically.

Ordering is the load-bearing part. The auditor judges documentation, so it runs after `/documentation`. The signals refresh scans final state, so it runs after the auditor. That fixes the finalize order at docs → audit → signals in both commands.

## Change tree

```
templates/agents/
└── atomic-auditor.md ................. A  (new agent: 4 passes, VERDICT contract)
agents/
└── atomic-auditor.md ................. A  (rendered)
templates/commands/
├── subagent-implementation.md ........ M  (Phase 3 step 5; steps 5-7 renumber to 6-8)
├── autopilot.md ...................... M  (Phase 4 audit step + authoring-mode docs step)
└── atomic-help.md .................... M  (agents topic row + tour count 5 -> 6)
docs/reference/agents.md .............. M  (auditor row, model/effort defaults)
docs/spec/atomic-auditor.md ........... A  (this file)
atomic/internal/embedded/ ............. M  (bundle mirror + manifest)
```

## Outline

- `templates/agents/atomic-auditor.md`
    - frontmatter — opus-5, max effort, read-only tools, three preloaded skills
    - Scope boundaries — four refusals routing to reviewer, strategist, orchestrator
    - Caller-provided context — spec, range, state, surfaces
    - The four passes — cumulative compliance, coherence, commits, documentation
    - Output format — four headers, totals, VERDICT
    - Rules — read-only, cite location, judge the whole, one dispatch
- `templates/commands/subagent-implementation.md`
    - Phase 3 step 5 — dispatch, verdict branch, one-dispatch cap
- `templates/commands/autopilot.md`
    - Phase 4 documentation step — authoring mode, self-answered prompts
    - Phase 4 audit step — dispatch, verdict branch, never re-audit

## Flows

**Finalize, both commands.**

1. Orchestrator confirms the suite is green.
2. Orchestrator runs `/documentation`, writing new pages and updating stale ones.
3. Orchestrator dispatches `atomic-auditor` with spec, range, state, surfaces.
4. Auditor runs four passes and returns findings plus a verdict.
5. `PASS` → orchestrator continues to the signals refresh.
6. `CHANGES_REQUESTED` → orchestrator runs one implementer/reviewer iteration against the findings, re-runs the suite, then continues to signals without re-auditing.

**Refusal.** Dispatched before the suite is green, or asked to fix, review one diff, or judge the approach: the auditor returns its `OUT OF SCOPE:` line and stops.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | The agent | `templates/agents/atomic-auditor.md` | Renders to `agents/`; frontmatter pins opus-5 + max effort + read-only tools + three preloaded skills; four passes and the VERDICT contract present; render+bundle parity clean |
| 2 | Dispatch wiring | `templates/commands/subagent-implementation.md`, `templates/commands/autopilot.md` | Dispatched once after `/documentation`, before signals, in both commands; one-dispatch cap stated; autopilot gains the authoring-mode docs step with self-answered prompts; Phase 3 steps renumbered without gaps |
| 3 | Discovery surfaces | `templates/commands/atomic-help.md`, `docs/reference/agents.md` | Help reports 6 subagents in the topic row and the tour; agents reference carries the auditor row and corrected defaults; MISSING-scan zero; `atomic validate` zero FAIL |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Audit loop never terminates under `/autopilot` | medium | One dispatch per task, stated in the agent rules and both command steps. |
| Auditor duplicates reviewer findings, burning an expensive pass | medium | Rule: a finding visible inside one checkpoint is out of scope and gets dropped. |
| Opus at `max` effort on every task is costly | high | It is one dispatch at finalize, not per iteration. Override via `[claude.agents.atomic-auditor]`. |
| Findings arrive too late to act on cheaply | medium | Accepted. Coherence problems are only visible once the whole exists. |

## Change log

<!-- Append a dated entry per amendment. Never delete prior entries.
     The body above is current truth; this log is history. -->

### 2026-08-14 — Initial

**What changed:** New `atomic-auditor` agent plus its dispatch wiring in `/subagent-implementation` Phase 3 and `/autopilot` Phase 4, taking the roster from five agents to six.

**Why:** Every gate in the loop was scoped to one diff, and the only finalize gate was the orchestrator running `atomic-verify` on itself, grading work its own context produced. Cumulative spec compliance had no whole-artifact check; coherence, commit soundness, and documentation adherence had none at all.
