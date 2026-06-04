# Code-intelligence engine — extraction (part 2/5)

Part 2 of the 5-part code-intelligence engine port. **Umbrella:**
`docs/spec/code-intel-engine.md` (goal, roadmap, dependency DAG, authoritative
**reference appendix A–O**). **Design:** `docs/design/code-intel-engine.md`.

**Depends on:** part 1 (`code-intel-substrate`) green — needs `types`, `db`, the
binding iface, and the 19-grammar `ts.wasm`. **Blocks:** part 3 (resolution)
onward — extraction emits the nodes + `unresolved_refs` resolution consumes.

Brand bindings (from the umbrella): commands `atomic code <verb>`; data dir
`<projectRoot>/.claude/.atomic-index/`. Never emit the reference's product name.

## Scope

Turn source files into nodes + unresolved references in the DB: the node-id
scheme + helpers (master CP5), the generic extractor core (CP6), the
`LanguageExtractor` contract + the first 5 languages (CP7), the remaining 14
languages (CP8), the 5 standalone format extractors (CP9), and the orchestrator
+ sync invariant (CP10).

**Contracts (authoritative, in the umbrella appendix):** B (node-id scheme — the
`line`-embedding + delete-then-reinsert consequence), D (extension→language map),
E (the extractor contract + `LanguageExtractor` shape + standalone pattern + the
SKIP list).

## Success criteria

- [ ] Node ids match what the reference generates for the same
      `(filePath, kind, name, line)` — **golden vectors exported from the
      reference impl**, committed as fixtures, run as a CI gate (master CP5).
- [ ] All 19 tree-sitter languages + 5 standalone formats extract nodes/edges on
      a fixture each; node-count is stable across re-index (no explosion).
- [ ] A deliberately-broken file is skipped and its error recorded in
      `files.errors` — extraction never aborts on one parse failure.
- [ ] Re-sync of a file with a line-shifted symbol leaves **no orphan ids** (sync
      deletes the file's nodes, cascade clears edges, before re-extract).
- [ ] Memory stays bounded across a full index (per-instance recycle wired).

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **(master CP5) Binding iface + node-id + helpers**: binding interface (impl A behind it), `generateNodeID`, text/childByField/precedingDocstring helpers. **Golden node-id vectors exported from the reference impl, committed as fixtures.** | `internal/codeintel/extraction`; ref `src/extraction/tree-sitter-helpers.ts` (COPY); appendix B | node-id vector test passes against reference-exported goldens (CI gate); docstring walk works |
| 2 | **(master CP6) Generic extractor core**: `TreeSitterExtractor` with `nodeStack`, `visitNode` dispatch, `createNode` (id + qualified-name + `contains` edge), `extract*` family, `visitFunctionBody`. Best-effort per file — record errors in `files.errors`, continue. | `internal/codeintel/extraction`; ref `src/extraction/tree-sitter.ts` (COPY logic; SKIP `tree.delete`); appendix E | A TS fixture yields the expected node/edge tree; a deliberately-broken file is skipped + recorded |
| 3 | **(master CP7) LanguageExtractor iface + registry + TS/JS/Python/Go + one non-C-family (Rust or C++)**: the config-object shape + 5 extractors spanning grammar families, to prove the binding generalizes before fan-out. | `internal/codeintel/extraction/languages`; ref `src/extraction/languages/*.ts` (COPY); appendix E | Each extracts functions/classes/imports/calls; the non-C-family grammar's distinctive nodes (macros/templates) extract correctly |
| 4 | **(master CP8) Remaining language extractors — 14 independent sub-slices**, each green on its own fixture + grammar-vocabulary check (Java, C/C++, C#, Swift, Kotlin, Ruby, PHP, Scala, Lua, Luau, Dart, Pascal, ObjC, + any deferred from CP7). | `internal/codeintel/extraction/languages`; ref `src/extraction/languages/*.ts` (COPY); appendix E | Each parses a fixture; node-count stable on re-index |
| 5 | **(master CP9) Standalone extractors** (Vue, Svelte, Liquid, DFM, MyBatis): regex/SFC parsing, line-offset fixup, component nodes. | `internal/codeintel/extraction/standalone`; ref `src/extraction/vue-extractor.ts` et al. (COPY); appendix E (standalone pattern) | `.vue` yields a component node + offset-correct child nodes |
| 6 | **(master CP10) Orchestrator + sync invariant**: gitignore-aware scan (git ls-files fast path + walk fallback), batched read via the parser pool, language detect, `storeExtractionResult` (hash dedup, filter-before-insert), `sync` (size/mtime pre-filter → hash). **Sync deletes a changed file's nodes (cascade clears edges) before re-extracting — node-id embeds `line`, so REPLACE-in-place leaves orphans.** Wire the per-instance memory-reclaim. | `internal/codeintel/extraction`; ref `src/extraction/index.ts` (COPY logic incl. `deleteNodesByFile` before insert; SKIP worker threads); appendix B, D | Full index; re-sync of a file with a line-shifted symbol leaves no orphan ids; memory bounded |

## Risks

Inherited from the umbrella: **R-WALK** (per-node Go↔WASM traversal ~70× slower —
walk via tree cursor / bulk in-WASM, never per-node `Child` from Go), **R-E**
(sync orphans because node-id embeds `line` — CP6/CP10 delete-then-reinsert),
**R3** (node-id divergence breaks edges — CP1 golden-vector CI gate), **R-C**
(broad-parity fan-out is large — CP3 proves the contract incl. a non-C-family
grammar; CP4 is 14 independently-green sub-slices).

## Change log

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP5–CP10 (binding/node-id, generic
extractor, language extractors ×19, standalone ×5, orchestrator + sync) from
`docs/spec/code-intel-engine.md`. Contracts authoritative in the umbrella
appendix (referenced by letter).

**Why:** Split the 25-checkpoint monolith into five dependency-ordered parts.
