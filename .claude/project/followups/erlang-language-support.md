---
id: erlang-language-support
title: Add Erlang tree-sitter language support
created: "2026-06-08"
origin: |
    user request after SQL support shipped
kind: plan
review_by: "2026-08-07"
status: open
file: atomic/internal/codeintel/extraction/languages
---

Add Erlang (`.erl`, `.hrl`) as a supported tree-sitter language. Independent of the Elixir plan — Erlang and Elixir are both BEAM languages but distinct grammars and syntax; this does NOT depend on `elixir-language-support`.

Pre-flight (do FIRST, like the SQL grammar spike): verify a `tree-sitter-erlang` grammar (e.g. WhatsApp/tree-sitter-erlang or the abandonware-friendly fork) compiles clean under our `zig cc --target=wasm32-wasi-musl` toolchain AND declares `LANGUAGE_VERSION` ≤ 14 (our runtime ceiling — `tsbinding/src/api.h`: TREE_SITTER_LANGUAGE_VERSION 14, MIN_COMPATIBLE 13). Modern tree-sitter CLI emits ABI 15 → would wall like dart did; pin a commit/npm tarball that ships ABI 14, or generate with `--abi 14`. Gate the whole effort on this spike.

Implementation (mirrors elixir-language-support / the 20 existing grammars):
- Vendor `tree-sitter-erlang` C sources into `tsbinding/src/erlang/`, add to `tsbinding/Makefile` compile list + `-Wl,--export=tree_sitter_erlang`, rebuild ts.wasm (`make build`).
- Go handle: `LanguageErlang()` in `tsbinding/language.go` + `languageErlang` field + `mod.ExportedFunction("tree_sitter_erlang")` in `tsbinding/treesitter.go`.
- `types.Language` enum (`LanguageErlang = "erlang"`) + `AllLanguages`; `extraction.Lang` enum + pool.go case; `.erl`/`.hrl` → LanguageErlang in `indexer/orchestrator.go`; `extraction/languages/erlang.go` extractor config + registry.
- Probe real node-type strings first (the probe harness) before writing the config.

Erlang symbol mapping (no classes; functions are name/arity):
- `-module(name).` → module (or namespace) node.
- function clauses `fname(...) -> ...` → function node; capture name/arity (Erlang identity is name+arity, e.g. `foo/2`).
- `-record(name, {...}).` → struct (or type_alias) node + fields → field nodes.
- `-behaviour(gen_server).` → references/implements edge to the behaviour module.
- `-export([f/1, g/2]).` → mark exported functions (IsExported); `-import` → import edges.
- `-define(MACRO, ...).` → constant/macro node.
- call edges: `mod:fun(args)` and local `fun(args)` → calls edges (resolve by name/arity).

Update the grammar manifest README (`tsbinding/.../grammars/README.md` ## Grammars included) and add an Erlang corpus row to `scripts/code-eval/corpus.tsv` (a real OTP app — e.g. a gen_server-based project — for the eval). 

Why: BEAM-ecosystem coverage; user has potential in-house Erlang. Note OTP behaviours (gen_server/gen_statem/supervisor) are the Erlang analogue of "framework routes" — a future Erlang "framework" resolver could surface OTP supervision trees, but v1 is language symbols only. User request after SQL support shipped.
