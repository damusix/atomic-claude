---
id: codeintel-dart-grammar-wall
title: Dart call extraction blocked by grammar (needs grammar upgrade)
created: "2026-06-07"
origin: |
    code-intel B-6/F-16; phase-3 triage
kind: plan
review_by: "2026-08-06"
status: open
file: atomic/internal/codeintel/extraction/languages/dart.go
---

Dart call extraction is impossible with the current tree-sitter-dart grammar (ABI-14): no call_expression node, so setState/method calls are never captured. Blocks flutter-build synthesizer (B-6) and Dart call edges (F-16). HARD WALL — not closable via our extraction code. Requires upgrading/replacing the Dart grammar in the ts.wasm bundle to one that emits call expressions, then adding Dart CallTypes + the flutter-build signals.
