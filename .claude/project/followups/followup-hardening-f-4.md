---
id: followup-hardening-f-4
title: Vue/Svelte node-ID staleness (inline contentLineOffset, no ID regen)
created: "2026-06-10"
origin: |
    docs/spec/followup-hardening.md, iter 1 reviewer (CP1)
kind: finding
severity: risk
review_by: "2026-08-09"
status: open
file: atomic/internal/codeintel/extraction/standalone/standalone.go:243
---

Vue/Svelte SFC extractors mint node IDs from script-relative lines, then shift StartLine via inline contentLineOffset (standalone.go:243,436) WITHOUT regenerating the node ID — the same latent collision CP1 fixed for embedded SQL via newline padding, but a separate code path. Surfaced by the iteration-1 reviewer during followup-hardening CP1 (the offsetResult dead-code discovery). Out of scope for that batch (non-goal). Fix: apply the same newline-padding trick to the SFC extractors so SFC node IDs are file-absolute at the source. Low probability (needs two same-named symbols at the same script-relative line in one SFC).
