# Typed follow-ups: findings vs plans

## Problem

The follow-up ledger (`.claude/project/followups/`) is the durable, committed,
`@`-ref'd record of deferred work. Investigation of GitHub issue #28 surfaced
two problems with one root cause: **the ledger has no notion of *kind*, so it
silently holds two different species of work and serves neither well.**

1. **It already mixes two species.** The `INDEX.md` carries both *findings*
   (loose ends from work done — "bundlemirror double-reads files") and *intended
   work* ("Write docs/spec/atomic-improve.md", "Design the atomic validate CLI").
   Same cabinet, different things.

2. **Intended work has no fitting home.** Deferred specs — e.g. three dependent
   specs done one at a time, where #2 and #3 are written but not started — have
   nowhere to surface. The user looks in the ledger, the spec isn't there, the
   thread is lost.

### The surfaces, disambiguated

| Surface | Anchor | Home | Surfaced by | Holds |
|---------|--------|------|-------------|-------|
| Reminders (`/remind-me`) | a **time** | `.claude/.scratchpad/reminders/` (gitignored) | SessionStart hook | timed nudges ("check deploy tomorrow") |
| **Follow-up ledger** | none — pulled by priority | `.claude/project/followups/` (committed) | `@`-ref'd `INDEX.md` | findings **and** plans |
| Claude `TaskList` | the current task | in-context | the harness | ephemeral session checklist |

The discriminating axis is **timed vs untimed**: timed → reminder (it fires);
untimed → ledger (you pull from it). Findings and plans are both untimed, so they
share the ledger — distinguished by a `kind` field, not a separate surface.

## Goals / Non-goals

- **Goals**
  - Give entries a `kind`: `finding` (current, default) or `plan` (intended future work / deferred specs).
  - Back-compatible: existing entries (no `kind`) read as `finding`.
  - Plans surface every session as a backlog, link to a spec when one exists, and are exempt from `review_by` staleness nagging.
  - Sharpen the ledger's charter in `CLAUDE.md` so the two kinds never blur again.

- **Non-goals**
  - A new surface (`BACKLOG.md`) or subsystem (`todos/`) — `kind` on the existing ledger is the cut (axiom 2).
  - An auto-firing capture skill (see below — considered and rejected).
  - Reworking `/follow-up review` staleness beyond exempting plans.
  - Wiring `/remind-me` into the ledger.

## Why an "epic manager" is not needed

A heavy ticket does not need a new tracking subsystem — `docs/spec/` and
`docs/design/` already are the epic manager. A `plan` entry is a thin pointer:
`- [ ] build X → docs/spec/x.md`. The ledger surfaces the pointer every session;
the spec holds the detail. Small ideas not yet worth a spec stay one-liners in
the same ledger.

## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | **Typed ledger** — add `kind: finding\|plan`, plans group separately | one field + render branch | low-med | Go CLI/render change, bounded |
| B | Separate `BACKLOG.md` surface | second `@`-ref'd file | med | a third surface to chart; splits "deferred work" |
| C | Full `todos/` subsystem | clone followups: per-entry + CLI + render | high | duplicates the entire stack (axiom 2) |
| D | Docs-only note | document the gap | low | does not fix the leak |

## Recommendation

**A, with manual CLI capture.** One ledger, typed. It resolves both the existing
finding/plan mix and the missing home for deferred specs with the least
machinery — a `kind` field plus a render branch — reusing the committed +
`@`-ref'd surfacing so plans appear in every session's context. Plans are filed
with `atomic followups add --kind plan` (Claude may offer to file one when work
is deferred in conversation, but no skill auto-fires). Rejected B and C per
axiom 2.

## Capture mechanism: considered and rejected (auto-firing skill)

The original issue #28 proposed an auto-firing **`atomic-defer` skill** that would
watch conversation for deferral language ("once X", "later", "TODO", "I spec'd X
but won't do it now"), apply a three-condition fire test, and offer to file a
`kind: plan` entry (offer-not-write, user-confirmed). It was built during the
implementation loop and then **removed before ship.**

**Why rejected:** the skill was carried over from the original issue framing into
this spec without an explicit re-confirmation after the design pivoted from
"build a capture skill" to "type the ledger." Once surfaced, the decision was
that the schema plus *manual* capture is the intended scope — the auto-fire
heuristic added a new always-on artifact and false-positive surface for marginal
benefit over typing `atomic followups add --kind plan` when you actually defer
something.

**Lesson (kept here deliberately):** when a design *reframes*, every component of
the superseded approach must be explicitly re-confirmed or dropped — a spec that
silently carries forward the old approach's pieces causes exactly this kind of
build-then-revert churn. Re-open the decision at the reframe, not after the build.

## Open questions

- Does session-load noise grow if findings + plans both render? Mitigation if so: `@`-ref only the plans + open-risk findings, keep nits on-demand. Deferred unless it bites.
