# Code-intelligence engine (atomic CLI) — spec (umbrella)

**This is the umbrella spec.** The implementation contract is split across five
dependency-ordered part-specs (see §Checkpoints for the map); this file holds the
shared goal, the master roadmap, the dependency DAG, and the **authoritative
reference appendix A–O** — the contracts the part-specs reference by letter. A
`/subagent-implementation` run targets one **part-spec** (plus this umbrella for
the appendix), not this file alone.

Design: `docs/design/code-intel-engine.md`. Read it first for the build-strategy
decision, the wazero-memory correction, the concurrency model, and the §atomic
CLI integration seams.

"The reference implementation" = the existing TypeScript engine whose source is
the learning anchor. "The engine" = the new Go subsystem inside the `atomic`
binary. This spec is **reference-annotated**: each part-spec checkpoint points at
the reference paths it derives from, and the **reference appendix** below carries
the extracted contracts. Verdicts: COPY (reproduce semantics exactly), ADAPT (same
intent, idiomatic Go), SKIP (Node/WASM-specific, do not port).

## Branding & naming (read first)

The engine ships **inside the `atomic` CLI**; the brand slug resolves to
**`atomic`**. **Never emit the reference implementation's product name** anywhere
in the engine's code, comments, identifiers, strings, MCP tool names, directory
names, or output.

- Resolved bindings (the `<brand>` placeholder = `atomic`):
  - **Commands:** `atomic code <verb>`.
  - **Data dir:** `<projectRoot>/.claude/.atomic-index/` (gitignored, created by
    `atomic setup` / first `atomic code index`).
  - **SQLite file:** `<projectRoot>/.claude/.atomic-index/atomic.db`.
  - **MCP tool prefix:** `atomic_code_*` (e.g. `atomic_code_explore`,
    `atomic_code_search`).
- The reference repo is a **read-only design anchor**. Port its *behavior*, never
  its branding. When you see a branded name or comment in the reference, rename
  to the neutral term or the `atomic` binding above.
- A fresh-context implementer will copy the reference's branding by reflex — do
  not. The appendix repeats this at point-of-use.

## Goal

A pure-Go (`CGO_ENABLED=0`) subsystem of the `atomic` static binary that indexes
a codebase with tree-sitter into SQLite, resolves references (including
synthesized dynamic-dispatch edges), and answers structural/flow queries over
**both** an MCP server (`atomic code mcp`) and `atomic code <verb>` CLI
subcommands — reproducing the reference's data model and tuned constants exactly,
on idiomatic Go runtime.

## atomic CLI integration (read with the design's §Integration)

- **Dispatch:** add `case "code": runCode(args[1:], repoOverride)` to
  `atomic/cmd/atomic/main.go`; `runCode` sub-dispatches the verbs with the same
  `flag.FlagSet` style as the other `run*` functions. Thin arg-parsing only —
  query logic lives in the `engine` facade.
- **Package home:** all engine code under `atomic/internal/codeintel/<pkg>`; the
  reference `context` package is renamed `codectx` (stdlib collision). The
  embedded `ts.wasm` blob lives in `internal/codeintel/grammars`.
- **DB path:** `<projectRoot>/.claude/.atomic-index/atomic.db`. Project root via
  `internal/repoctx` / `repoOverride`.
- **Setup:** `atomic setup` adds `.claude/.atomic-index/` to `.gitignore`; it
  does not auto-index.
- **Deps (go.mod):** `modernc.org/sqlite`, `github.com/tetratelabs/wazero`, the
  tree-sitter wasm binding (forked to rebuild `ts.wasm` with all 19 grammars),
  `github.com/modelcontextprotocol/go-sdk`. `go mod tidy`; keep `CGO_ENABLED=0`.
- **Pipeline:** Go-only under `atomic/` — **not** a bundle artifact, so
  `make render`/`make bundle` do **not** apply. Obligations: `gofmt`, `go vet`,
  `go test ./...`, register `atomic code` in `CLAUDE.md` "Atomic binary
  subcommands" + `/atomic-help` binary topic + `docs/reference/`.

## Non-goals

