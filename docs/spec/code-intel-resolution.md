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
| 6 | **(master CP16) Callback synthesizer**: the 14 synthesizers, provenance convention (`heuristic` + `synthesizedBy`/`via`/`registeredAt`), dedup `source>target`, fan-out caps; runs last. Built in batches; **batches 2+ depend on the extraction enrichment EE1–EE3** (see `code-intel-extraction.md` → "Synthesis-enabling enrichment"). | `internal/codeintel/resolution/synthesis`; ref `src/resolution/callback-synthesizer.ts` (COPY); appendix G | React setState→render + JSX-child edges synthesized with correct provenance; node-count stable; all 14 synthesizers emit on a fixture each |

CP16 build order (full-enrichment decision, 2026-06-05 — all 14 synthesizers):

1. **batch 1 (done, `375514e`)** — synthesis infra (`Synthesizer` iface,
   `Composite` = the `resolution.CallbackSynthesizer` seam, dedup `source>target`,
   caps 40/6/8, `heuristic` provenance stamping) + **react-render** (real,
   gated). jsx-render/event-emitter/callback registered as stubs pending EE1–EE3.
2. **extraction enrichment** — EE1 (TSX/JSX refs), EE2 (call-argument capture +
   migration), EE3 (field-assignment capture). Owned by `code-intel-extraction.md`.
3. **batch 2 (done)** — jsx-render + vue-handler (EE1). Origin-ref discriminator
   mechanism: EE1 sets `Arguments[0]="jsx:<Tag>"` on each `UnresolvedReference`;
   `createEdges` propagates this into edge `Metadata` as `{"refArgs":["jsx:<Tag>"]}`.
   Synthesizers read edges and call `hasJSXDiscriminator(e.Metadata)` to distinguish
   JSX-origin `references` edges from type-annotation `references` edges. Reused by
   batches 3–6. Vue-handler gap: `@event="handler"` bindings not captured by Vue
   extractor — documented stub, zero fake edges.
4. **batch 3 (done)** — event-emitter + rn-event-channel (EE2). `synthesizeEventEdges`
   shared helper; callee-suffix registration/dispatch predicates; EVENT_FANOUT_CAP=6;
   rn-event-channel runs first in `Default(d)` (first-wins dedup). `Default(d)` = 6 synthesizers.
5. **batch 4 (done — callback + closure-collection real; flutter-build stub)** —
   `callback` is real (EE3 field-assignment registration ↔ invocation →
   `calls+heuristic` edge to the stored callable; MAX_CALLBACKS_PER_CHANNEL=40).
   `closure-collection` is real, **activated by EE5** (identifier call-argument
   capture: `Arguments` also holds `"arg:<ident>"` entries): correlates
   `.append`/`.add` (identifier-handler args) with `.forEach`/`.each` by the
   receiver collection name, resolves the handler to a node, emits
   `calls+heuristic` edges with `synthesizedBy="closure-collection"`,
   CC_FANOUT_CAP=8, receiver-isolated; anonymous trailing closures remain a
   documented gap (no identifier to resolve). `flutter-build` is a **documented
   stub** — the Dart grammar (ABI 14) has no `call_expression` node, so `setState`
   calls are uncapturable.
6. **batch 5 (done, real — activated after EE4)** — interface-impl + cpp-override
   (structural: `implements`/`extends`). Both are real implementations: EE4 wired
   heritage extraction for TypeScript, C++, and Java, producing `EdgeKindImplements`
   and `EdgeKindExtends` edges. `InterfaceImplSynthesizer`: walks `implements` edges
   C→I, finds interface method nodes (method_signature IS in MethodTypes), matches
   by name with implementing class methods, emits `calls+heuristic` I.m→C.m edges
   with `synthesizedBy="interface-impl"`. `CppOverrideSynthesizer`: walks C++ `extends`
   edges D→B (Language==cpp scope guard), matches base method names with derived class
   methods, emits `calls+heuristic` B.m→D.m edges with `synthesizedBy="cpp-override"`.
   Both: dedup `source>target`, no self-loops, node-count stable. `Default(d)` = 10
   synthesizers.
