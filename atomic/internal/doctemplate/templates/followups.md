<!--
Template for $SCRATCH/FOLLOWUPS.md — emitted by `atomic template followups`. The ledger of non-blocking reviewer findings
(🟡 risk / 🔵 nit / ❓ question) kept by the loop orchestrator. Copy this body, fill
every <angle-bracket> placeholder, delete this comment. Initialize on first iteration;
append after every reviewer pass that returns findings — even on PASS verdicts.
Numbering is sequential across all severities (F-1, F-2, F-3...). When a follow-up
closes in a later iteration, mark `*(closed iter N — <commit-sha>)*` next to its
title and keep the entry for traceability — don't delete it. Drop the ❓ questions
section only if the loop never produces one.
-->
# Follow-ups: <topic>

Non-blocking findings carried across iterations. At finalization: review with the user, decide what to fix in a polish pass, what to defer to a tracked issue, what to drop.

---

## 🟡 risks

### F-1 — <one-line title>

`<path:line>`

<problem + suggested fix in 1-3 sentences>

Origin: iteration <N> reviewer.

## 🔵 nits

### F-N — <title>

...

## ❓ questions

### F-N — <title>

...
