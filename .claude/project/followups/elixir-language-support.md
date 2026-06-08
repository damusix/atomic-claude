---
id: elixir-language-support
title: Add Elixir tree-sitter language support
created: "2026-06-07"
origin: |
    code-intel eval — rw-phoenix 0 nodes; user request
kind: plan
review_by: "2026-08-06"
status: open
---

Elixir is not among the 19 supported tree-sitter languages, so Elixir source extracts 0 nodes (rw-phoenix indexed 1 file / 0 nodes in the corpus eval). Add Elixir: vendor the tree-sitter-elixir grammar into the ts.wasm bundle (zig cc build-time), add an Elixir extractor config (functions, modules, defmodule/def/defp, struct, etc.) mirroring the existing language extractors. User has in-house Elixir projects — high value. Pairs with phoenix-route-resolver (Phoenix routes need Elixir parsing first).