7. **batch 6 (done — 2 real, 2 documented stubs)** — gin-middleware-chain +
   mybatis-java-xml are **real**; go-grpc-stub-impl + fabric-native-impl are
   **documented stubs**. `Default(d)` = 14 synthesizers. CP16 complete
   (flutter-build remains the lone grammar-blocked stub).

   - `gin-middleware-chain` (**real**): EE5 captures `r.Use(authMiddleware)` as a
     calls-kind unresolved ref with `Arguments=["arg:authMiddleware"]`. CP15 Gin
     resolver emits `NodeKindRoute` nodes (Language=go). File-level heuristic:
     all Go route nodes in the same file as a `.Use()` call are assumed protected
     by the registered middleware. Synthesizes `calls+heuristic` edges route→middleware
     (Go-only scope; file-path suffix-match for absolute/relative path normalization).

   - `mybatis-java-xml` (**real**): CP9 MyBatis XML extractor emits function nodes
     with `QualifiedName="namespace.stmtId"` (Language=xml). Java extractor emits
     method nodes contained by interface nodes (Language=java). Correlation: parse
     stmtId = last segment of QualifiedName; Java interface name = last segment of
     namespace; find Java method with name=stmtId in that interface; emit
     `calls+heuristic` Java-method→XML-function edge.

   - `go-grpc-stub-impl` (**documented stub** — 3 missing signals):
     (1) Go interface method signatures (method_spec inside interface_type) are
     NOT extracted as method nodes — Go MethodTypes captures only method_declaration.
     (2) `&fooImpl{}` composite literal arg is not a plain identifier; EE5 misses it.
     (3) Go uses structural typing; no EdgeKindImplements edges exist for Go.

   - `fabric-native-impl` (**documented stub** — 3 missing signals):
     (1) ObjC RCT_EXPORT_VIEW_PROPERTY, Java @ReactModule, and C++ template
     specializations are not captured by any extractor.
     (2) JS/TS `codegenNativeComponent<T>("ComponentName")` EE2 arg exists but
     cross-language name resolution is absent.
     (3) No cross-language name correlation mechanism for ObjC/Java/C++ ↔ JS/TS.

A synthesizer that genuinely cannot derive its edge from any feasible capture is
recorded as a documented stub (zero fake edges) — fail loud, never fabricate.

## Risks

Inherited from the umbrella: **R6** (calibrated constants drift — centralize as
named consts mirroring appendix F/G; a test asserts the scoring weights + caps
literally), **R-C** (broad-parity + synthesis-in-v1 is large — CP4 proves the
framework template, CP5 fans out, CP6 is the synthesizer; partial parity is a
release lever, not a default).

## Change log

### 2026-06-06 — fuzzy tier rewritten in-memory (perf; behavior-preserving)

**What changed:** The CP12 name-matcher fuzzy tier (`byFuzzy`) no longer generates
edit-distance variant strings and probes the DB once per variant
(`levenshteinVariants` + `GetNodesByName` in a loop). It now scans the
already-warmed in-memory known-names set (the `knownNames` cache built once per
`ResolveAndPersistBatched` run) computing bounded Levenshtein distance to the ref
name, then fetches only the matched names from the DB. Same edit-distance
thresholds (1 for len≤4, 2 for len>4, appendix F/J). `levenshteinVariants` is
removed; a bounded `levenshteinDistance` (early-exit at maxDist+1) replaces it.
The warmed name set is threaded from the pipeline into the matcher.

**Why:** real-repo eval (measured via `code index --profile`) showed `resolve.match`
was the entire indexing cliff — rw-gin spent 18.07s in fuzzy across 2298 refs
(rw-django, zod timed out at 150s). A control run with fuzzy disabled dropped
rw-gin `resolve.match` to 21ms while producing the **identical** 317 edges: the
variant×SQL fuzzy added zero resolution value at ~860× the cost. The rewrite
collapses thousands of per-ref SQL probes into one in-memory scan + a handful of
fetches. The reference impl (codegraph) omits edit-distance fuzzy entirely for the
same reason; this rewrite keeps the tier but makes it cheap.

**Result set is unchanged:** both the variant-probe and the in-memory scan return
exactly the nodes whose lowercased name is within the edit-distance threshold of
the ref name. This is a perf rewrite, not a semantics change — existing fuzzy
resolution tests must stay green.

