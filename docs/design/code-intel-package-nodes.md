# Code-intel: synthesized package nodes for external imports


## Problem


External import specifiers classify `ResolvedKindExternal` in `ResolveImport` and produce no target — `resolveOne` reaches Step 7 with zero candidates and skips. Every external import node keeps only its inbound `contains` edge: on taxgentic/server that is ~900 orphan import nodes; on this repo, 1,258 import nodes with zero outgoing edges whose unresolved refs are re-scanned by every resolve run, forever (refs are only deleted when they yield an edge). All files importing `@hapi/hapi` should converge on one hub — that convergence is the dependency-graph signal the code graph currently lacks.


## Goals / Non-goals


- Goals: one shared node per external npm package; `imports` edges from each import node to it; survives pruning; idempotent across runs; visible in graph/search/callers without new UI machinery.
- Non-goals: Go ecosystem (94% of this repo's leftover refs are Go, but 699 of 1,180 are a *classification/resolution* bug — own-module paths and third-party misclassified — that a `go.mod`-driven resolver should fix first; minting 481 stdlib hubs on that foundation is decoration); Python/Rust seams (no validation corpus here); URL-specifier package parsing (per-CDN formats); manifest-driven identity (no `package.json` reader exists in the resolver — only tsconfig aliases).


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | No package nodes; just delete external refs post-verdict | Stops the ref accumulation; ~10 lines | Delivers none of the asked-for graph signal; imports stay orphans |
| B | Mint in the resolution batch loop: `ResolvedImport` gains `PackageName`; Step 5 returns early with the package target; loop upserts unseen package nodes inside the existing edge-insert transaction | Single-threaded resolution → no locking; reuses ref-deletion, edge-insert, and `NodeExists`-style guard machinery; nothing foreclosed | `resolveOne` return contract grows by one concept |
| C | Post-pass mirroring `resolveSQLStringRefs` (Phase-5 sweep over leftover import refs) | `resolveOne` untouched; exact structural precedent | Must re-call full `ResolveImport` per ref to stay correct — alias expansion precedes external classification, so calling `isExternal` alone would mint bogus packages for unresolved tsconfig aliases |


## Recommendation


**B, scoped to npm (JS-family languages) in v1.**

Identity: node ID `package:npm/<name>`; name per the npm rule — `@` prefix → first two slash-segments (`@modelcontextprotocol/sdk/client/stdio.js` → `@modelcontextprotocol/sdk`); `node:` prefix → specifier verbatim (`node:fs/promises` stays whole — each builtin module its own unit); otherwise first segment (`react-dom/client` → `react-dom`). No canonicalizing bare `fs` → `node:fs` (needs a Node-version-dependent allowlist that rots). No minting for `://` specifiers. Deep-import fidelity is not lost — the import node's name is the full specifier and it is the edge's source.

Node shape: `Kind` `package` (new, `AllNodeKinds` 38 → 39); `Name`/`QualifiedName` the normalized package name (FTS-searchable); `FilePath` `""` — legal (`TEXT NOT NULL`, empty allowed), honest, and unmatchable by both pruning paths (`pruneDeleted` and `DeleteNodesByFile` key on real file paths), so survival across runs is by construction; lines 0; `Language` unknown (deterministic — first-importer language would churn on batch order); no metadata (ecosystem lives in the ID).

Edge: kind `imports`, import node → package node, empty provenance (static truth, not synthesized — `isSynthesizedProvenance` untouched). Hub has out-degree 0: a pure sink, so no `callees`/`explore` traversal grows; only `callers`/`impact` on the package fan out — exactly the useful queries. Already traversed by callers/callees (`imports` ∈ `callerCalleeKinds`).

Minting: in the batch loop's existing per-ref transaction, node upserted before edge (FK order), tracked by a map warmed once from `GetNodesByKind(package)` so unchanged runs re-fire no FTS triggers (`UpsertNodeAt` is INSERT OR REPLACE — upsert only genuinely-new IDs). Early return at Step 5 must bypass `targetKind` (a `GetNode` on a not-yet-minted ID is a fatal batch abort); `promoteEdgeKind` is a no-op for `imports` anyway. End-of-resolution sweep deletes package nodes with zero inbound edges (reclaims a package whose last importer vanished — edges cascade on import-node deletion).

UI: `package` maps to the existing `import-export` visual group — degree sizing and quintile shading already make a 900-in-degree hub pop; no 9th group, no SC5 amendment.

Healing: no re-index required — leftover external refs are still in `unresolved_refs` and the batch loop keyset-scans the whole table, so existing DBs grow package nodes on their next `sync`/`resolve`.


## Open questions


- Go: ship npm-only now and file the `go.mod`-driven Go resolver as its own design (recommended), or hold this until that lands so both ecosystems ship together?
- Leftover non-mintable refs (URL-scheme; Go if deferred): keep accumulating (status quo, recommended — deleting them means a later alias/config change can't re-resolve without re-indexing importers), or delete post-verdict?
- Python/Rust: near-zero marginal cost once the seam exists, but no local validation corpus — include or defer (recommended: defer)?
