# tsbinding — tree-sitter WASM binding


This folder is a **separate Go module** (`github.com/malivvan/tree-sitter`, wired into the parent via a
`replace` in `atomic/go.mod`). It loads tree-sitter grammars compiled to a single WASM blob through
wazero — no CGO. The code-intel engine parses source by calling into this binding.

**Branding hard rule:** the reference TypeScript engine's product name must never appear in code,
comments, identifiers, strings, or output anywhere in the code-intel subsystem.


## Artifact model — read this before touching anything


There are two tracks. Do not confuse them.

- `lib/ts.wasm` — **committed, 35MB, the runtime dependency.** Embedded via `//go:embed lib/ts.wasm`
  (`treesitter.go`). `go build`, CI, and goreleaser depend only on this file. It is what ships.
- `src/` — **gitignored, ~208MB, build-time input only.** The tree-sitter runtime C plus the grammar
  parser tables. Fetched on demand from pinned upstreams (`grammars.json` → `fetch-grammars.sh`). The
  Go toolchain never compiles these; only `make build` (zig → wasm) reads them.

Why `src/` is not committed: the generated grammar `parser.c` files are millions of lines of machine
output (`csharp/parser.c` alone is 33MB). Committing them bloated the repo and made `atomic code index`
OOM (it indexed them as C source). Background and the proving experiment:
`/docs/research/tsbinding-vendor-on-demand.md`.

**Never `git add` anything under `src/`.** It is gitignored on purpose.


## Rebuild the wasm


Cold path. Only needed when a grammar, the runtime, or the export surface changes — never for normal
Go work.

```
make build        # fetches src/ if absent, then compiles lib/ts.wasm
make fetch        # (re-)fetch src/ from the pins in grammars.json
make clean-src    # remove the fetched src/
```

Prerequisites: `zig >= 0.13` on PATH (tested with 0.15.2). `make fetch` also needs `git` and `jq`.

`make build` only fetches when `src/` is **absent**. After editing `grammars.json` you must
`make fetch` (or `make clean-src && make build`) to pull the new pins — an existing `src/` is not
re-fetched automatically.


## Verify after any wasm rebuild


From the repo root, run the extraction suite against the new wasm. Use `-count=1` — a wasm change
can otherwise hit the Go test cache.

```
cd atomic
go test -count=1 ./internal/codeintel/extraction/...
```

`extraction/languages` is the multi-grammar parse test; it is the real gate. Equivalence is proven by
tests, not by byte-comparing wasm (compiler output is not deterministic across versions).

Commit `lib/ts.wasm` together with whatever pin/Makefile/Go change produced it. Do not commit `src/`.


## Upgrade an existing grammar


1. Edit the `revision` (and `url` if it moved) in `grammars.json`. For a runtime/bundled grammar that
   means bumping `runtime.revision`; for dart/luau/objc/pascal, the matching `external[]` entry.
2. `make fetch && make build`.
3. Verify (above). Add or update a fixture if the grammar gained syntax.
4. Commit `grammars.json` + `lib/ts.wasm`.

Bumping `runtime.revision` re-pins **all** bundled grammars at once (they share one upstream). To move
a single bundled grammar independently, promote it to an `external[]` entry instead.


## Add a new grammar


Two layers: get it into the wasm, then teach the engine the language.

**Into the wasm:**

1. Add the grammar to `grammars.json` — to `runtime.grammars` if the runtime upstream already bundles
   it, otherwise as a new `external[]` entry with `url`, `revision`, and `files`
   (`parser.c`, plus `scanner.c` only if the grammar ships one).
2. In the `Makefile` `build` recipe, add the source paths (`src/<lang>/parser.c` and `scanner.c` if
   present) **and** the export flag `-Wl,--export=tree_sitter_<name>`.
   The export name is the grammar's internal symbol, which is **not always the dir name** —
   e.g. `csharp` → `tree_sitter_c_sharp`, `golang` → `tree_sitter_go`, `typescript` exports both
   `tree_sitter_typescript` and `tree_sitter_tsx`. Confirm the symbol in the grammar's
   `src/parser.c` (`TSLanguage *tree_sitter_<name>(void)`).
3. `make fetch && make build`, then verify.

**Into the engine** (so files of that language are actually parsed — larger change, see the code-intel
signals domain for the full map):

- `extraction/binding.go` — add a `Lang<Name>` constant.
- `extraction/pool.go` — map it in the `SetLanguage` switch to the exported `tree_sitter_<name>`.
- `extraction/languages/registry.go` + `types` — register a `types.Language<Name>` and its extractor config.
- `indexer/orchestrator.go` — add the file-extension → language entries to `extToLanguage`.
- Add a fixture and an extraction test; verify with `-count=1`.

Commit `grammars.json` + `Makefile` + `lib/ts.wasm` + the Go changes together. Not `src/`.


## Grammar-facing header note


External grammars `#include "tree_sitter/parser.h"`. Every grammar repo ships `src/tree_sitter/parser.h`;
the fetch copies it from one designated grammar (`grammar_header_from` in `grammars.json`, currently
`dart`) into `src/tree_sitter/parser.h` so all external grammars resolve it via `-I src/`. Bundled
grammars instead carry their own local `parser.h` inside each grammar dir.
