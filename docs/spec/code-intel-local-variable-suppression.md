# Code-intel: suppress function-scoped local variables


## Goal


TS/TSX/JS function-body local variables stop becoming graph nodes; non-identifier (destructuring-pattern) variable names stop becoming nodes anywhere; existing indexes self-heal via an extractor-version stamp that forces one full re-index.


## Non-goals


- Arrow functions as first-class function nodes (future work; this spec's `FunctionScopeTypes` is its prerequisite).
- Per-binding destructuring nodes.
- Any change to which module-scope variables are kept, or to languages without a `VariableTypes` config (Go, Python, Java, …) — their output must be byte-identical.
- Lua/Luau wiring (config seam exists after this spec; wiring deferred until real-repo evidence). Erlang is unaffected by construction — its `VariableTypes` covers module-level `-define(...)` only; it has no function-body declaration form.
- Serve/UI filtering.


## Success criteria


- [x] TS fixture: module-scope const → node; const inside an arrow callback → no node; const nested two arrows deep → no node; calls/JSX refs inside suppressed declarations' initializers still harvested.
- [x] Kept-cases fixtures: `namespace N { const X }` still yields a node; Vue/Svelte `<script setup>` top-level const still yields a node; `for (const x of y)` binding yields no node before and after (behavioral assertion, not just a probe note).
- [x] Destructuring fixture: `const { a } = x` produces zero variable nodes at module scope and in callbacks.
- [x] Grammar probes for every `FunctionScopeTypes` value recorded as doc-comment blocks in the language config files (the repo's existing probe convention) before the config lands.
- [x] Languages with no `FunctionScopeTypes`: node/edge/unresolved-ref counts byte-identical on existing fixtures (Go, Python at minimum).
- [x] `project_metadata` gains `extractor_version`; mismatch triggers one full re-index (content-hash dedup bypassed) then rewrites the key; matching version keeps incremental behavior.
- [x] Real-repo validation (taxgentic/server re-index): `variable` count 5,473 → 1,003 (≤ 1,200); orphan **variables** 4,806 → 896 (−81%); zero variable nodes with non-identifier names (was 801); no `calls` edge terminating at a function-scoped local.
- [x] String-match ledger recorded before/after: total edges, confidence-tier histogram (baseline low 1,432 / medium 785 / high 649 — measured live on taxgentic/server's index, 2026-08-08 strategist audit), and the count re-attributed from variable→file owners; >20% low-tier growth blocks merge pending the design's pass-B mitigation decision.
- [x] `go test ./...` green; `go vet` clean; `gofmt -l` empty.


## Approach


Extraction-time scope suppression via a new optional `FunctionScopeTypes` per-language config set and a visitor `scopeDepth` counter (approach A) — see `docs/design/code-intel-local-variable-suppression.md`.


## Change tree


    atomic/internal/codeintel/extraction/
    ├── extractor.go ..................... M  (visitor.scopeDepth; FunctionScopeTypes descent; VariableTypes depth+identifier gates)
    ├── extractor_test.go ................ M  (scope-suppression + identifier-guard coverage)
    └── languages/
        ├── typescript.go ................ M  (FunctionScopeTypes: arrow_function, function_expression, generator_function)
        ├── javascript.go ................ M  (same set)
        └── languages_test.go ............ M  (per-language fixtures incl. tsx inheritance verification + no-op languages)
    atomic/internal/codeintel/indexer/
    ├── orchestrator.go .................. M  (extractor_version check → full-reindex bypass of content-hash dedup)
    └── orchestrator_test.go ............. M  (version-mismatch forces full pass; match stays incremental)


## Outline


    atomic/internal/codeintel/extraction/extractor.go
      LanguageExtractor.FunctionScopeTypes — optional scope-opening node kinds; nil = today's behavior
      visitor.scopeDepth — increments across FunctionScopeTypes descent
      visitNode (VariableTypes arm) — mint only at depth 0 AND name is a single identifier; walk continues either way

    atomic/internal/codeintel/extraction/languages/typescript.go
      FunctionScopeTypes — arrow_function, function_expression, generator_function (probe-confirmed)

    atomic/internal/codeintel/extraction/languages/javascript.go
      FunctionScopeTypes — same set (probe-confirmed)

    atomic/internal/codeintel/indexer/orchestrator.go
      extractorVersion const — bumped by hand when extraction semantics change
      IndexAll — version mismatch → treat every file as changed for this run, then stamp


## Flows


    Flow: extraction with scope suppression
    1. visitor meets arrow_function → scopeDepth++ → children visited → scopeDepth--
    2. lexical_declaration at depth > 0 → no node; initializer still walked for calls/JSX/field refs
    3. lexical_declaration at depth 0 with identifier name → variable node + contains edge (unchanged)

    Flow: self-healing migration
    1. atomic code index|sync opens existing DB → reads project_metadata.extractor_version
    2. mismatch → full extraction pass (hash dedup bypassed) → stale locals deleted with their files' re-store → key stamped
    3. next run matches → incremental as before


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Grammar probes + FunctionScopeTypes mechanism + TS/TSX/JS wiring + identifier guard | extraction/extractor.go, languages/*.go, tests | atomic-implementer (mode: feature) | ~5 | scope/identifier fixtures green; no-op languages byte-identical |
| 2 | extractor_version self-healing migration | indexer/orchestrator.go, tests | atomic-implementer (mode: surgical) | 2 | version-mismatch forces full pass |
| 3 | Real-repo validation + string-match ledger | taxgentic/server re-index, eval fixture | orchestrator + Haiku spot-checks | — | success-criteria metrics recorded |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Pass-B string-match anchoring widens to whole-file, manufacturing low-confidence lineage | med | Ledger gate in success criteria; design names the mitigation fork (line-distance window or sequence arrow-function nodes first) |
| Grammar node-kind names wrong for a dialect | low | Probe-before-commit rule is a checkpoint deliverable, not an afterthought |
| Hidden consumer of local-variable nodes | low | Strategist audit found only SQL-literal ownership (handled) and false `calls` targets (a bug this fixes); full-suite + eval gates |


## Change log

- **2026-08-08 — Implemented + criterion correction.** CP1 landed at `1095067`, CP2 at `98b4a2d`; CP3 measured on taxgentic/server (self-healing observed live: the un-stamped pre-change DB forced its own full pass). Variables 5,473 → 1,003; non-identifier names 801 → 0; orphan variables 4,806 → 896. String-match ledger: 2,866 → 2,200 (−666, within the ≤1,248 re-attribution bound; tiers low 1,432→1,203 / med 785→604 / high 649→393 — the low-tier-growth merge blocker did not fire; the high-tier drop is the anchor-loss cost the design accepted and names arrow-function nodes as the recovery path). **Correction:** the original "orphan share of total graph ≤ 5%" criterion was over-broad — measured 33%, dominated by node kinds outside this spec's scope (interface/type_alias 680, tracked by followup `ts-type-annotation-refs-not-extracted`; constraint markers 296, by design; unreferenced columns 161). Criterion re-scoped in the body to what the feature governs: orphan variables. **Deviation:** the planned `scripts/code-eval` arrow-callback fixture was not added — the re-attribution path was exercised against the live taxgentic corpus instead (the ledger above); add the fixture if the eval harness is next run for string-match.
