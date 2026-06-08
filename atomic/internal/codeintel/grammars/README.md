# grammars

Manifest for the 19-grammar tree-sitter WebAssembly binary used by the
`atomic code` engine. Loaded at runtime via `wazero`; no C toolchain required on
the target host.

The compiled `ts.wasm` is embedded by the binding fork via `//go:embed lib/ts.wasm`
in `../tsbinding/treesitter.go` — that is the single committed copy (`go:embed`
cannot reach across module dirs, so the binding embeds from within its own
module). This dir holds only this manifest; CP0b decides whether the embed home
moves to a dedicated `grammars` package that injects bytes into the binding.

## Regen command

Run from the `tsbinding/` directory:

```
cd atomic/internal/codeintel/tsbinding
make build
```

Output: `tsbinding/lib/ts.wasm` (embedded directly — no copy step).
Prerequisites: `zig` >= 0.13.0 on PATH (build-time only).

## Grammars included

| Language | Grammar function | Source |
|----------|-----------------|--------|
| C | `tree_sitter_c` | malivvan/tree-sitter (tree-sitter/tree-sitter-c v0.23.4) |
| C++ | `tree_sitter_cpp` | malivvan/tree-sitter (tree-sitter/tree-sitter-cpp v0.23.4) |
| C# | `tree_sitter_c_sharp` | malivvan/tree-sitter (tree-sitter/tree-sitter-c-sharp v0.23.1) |
| Java | `tree_sitter_java` | malivvan/tree-sitter (tree-sitter/tree-sitter-java v0.23.5) |
| JavaScript | `tree_sitter_javascript` | malivvan/tree-sitter (tree-sitter/tree-sitter-javascript v0.23.1) |
| Go | `tree_sitter_go` | malivvan/tree-sitter (tree-sitter/tree-sitter-go master@7cb21a6) |
| Kotlin | `tree_sitter_kotlin` | malivvan/tree-sitter (fwcd/tree-sitter-kotlin 0.3.8) |
| Lua | `tree_sitter_lua` | malivvan/tree-sitter (tjdevries/tree-sitter-lua master@4932594) |
| PHP | `tree_sitter_php` | malivvan/tree-sitter (tree-sitter/tree-sitter-php v0.23.11) |
| Python | `tree_sitter_python` | malivvan/tree-sitter (tree-sitter/tree-sitter-python v0.23.6) |
| Ruby | `tree_sitter_ruby` | malivvan/tree-sitter (tree-sitter/tree-sitter-ruby v0.23.1) |
| Rust | `tree_sitter_rust` | malivvan/tree-sitter (tree-sitter/tree-sitter-rust v0.23.2) |
| Scala | `tree_sitter_scala` | malivvan/tree-sitter (tree-sitter/tree-sitter-scala v0.23.4) |
| Swift | `tree_sitter_swift` | malivvan/tree-sitter (alex-pinkus/tree-sitter-swift 0.5.0) |
| TypeScript | `tree_sitter_typescript` | malivvan/tree-sitter (tree-sitter/tree-sitter-typescript v0.23.2) |
| TSX / JSX | `tree_sitter_tsx` | malivvan/tree-sitter (tree-sitter/tree-sitter-typescript v0.23.2 tsx/) |
| Dart | `tree_sitter_dart` | UserNobody14/tree-sitter-dart @ 311a009 (ABI 14; a9bdfa3 bumped to ABI 15 which is incompatible with our ts runtime) |
| Luau | `tree_sitter_luau` | amaanq/tree-sitter-luau @ a8914d6 |
| Objective-C | `tree_sitter_objc` | amaanq/tree-sitter-objc @ 181a81b |
| Pascal | `tree_sitter_pascal` | Isopod/tree-sitter-pascal @ 042119e |

## Size note

35 MB uncompressed, embedded unconditionally via `//go:embed`. Binary size is an
accepted trade-off per the project owner.