- **No new CLI framework.** `atomic`'s existing flag-based dispatch is reused via
  `runCode`; no `cobra`/`commander`. (This is the *only* "host owns it" item that
  flipped: atomic IS the host now, so the thin arg-parsing layer is in scope —
  but it is built in atomic's existing style, not a new framework.)
- The reference's multi-agent installer, its docs site, its npm/release pipeline.
- Re-tuning calibrated constants (BM25 weights, scoring, budgets) — reproduce.
- **File watching** in v1 — sync-on-demand only. Mitigation (a stated v1 safety
  property): `status` surfaces last-indexed time + pending-change count so a
  stale graph is never served silently.

## Success criteria

- [ ] `CGO_ENABLED=0 go build` produces the single `atomic` static binary with
      the engine compiled in; cross-compiles to darwin/linux × amd64/arm64 with
      no C toolchain (matching atomic's goreleaser targets; windows best-effort,
      not a correctness gate per repo platform policy).
- [ ] Indexing a TS repo and a Go repo produces a SQLite DB whose schema is
      **byte-identical** to the reference schema — verified by schema dump diff.
- [ ] Node ids match what the reference would generate for the same
      `(filePath, kind, name, line)` — verified by a vector test whose golden
      pairs are **exported from the reference impl** (CP5), as a CI gate.
- [ ] `NodeKind`/`EdgeKind`/`Language` consts equal the verbatim appendix lists
      (count + spelling), enforced by a test.
- [ ] All 19 tree-sitter languages + 5 standalone formats extract nodes/edges on a
      fixture each; node-count stable across re-index (no explosion).
- [ ] Resolution links imports, names, frameworks; synthesized edges carry
      `provenance='heuristic'` + `synthesizedBy` and run **after** static edges.
- [ ] Graph queries (`callers`/`callees`/`impact`/`path`) correct on a fixture
      with known call structure.
- [ ] FTS search returns results in the **same rank order** as the reference on a
      known corpus (BM25 weights from appendix J); 3-tier FTS→LIKE→fuzzy present.
- [ ] MCP `initialize` returns the server-instructions text; the node tool returns
      all overloads in one call on an ambiguous name; the explore tool respects
      the exact budget tiers, the 25k ceiling, and the section-boundary cut.
- [ ] `atomic code <verb>` subcommands exist for index/sync/status/search/
      callers/callees/impact/node/files/affected/explore/mcp — each query verb
      with a `--json` mode — dispatched from `runCode` over the engine facade.
- [ ] CP0 proven: all 19 grammars load under wazero with **node-type vocabulary
      matching** the reference's grammars, **parallel** parse across the instance
      pool, and a recycle that returns RSS to within K% of baseline on a 10k-file
      repo — before any extractor work begins.

## Approaches

Tree-sitter binding is the only live fork (from the design):

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | wazero + WASM grammars | pure Go, grammars as WASM | med | pre-release; grow-only mem; **grammar ABI must match reference** |
| B | `gotreesitter` pure-Go reimpl | no WASM, no cgo | med | grammar correctness across 19 langs |
| C | cgo `go-tree-sitter` behind build tag | native, fast | high build | needs C toolchain; cross-compile pain |

## Recommendation

Default **A**, gate on the CP0 spike, keep **C** behind `//go:build cgo` as the
fast-path fallback. The binding sits behind one Go interface so extractors never
see which is active. CP0 fills in a **three-way decision table** written to this
spec body: all-19 → proceed (A); most → A + cgo-tag for the failing grammars;
broad-fail → cgo-default *or* narrow scope (partial parity as a release lever,
not a default). The spike's **first** check is ABI/version alignment of the WASM
grammars against the reference's — a mismatch makes A a dead end.

### Spike result (recorded 2026-06-03 — run in `tmp/spike-go`)

The spike was executed. Findings:

- **SQLite substrate — PROVEN.** `modernc.org/sqlite` v1.51.0 (bundled SQLite
  3.53.1, pure Go) runs the FTS5 external-content vtable + the 3 sync triggers +
  `bm25(nodes_fts, 0,20,5,1,2)` + the delete-sentinel correctly. No obstacle to
  appendix A/J.
- **wazero parse path — PROVEN.** A wazero tree-sitter binding parses real C
  end-to-end and emits standard tree-sitter node types (`function_definition`,
  `struct_specifier`, `call_expression`, `parameter_declaration`). ABI =
  tree-sitter `LANGUAGE_VERSION 14` (current; same family as the reference's
  0.25.x grammars). Node-type vocabulary matches what extractor configs key on.
- **Coverage — the real constraint.** `malivvan/tree-sitter` v0.0.1 compiles
  only **C + Cpp** into its shipped `ts.wasm`, but vendors grammar C sources for
  ~29 languages in `src/` — **14 of our 19 present** (c, cpp, csharp, php, ruby,
  swift, kotlin, scala, lua, python, rust, java, javascript, go; bonus svelte).
  **Missing 5–6:** typescript, tsx, dart, luau, objc, pascal.

**Decision (was the spike's job): option A is the path.** It is *not* "go get and
use" — it is **fork malivvan's build harness and rebuild `ts.wasm` with all 19
grammars compiled in** (add `src/<lang>/parser.c` + scanner, export
`tree_sitter_<lang>`, add a `Language<X>` method). The build needs `zig cc` at
**build-time only** — the shipped Go binary stays CGO-free, wazero-only, static.
The 5 missing grammars come from upstream tree-sitter repos (a `_gen` tool
exists). For node-type parity beyond ABI, vendor the **same grammar versions**
the reference's `tree-sitter-wasms` uses. cgo (C) remains the fallback only if
the rebuild proves impractical — the spike says it won't (harness exists, 14/19
pre-vendored). Partial parity is now an informed staging lever, **typescript
prioritized** (it is both highest-value and in the missing set).

### Spike round 2 — every hypothesis tested (2026-06-03)

Four parallel experiments (artifacts in `tmp/spike-*`). **All PASS.** The
pure-Go architecture is locked; no viability question remains.

- **A1+B2 grammar bundle — PASS.** Rebuilt `ts.wasm` with TypeScript (vendored
  fresh from upstream) + Rust + Swift via `zig cc`; all load, ABI 14, **full
  extractor vocab, zero missing node types, no ERROR nodes**; binary
  `CGO_ENABLED=0`. Cost ~10 min per new grammar → **~1–2 days for all 19**.
  *Caveat → R-WASM:* wasm grows with grammar tables (3 grammars → 9.2 MB; all 19
  ≈ **30–60 MB** `//go:embed` blob).
- **A2 concurrency — PASS.** Sharing one instance across goroutines is a
  **proven data race + panic** (wazero linear memory is not concurrently
  mutable). Correct unit = **one full `TreeSitter` instance (own wazero
  runtime+module+parser) per goroutine, borrowed from a bounded pool (N≈cores)**.
  Pool run race-clean and deterministic.
- **A3 memory — PASS.** No recycle → unbounded (8k parses → 6.5 GB). **Recycle
  (drop instance + `runtime.GC()` + recreate) every ~500 parses/instance → RSS
  flat ~1 GB regardless of total**, and *faster* (smaller heap = cheaper GC;
  recycle ~90 ms). **Cadence: ~500 parses/instance** (tighten to 200 if
  RSS-critical).
- **A4 throughput — PASS for parse; the walk is the cost center.** Pool parses
  **~1,800 files/sec** (near-linear to cores). BUT a naive Go-side DFS calling
  `Child`/`ChildCount` per node is **70× slower (~25 f/s)** — each call
  re-crosses the WASM boundary. *Design principle → R-WALK:* extraction must
  **minimize Go↔WASM round-trips** (tree cursor and/or bulk in-WASM traversal
  returning structure in few calls); never per-node iteration from Go.
  cgo-native would speed the *walk*, not the parse.
- **B1 search parity — PASS.** `modernc` (SQLite 3.53.1) vs `node:sqlite`
  (3.50.4): **byte-identical rank order + scores** across 12 queries incl. 601
  tied rows. *Caveat:* ties fall back to rowid (insertion order) and the
  reference SQL has no secondary key → **add `ORDER BY score, nodes.id`** so the
  two ports can't drift; pin/track SQLite versions + tokenizer (appendix J).
- **B3 MCP go-sdk — PASS.** Official `go-sdk` v1.6.1, `CGO_ENABLED=0`:
  `initialize` instructions set + read **byte-equal**, typed `{query}` schemas,
  descriptions, `tools/call` text. Validation failures surface as
  `result.IsError` (handlers must check).

**CP0's only leftover is mechanical** (produce the full 19-grammar wasm) plus
the two optimizations above (low-round-trip traversal, wasm-blob size).

## Checkpoints

**This is the master roadmap.** The 25 checkpoints are split across five
dependency-ordered part-specs; each row points at the part that owns the full
contract. The detailed checkpoint description + per-checkpoint verification live
in the owning part-spec — this table is the map, not the contract.

Dependency order (strict, linear):

    substrate → extraction → resolution → query → surfaces

| Part-spec | Owns master CPs |
|-----------|-----------------|
| `docs/spec/code-intel-substrate.md` | CP0–CP4 |
| `docs/spec/code-intel-extraction.md` | CP5–CP10 |
| `docs/spec/code-intel-resolution.md` | CP11–CP16 |
| `docs/spec/code-intel-query.md` | CP17–CP19 |
| `docs/spec/code-intel-surfaces.md` | CP20–CP24 |

The canonical 4-column checkpoint table below is the master list; `Files/areas`
names the owning part-spec.

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 0 | Build-strategy gate: 19-grammar `ts.wasm` + proven pool/recycle/traversal | code-intel-substrate (CP1) | grammars load + vocab-match; pool race-clean; RSS bounded |
| 1 | Types contract + Go conventions | code-intel-substrate (CP2) | 22 NodeKind / 12 EdgeKind / 29 Language asserted |
| 2 | DB connection + schema + pragmas | code-intel-substrate (CP3) | schema byte-identical; WAL + FK on; single conn |
| 3 | Migrations v2–v4 | code-intel-substrate (CP4) | old DB migrates to v4 idempotently |
| 4 | Query layer + FTS parity | code-intel-substrate (CP5) | CRUD; cascade delete; FTS rank order matches |
| 5 | Binding iface + node-id + helpers | code-intel-extraction (CP1) | node-id golden-vector CI gate passes |
| 6 | Generic extractor core | code-intel-extraction (CP2) | TS fixture node/edge tree; broken file skipped |
| 7 | LanguageExtractor iface + 5 langs | code-intel-extraction (CP3) | 5 extractors incl. a non-C-family grammar |
| 8 | Remaining 14 language extractors | code-intel-extraction (CP4) | each fixture parses; node-count stable |
| 9 | Standalone extractors ×5 | code-intel-extraction (CP5) | `.vue` component + offset-correct children |
| 10 | Orchestrator + sync invariant | code-intel-extraction (CP6) | line-shift re-sync leaves no orphans; bounded mem |
| 11 | Import resolver + path aliases | code-intel-resolution (CP1) | relative/aliased/barrel imports resolve |
| 12 | Name matcher | code-intel-resolution (CP2) | `obj.method` + overloads resolve |
| 13 | Resolver pipeline + kind promotion | code-intel-resolution (CP3) | refs→edges; promotions; batch loop terminates |
| 14 | Framework iface + Express | code-intel-resolution (CP4) | Express routes → `route` nodes + edges |
| 15 | Remaining 22 frameworks | code-intel-resolution (CP5) | each framework emits routes on a fixture |
| 16 | Callback synthesizer ×14 | code-intel-resolution (CP6) | synth edges w/ `heuristic` provenance; stable |
| 17 | Graph traversal + query manager | code-intel-query (CP1) | callers/callees/impact/path correct |
| 18 | Search | code-intel-query (CP2) | ranked results; field filters; fuzzy fallback |
| 19 | Context builder + formatter | code-intel-query (CP3) | stable headings; JSON shape; reproducible |
| 20 | Engine facade | code-intel-surfaces (CP1) | method set matches appendix M; adapters compile |
| 21 | `atomic code` CLI subcommands | code-intel-surfaces (CP2) | verbs + `--json`; `case "code"` dispatches |
| 22 | MCP server `atomic code mcp` | code-intel-surfaces (CP3) | instructions; explore budgets; node overloads |
| 23 | MCP singleton daemon lifecycle (socket auto-start + idle reap/shutdown) | code-intel-surfaces (CP4) | dead-socket auto-start; conn reaped at 30m idle; server exits after 30m idle |
| 24 | Validation harness | code-intel-surfaces (CP5) | every success criterion has an automated check |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| **R-A** Parallel parsing under wazero unsafe if one parser is shared across goroutines | high | Bounded pool of wazero instances, one parser per in-flight file (design §Extraction concurrency); CP0 proves parallel parse |
| **R-B** Pre-release binding × 19 grammars; WASM grammar ABI may differ from the reference's → node-type strings mismatch → empty extraction | high | CP0 first checks ABI alignment, then per-grammar vocabulary match; explicit kill-criteria + three-way fallback |
| **R1** wazero grow-only memory unmanaged | low *(proven mitigable)* | Recycle every ~500 parses/instance → RSS flat ~1 GB (spike A3); unbounded only if never recycled |
| **R-WALK** per-node Go↔WASM traversal is ~70× slower than parsing (spike A4) | high | Walk via tree cursor / bulk in-WASM traversal returning structure in few calls; never per-node `Child` calls from Go. The one real pure-Go cost — design the extractor traversal around it |
| **R-WASM** 19-grammar `//go:embed` blob ≈ 30–60 MB (spike A1) | med | `-Os` already on; consider per-language lazy wasm modules or splitting the blob; accept larger binary if not |
| **R-E** Sync orphans because node-id embeds `line` | med | CP10 deletes a file's nodes (cascade) before re-insert; fixture test with a line-shifted symbol |
| **FK trap** `database/sql` pooling skips per-connection `foreign_keys=ON` → cascade silently fails | med | Single-connection mandate (design); pragmas applied once |
| **R3** Node-id divergence breaks edges | med | CP5 vector test against **reference-exported** goldens; CI gate |
| **R-F** modernc vs node:sqlite FTS5/bm25 ranking drift | low-med | CP4 asserts rank order on a known corpus; pin both SQLite versions |
| **R-C** Broad-parity + synthesis-in-v1 is large; half-built extractors | med | CP7 proves the contract incl. a non-C-family grammar; CP8 is 14 independently-green sub-slices; partial parity as a release lever |
| **R6** Calibrated constants drift | low | Centralize as named consts mirroring the appendix; a test asserts budget tiers + BM25 weights literally |

## Reference appendix

The authoritative extraction from the reference implementation. Full paths are
valid in the reference repo and are a **read-only learning anchor** — see
§Branding: port behavior, never the reference's product name or branded
identifiers. Verdicts: COPY / ADAPT / SKIP.

### A. Schema DDL (COPY verbatim) — `src/db/schema.sql`

Reproduce every statement. `nodes(id TEXT PK, kind, name, qualified_name,
file_path, language, start/end line+column, docstring, signature, visibility,
is_* INTEGER flags, decorators/type_parameters TEXT json, updated_at)`.
`edges(id INTEGER PK AUTOINCREMENT, source, target, kind, metadata TEXT json,
line, col, provenance TEXT DEFAULT NULL, FK source/target → nodes(id) ON DELETE
CASCADE)`. `files(path PK, content_hash, language, size, modified_at, indexed_at,
node_count, errors TEXT json)`. `unresolved_refs(id, from_node_id FK→nodes ON
DELETE CASCADE, reference_name, reference_kind, line, col, candidates TEXT json,
file_path DEFAULT '', language DEFAULT 'unknown')`. `project_metadata(key PK,
value, updated_at)`. FTS5: `CREATE VIRTUAL TABLE nodes_fts USING fts5(id, name,
qualified_name, docstring, signature, content='nodes', content_rowid='rowid')` +
triggers `nodes_ai`/`nodes_ad`/`nodes_au` (the `('delete', …)` sentinel on
delete/update is mandatory FTS5 content-table protocol). Edge indexes: **only**
`idx_edges_kind`, `idx_edges_source_kind`, `idx_edges_target_kind`,
`idx_edges_provenance` — **do NOT** add narrow `idx_edges_source`/`_target`; the
composites cover source-only/target-only via left-prefix; v4 drops the narrow
ones. Node indexes include `idx_nodes_lower_name ON nodes(lower(name))` (v3).

### B. Node-id scheme (COPY exactly) — `src/extraction/tree-sitter-helpers.ts:18`

```go
// All nodes except file nodes:
func generateNodeID(filePath, kind, name string, line int) string {
    input := fmt.Sprintf("%s:%s:%s:%d", filePath, kind, name, line)
    sum := sha256.Sum256([]byte(input))
    return kind + ":" + hex.EncodeToString(sum[:])[:32]
}
// File node exception (src/extraction/tree-sitter.ts:201):  id = "file:" + filePath
```

`line` is 1-based. Edges reference ids by value — divergence breaks every edge
(R3). **The `line` component means sync must delete-then-reinsert a file's nodes**
(appendix re storeExtractionResult). Make the vector test gate on goldens
exported from the reference impl.

### C. Kind & language strings (COPY verbatim) — `src/types.ts`

- **NodeKind (22):** `file, module, class, struct, interface, trait, protocol,
  function, method, property, field, variable, constant, enum, enum_member,
  type_alias, namespace, parameter, import, export, route, component`.
- **EdgeKind (12):** `contains, calls, imports, exports, extends, implements,
  references, type_of, returns, instantiates, overrides, decorates`.
- **Language (29):** `typescript, javascript, tsx, jsx, python, go, rust, java,
  c, cpp, csharp, php, ruby, swift, kotlin, dart, svelte, vue, liquid, pascal,
  scala, lua, luau, objc, yaml, twig, xml, properties, unknown`.
- Mirror structs: `Node`, `Edge`, `FileRecord`, `ExtractionResult`,
  `UnresolvedReference`, `Subgraph` (`map[string]Node` + `[]Edge` + `roots
  []string` + optional `confidence` — **sort on serialize**), `TraversalOptions`,
  `SearchOptions`, `SearchResult`, `Context`, `CodeBlock`, `GraphStats`.

### D. Extension → language map (COPY) — `src/extraction/grammars.ts:46`

Non-obvious entries: `.mts/.cts`→ts, `.mjs/.cjs/.xsjs/.xsjslib`→js, `.h`→c
(promote to cpp/objc by content heuristic), `.module/.install/.theme/.inc`→php,
`.rake`→ruby, `.kts`→kotlin, `.pas/.dpr/.dpk/.lpr/.dfm/.fmx`→pascal, `.sc`→scala.
Special non-extension: `conf/routes` + `*.routes`→yaml (Play; file-level only).
File-level-only (no symbol extraction, file record only): `yaml`, `twig`,
`properties`.

### E. The extractor contract (COPY) — `src/extraction/tree-sitter.ts`

Order: (1) receive `(filePath, source, language)`; (2) parse (via a **pooled
parser instance** — appendix-level note: one parser per goroutine, never shared);
(3) create the `file:` node, push onto `nodeStack`; (4) JVM package wrapping if
`packageTypes` defined; (5) `visitNode` walks named children, checking the
extractor's type arrays in order (function→class→method→interface→struct→enum→
typeAlias→property→field→variable→import→call→instantiation), calling the matching
`extract*` and setting `skipChildren`; (6) `createNode` = generate id + build
`::`-joined qualified-name from `nodeStack` + emit `contains` edge to parent; (7)
functions push onto stack, extract type annotations (`references`) + decorators
(`decorates`) + walk body for `calls`/`instantiates`; (8) classes extract
inheritance (`extends`/`implements`); (9) calls emit `UnresolvedReference{
fromNodeId, referenceName, referenceKind, line, column}` — **not** edges
(resolution makes edges); (10) return `ExtractionResult{nodes, edges,
unresolvedReferences, errors}` — best-effort, errors recorded, never abort.

