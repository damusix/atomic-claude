# Stuck-fix escalation + suppression awareness

## Problem

When the implement→review loop hits a hard bug, the first fixes often fail. The
current loop is failure-tolerant in the wrong direction: it keeps iterating
variants of the same wrong fix (wrap, try/catch, swallow, monkey-patch),
accumulating suppression debt instead of stopping to investigate root cause. The
user has to manually redirect ("don't suppress the error", "maybe you need a
fresh agent for a root-cause analysis"). GitHub issue #29.

The existing principles (`Stop when confused`, `Root cause over symptom`, `Fail
loud`) cover this in spirit but are advisory prose — they do not *fire* when the
conditions match.

### What already exists (investigation, PARTIAL gap)

| Mechanism | Location | What it does | Gap |
|-----------|----------|--------------|-----|
| Same-failure bail | `subagent-diagnose.md` (Phase 2/3) | Normalizes `top_level_error` across iterations; 3 consecutive matches → bail before the 5-iter cap | Diagnose loop only; compares **error strings**, not edit-shape; **stops, does not escalate to RCA** |
| Soft iteration stop | `subagent-implementation.md` Phase 2 | "hard stop default 6 iterations — ask user before exceeding" | Just asks permission; no stuck-detection, no escalation target |
| Repeat-finding nudge | `subagent-implementation.md` constraints | "same finding twice → re-examine the brief/spec" | Advisory prose, not a trigger; no escalation |
| Strategist dispatch criterion | `agents/atomic-strategist.md` | Description names "stuck or repeatedly failing review" as a dispatch trigger | **Neither loop wires it** — target exists, no caller |
| Suppression-shape detection | — | nothing | **Absent everywhere** |

So the escalation *targets* (`atomic-strategist`, `/pressure-test`, `atomic-debug`)
all exist as artifacts but are unwired into the loop as defaults; the
suppression-shaped-edit detector does not exist at all.

## Goals / Non-goals

- **Goals**
  - A **stuck-fix escalator** wired as a loop default: after repeated failure on the same signal, surface a runnable next step — `/pressure-test` or an `atomic-strategist` RCA dispatch.
  - **Suppression-pattern awareness**: the reviewer recognizes when iterations add error-catching code without adding investigation, and flags it.
  - Both are **defaults** of the loop, not opt-in flags — the failure mode is "Claude doesn't know it's stuck."
  - Escalation is **surfaced, never auto-invoked** (axiom 3 — `atomic-strategist` is opus/expensive; user opts in).

- **Non-goals**
  - Hard-coded line-level lint for suppression patterns (false-positives on legitimate defensive programming — issue out-of-scope).
  - Auto-dispatching the strategist without opt-in.
  - Replacing the reviewer's `CHANGES_REQUESTED` loop — this complements it for the case where the reviewer keeps approving suppressions because they fix the symptom.

## Approaches

| # | Approach | Sketch | Pros | Cons |
|---|----------|--------|------|------|
| A | Escalator in the orchestrator's triage step + suppression flag in the reviewer | Each detection lives where its evidence is: orchestrator sees iteration history (STATE.md), reviewer sees the diff | Minimal new surface; defaults ride the existing loop; reviewer already classifies findings | Two files change in concert |
| B | New `atomic-stuck` skill that fires on stuck language | A skill watches for stuck conditions | Discoverable | The loop is the right home, not conversation; axiom 2 (no new artifact before the in-loop path is shown insufficient) |
| C | Port the diagnose same-failure detector verbatim to the impl loop | Reuse the error-string-normalization bail | Reuses code | Compares error strings, not edit-shape; still doesn't escalate to RCA — insufficient for the ask |

## Recommendation

**A.** The escalation is an *orchestrator* decision — only the orchestrator sees
the cross-iteration history (`STATE.md`: same failing signal? same finding
twice? N rounds?). The suppression-shape judgment is a *reviewer* decision — only
the reviewer reads the diff and can tell "this iteration only added a try/catch,
no investigation." Wire both as loop defaults. The orchestrator surfaces a
runnable escalation (copyable `/pressure-test` line + an `atomic-strategist`
dispatch offer); it never auto-dispatches. The existing `subagent-diagnose`
same-failure bail gains the same escalation surface (today it stops and hands
back; it should also offer the RCA path).

The escalation flow:

```
iteration → reviewer verdict
  PASS                      → commit, next checkpoint
  CHANGES_REQUESTED         → loop back
  CHANGES_REQUESTED again,  → STUCK: surface escalation
    same signal / 2nd round    "N rounds on the same signal. Before another
                                wrap-and-retry: /pressure-test @spec, or
                                dispatch atomic-strategist (opus, RCA). Escalate?"
```

## Open questions

- Threshold: 2 rounds on the same signal, or 3? Lean 2 — the cost of a premature escalation offer is one ignorable line; the cost of 5 wasted wrap-iterations is real.
- Should the reviewer's suppression finding be 🔴 or 🟡? Lean 🟡 (risk) by default, 🔴 only when it's the *Nth* suppression on the same error across iterations (the orchestrator escalates on the pattern).