### 2026-06-06 — CP16 batch 4 done: callback + closure-collection real (EE5)

**What changed:** Marked batch 4 done. `callback` is real (EE3-driven).
`closure-collection` is real, activated by the new EE5 identifier-call-argument
capture, correlating `.append`/`.forEach` by receiver collection name with
CC_FANOUT_CAP=8 and receiver isolation. `flutter-build` stays a documented stub
(Dart grammar has no `call_expression`).

**Why:** EE5 added identifier-argument capture, which gives closure-collection the
appended-handler identity it needs. Updated the batch-4 body line (previously
"(EE2/EE3)") so a fresh subagent sees the current state, not the old gap-stub.

**Superseded:** batch-4 line previously implied callback/closure-collection/
flutter-build were pending stubs; callback + closure-collection are now real.

### 2026-06-06 — CP16 batch 5: interface-impl + cpp-override activated (real, post-EE4)

**What changed:** Both batch-5 synthesizers are now real implementations.
`InterfaceImplSynthesizer`: replaced `nil, nil` stub with a graph-walking
implementation — builds `methodToClass` (contains-edge lookup) and
`classToMethods` (name→methodID per class) maps; for each `implements` edge
C→I finds I's method nodes, looks up matching C method by name, emits
`calls+heuristic` I.m→C.m edges with `synthesizedBy="interface-impl"`.
`CppOverrideSynthesizer`: replaced `nil, nil` stub — scans all C++ class nodes
for `extends` edges D→B where `B.Language==cpp`, matches base member names
(NodeKindFunction or NodeKindMethod) with derived class members, emits
`calls+heuristic` B.m→D.m edges with `synthesizedBy="cpp-override"`. Both:
dedup source>target, no self-loops, node-count stable.
Tests: `TestInterfaceImplSynthesizer_GapDocumented` and
`TestCppOverrideSynthesizer_GapDocumented` replaced by real gate tests
(`_EmitsDispatchEdge`, `_EmitsOverrideEdge`, `_IdempotentViaComposite`,
`_SynthesizedByMetadata`). Wrong comment at `ee4_e2e_test.go` (claiming
implements_clause yields extends refs) corrected — extractor emits
EdgeKindImplements directly for implements_clause. All tests green.

**Why:** EE4 (`4cf33d1`) wired heritage extraction for TypeScript, C++, and Java,
producing the `EdgeKindImplements` / `EdgeKindExtends` edges and interface method
nodes that both synthesizers require.

**Superseded:** Batch-5 synthesizers were documented stubs returning `nil, nil`
(SIGNAL ABSENT — heritage extraction absent). Now real.

### 2026-06-05 — CP16 batch 5: interface-impl + cpp-override (documented gaps)

**What changed:** Batch 5 is done as documented gaps. `InterfaceImplSynthesizer`
(new): registered stub — extraction pipeline has no heritage capture for TypeScript
or any other OO language. `extractClass()` does not walk `class_heritage` or
`implements_clause` nodes, so no `EdgeKindExtends` unresolved refs are emitted for
`class Dog implements Animal`. CP13 resolution therefore never promotes an `extends`
ref to `EdgeKindImplements`. Additionally, interface method declarations
(`method_signature`) are not in `MethodTypes`, so interface method nodes do not exist
in the graph even if an `implements` edge could be found. `CppOverrideSynthesizer`
(new): registered stub — `CppExtractor()` visits `class_specifier` bodies (to capture
member functions via `function_definition`) but the base-class list
(`class Dog : public Animal`) is not extracted as an `EdgeKindExtends`
`UnresolvedReference`. Without an `extends` edge derived→base, no (base, derived)
class pairs can be identified for override correlation. Both synthesizers are
registered in `Default(d)` (now 10 total, up from 8). Tests: gap-documented tests
with real fixtures confirm zero `implements`/`extends` edges from the extraction
pipeline; unit-level tests confirm zero edges with manually seeded partial signals;
node-count stable; non-cpp scope guard tested. Build, vet, and all tests green.

**Why:** Grounding probes (running real TypeScript and C++ fixtures through the full
indexer + resolution pipeline) confirmed both signals absent. Appendix G mandates
honest stubs over fabricated edges. Registering the stubs locks the synthesizer
names in `Default(d)` and documents exactly which extraction change is needed to
activate them. A future batch activates these stubs once heritage extraction lands.

