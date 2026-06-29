---
id: codeintel-language-pack-expansion
title: Expand code-intel language support toward the tree-sitter language pack
created: "2026-06-29"
origin: |
    user request — track xberg-io/tree-sitter-language-pack (306 grammars) as the target language set
kind: plan
review_by: "2026-08-28"
status: open
file: atomic/internal/codeintel/extraction/languages/registry.go
---

Track the [tree-sitter language pack](https://github.com/xberg-io/tree-sitter-language-pack/blob/main/sources/language_definitions.json) (306 grammars) as the aspirational target set for code-intel extraction. The engine currently extracts symbols for ~24 programming languages (typescript/tsx/jsx, javascript, python, go, rust, java, c, cpp, csharp, php, ruby, swift, kotlin, dart, pascal, scala, lua, luau, objc, elixir, erlang) plus standalone SQL.

## Scope discipline (do NOT target all 306)

Most pack entries are config / data / markup / doc grammars (gitignore, csv, json, toml, dockerfile, make, markdown, latex, the git_* family, regex, comment, diff, ...). They have no call graph and would only add index noise. Target only real programming languages whose definition + call/reference edges carry query value. Filter the pack to that subset before committing to a number — the real candidate set is closer to 60-80, not 306.

## Per-language wiring cost (mirrors the elixir recipe)

Each new language needs:

1. Grammar vendored into the ts.wasm bundle at build time, wired in `tsbinding/language.go`.
2. A `Language` constant in `types/types.go` plus file-extension routing.
3. An extractor in `extraction/languages/<lang>.go`: definitions (functions, types, modules) plus call/reference edges, mirroring an existing extractor.
4. Tests, ideally a real-repo + Haiku-verified validation pass (clone a real repo, index, fan out Haiku agents to check extracted edges against source; `code index` is incremental, rm the db to force a re-index).

Grammar quality varies. See codeintel-dart-grammar-wall — Dart call extraction is blocked by an upstream grammar limit, so adding the grammar is necessary but not always sufficient.

## Suggested priority tiers (curate before building)

- Tier 1 (common, real call graphs, likely user demand): zig, nim, ocaml, haskell, julia, r, clojure, gleam, crystal, fsharp, gdscript, solidity, bash, hcl/terraform, proto, graphql.
- Tier 2 (niche but real): ada, cobol, fortran, d, haxe, v, racket, scheme, commonlisp, elisp, purescript, rescript, odin, mojo, matlab, powershell, move, cairo, clarity, prisma, vhdl, systemverilog, tcl, prolog.
- Tier 3 (config / markup / data): descope, or extract references only if a concrete need appears.

## Cheaper parallel win

The embedded-SQL harvester (`HarvestEmbeddedLiterals`) already covers 16 host languages. Extending its per-language `EmbeddedLiteralConfig` is a much lower-cost way to widen SQL-lineage coverage than a full extractor, and it compounds the SQL moat.

## Related

elixir-language-support, erlang-language-support, codeintel-dart-grammar-wall, phoenix-route-resolver, rails-dsl-symbol-synthesis, codeintel-go-grpc-impl, codeintel-fabric-native-impl.
