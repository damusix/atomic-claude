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

## Synthesis-enabling enrichment (CP16 prerequisite)

The callback synthesizers in `code-intel-resolution.md` master CP16 (appendix G)
run over the committed graph and can only synthesize an edge from a signal the
extractor captured. The v1 extractors (CP5–10) capture declarations + call
*callees*, which is enough for static resolution but not for dynamic-dispatch
synthesis. CP16 (full-enrichment decision, 2026-06-05) reopens extraction to add
three capture abilities. Each is additive — it emits *more* `UnresolvedReference`
rows / node metadata; it does not change existing extraction behavior.

- **EE1 — TSX registration + JSX child references.** Register a TSX
  `LanguageExtractor` in `extraction/languages/registry.go` (the `LangTSX` grammar
  is already wired in `extraction/pool.go`); ensure `.jsx` (JavaScript grammar)
  is handled too. The TS/JS/TSX configs emit a `references` `UnresolvedReference`
  for each JSX element tag (`jsx_element` / `jsx_self_closing_element`) whose name
  is PascalCase (a component usage), from the enclosing function/component node.
  Enables `jsx-render` + Vue/template child synthesis.
- **EE2 — call-argument capture.** `UnresolvedReference` gains an `Arguments`
  field (string-literal arguments of the call, in order), persisted to a new
  `unresolved_refs.arguments TEXT` column via a **forward migration** (the CP4
  migration machinery). The extractor populates it for `call_expression` sites
  (at least the leading string-literal args). Enables `event-emitter`,
  `rn-event-channel`, and any arg-keyed synthesizer to correlate `.on('e',…)` ↔
  `.emit('e')` by event name.
- **EE3 — field-assignment capture.** Assignment expressions that store a
  callable into a field/property (`this.x = fn`, `obj.cb = handler`) emit a
  `references` UnresolvedReference from the enclosing method node. The
  representation is:

  - `ReferenceKind` = `references`
  - `ReferenceName` = the RHS identifier name (e.g. `"handleData"`); empty
    string `""` for anonymous inline callables (`arrow_function` /
    `function_expression`)
  - `Arguments[0]` = `"field:<fieldName>"` sentinel (e.g. `"field:onData"`)
    — this single-element slice is the discriminator the batch-4 callback
    synthesizer uses to distinguish EE3 field-assignment refs from EE1 JSX
    refs (no Arguments) and EE2 call refs (Arguments = event-name strings)
  - `FromNodeID` = enclosing method/function node ID

  Only emitted when RHS is a plausibly-callable node kind: `identifier`,
  `arrow_function`, or `function_expression`. Non-callable RHS values
  (`number`, `string`, …) are silently skipped.

  Implemented via `LanguageExtractor.FieldAssignmentTypes` (set to
  `{"assignment_expression"}` on TS/JS/TSX configs) + `extractFieldAssignment`
  in `extraction/extractor.go`. Language-agnostic at the
  `assignment_expression` level — any grammar that uses `assignment_expression`
  with `"left"` / `"right"` field names works without language-specific hooks.

  Enables `callback` (field-backed observer) + `closure-collection` synthesizers
  to find the *registration* site, not just the invocation.