`LanguageExtractor` shape (config object) — exemplar
`src/extraction/languages/typescript.ts`: type-name arrays (`functionTypes`,
`classTypes`, …) + field names (`nameField`, `bodyField`, `paramsField`,
`returnField`) + hooks (`resolveBody`, `getSignature`, `getVisibility`,
`isExported`, `isAsync`, `isStatic`, `isConst`, `extractImport`). In Go: an
interface or struct of funcs. **AST node-type strings come from each grammar and
must match what that grammar emits — CP0 verifies this per language (R-B).**

Standalone pattern — `src/extraction/vue-extractor.ts`: same `extract()
ExtractionResult` signature; create a `component` node at line 1
(`isExported=true`); run the JS/TS extractor on `<script>` blocks then **offset
all node/edge/ref line numbers** by the block's start line; emit `contains` from
component to each child; PascalCase/kebab template tags → `references` refs.

**SKIP (WASM/Node-only):** worker-thread spawn/recycle, `PARSE_TIMEOUT_MS`,
`WORKER_RECYCLE_INTERVAL=250`, `PARSER_RESET_INTERVAL=5000`, the Emscripten
stderr filter, OOM `process.exit(1)` retry. BUT re-express the *memory intent* as
a per-instance wazero module-recycle (design R1) — under wazero the grow-only
memory problem is real.

