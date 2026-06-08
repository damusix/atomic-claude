# code-intel

## What it does

Tree-sitter/wazero-based code-intelligence engine embedded in the [`atomic`](../../../atomic) CLI. Parses source files into a SQLite-backed symbol graph, resolves cross-file references, and exposes the graph via the `atomic code` CLI verbs and an MCP server. Index lives at `<projectRoot>/.claude/.atomic-index/atomic.db`.

## CLI code

- [`atomic/internal/codeintel/cli/code.go`](../../../atomic/internal/codeintel/cli/code.go) — `RunCode` dispatcher for `atomic code <verb>`: index, sync, status, search, callers, callees, impact, node, files, affected, explore, mcp. Internal `__serve` verb spawns the daemon.
- [`atomic/internal/codeintel/engine/engine.go`](../../../atomic/internal/codeintel/engine/engine.go) — `Engine` facade (CP20): wraps db, pool, orchestrator, resolution pipeline, graph manager, searcher, and context builder. Both CLI and MCP compile against this.
- [`atomic/internal/codeintel/tsbinding/`](../../../atomic/internal/codeintel/tsbinding) — wazero binding that loads `lib/ts.wasm` (embedded via `//go:embed`) and exposes tree-sitter parse/query APIs. Owns a separate `go.mod` (`module github.com/malivvan/tree-sitter`, go 1.23.4).
- [`atomic/internal/codeintel/extraction/extractor.go`](../../../atomic/internal/codeintel/extraction/extractor.go) — `TreeSitterExtractor`: drives one file through the grammar, produces `ExtractionResult{Nodes, Edges, UnresolvedReferences}`. Appendix-E contract.
- [`atomic/internal/codeintel/extraction/languages/`](../../../atomic/internal/codeintel/extraction/languages) — per-language `LanguageExtractor` configs: Go, TypeScript, TSX, JavaScript, Python, Ruby, Rust, Java, C, C++, C#, Swift, Kotlin, Dart, Lua, Luau, Objective-C, PHP, Pascal, Scala, plus a `registry.go`.
- [`atomic/internal/codeintel/extraction/standalone/sql.go`](../../../atomic/internal/codeintel/extraction/standalone/sql.go) — SQL language extractor (standalone; not tree-sitter-backed). Parses DDL/DML to extract table, view, procedure, trigger, and function nodes.
- [`atomic/internal/codeintel/extraction/pool.go`](../../../atomic/internal/codeintel/extraction/pool.go) — pooled wazero runtimes for parallel extraction.
- [`atomic/internal/codeintel/db/`](../../../atomic/internal/codeintel/db) — SQLite layer: `db.go` enforces single-connection + 7-pragma appendix-O order; `schema.sql` embedded via `go:embed`; CRUD, migrations, stats, search, resolution helpers.
- [`atomic/internal/codeintel/indexer/orchestrator.go`](../../../atomic/internal/codeintel/indexer/orchestrator.go) — `Orchestrator`: `IndexAll`, `IndexPaths`, `Sync` — walks project files, dispatches to pool, persists results.
- [`atomic/internal/codeintel/resolution/pipeline.go`](../../../atomic/internal/codeintel/resolution/pipeline.go) — three-phase resolution pipeline (warm cache → name-match → synthesis); `ResolveAndPersistBatched` with optional `PhaseEmitFunc` profiling hook.
- [`atomic/internal/codeintel/resolution/frameworks/`](../../../atomic/internal/codeintel/resolution/frameworks) — framework-specific route extractors (Node, Python, Ruby, PHP, Rust, Spring, Go gRPC stub). `Registry.ExtractAndPersist` runs before the resolution pipeline.
- [`atomic/internal/codeintel/resolution/synthesis/`](../../../atomic/internal/codeintel/resolution/synthesis) — symbol synthesis for indirect patterns (closures, event emitters, interface impls, batch callbacks).
- [`atomic/internal/codeintel/graph/`](../../../atomic/internal/codeintel/graph) — `Manager`: BFS callers/callees, impact radius, path finding, type hierarchy, dead-code detection.
- [`atomic/internal/codeintel/search/search.go`](../../../atomic/internal/codeintel/search/search.go) — 3-tier FTS → LIKE → fuzzy search over indexed nodes.
- [`atomic/internal/codeintel/codectx/codectx.go`](../../../atomic/internal/codeintel/codectx/codectx.go) — `Builder.FindRelevantContext` + `BuildContext`: assembles a markdown context block for AI agents from a subgraph.
- [`atomic/internal/codeintel/mcp/server.go`](../../../atomic/internal/codeintel/mcp/server.go) — MCP server (CP22): 8 tools (`atomic_code_explore`, `atomic_code_search`, `atomic_code_node`, `atomic_code_callers`, `atomic_code_callees`, `atomic_code_impact`, `atomic_code_files`, `atomic_code_affected`); tool-gating: tiny repos (< 500 files) get 3 tools.
- [`atomic/internal/codeintel/mcp/daemon.go`](../../../atomic/internal/codeintel/mcp/daemon.go) — singleton daemon (`RunDaemon`); flock-guarded auto-start path.
- [`atomic/internal/codeintel/mcp/proxy.go`](../../../atomic/internal/codeintel/mcp/proxy.go) — `RunProxy`: connect-or-start daemon, pipe stdin↔socket.
- [`atomic/internal/codeintel/types/`](../../../atomic/internal/codeintel/types) — shared type definitions: `Node`, `Edge`, `Subgraph`, `FileRecord`, `SearchOptions`, `GraphStats`, `Context`, `CodeBlock`, node/edge kind enums.
- [`atomic/internal/codeintel/validation/`](../../../atomic/internal/codeintel/validation) — grammar validation helpers.
- [`atomic/internal/codeintel/grammars/`](../../../atomic/internal/codeintel/grammars) — embedded grammar WASM assets.

