# Code-intel: synthesized package nodes for external imports


## Goal


Every external npm import resolves to a shared `package` node (`package:npm/<name>`) via an `imports` edge, so importers of one package converge on one hub; leftover JS-family external refs stop accumulating in `unresolved_refs`. Existing DBs heal on their next resolve run without re-indexing.


## Non-goals


- Go, Python, Rust ecosystems (Go needs a `go.mod`-driven resolver first — its leftover refs are a classification bug, not a package-node gap).
- URL-scheme specifiers (no minting; refs remain, status quo).
- Canonicalizing bare builtins (`fs`) onto `node:` names.
- Manifest (`package.json`) driven identity.
- Deleting non-mintable leftover refs (a design open question, resolved here to the recommended status quo — flip this bullet if the approval answers otherwise).
- New edge kinds, provenance marking, or UI group additions.


## Success criteria


- [x] Normalizer unit tests: `@scope/pkg/deep/path.js` → `@scope/pkg`; `pkg/sub` → `pkg`; `node:fs/promises` → `node:fs/promises`; `vitest` → `vitest`; `https://…` → no package.
- [x] Two files importing `@scope/pkg` (one deep, one bare) yield edges to the same `package:npm/@scope/pkg` node.
- [x] On this repo after `index` + `sync`: external-classifiable JS-family leftover import refs reach 0 (10 remain, all *relative* imports of non-indexed asset files — `./style.css`, `./SessionPlayer.vue`; see followup `extension-probe-missing-vue-svelte` — which external classification correctly never touches); import-node zero-outgoing count drops to the same residue.
- [x] Idempotence: two consecutive `sync` runs — identical package-node `updated_at` values, identical edge counts (no FTS/trigger churn).
- [x] Deletion round-trip: removing a package's last importer and re-indexing sweeps the package node.
- [x] Regression pin: a tsconfig-aliased specifier whose target file is not indexed mints **no** package node.
- [x] `atomic code callers "@hapi/hapi"` returns the importing nodes (verified live, 185 importers). The hub's out-degree is 0 in the graph (verified by query); the CLI `callees` *surface* additionally unions the same-named import nodes' edges — recorded as followup `cli-callers-name-collision-package-imports`, not a graph defect.
- [x] `TestNodeKindCount` updated (38 → 39); full `go test ./...` green; vet/gofmt clean.
- [x] Spec amendments landed: `code-intel-resolution.md` (external classification now yields a target), `code-intel-engine.md` node-kind roster (appendix C), `code-graph.md` kind→group note.


## Approach


Mint in the resolution batch loop with a `ResolvedImport.PackageName` seam and Step-5 early return (approach B, npm-only v1) — see `docs/design/code-intel-package-nodes.md`.


## Change tree


    atomic/internal/codeintel/types/
    ├── types.go ......................... M  (NodeKindPackage; AllNodeKinds)
    └── types_test.go .................... M  (count 39 + want list)
    atomic/internal/codeintel/extraction/
    └── helpers.go ....................... M  (GenerateNodeID: package early-return, mirrors file:)
    atomic/internal/codeintel/resolution/
    ├── resolver.go ...................... M  (ResolvedImport.PackageName; npmPackageName normalizer)
    ├── resolver_test.go ................. M  (normalizer + external-with-package coverage)
    ├── pipeline.go ...................... M  (Step-5 early return bypassing targetKind; batch-loop mint; orphan-package sweep)
    └── pipeline_test.go ................. M  (mint/dedup/sweep/alias-regression tests)
    atomic/internal/serve/frontend/public/
    └── code-graph.js .................... M  (kind 'package' → import-export group)
    docs/spec/
    ├── code-intel-resolution.md ......... M  (amendment + change-log entry)
    └── code-graph.md .................... M  (kind→group note + change-log entry)


## Outline


    atomic/internal/codeintel/types/types.go
      NodeKindPackage — "package"

    atomic/internal/codeintel/resolution/resolver.go
      ResolvedImport.PackageName — set only when Kind == External and a package is derivable
      npmPackageName — the npm identity rule; "" for URL-scheme

    atomic/internal/codeintel/extraction/helpers.go
      GenerateNodeID — package-kind early return (deterministic id, mirrors the file: exception)

    atomic/internal/codeintel/resolution/pipeline.go
      resolveOne (Step 5) — an External verdict with a derivable package resolves to the package target
      packageMintSet — known-package tracking; genuinely-new nodes upserted before their edge within the batch transaction
      sweepOrphanPackages — post-batch delete of zero-inbound package nodes

    atomic/internal/serve/frontend/public/code-graph.js
      KIND_GROUPS — package: 'import-export'


## Flows


    Flow: external import resolution
    1. batch loop hands an imports-kind ref to resolveOne
    2. ResolveImport → External + PackageName "@hapi/hapi"
    3. resolveOne returns target package:npm/@hapi/hapi (no GetNode probe)
    4. loop: package node absent from mint set → upsert node, add to set
    5. InsertEdge import-node → package node; ref deleted (existing path)

    Flow: orphan sweep
    1. all batches committed
    2. sweep deletes package nodes with zero inbound imports edges


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Kind + ID + normalizer | types.go, helpers.go, resolver.go + tests | atomic-implementer (mode: feature) | ~5 | normalizer table tests; count test |
| 2 | Mint loop + sweep + alias regression pin | pipeline.go + tests | atomic-implementer (mode: feature) | ~2 | mint/dedup/sweep/idempotence tests |
| 3 | UI map + spec amendments + this-repo validation | code-graph.js, docs/spec/*, live index | atomic-implementer (mode: surgical) + orchestrator | ~3 | criteria 3-4, 7, 9 measured/landed |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| FTS trigger churn from re-upserting existing packages each run | med | warm-set guard; idempotence criterion pins updated_at stability |
| targetKind fatal-abort path hit by a not-yet-minted target | med | Step-5 early return bypasses it; test constructs the exact path |
| Alias-miss minting bogus packages | low | structural (B mints only on External verdict, post-alias); regression test pins it |


## Change log

- **2026-08-08 — Implemented + two criterion corrections.** CP1 landed at `a9d57d4`, CP2 at `5a04921`, CP3 at `3470085`. Measured: taxgentic/server mints 72 package hubs (`@hapi/hapi` in-degree 185 — the exact importing-statement count; `vitest` 124, `joi` 87); this repo mints npm hubs (`bun:test` 77, `kysely` 28) with Go fully untouched (2,816 Go refs remain, zero non-npm package nodes). **Corrections:** (a) the "leftover JS-family refs reach 0" criterion assumed all leftovers were external — measured residue (10 this repo / 7 taxgentic) is entirely relative imports of non-indexed asset files, which external classification rightly never touches; criterion re-scoped to external-classifiable refs. (b) the "callees returns empty" criterion held at the graph level (hub out-degree 0) but the CLI name-lookup surface unions same-named import nodes; tracked by followup `cli-callers-name-collision-package-imports`.