### F. Resolution order (COPY) — `src/resolution/index.ts`

Startup: `createResolver` → `initialize` (detect frameworks, clear caches) →
optional `runPostExtract`. Batch (after extraction):
`resolveAndPersistBatched(batchSize=5000)` → `warmCaches` (`knownFiles`,
`knownNames`) → loop reading `unresolved_refs` at **offset 0** (re-read after
delete — do NOT advance offset) → per ref `resolveOne` → `createEdges` (with kind
promotion) → `insertEdges` → `deleteSpecificResolvedReferences` → break when a
batch yields nothing → **then** `synthesizeCallbackEdges` LAST.

`resolveOne`: (1) built-in/external skip (per-language sets); (2) pre-filter
`hasAnyPossibleMatch` + `matchesAnyImport` + framework `claimsReference`; (3) JVM
FQN fast path (conf 0.95, return); (4) frameworks `resolve`, return if ≥0.9 else
accumulate; (5) `resolveViaImport`, return if ≥0.9 else accumulate; (6)
`matchReference`; (7) return highest-confidence candidate.

`matchReference` sub-order: filePath → qualifiedName → methodCall → exactName →
fuzzy (first match wins). `findBestMatch` scoring (calibrated — do not change
without A/B): same-file +100, path proximity 0–80, same-language +50 /
cross-language −80, call→function / instantiates→class / decorates→function +25,
exported +10, line-distance adjustment. Re-export chain depth cap
`REEXPORT_MAX_DEPTH=8`, cycle-safe. Edge-kind promotion: `extends`→`implements`
when target is interface; `calls`→`instantiates` when target is class/struct.

