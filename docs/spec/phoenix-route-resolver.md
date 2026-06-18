# Spec: wire the Phoenix route resolver to Elixir

## Goal

`resolution/frameworks/elixir.go` already implements a complete `PhoenixResolver` (regex over the Phoenix router DSL: `get/post/put/patch/delete "/path", Controller, :action`, mix.exs `:phoenix` detection, route nodes + handler refs). It was hard-coded to `types.LanguageUnknown` because Elixir was not a supported language, so the resolution pipeline's `getApplicableResolvers(lang)` never matched it to any file → 0 routes on Phoenix corpora. Now that `elixir-language-support` landed (this branch is stacked on it), wire the resolver to `LanguageElixir` so it actually runs on indexed `.ex` router files.

## Approach

Switch the resolver from `LanguageUnknown` to `types.LanguageElixir` — the regex extraction logic itself is correct and stays as-is. This un-gates it: `getApplicableResolvers(LanguageElixir)` will now include PhoenixResolver, so its `Extract` runs on `.ex` files. No new parsing, no AST dependency (the resolver regexes router.ex content directly).

## Success criteria

- `PhoenixResolver.Languages()` returns `[types.LanguageElixir]`; route nodes (`MakeRouteNode`) and handler refs carry `LanguageElixir`, not `LanguageUnknown`.
- The now-stale package/method comments ("Elixir is NOT in the … set / appendix C", "LanguageUnknown") are corrected to reflect that Elixir is a supported language.
- A test proves routes extract from a representative Phoenix `router.ex` (verb lines → route nodes with method+path; `:action` atoms → handler refs), with `LanguageElixir` on the nodes/refs. The test fails if extraction regresses to 0 routes.
- `getApplicableResolvers(types.LanguageElixir)` includes the Phoenix resolver (assert, or prove via the extraction test path).
- `go test -count=1 ./internal/codeintel/resolution/...` green (the existing `frameworks/elixir_test.go` updated to expect `LanguageElixir`).

## Out of scope

- `scope "/prefix" do … end` block prefix expansion and `resources` macro expansion — the existing resolver documents these as unsupported/best-effort; keep that boundary unless trivially free.
- Cloning the upstream gothinkster realworld app into the corpus; a representative fixture in the test is sufficient. A `corpus.tsv` row is nice-to-have, not a gate.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|-----------|-------------|----------|
| 1 | Un-gate Phoenix to Elixir | `resolution/frameworks/elixir.go` + `elixir_test.go` | `Languages()` + route nodes + refs use `LanguageElixir`; stale comments fixed; existing test updated; a router.ex fixture test proves non-zero route extraction with `LanguageElixir`; `resolution/...` suite green. |

## Change log

(empty — first version)

## Implementation log

- **CP1** — switched `PhoenixResolver` from `LanguageUnknown` to `types.LanguageElixir` (`Languages()`, route nodes, handler refs) and corrected the stale "Elixir absent from appendix C" comments. The regex route-parsing was already complete; this un-gates it so `getApplicableResolvers(LanguageElixir)` runs it on indexed `.ex` router files. Tests: realworld-style `router.ex` fixture extracts 7 routes + 5 handler refs (all `LanguageElixir`), and `Languages()` ⊇ `{LanguageElixir}` proves the un-gating. `resolution/...` suite green. Reviewer: PASS, 0 findings.