### 2026-06-05 — CP16 batch 4: callback (EE3 real) + closure-collection + flutter-build (documented gaps)

**What changed:** Batch 4 is done. `CallbackSynthesizer` replaced its stub with
a real EE3-driven implementation: walks all static `references` edges whose
`Metadata["refArgs"][0]` has a `"field:"` prefix (produced by EE3 field-assignment
capture) to build `fieldName → callableTargetID` maps; scans all unresolved `calls`
refs whose `ReferenceName` ends with `".fieldName"` or equals `fieldName` to find
invokers; synthesizes `invoker → callableTargetID` edges with `Metadata={"field":
fieldName}` (synthesizedBy stamped by Composite). Cap: `MAX_CALLBACKS_PER_CHANNEL=40`
per (field, callable) channel. Self-loops skipped. Within-synthesizer dedup by
`invoker>target`. `ClosureCollectionSynthesizer` (new): documented gap stub — EE2
does not capture closure/identifier args from `.append()`, so no handler identity is
available for correlation. `FlutterBuildSynthesizer` (new): documented gap stub —
Dart grammar has no `call_expression` node (`CallTypes` is empty for Dart per F-16),
so `setState` calls are not captured. `Default(d)` now registers 8 synthesizers.
All unit tests, gate test (two-channel TypeScript fixture), idempotency, node-count
stability, and gap-documented tests green.

**Why:** EE3 (field-assignment capture) landed in a prior extraction batch, making
the callback registration signal available as static `references` edges with
`refArgs=["field:fieldName"]`. Batch 4 is the first consumer of that signal.
The closure-collection and flutter-build gaps are documented honestly per appendix
G mandate — fabricating edges without a real signal would degrade graph quality.

**Superseded:** `CallbackSynthesizer` was a documented stub with comment "SIGNAL
ABSENT: variable assignments to fields are not extracted" (no longer true after
EE3 landed). It now has a real implementation.

### 2026-06-05 — CP16 batch 3: event-emitter + rn-event-channel (EE2)

**What changed:** Batch 3 is done. `EventEmitterSynthesizer` replaced its stub with a
real implementation: reads all unresolved_refs, classifies each as registration
(`.on` / `.addListener` / `.addEventListener`) or dispatch (`.emit` / `.dispatch`)
by callee suffix, correlates matching pairs by `Arguments[0]` (event name captured
by EE2), and emits `calls+heuristic+synthesizedBy=event-emitter` edges from the
dispatch-enclosing function → registration-enclosing function. Cap: `EVENT_FANOUT_CAP=6`
per event per dispatch site. Handler identity not captured (only string-literal args) —
edge granularity is enclosing-fn→enclosing-fn; documented coarser-but-honest choice.
`RNEventChannelSynthesizer` (new) mirrors the same logic with RN-specific callee
patterns: `DeviceEventEmitter.addListener` / `NativeEventEmitter(...).addListener` for
registration; `DeviceEventEmitter.emit` / `sendEvent` for dispatch;
`synthesizedBy=rn-event-channel`. `Default(d)` now registers 6 synthesizers; rn-event-channel
runs before event-emitter (first-wins dedup). All gate tests, unit tests, idempotency,
and node-count-stable checks green.

**Why:** EE2 (call-argument capture) landed in batch 2 / extraction enrichment, making
the event-name signal available in `UnresolvedReference.Arguments`. Batch 3 is the
first consumer of that signal.

**Superseded:** `EventEmitterSynthesizer` was a documented stub returning nil, nil with
comment "SIGNAL ABSENT". It now has a real implementation.

### 2026-06-05 — CP16 batch 2: jsx-render + vue-handler + origin-ref discriminator

**What changed:** Batch 2 is done. `JSXRenderSynthesizer` replaced its stub with a
real implementation: reads `references` edges with empty Provenance and
`hasJSXDiscriminator(Metadata)` true → emits `calls+heuristic+synthesizedBy=jsx-render`
edges. `VueHandlerSynthesizer` emits `calls` edges for Vue component → child component
references via `Language=vue` component nodes; `@event="handler"` gap documented and
tested. Origin-ref discriminator mechanism: EE1 `Arguments[0]="jsx:<Tag>"` propagated
by `createEdges` into edge `Metadata={"refArgs":["jsx:<Tag>"]}` — reusable by batches
3–6. `Default(d)` now registers 5 synthesizers. All gate tests green; node-count
stable; idempotent.