### G. Synthesized-edge provenance (COPY) — `src/resolution/callback-synthesizer.ts`

Every synthesized edge: `kind:'calls'`, `provenance:'heuristic'`,
`metadata:{synthesizedBy, via?, field?, event?, registeredAt?}`. Dedup key
`"source>target"`. Caps: `MAX_CALLBACKS_PER_CHANNEL=40`, `EVENT_FANOUT_CAP=6`,
`CC_FANOUT_CAP=8`. Runs after all static edges commit.

14 synthesizers (`synthesizedBy` tags): `callback` (field-backed observer),
`closure-collection` (Swift/Kotlin `.forEach{$0()}` ↔ `.append`), `event-emitter`
(`.on('e',fn)`/`.emit('e')`), `react-render` (`this.setState`→`render`),
`jsx-render` (`<PascalChild>`→component), `vue-handler` + Vue kebab children,
`flutter-build` (`setState`→`build`), `cpp-override` (vtable base→override),
`interface-impl` (JVM/TS/Swift interface dispatch), `go-grpc-stub-impl`,
`rn-event-channel`, `fabric-native-impl`, `mybatis-java-xml`,
`gin-middleware-chain`. `provenance='heuristic'` is the ONLY provenance value;
static edges carry none. The explore/node renderers surface these with their
`registeredAt` site *because* of this tag — load-bearing.

