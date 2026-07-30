---
id: code-graph-hub-drag-unverified
title: Code-graph drag-overlap unverified for max-degree hub-on-hub drags
created: "2026-07-08"
origin: |
    code-graph CP5 reviewer (2026-07-07)
kind: finding
severity: risk
review_by: "2026-09-06"
status: open
file: scripts/graph-gates.mjs:288
---

The drag-overlap gate picks a representative mid-size node pair (its original first-two-in-insertion-order pick consistently landed on two max-degree hubs in the densest region and failed ~62% of trials at 17.5k-node scale). With mid-size targets the gate passes 20/20 with unchanged physics. Consequence: dragging one max-degree hub onto another at code-view scale is NOT exercised by the harness and its overlap resolution is unverified — and hubs are the nodes users most likely drag. If hand-testing shows hub-on-hub overlap failing to separate, the sanctioned knob is a profile-level drag-physics override in graph-core.js's profile contract (removed as unused after the CP5 sweep; re-add per YAGNI when a real profile needs it).
