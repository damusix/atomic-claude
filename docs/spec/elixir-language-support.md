# Spec: Elixir tree-sitter language support

## Goal

Elixir source (`.ex`, `.exs`) currently extracts 0 nodes because Elixir is not among the supported tree-sitter languages. Add Elixir end-to-end so the code-intel engine extracts module and function symbols (and the obvious edges) from Elixir files. Unblocks `phoenix-route-resolver` (the Phoenix router DSL needs a real Elixir AST).

## Approach

Tree-sitter, not regex. A grammar spike (2026-06-18) confirmed `elixir-lang/tree-sitter-elixir` v0.3.5 (commit `e2d9e6e0e76b0c436fa48a0b8c32a031d0cbdf49`) ships `parser.c` at `LANGUAGE_VERSION 14` — inside the binding's ABI ceiling (`tsbinding/src/api.h`: version 14, min-compatible 13) — plus a `scanner.c` external scanner, and exports the symbol `tree_sitter_elixir`. This is the same shape as the working dart/luau/objc/pascal external pins, so no ABI wall (the Dart *call-extraction* wall is a different grammar's missing node type, not an ABI problem). The regex-matcher fallback in the original directive is therefore not used; record that in the change log if the build later proves otherwise.

Follow the documented "Add a new grammar" procedure in `atomic/internal/codeintel/tsbinding/CLAUDE.md` (two layers: into the wasm, then into the engine). The 20 existing language extractors under `atomic/internal/codeintel/extraction/languages/` are the reference shape.

## Success criteria

- `lib/ts.wasm` rebuilds with the Elixir grammar compiled in and exports `tree_sitter_elixir`.
- A real `.ex` file parses to a non-empty named-node tree (no longer 0 nodes).
- `.ex` and `.exs` files route to the Elixir language in the indexer.
- Extraction of a representative Elixir fixture yields, at minimum: `defmodule` → module symbol, `def`/`defp` → function symbols (public vs private reflected in IsExported), `defstruct` → struct symbol. Edges where cheap and unambiguous: `alias`/`import`/`use` → import/reference edges; local and remote (`Mod.fun(...)`) calls → call edges.
- `go test -count=1 ./internal/codeintel/extraction/...` passes, including a new Elixir extraction test that asserts the symbols above on the fixture (the test fails if Elixir extraction regresses to 0).
- Render/bundle parity and the existing suite stay green; `src/` is never committed (gitignored); `grammars.json` + `Makefile` + `lib/ts.wasm` + Go changes commit together.

## Checkpoints

| # | Checkpoint | Scope | Done when |
|---|-----------|-------|-----------|
| 1 | Grammar into the wasm | Add an `external[]` entry to `tsbinding/grammars.json` pinning tree-sitter-elixir at the verified ABI-14 commit (`files: ["parser.c","scanner.c"]`); add the source paths + `-Wl,--export=tree_sitter_elixir` to the `Makefile` `build` recipe; `make fetch` then `make build` to regenerate `lib/ts.wasm`. | `lib/ts.wasm` rebuilt; a probe confirms the binding can load `tree_sitter_elixir` and parse a sample `.ex` into named nodes. `src/` left untracked. |
| 2 | Engine wiring + extractor + test | Add the `Lang`/`types.Language` Elixir entries, the `SetLanguage` (pool) mapping to `tree_sitter_elixir`, the `.ex`/`.exs` → Elixir entries in the indexer's `extToLanguage`, and an Elixir extractor config in `extraction/languages/` (registered). Probe the grammar's real node-type strings before writing the config — do not guess node names. Add an Elixir fixture + an extraction test. | `go test -count=1 ./internal/codeintel/extraction/...` green; the Elixir fixture extracts module + public/private functions + struct per the success criteria. |

## Out of scope

- Phoenix route resolution (separate follow-up `phoenix-route-resolver`, depends on this).
- Column-level or macro-expansion fidelity beyond the symbol/edge set above.
- A corpus.tsv eval row is nice-to-have, not a gate (add if cheap).

## Change log

(empty — first version)

## Implementation log

- **CP1** (`42135a8`) — vendored `elixir-lang/tree-sitter-elixir` v0.3.5 (`e2d9e6e`, ABI 14, parser.c + scanner.c) into `grammars.json` + `Makefile`; rebuilt `lib/ts.wasm`; added the binding handle + a parse probe. Tree-sitter path confirmed; regex fallback not needed.
- **CP2** (`198fcd1`) — wired Elixir through the extraction engine (`LangElixir`, pool mapping, `types.LanguageElixir`, `.ex`/`.exs` routing) and added the `elixir.go` extractor. tree-sitter-elixir models macros as `call` nodes, so the extractor matches call targets: `defmodule`→module, `def`/`defp`→function (exported/private), `defstruct`→struct, `alias`/`import`/`use`→import edges, calls→call edges. A nil-safe `GetName` hook + a `StructTypes`/`ResolveKind` guard in `visitFunctionBody` support the call-node model without affecting the other 21 languages. 8 Elixir extraction tests; full codeintel suite green.
