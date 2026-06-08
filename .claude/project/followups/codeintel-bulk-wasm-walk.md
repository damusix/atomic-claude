---
id: codeintel-bulk-wasm-walk
title: Bulk in-WASM tree walk (deferred perf — not a bottleneck)
created: "2026-06-07"
origin: |
    code-intel F-3; phase-3 triage
kind: plan
review_by: "2026-08-06"
status: open
file: atomic/internal/codeintel/extraction/binding.go
---

Bulk in-WASM tree serialization to cut the per-node wazero boundary cost in WalkNamed (extraction/binding.go). Requires forking the tsbinding to add a C export that walks + serializes the named-node structure in a few calls, then rebuilding ts.wasm (zig cc). DEFERRED: measurement (code index --profile) shows extraction is NOT a bottleneck — 130-340ms even on 940-file repos, 0 timeouts. High effort (binding fork + wasm rebuild), low payoff at current scale. Revisit only if extraction throughput becomes a measured problem (e.g. very large monorepos).