**Why:** Batch 2 was the next scheduled step after EE1 landed.

### 2026-06-05 — CP16 full-enrichment build order + extraction dependency

**What changed:** Recorded the CP16 build order under the checkpoint table:
batch 1 (infra + react-render) shipped at `375514e`; batches 2–6 build the
remaining 13 synthesizers and depend on the extraction enrichment EE1–EE3 now
specified in `code-intel-extraction.md`. Added "all 14 synthesizers emit on a
fixture" to the CP16 Verifies column.

**Why:** CP16 batch 1 proved (reviewer-confirmed) that 13 synthesizers cannot
derive edges from the v1 graph; project owner chose full enrichment over deferral.
The build order makes the extraction→synthesis dependency explicit so batches run
in the right order.

### 2026-06-06 — CP16 batch 6: gin-middleware-chain + go-grpc-stub-impl + mybatis-java-xml + fabric-native-impl

**What changed:** Batch 6 is done. `Default(d)` now registers 14 synthesizers.
`GinMiddlewareChainSynthesizer` (real): reads `.Use()` call-sites with `arg:`
unresolved refs (EE5), collects Go `route` nodes emitted by the Gin framework
resolver (CP15), correlates by file-path with suffix matching to handle the
relative-vs-absolute path discrepancy between the generic extractor and the
framework extractor, then emits `calls+heuristic` edges from each route node to
each referenced middleware function node. `MyBatisJavaXMLSynthesizer` (real):
reads CP9 XML `function` nodes whose `QualifiedName` encodes `namespace.stmtId`,
reads Java `interface` + `method` nodes extracted by the Java extractor, resolves
the mapper interface by the last segment of the namespace, then emits
`calls+heuristic` edges from each Java mapper method to the corresponding XML
statement node. `GoGRPCStubImplSynthesizer` (documented stub, `nil, nil`): gaps
are (1) Go interface method signatures (`method_spec` inside `interface_type`) are
not extracted as method nodes — Go `MethodTypes` captures only
`method_declaration`; (2) the `&fooImpl{}` composite-literal arg of
`RegisterFooServer` is not a plain identifier, so EE5 misses it; (3) Go uses
structural typing, so no `EdgeKindImplements` edges exist to link the service
interface to its impl. `FabricNativeImplSynthesizer` (documented stub, `nil,
nil`) — React-Native Fabric: gaps are (1) the native registration surfaces (ObjC
`RCT_EXPORT_VIEW_PROPERTY`, Java `@ReactModule`, C++ template specializations) are
not captured by any extractor; (2) the JS/TS `codegenNativeComponent<T>("Name")`
arg is captured by EE2 but there is no cross-language name-resolution mechanism;
(3) no cross-language correlation index exists to link the JS spec to the native
impl. Test
file `batch6_test.go` added with 7 tests covering all 4 synthesizers; the
`TestDefaultCompositeHasFourteenSynthesizers` test guards the 14-synthesizer
registry. Suffix-path matching is the canonical pattern for correlating
framework-extractor output (absolute paths) with generic-extractor output
(relative paths) inside synthesizers.

**Why:** Batch 6 completes CP16. All 14 synthesizers specified in appendix G now
have implementations or documented stubs with explicit gap callouts so CP17+ can
target the exact extraction enrichment needed to promote stubs to real.

**Superseded:** Batch-6 line in checkpoint table previously read "gin-middleware-chain,
go-grpc-stub-impl, mybatis-java-xml, fabric-native-impl" with no real/stub breakdown.
The body now carries per-synthesizer status and `Default(d) = 14 synthesizers`.

### 2026-06-04 — Created by splitting the monolithic engine spec

**What changed:** Extracted master checkpoints CP11–CP16 (import resolver, name
matcher, resolver pipeline, frameworks ×23, callback synthesizer ×14) from
`docs/spec/code-intel-engine.md`. Contracts authoritative in the umbrella
appendix (referenced by letter).