## Docs

- [`docs/reference/code-intel.md`](../../../docs/reference/code-intel.md) — user-facing reference: what the engine indexes (29 languages, 15 web-route frameworks, SQL graph), all `atomic code` verbs, index lifecycle, workflow integration diagram, and MCP setup pointer.
- [`docs/spec/code-intel-engine.md`](../../../docs/spec/code-intel-engine.md) — umbrella spec; shared appendices A–O (schema, extraction contract, resolution contract, MCP tools, profiling, watch-stub). Part-specs reference this by letter.
- [`docs/spec/code-intel-extraction.md`](../../../docs/spec/code-intel-extraction.md) — extraction part-spec (CP0–CP9).
- [`docs/spec/code-intel-resolution.md`](../../../docs/spec/code-intel-resolution.md) — resolution part-spec (CP10–CP14).
- [`docs/spec/code-intel-substrate.md`](../../../docs/spec/code-intel-substrate.md) — DB/schema/indexer part-spec (CP1–CP5).
- [`docs/spec/code-intel-query.md`](../../../docs/spec/code-intel-query.md) — query/graph/context part-spec (CP15–CP20).
- [`docs/spec/code-intel-surfaces.md`](../../../docs/spec/code-intel-surfaces.md) — CLI + MCP surfaces part-spec (CP21–CP23).
- [`docs/spec/sql-language-support.md`](../../../docs/spec/sql-language-support.md) — SQL extractor spec; standalone (no tree-sitter); DDL/DML symbol extraction for table, view, procedure, trigger, function nodes.
- [`docs/design/code-intel-engine.md`](../../../docs/design/code-intel-engine.md) — design doc: wazero-memory correction, concurrency model, CLI integration seams, framework-extraction ordering.
- [`docs/spec/code-intel-integration.md`](../../../docs/spec/code-intel-integration.md) — integration spec: how agents consume the engine. Defines the `agent-code-intel` partial contract, agent composition matrix (investigator + builder + surgeon + reviewer + haiku + signals-inferrer), lifecycle index/sync contract for `/subagent-implementation` / `/autopilot` / `/refresh-signals` / `/refresh-wiki`, doctor check 11 behavior, and `/atomic-help` coverage requirements.
- [`docs/design/code-intel-integration.md`](../../../docs/design/code-intel-integration.md) — design doc: approach B rationale (dedicated partial over overloading `agent-search-tooling`), agent composition matrix, degradation contract.
- [`docs/guides/code-intel-mcp.md`](../../../docs/guides/code-intel-mcp.md) — user guide: manual `.mcp.json` setup for `atomic code mcp`. MCP registration is opt-in, not auto-registered by `atomic claude install`.