### H. Framework resolver contract (COPY template) — `src/resolution/frameworks/express.ts`

Interface (`src/resolution/types.ts`): `name`, `languages?`, `detect(ctx)bool`,
`resolve(ref,ctx)→ResolvedRef?`, `claimsReference?(name)bool`,
`extract?(filePath,content)→{nodes,references}`, `postExtract?(ctx)→[]Node`. Route
node id `route:{filePath}:{line}:{METHOD}:{path}`; qualifiedName
`{filePath}::METHOD:{path}`; name `"METHOD /path"`. `detect` reads a config file
(package.json/go.mod/Gemfile) then falls back to path + content patterns.
`extract` strips comments first, regex-finds routes, emits the route node +
handler `references` (named) or `calls` (inline body call sites, minus reserved
names). `resolve` confidence 0.8–0.9. Registry `FRAMEWORK_RESOLVERS` +
`detectFrameworks` (filter by `detect`) + `getApplicableFrameworks` (by
language). **23 resolvers** in the registry (`frameworks/index.ts:32-66`); port
Express first as the template, fan out the other 22 in CP15.

### I. Graph traversal (COPY) — `src/graph/{traversal,queries}.ts`

BFS batches neighbor fetch (`getNodesByIds`, not N+1) and sorts edges
`contains`(0) < `calls`(1) < other(2). `getCallers`/`getCallees` follow
`calls|references|imports` recursively to `maxDepth` (default 1).
`getImpactRadius` (default depth 3) follows all **incoming except `contains`**;
container kinds first descend into children via `contains` outgoing (avoids
climbing to parent then re-expanding siblings). `findPath` = BFS shortest path,
optional `edgeKinds`. `findDeadCode` = `function/method/class`, no non-`contains`
incoming, `isExported=false`. `findCircularDependencies` = DFS over file-level
`imports` with a recursion stack.

