# Spec: atomic-auditor

## Goal

A sixth subagent that gates the finished implementation once, in a context that never saw the loop produce it. It closes four holes that per-checkpoint review cannot reach: cumulative spec compliance, cross-iteration coherence, commit-message soundness, and documentation adherence.

## Non-goals

- Not a diff reviewer. `atomic-reviewer` keeps gating each iteration; the auditor never re-litigates a single checkpoint.
- Not an approach critic. Whether the design was right stays with `atomic-strategist`.
- No repo writes. The auditor reports findings; only an audit with a scratchpad may write `AUDIT.md` there.
- No second audit pass. One dispatch per task, always.
- No new CLI verb, no Go change.

## Success criteria

- [ ] `templates/agents/atomic-auditor.md` renders to `agents/atomic-auditor.md` and lands in the embedded bundle.
- [ ] Frontmatter leaves `model:` unset (the caller's model applies), pins `effort: max`, grants `Write` alongside the read tools, and preloads `atomic-git-discipline`, `atomic-writing`, `atomic-verify`.
- [ ] Output ends with exactly one `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`, matching the contract the orchestrator already parses for `atomic-reviewer`.
- [ ] Emits all four section headers every run, including when empty, so the orchestrator can grep them.
- [ ] `/implement` and `/autopilot` dispatch one audit after verification and affected doc updates, before signals refresh.
- [ ] Without a spec, the auditor uses the supplied brief or inline success criteria. It writes findings to `AUDIT.md` only when the caller supplied a scratchpad. It treats documentation as deferred only when the caller says so.
- [ ] `/atomic-help` reports 7 subagents and names the auditor in both the topic row and the tour listing.
- [ ] `docs/reference/agents.md` carries an auditor row and the corrected model/effort defaults table.
- [ ] `make render` and `make -C atomic bundle` are clean; `atomic validate` reports zero FAIL.

## Approach

The auditor runs once at finalize and returns the same `VERDICT:` contract as the reviewer. It saves findings in the task scratchpad when one exists.

The audit runs after documentation updates and before the signals refresh.

## Change tree

```
context/
  agents/atomic-auditor.md  M
  commands/implement.md  M
docs/
  reference/agents.md  M
  spec/atomic-auditor.md  M
```

## Outline

- `context/agents/atomic-auditor.md`: accept a spec or brief and optional scratchpad. Return all four sections and a verdict. Save findings only when a scratchpad is supplied.
- `context/commands/implement.md`: pass the commit range and success criteria; include scratchpad paths when used.

## Flows

1. The caller verifies the behavior and updates affected documentation.
2. The caller supplies the commit range, spec or brief, and any scratchpad paths.
3. The auditor runs four passes and returns findings and a verdict. With a scratchpad, it also saves findings in `AUDIT.md`.
4. The caller resolves blocking findings through checkpoint review before refreshing signals. It does not dispatch another auditor.

**Refusal.** Dispatched before the suite is green, or asked to fix, review one diff, or judge the approach: the auditor returns its `OUT OF SCOPE:` line and stops.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Align audit input and output for planned work and inline fixes | `context/agents/atomic-auditor.md`, `context/commands/implement.md`, `docs/reference/agents.md` | The audit uses the same verdict in both cases and does not require a scratchpad for inline work |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Audit loop never terminates under `/autopilot` | medium | One dispatch per task, stated in the agent rules and both command steps. |
| Auditor duplicates reviewer findings, burning an expensive pass | medium | Rule: a finding visible inside one checkpoint is out of scope and gets dropped. |
| `max` effort on the caller's model at every finalize is costly | high | It is one dispatch at finalize, not per iteration. Override via `[claude.agents.atomic-auditor]`. |
| Findings arrive too late to act on cheaply | medium | Accepted. Coherence problems are only visible once the whole exists. |

## Change log

<!-- Append a dated entry per amendment. Never delete prior entries.
     The body above is current truth; this log is history. -->

### 2026-08-14 — Initial

**What changed:** New `atomic-auditor` agent plus its dispatch wiring in `/subagent-implementation` Phase 3 and `/autopilot` Phase 4, taking the roster from five agents to six.

**Why:** Every gate in the loop was scoped to one diff, and the only finalize gate was the orchestrator running `atomic-verify` on itself, grading work its own context produced. Cumulative spec compliance had no whole-artifact check; coherence, commit soundness, and documentation adherence had none at all.

### 2026-08-21 — Unpin the model; write findings to the scratchpad

**What changed:** Frontmatter drops `model: claude-opus-5` so the caller's session model (or a `[claude.agents.atomic-auditor]` override) decides what runs the audit; `effort: max` stays. The agent gains `Write`, scoped to one file: when any finding exists it writes the full report to `$SCRATCH/AUDIT.md` before returning it. Both dispatch sites now pass `scratch: $SCRATCH`.

**Why:** the user picks the model per session; a hard pin in the artifact overrode that choice for the most expensive dispatch in the loop. Findings that the single fix iteration did not fully address had no home once the return value scrolled away.

**Superseded:** `model: claude-opus-5` pinned; tools read-only with no write path at all.

### 2026-08-21 — Readability in the coherence pass; PR bodies in the commit pass

**What changed:** The agent composes `agent-yagni`, `agent-comment-discipline`, and the new `agent-readability` partial. Pass 2 reads the cumulative diff as prose and reports comment drift, repeated explanations, and YAGNI misses that only the whole exposes; these are exempt from the "visible inside one checkpoint" drop rule. Pass 3 judges a PR title and body when the caller passes `pr:`.

**Why:** the reviewer sees one iteration at a time, so a comment that went stale by checkpoint 5, or the same why restated in four files, had no gate. Commit and PR discipline were split across two skills with no reader of the whole record.

### 2026-08-21 — `/quick-fix` dispatches the auditor

**What changed:** `/quick-fix` Finalize gains the audit step between `atomic-verify` and `FOLLOWUPS.md` triage, passing `brief: $SCRATCH/BRIEF.md` since no spec exists. The agent reads the brief's success criteria wherever it would read a spec's, and under a brief reports an untouched documentation surface as 🟡, because `/documentation` is deferred to the user by design.

**Why:** the fast loop had no whole-delivery gate at all, and it is the loop where comment noise and a duplicated helper most often slip past per-iteration review.

### 2026-11-22: Audit fixes without a scratchpad


**What changed:** `/implement` can pass inline criteria and omit scratchpad paths. The auditor returns findings to the caller and saves them only when a scratchpad exists.

**Why:** A one-checkpoint fix can use the same audit without creating a working bundle.

**Superseded:** The audit dispatch from `/quick-fix` and the requirement to write every finding to `$SCRATCH/AUDIT.md`.