## Coupling

- [`atomic/cmd/atomic/main.go`](../../../atomic/cmd/atomic/main.go) dispatches `"code"` to `codecli.RunCode` — any change to the CLI surface requires updating `main.go:codeAction` and the `atomic code` help text.
- [`atomic/internal/codeintel/tsbinding/`](../../../atomic/internal/codeintel/tsbinding) is a separate Go module (`go.mod`) — changes to wazero or tree-sitter version require a `go mod tidy` there independently.
- [`atomic/internal/codeintel/db/schema.sql`](../../../atomic/internal/codeintel/db/schema.sql) is the schema source of truth — schema changes require a migration in `migrations.go` and version bump in `db.go`.
- Framework route extraction (`resolution/frameworks/`) runs after `IndexAll` and before `ResolveReferences` — pipeline ordering in `cli/code.go:runIndex` and `engine.go:ExtractFrameworkNodes` must stay in sync with spec appendix ordering.
- MCP tool names use `atomic_code_*` prefix — any rename breaks agents or IDE integrations that use the MCP protocol.
- [`scripts/code-eval/`](../../../scripts/code-eval) eval harness (fetch-corpus + run-eval) exercises the full extraction pipeline; changes to extraction output format invalidate the corpus results.
- Index lives at `<projectRoot>/.claude/.atomic-index/` (gitignored via `EnsureGitignore`). Doctor check 11 (`checks_code_index.go`) reports index health — absence is informational PASS; stale is WARN; fresh is PASS. The check imports `engine.IndexPath`; if the path convention changes, both must change together.
- `engine.IndexPath` is the canonical function for deriving the DB path from a project root — consumers outside the engine package must call this function, not construct the path inline.
- **Agent consumption**: all keystone agents (investigator, builder, surgeon, reviewer, haiku, signals-inferrer) plus strategist carry the `agent-code-intel` partial. The partial contract: index present → lead with `atomic code explore "<query>"` for orientation (returns bundled context digest); then targeted verbs (`search`/`callers`/`callees`/`impact`) for specific symbol questions; bounded queries only (one explore question or one symbol at a time); graceful degradation to sg/grep on any failure (binary absent, DB missing, query error) — never surface errors about the index being unavailable.

## Conventions worth knowing

- **Branding hard rule**: the reference TypeScript engine's product name must never appear in code, comments, identifiers, strings, tool names, or output. Brand slug = [`atomic`](../../../atomic); MCP tools = `atomic_code_*`; data dir = [`.claude/.atomic-index/`](../../.atomic-index).
- **Engine lifecycle**: `New` → (`Init` or `Open`) → use → `Close`. `Init` is idempotent; `Open` errors if index absent. All facade methods call `requireDB()` first.
- **Single-connection SQLite**: `SetMaxOpenConns(1)` + `SetMaxIdleConns(1)` enforced in `db/db.go`. Appendix-O pragma order is mandatory — `busy_timeout` first.
- **Extraction is best-effort**: errors are recorded in `ExtractionResult.Errors`, never abort. Unresolved calls emit `UnresolvedReference` (not edges); edges are created only after resolution.
- **Framework extraction ordering**: `ExtractFrameworkNodes` must run after `IndexAll`/`Sync` and before `ResolveReferences` so route→handler refs exist when the resolution pipeline runs.
- **Watch stubbed in v1**: `Watch`/`StopWatch` return `ErrWatchNotImplemented` per appendix M.
- **SQL extractor is standalone**: `extraction/standalone/sql.go` does not use tree-sitter; it has its own parse logic. `embedded-sql-extraction` follow-up tracks extracting SQL embedded in host-language string literals.
- **Tool gating**: MCP server registers only 3 tools when `fileCount < 500` (`atomic_code_explore`, `atomic_code_search`, `atomic_code_node`).