### J. Search (COPY) — `src/search/*.ts`, `src/db/queries.ts`

Query parser fields: `kind:` (a NodeKind), `lang:`/`language:` (a Language),
`path:` (case-insensitive substring on file_path), `name:` (substring on name);
remainder → FTS text. FTS5 BM25 column weights `(0, 20, 5, 1, 2)` for `(id, name,
qualified_name, docstring, signature)`; over-fetch 5× then rescore with
`kindBonus + scorePathRelevance + nameMatchBonus`. 3-tier: FTS5 prefix → LIKE
fallback (CASE: exact 1.0 / prefix 0.9 / contains 0.8 / qualified 0.7) →
Levenshtein fuzzy (`maxDist = 1 if len≤4 else 2`, bounded Damerau-Levenshtein).
Escape FTS special chars; treat `::` as whitespace. `kindBonus`: function/method
10, interface/trait/protocol/route 9, class/component 8, enum 5,
type_alias/struct 6, module/namespace 4, property/field/constant 3, variable 2,
import/export 1, file/parameter 0. Test-file downrank −15 unless test query.
**CP4 asserts rank order, not just non-empty.** **Add `ORDER BY score,
nodes.id`** (a secondary key the reference lacks): spike B1 proved rank+score
parity with `node:sqlite` but tied rows fall back to rowid/insertion order,
which the TS and Go indexers could assign differently — the explicit tiebreaker
removes that drift. Pin/track both SQLite versions and the FTS5 tokenizer.

### K. Explore budget constants (COPY exactly) — `src/mcp/tools.ts`

`getExploreBudget(fileCount)` → call budget: `<500→1, <5000→2, <15000→3,
<25000→4, ≥25000→5` (max 5). `getExploreOutputBudget(fileCount)` → per-call
output. **Invariant: `maxCharsPerFile` monotonically non-decreasing.**

| Tier (fileCount) | maxOutputChars | defaultMaxFiles | maxCharsPerFile | gapThreshold | excludeLowValueFiles |
|---|---|---|---|---|---|
| `<150` | 13000 | 4 | 3800 | 7 | true |
| `<500` | 18000 | 5 | 3800 | 8 | true |
| `<5000` | 24000 | 8 | 6500 | 12 | false |
| `≥5000` | 24000 | 8 | 7000 | 15 | false |

(Source collapses the top tiers into one `else`; values as shown.) Hard ceiling:
`min(maxOutputChars*1.5, 25000)` chars, **cut at the last `\n####` section
boundary** in the back half (reproduce the cut, not just the number) — never
exceed 25,000 (above it the host externalizes the result to a file, forcing a
Read). Whole-file thresholds: central ≤280 lines, peripheral ≤220 lines.
Tiny-repo (`<500` files) tool gating: expose only the explore, search, and node
tools. Explore output must **never** tell the agent to "use Read" — steer to
another explore call and treat returned source as already read.

### L. MCP tool catalog (COPY logic; ADAPT transport to go-sdk; `atomic_code_*` names) — `src/mcp/tools.ts`

8 tools (name them `atomic_code_…`): search`(query, kind?, limit?)`, callers`(symbol,
limit?)`, callees`(symbol, limit?)`, impact`(symbol, depth?)`, node`(symbol,
includeCode?, file?, line?)`, explore`(query, maxFiles?)`, status`()`,
files`(path?, pattern?, format?)`. Input limits: query/symbol ≤10000, paths
≤4096. The **node** tool returns **all overloads in one call** on an ambiguous
bare name (`getNodesByName`, full scan); container kinds get a structural outline
(member signatures) not full body; code line-numbered; trail = up to 12 callers +
12 callees with dynamic-dispatch annotation. `buildFlowFromNamedSymbols`:
tokenize query → resolve named symbols → BFS along `calls` (≤1 unnamed bridge
hop) → longest chain → prepend a `## Flow` section; supplement with synthesized
(heuristic) edges incident to named symbols. `server-instructions.ts` (COPY
behavior, **de-brand the text** to `atomic` + the `atomic_code_*` tool names) is
returned as the `initialize` `instructions` field and is the single source of
truth for agent guidance — keep it the only place that guidance lives.

### M. The shared query core / facade (COPY surface) — `src/index.ts`