**Why:** Split the 25-checkpoint monolith into five dependency-ordered parts.

### 2026-06-06 — ExtractAndPersist seam now invoked by the index pipeline

**What changed:** `frameworks.Registry.ExtractAndPersist` (the route-node
extraction seam) is now called by `Engine.ExtractFrameworkNodes` as part of the
index pipeline, before `ResolveAndPersistBatched`. Previously defined but never
called — route nodes were never created, so framework route resolution had no
route nodes to link handlers to.

**Why:** routes = 0 on real apps. The resolvers' `Extract` methods were dead code
with no caller. See `docs/spec/code-intel-surfaces.md` 2026-06-06 entry for the
full pipeline-wiring contract.

### 2026-06-06 — flask + fastapi resolvers match real-app route idioms

**What changed:** `FlaskResolver` and `FastAPIResolver` (`frameworks/python.go`)
fixed to extract routes from real apps. Flask: `Detect`'s flask-import check now
matches sub-module imports (`from flask.helpers import …`, `from flask_jwt_extended
import …`) via `from\s+flask[\w.]*\s+import`, and the route regex accepts
`methods=(...)` tuple form (not only `[...]` list). FastAPI: the route path capture
now accepts empty-string paths (`@router.get("")`) and paths followed by trailing
kwargs (`response_model=`, `name=`) — first string literal is the path, closing
delimiter is `,` or `)`.

**Why:** corpus eval on the gothinkster RealWorld apps: rw-flask extracted 0 routes
(Detect failed on `from flask.helpers import` + the route regex rejected
`methods=('GET',)`); rw-fastapi extracted 1 (regex required ≥1 path char and an
immediate `)`). After: rw-flask 0→19, rw-fastapi 1→20. Django/other resolvers
unchanged.

**Correction:** the prior flask/fastapi resolvers were only validated against
synthetic `@app.route('/x', methods=['GET'])` / `@app.get('/x')` fixtures, which
masked the blueprint-tuple and APIRouter-empty-path-with-kwargs idioms real apps
use.

### 2026-06-06 — Rails resources/resource DSL route expansion

**What changed:** `RailsResolver.Extract` (`frameworks/ruby.go`) gained a second
pass (`railsParseDSL`) that expands the Rails RESTful DSL, in addition to the
existing imperative-form (`get '/path', to: '…'`) matching. `resources :x`
expands to the 7 RESTful routes (index/create/show/update[PATCH+PUT]/destroy),
`resource :x` to the singular set (no index, no `:id`), both honoring
`only:`/`except:` and `param:`. Single-line `get :y, on: :collection|:member`
member/collection routes and one level of block nesting (nested resource paths
prefixed by the parent's id-path via a block stack) are handled. An `end` decrement
is guarded against underflow on malformed input.

**Why:** corpus eval: rw-rails extracted 0 routes because `config/routes.rb` uses
the `resources`/`resource` DSL, which the imperative-only regex never matched.
After: rw-rails 0→19. Only the Rails resolver changed.

**Known simplification:** `scope`/`namespace` blocks are not treated as path
prefixes — routes inside `scope :api` emit at their canonical path without the
`/api` prefix (tracked as a follow-up). The generic Ruby tree-sitter extraction is
separately thin and unrelated to this change.

### 2026-06-06 — actix resolver matches `.route()` chain form

**What changed:** `ActixResolver`'s `actixDirectRouteRe` (`frameworks/rust.go`)
now matches the `.route(PATH, METHOD().to(HANDLER))` chain form: the `web::`
method prefix is optional (`(?:web::)?`), the path accepts empty string `""`
(scope-relative), the method is an explicit enum, and `(?s)` handles `.route(...)`
calls split across lines. The `#[get("/path")]` macro and `web::resource` forms
are unchanged.

**Why:** corpus eval: rw-actix extracted 0 routes — the app registers routes via
`.route("", get().to(handler))` (often without the `web::` prefix, inside
`web::scope(...)`), which the prior regex (requiring `web::` + non-empty path)
missed. After: rw-actix 0→20. Only the Actix resolver changed.

**Known simplification:** `web::scope(...)` path prefixes are not composed onto the
route paths (same class as the Rails `scope` follow-up F-76).
