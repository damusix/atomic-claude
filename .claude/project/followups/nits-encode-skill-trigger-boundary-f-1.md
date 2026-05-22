---
id: nits-encode-skill-trigger-boundary-f-1
title: Encode skill trigger boundary in atomic-tdd and atomic-debug descriptions
created: "2026-05-17"
origin: |
    chat session 2026-05-17 audit review, deferred at user's request pending evidence of misrouting.
severity: nit
review_by: "2026-07-16"
status: open
---

No file:line — descriptions live in `skills/atomic-tdd/SKILL.md` and `skills/atomic-debug/SKILL.md`.


The two skills can both auto-fire on phrases like "let's fix the broken X" — `atomic-debug` matches "broken", `atomic-tdd` matches "fix". A word-order precedence rule would be brittle. Proposed approach: encode the boundary in each skill's description itself, so the model routes correctly without an explicit rule. `atomic-tdd` description should say "NEW behavior only; for existing-broken-thing fixes, atomic-debug owns that." `atomic-debug` should say the reciprocal. The model reads both descriptions when picking, so sharp boundaries beat ordering.


Open question: does this actually work in practice, or do we see misroutes? Decision deferred pending real-world routing observations.