- **EE4 — inheritance (heritage) capture.** *(Foundational — also required by the
  query layer's type-hierarchy, not only synthesis.)* The v1 extractor has **no**
  heritage field, so class `extends` / `implements` clauses were never captured and
  no `extends`/`implements` edges are ever created (CP13's extends→implements
  promotion is therefore dead on real data). EE4 adds a
  `LanguageExtractor.InheritanceTypes`/heritage mechanism: when a class/struct
  node is created, the extractor reads its heritage clause and emits one
  `UnresolvedReference` per base type — `ReferenceKind = extends` for superclasses
  and (where the grammar distinguishes it) `implements` for interfaces, from the
  class node, `ReferenceName` = the base/interface name. Resolution then turns
  these into `extends`/`implements` edges (with the appendix-F
  extends→implements promotion when the target is an interface). Wire the heritage
  node types per language (TS `class_heritage`/`extends_clause`/
  `implements_clause`; C++ `base_class_clause`; Java `superclass`/`super_interfaces`;
  etc. — confirm each by real parse). **Also add `method_signature` to the TS
  `MethodTypes`** so interface methods become method nodes. Enables
  `interface-impl` + `cpp-override` + correct `GetTypeHierarchy`.
- **EE5 — identifier call-argument capture.** Extends EE2: a `call_expression`
  ref also records its **identifier** arguments (not just string literals), so a
  registered handler's identity (`.append(handler)`, `.on('e', handler)`) is in
  the graph. Representation contract (additive — does not replace EE2 string args):
  - String literal arg `"login"` → `Arguments` entry: `"login"` (unchanged EE2)
  - Identifier arg `onLogin` → `Arguments` entry: `"arg:onLogin"` (EE5 prefix)

  The `"arg:"` prefix is the discriminator. Non-colliding prefix table:
  - `"arg:"` — EE5 identifier arg (this enrichment)
  - `"field:"` — EE3 field-assignment discriminator
  - `"jsx:"` — EE1 JSX child-component discriminator

  Implementation: `extractStringArgs` renamed to `extractCallArgs`; adds an
  `"identifier"` case alongside the existing `"string"` / `"string_literal"` case.
  Returns nil when no capturable args exist (nil convention preserved).
  Enables `closure-collection` (+ sharpens `event-emitter`/`callback` handler
  linkage). Anonymous closures (Swift/Kotlin trailing `{ ... }`) have no identifier
  node and produce no `"arg:"` entry — honest documented gap.

Specialized synthesizers (go-grpc-stub-impl, mybatis-java-xml, fabric-native-impl,
gin-middleware-chain) rely on EE1–EE5 plus method/standalone-XML nodes; where a
specific signal is still missing, the owning synthesizer batch adds the minimal
capture it needs and records it here. `flutter-build` is **not** closable by
enrichment — the Dart grammar (ABI 14) has no `call_expression` node, so Dart
`setState` calls cannot be captured; it remains a documented stub.

Each enrichment ships with its own fixtures proving the new rows appear; no
existing extraction test may regress.

## Change log

### 2026-06-06 — EE5 representation finalized; closure-collection synthesizer activated

**What changed:** The EE5 bullet in "Synthesis-enabling enrichment" was updated from
a speculative "e.g." description to the canonical, implemented contract. Chosen
representation: identifier args recorded with `"arg:<ident>"` prefix in `Arguments`
(additive alongside EE2 string args). Implementation: `extractStringArgs` renamed to
`extractCallArgs` + `"identifier"` case added. Anonymous closures (no extractable
identifier) produce no `"arg:"` entry — honest documented gap. `ClosureCollectionSynthesizer`
is now activated (was a documented stub); correlates `<receiver>.append(<handler>)` +
`<receiver>.forEach` by receiver name, capped at `CC_FANOUT_CAP=8`.

**Why:** Completing the EE5 spec entry with the confirmed representation allows
downstream synthesizers and fresh subagents to know exactly what to read and produce.
The closure-collection synthesizer was gated on EE5 — with identifier capture live,
the signal needed for Swift/Kotlin forEach→append correlation is available.

**Superseded:** EE5 bullet previously described the representation as `"e.g. an 'arg:<ident>'-prefixed entry, or a parallel field"` without committing. Now committed to `"arg:"` prefix.

### 2026-06-05 — EE4 (heritage) + EE5 (identifier call-args) enrichment added

**What changed:** Added EE4 (inheritance/heritage capture — emit `extends`/
`implements` refs from class heritage clauses + add `method_signature` to TS
`MethodTypes`) and EE5 (identifier call-argument capture, extending EE2). Corrected
the "Specialized synthesizers" paragraph, which previously claimed `extends`/
`implements` edges were "already-captured" — they were not; the v1 extractor had
no heritage field.

**Why:** CP16 batch 5 proved (reviewer-confirmed) that interface-impl + cpp-override
have no inheritance edges to work with — the extractor never captured heritage.
This is foundational (the query layer's `GetTypeHierarchy` needs `extends`/
`implements` edges too), so the owner chose to close it. EE5 unblocks
closure-collection (identifier handler identity). `flutter-build` stays a stub —
the Dart grammar has no `call_expression` node, uncapturable by any enrichment.

**Correction:** the prior "Specialized synthesizers… rely on already-captured…
`extends`/`implements` edges" line was false — those edges were never produced.
EE4 makes it true.

### 2026-06-05 — EE3 representation documented (field-assignment capture)

**What changed:** The EE3 bullet in "Synthesis-enabling enrichment" was expanded
from a one-line description to a full representation contract: `ReferenceKind`,
`ReferenceName`, `Arguments[0]` sentinel format (`"field:<fieldName>"`),
`FromNodeID`, callable-only emission rule, and implementation pointers
(`LanguageExtractor.FieldAssignmentTypes`, `extractFieldAssignment`).

**Why:** The batch-4 callback synthesizer builder needs an exact, unambiguous
contract to distinguish EE3 field-assignment refs from EE1 JSX refs and EE2
call refs. A one-line description was insufficient as ground truth.

### 2026-06-05 — Synthesis-enabling enrichment (EE1–EE3) added for CP16

**What changed:** Added the "Synthesis-enabling enrichment" section: TSX/JSX child
references (EE1), call-argument capture on `UnresolvedReference` via a forward
migration (EE2), and field-assignment capture (EE3). These are additive captures
that feed the CP16 callback synthesizers.

**Why:** CP16 batch 1 proved (reviewer-confirmed, 2026-06-05) that 13 of the 14
synthesizers cannot derive their edges from the v1 graph — the needed signals
(JSX usages, call arguments, field assignments) were never captured. Project
owner chose full enrichment, so extraction is reopened to capture them.

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP5–CP10 (binding/node-id, generic
extractor, language extractors ×19, standalone ×5, orchestrator + sync) from
`docs/spec/code-intel-engine.md`. Contracts authoritative in the umbrella
appendix (referenced by letter).

**Why:** Split the 25-checkpoint monolith into five dependency-ordered parts.

## Implementation log

### v1 extraction (master CP5–10) — 2026-06-05

Built across iterations 9–22 of /subagent-implementation (worktree
`code-intel-engine`), on top of the substrate. Commits (chronological):

- `53b675e` — CP5: node-id (`generateNodeID`, appendix B) + extraction helpers; binding extended (IsNull/ChildByFieldName/PrevNamedSibling), ts.wasm rebuilt
- `fae0eff` — CP6: generic TreeSitterExtractor + LanguageExtractor config + Go config
- `986ed13` — CP7: language registry + TS/JS/Python/Rust configs
- `291a9d1` — CP8a: Java/C/C++/C# (+ NodeKindClass engine fix)
- `1dd9e9d` — CP8b: Swift/Kotlin/Scala
- `2ef1ade` — CP8c: Ruby/PHP/Lua/Luau (+ ModuleTypes engine arm)
- `692ee94` — CP8d: Dart/ObjC/Pascal — all 19 languages complete
- `3d27a22` — CP9: standalone extractors (Vue/Svelte/Liquid/DFM/MyBatis) + line-offset
- `17011f0` — CP10: indexing orchestrator + sync (delete-by-file R-E invariant, transactional store)

**Out-of-scope / engine evolutions during the build:**
- The LanguageExtractor config grew hooks as languages demanded them: ResolveKind (struct/interface/enum/type-alias/module disambiguation), ExportStatementTypes (AST-based export detection, replacing a fragile text-scan), IsExported, ModuleTypes arm. Each was added when a language proved the need.
- ts.wasm was rebuilt once (CP5) to export 3 more tree-sitter node ops.

**Unforeseens:**
- Reviewer caught real per-language kind bugs repeatedly (Rust enum→struct, C++ class→struct, Ruby module→class, TS/JS export-default/export-const) — node-type strings and kind routing are the dominant failure mode; every config was verified against a real parse.
- The Dart grammar's call structure blocked call-site extraction (documented, F-16). require()/top-level calls outside function bodies aren't walked (F-15).

**Deferred items still open (scratchpad FOLLOWUPS, engine-wide):** F-1..F-17 — binding mem-leaks, true bulk-walk traversal, async recycle, single-conn FK hardening, updated_at, Java/C# field over-export, Kotlin interface header-anchoring, top-level-require extraction, Dart calls, standalone nits, CP5-test hardening. Dispositioned with the user when the full engine completes.
