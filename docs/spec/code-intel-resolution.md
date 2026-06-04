# Code-intelligence engine — resolution (part 3/5)

Part 3 of the 5-part code-intelligence engine port. **Umbrella:**
`docs/spec/code-intel-engine.md` (goal, roadmap, dependency DAG, authoritative
**reference appendix A–O**). **Design:** `docs/design/code-intel-engine.md`.

**Depends on:** parts 1–2 green — needs the DB, `types`, and the
`unresolved_refs` rows extraction produced. **Blocks:** part 4 (query) — graph
traversal walks the edges resolution commits.

Brand bindings (from the umbrella): commands `atomic code <verb>`. Never emit the
reference implementation's product name.

## Scope

Turn `unresolved_refs` into edges, including synthesized dynamic-dispatch edges:
the import resolver + path aliases (master CP11), the name matcher (CP12), the
resolver pipeline with kind promotion (CP13), the framework iface + Express as
template (CP14), the remaining 22 frameworks (CP15), and the 14-channel callback
synthesizer (CP16) which runs **last**, after all static edges commit.

**Contracts (authoritative, in the umbrella appendix):** F (resolution order +
`resolveOne` sub-order + `findBestMatch` scoring weights + edge-kind promotion +
re-export depth cap), G (synthesized-edge provenance convention + the 14
synthesizers + dedup key + fan-out caps), H (framework resolver contract +
route-node id format + the 23-resolver registry).

## Success criteria

- [ ] Resolution links imports (relative + aliased + barrel re-export), names
      (`obj.method` + overloads), and frameworks; each detected framework emits
      `route` nodes on a fixture.
- [ ] Edge-kind promotions are correct (`extends`→`implements` when target is an
      interface; `calls`→`instantiates` when target is a class/struct).
- [ ] Synthesized edges carry `provenance='heuristic'` + `synthesizedBy` and run
      **after** all static edges; dedup key `source>target`; fan-out caps held;
      node-count stable across re-run.
- [ ] The batched persist loop (re-read at offset 0 after delete) terminates.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | **(master CP11) Import resolver + path aliases**: per-language extension resolution, relative/aliased/external classification, tsconfig JSONC alias load, re-export chain (depth 8), JVM/Go cross-package. | `internal/codeintel/resolution`; ref `src/resolution/import-resolver.ts`, `path-aliases.ts` (COPY); appendix F | Relative + aliased + barrel re-export imports resolve |
| 2 | **(master CP12) Name matcher**: filePath/qualified/methodCall/exact/fuzzy dispatch, `findBestMatch` scoring weights, C++/Java receiver inference. | `internal/codeintel/resolution`; ref `src/resolution/name-matcher.ts` (COPY weights); appendix F | `obj.method` and overloaded names resolve to the right def |
| 3 | **(master CP13) Resolver pipeline**: `resolveOne` ordered dispatch, built-in skip sets, pre-filter, edge creation + kind promotion (`extends`→`implements`, `calls`→`instantiates`), batched persist (re-read-at-offset-0-after-delete loop). | `internal/codeintel/resolution`; ref `src/resolution/index.ts` (COPY order); appendix F | Unresolved refs become edges; promotions correct; batch loop terminates |
| 4 | **(master CP14) Framework iface + registry + Express**: `detect`/`resolve`/`extract`/`postExtract`/`claimsReference`, route-node id format, registry. | `internal/codeintel/resolution/frameworks`; ref `src/resolution/frameworks/{index,express}.ts` (COPY template); appendix H | Express routes become `route` nodes + handler `references` edges |
| 5 | **(master CP15) Remaining frameworks — the other 22 of 23**, grouped by language cluster. | `internal/codeintel/resolution/frameworks`; ref `src/resolution/frameworks/*.ts` (COPY); appendix H | Each detected framework emits routes on a fixture |
| 6 | **(master CP16) Callback synthesizer**: the 14 synthesizers, provenance convention (`heuristic` + `synthesizedBy`/`via`/`registeredAt`), dedup `source>target`, fan-out caps; runs last. | `internal/codeintel/resolution/synthesis`; ref `src/resolution/callback-synthesizer.ts` (COPY); appendix G | React setState→render + JSX-child edges synthesized with correct provenance; node-count stable |

## Risks

Inherited from the umbrella: **R6** (calibrated constants drift — centralize as
named consts mirroring appendix F/G; a test asserts the scoring weights + caps
literally), **R-C** (broad-parity + synthesis-in-v1 is large — CP4 proves the
framework template, CP5 fans out, CP6 is the synthesizer; partial parity is a
release lever, not a default).

## Change log

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP11–CP16 (import resolver, name
matcher, resolver pipeline, frameworks ×23, callback synthesizer ×14) from
`docs/spec/code-intel-engine.md`. Contracts authoritative in the umbrella
appendix (referenced by letter).

**Why:** Split the 25-checkpoint monolith into five dependency-ordered parts.