The engine struct both MCP and CLI handlers sit on. Method set (idiomatic Go,
same semantics): lifecycle `Init/Open/IsInitialized/Close/ProjectRoot/
Uninitialize`; indexing `IndexAll/IndexFiles/Sync/ResolveReferences/
GetDetectedFrameworks/IsIndexing/ExtractFromSource/GetLastIndexedAt`; stats
`GetStats/GetBackend/GetJournalMode`; nodes `GetNode/GetNodesInFile/
GetNodesByKind/GetNodesByName/SearchNodes/GetTopRouteFile/GetRoutingManifest`;
edges `GetOutgoingEdges/GetIncomingEdges`; files `GetFile/GetFiles`; graph
`GetContext/Traverse/GetCallGraph/GetTypeHierarchy/FindUsages/GetCallers/
GetCallees/GetImpactRadius/FindPath/GetAncestors/GetChildren/GetFileDependencies/
GetFileDependents/FindCircularDependencies/FindDeadCode/GetNodeMetrics`; context
`GetCode/FindRelevantContext/BuildContext`; db `Optimize/Clear`. Watch methods
stubbed in v1.

### N. `atomic code` subcommands (COPY logic; ADAPT to atomic dispatch) — `src/bin/`

Reproduce the *handler logic* for: index/sync/status/search/files/callers/
callees/impact/affected/explore, wired as `atomic code <verb>` via `runCode` in
`atomic/cmd/atomic/main.go` (existing `flag.FlagSet` style — NOT commander). Each
calls the facade; query verbs support `--json` (atomic prints). `affected` = BFS
over `GetFileDependents` from changed files to impacted test files (default depth
5, optional test-glob, `--stdin` input). `status` JSON: `initialized, version,
indexPath, lastIndexed` (ISO8601), file/node/edge counts, backend, journal mode,
nodes-by-kind, pending changes — the **stale-graph visibility** property that
substitutes for v1's missing watcher. DB path:
`<projectRoot>/.claude/.atomic-index/atomic.db`.

### O. Connection pragmas (COPY order; single connection) — `src/db/index.ts:30`

Exact order on the **one** connection: `busy_timeout=5000` (FIRST),
`foreign_keys=ON` (per-connection — required for cascade; the single-connection
mandate exists so this never silently lapses), `journal_mode=WAL`,
`synchronous=NORMAL`, `cache_size=-64000` (64 MB), `temp_store=MEMORY`,
`mmap_size=268435456` (256 MB). After bulk writes: `PRAGMA optimize` then `PRAGMA
wal_checkpoint(PASSIVE)`. 500-row chunking for any variadic `IN (...)`
(`SQLITE_PARAM_CHUNK_SIZE=500`).

## Change log

### 2026-06-04 — Adapted from the standalone Go-port bundle into the atomic CLI

**What changed:** Re-homed the engine from a standalone `code-intel-engine-go`
binary to a subsystem of the `atomic` CLI. Resolved the `<brand>` placeholder to
`atomic`; bound the command surface to `atomic code <verb>`, the data dir to
`<projectRoot>/.claude/.atomic-index/`, the SQLite file to `atomic.db`, and the
MCP tool prefix to `atomic_code_*`. Added an "atomic CLI integration" section
(dispatch via `runCode` in `atomic/cmd/atomic/main.go`, packages under
`atomic/internal/codeintel/`, `context`→`codectx` rename, go.mod deps, build
pipeline note). Flipped the "CLI framework — host owns it" non-goal: atomic is
the host, so the thin `runCode` arg-parsing layer is now in scope (atomic's
existing `flag.FlagSet` style, not a new framework). Rewrote CP21 from "Go-API
handlers" to "`atomic code` subcommands"; CP22 + appendices L/N to the `atomic`
bindings. Cross-compile target narrowed to darwin/linux × amd64/arm64 (repo
platform policy; windows best-effort).

**Why:** Project owner directed the engine be implemented inside the atomic CLI
rather than as a separate binary. Decisions captured from owner: embed the
~30–60 MB grammar blob unconditionally (binary size not a constraint); full 19
languages + 5 standalone in scope (no staged subset); DB lives under the
project's `.claude/` folder, gitignored, wired by `atomic setup`.

**Superseded:** prior contract targeted a standalone static binary whose host CLI
owned all arg parsing and whose data dir was `<projectRoot>/.<brand>/<brand>.db`.

### 2026-06-04 — Split the monolithic checkpoints into five part-specs

**What changed:** This file became the **umbrella**. The single 25-row
`## Checkpoints` table was replaced with a master roadmap that maps each CP to one
of five new dependency-ordered part-specs; the detailed per-checkpoint contracts
moved into those files. The reference appendix A–O stays here as the single
authoritative source — part-specs reference it by letter, never copying it.

**Split into:** `docs/spec/code-intel-substrate.md` (CP0–4),
`docs/spec/code-intel-extraction.md` (CP5–10),
`docs/spec/code-intel-resolution.md` (CP11–16),
`docs/spec/code-intel-query.md` (CP17–19),
`docs/spec/code-intel-surfaces.md` (CP20–24).

**Why:** The 25-checkpoint monolith was too large for one `/subagent-implementation`
run and made the dependency order implicit. Five parts make each run digestible
and the substrate → extraction → resolution → query → surfaces order explicit.
