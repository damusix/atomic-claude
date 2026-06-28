# llm-memory-db-pg


A full-surface noorm example: an LLM memory + task tracker schema, built end-to-end against PostgreSQL with the noorm CLI, the noorm SDK, and the noorm MCP server. It exists as both a demo and a stress test of the noorm tooling — the schema covers every relational pattern noorm is designed to model (state-machine guards, cascading subtype tables, exclusivity triggers, recursive cycle detection, audit logging, soft-delete + restore + TTL cleanup), and the tests exercise every SDK feature (typed `ctx.proc/func/tvf`, Kysely views, observer events, lock manager, vault, impersonation, and the change system).

The schema artifact this project implements lives at `tmp/llm-memory-db.pseudo` in the monorepo root.


## Quick stats


| Surface | Count |
|---|---:|
| Tables | 38 |
| Views | 18 |
| Procedures (PG `PROCEDURE`) | 59 |
| Functions (PG `FUNCTION`) | 24 (10 helpers + 10 `sp_*_Create` + 4 trigger functions) |
| Triggers | 10 |
| SQL files | 140 |
| Test files | 26 |
| Tests | 230 passing, 0 failing |
| `expect()` calls | 597 |
| Type casts | 4 (all annotated `// cast-justified:`) |
| `any` types | 0 |
| Applied changes | 1 (`add-memory-tag-color`) |


## Prerequisites


- **Bun** 1.3+ (`bun --version`)
- **Docker** with `docker-compose` running the monorepo's PostgreSQL container at `localhost:15432`. Start it from the monorepo root:

    ```bash
    docker-compose up -d postgres
    ```

- A globally installed **noorm CLI** with the `mcp` subcommand. The MCP discovery test (`tests/mcp-discovery.test.ts`) spawns `noorm mcp serve` and resolves the binary from PATH, skipping any `node_modules/.bin` shim. Install via:

    ```bash
    npm install -g @noormdev/cli
    # or, for development against the workspace, build the local arm64 binary:
    cd /path/to/noorm/monorepo && bun run build:cli
    cp packages/cli/bin/noorm-darwin-arm64 ~/.local/bin/noorm
    ```


## Setup


From inside `examples/llm-memory-db-pg/`:

```bash
bun install
noorm db create --name noorm_llm_dev
noorm db create --name noorm_llm_test
noorm config use dev
noorm run build
noorm change ff
```

The build produces 38 tables, 18 views, 69 procedures (10 PG `FUNCTION` + 59 `PROCEDURE`), 10 helper functions, and 10 triggers in the `dev` database. The `noorm change ff` step applies the one shipped change (`changes/2026-05-10-add-memory-tag-color/`) which adds a `color` column to `Tag` and threads it through `vw_Tag`.


## Running tests


```bash
bun run test              # full suite, serial
bun run test:watch        # watch mode (still serial)
bun run typecheck         # tsc --noEmit
```

Tests **must** be run with `--serial` (set by `bun run test`). The suite uses a memoized `bootstrap()` helper (`tests/helpers/test-context.ts`) that does `db.reset()` + `changes.ff()` once per process and shares the connected context across all 26 test files. Bun's default parallel mode breaks this assumption.


## Project layout


```
examples/llm-memory-db-pg/
├── .noorm/                      # noorm settings + encrypted state
├── changes/                     # evolutionary changes (one applied)
├── sql/
│   ├── 00_types/                # composite types (empty — no procs need them)
│   ├── 01_reference/            # 10 reference tables
│   ├── 02_tables/               # 8 elevated entities + 1 hierarchic (Task)
│   ├── 03_subtypes/             # 3 *_Note + 5 *_StateTransition
│   ├── 04_binary/               # 11 binary fact tables
│   ├── 05_seeds/                # ref-data seeds + sentinel rows (Agent(0), Project(0))
│   ├── 06_functions/            # 10 helper functions
│   ├── 07_views/                # 18 views
│   ├── 08_procedures/           # 69 procs across 10 domain folders
│   ├── 09_triggers/             # exclusivity triggers (Note + StateTransition)
│   └── $helpers.ts              # template helpers (quoteIdent, pgArray, boolLit)
├── src/
│   ├── core/                    # Repo base, createContext factory, type composer
│   ├── agent/                   # 9 domain folders, each with
│   ├── project/                 # types.ts, schema.ts, commands.ts,
│   ├── memory/                  # queries.ts, index.ts
│   ├── note/
│   ├── tag/
│   ├── artifact/
│   ├── milestone/
│   ├── task/
│   ├── audit/                   # read-only — no commands.ts
│   └── index.ts                 # LlmMemoryDb facade
├── tests/
│   ├── helpers/test-context.ts  # bootstrap() + truncateAll()
│   ├── sql/                     # 11 files — direct SDK calls (proc/func/tvf, kysely)
│   ├── domain/                  # 8 files — facade-only (db.<domain>.cmd|qry.*)
│   ├── integration/             # 4 files — observer, lock, vault, impersonation
│   └── mcp-discovery.test.ts    # spawns `noorm mcp serve`, validates discovery surface
├── .mcp.json                    # auto-attached when Claude Code launches here
├── mcp-config.json              # standalone MCP config snippet
├── package.json
├── tsconfig.json
└── REPORT.md                    # post-build report (Phase 2 deliverable)
```


## What this example demonstrates


### Schema features

- **State machines** with allow-list tables (`TrackingStatus_Allowed`, `RelevanceStatus_Allowed`) plus PG `RAISE EXCEPTION 'transition X -> Y not allowed'` rejections.
- **Audit logging**: every `Set*` proc writes a `StateTransition` basetype row + a discriminated subtype row (`Note_StateTransition`, `Memory_StateTransition`, etc.) atomically.
- **Exclusivity triggers**: a `Note` of type `'project'` cannot have a row in `Milestone_Note` or `Task_Note`. Enforced via two-direction triggers (subtype-side INSERT guard + basetype-side type-update guard).
- **Recursive cycle detection**: `fn_TaskDependencyWouldCycle` walks `Task_Dependency` via a recursive CTE before `sp_Task_Depend` accepts the edge.
- **Soft-delete + TTL hard-delete**: every entity has `relevance_status='deleted'` for soft-delete; `sp_Cleanup(p_ttl_days)` hard-deletes rows whose `updated_at` is past the TTL.
- **Sentinel rows**: `Agent(0)` and `Project(0)` are protected against UPDATE/DELETE; they're the fallback owners when an agent or project is removed.
- **Composite primary keys**: `Task` is hierarchic on `(milestone_id, task_no)`. Subtype tables use the basetype's PK as their FK.
- **UNION-ALL polymorphic views**: `vw_Tag`, `vw_Artifact`, `vw_Related_Memory`, `vw_Recent_Activity`, `vw_StateTransition` flatten subtype tables into a single projection with a discriminator column.


### SDK features

- **Domain-local typings**: each `src/<domain>/types.ts` declares the domain's `Row`, `Procs`, `Funcs`, and `Tvfs` interfaces. `src/core/types.ts` composes them via intersection. Adding a new proc means editing one file.
- **Zod-at-the-boundary**: every `cmd.*` method validates input through a Zod schema (`src/<domain>/schema.ts`) before the proc call. The Zod schemas use camelCase user-facing field names; the camelCase→snake_case+`p_*` mapping happens inside the wrapper.
- **Repo base class**: `src/core/repo.ts` declares `abstract class Repo { constructor(protected readonly ctx: ...) {} }`. Every `*Commands` and `*Queries` extends `Repo` so the four-generic context type is declared once.
- **Read/write split per domain**: writes (`commands.ts`) go through stored procs; reads (`queries.ts`) go through views, Kysely, and helper functions. Different shapes, different test patterns.
- **Read-only audit domain**: `src/audit/` has only `queries.ts` and `index.ts` — no `cmd`, no Zod input layer. Demonstrates the SDK doesn't force every domain into the same shape.


### Test-layer split

| Layer | Validates | Bypasses |
|---|---|---|
| `tests/sql/` (11 files) | The SQL contract — proc behavior, view projections, function logic, state-machine rejections, exclusivity triggers, cycle detection. | Zod, the facade |
| `tests/domain/` (8 files) | The facade plumbing — Zod validation, camelCase→snake_case, proc binding, query routing. Trusts that `tests/sql/` already proved the SQL works. | The SQL layer's own internals |
| `tests/integration/` (4 files) | SDK-level features — observer events, lock manager, vault, impersonation. | The schema (these tests are independent of the LLM memory model) |
| `tests/mcp-discovery.test.ts` | The MCP server itself — spawns `noorm mcp serve`, sends JSON-RPC, asserts an external agent can discover the schema purely through the MCP. | Local in-process SDK access |


## How the schema was built


Phase 1 used the noorm CLI to bootstrap `.noorm/`, declare two stages (`dev` + `test`), build the schema from SQL files, apply an evolutionary change, generate the SDK shape, and write the test suite. Phase 2 used the noorm MCP server to verify schema integrity, grade test quality, and add a meta-test that exercises the MCP itself.

The full play-by-play is in `REPORT.md` (final deliverable) and `REPORT-PHASE-1.md` (intermediate report). Three real noorm SDK bugs surfaced during Phase 1 and were fixed at the source — see `../../postgres-problems.md` (monorepo root) for the full list of CLI/SDK/MCP issues encountered.


## What this is NOT


- A production schema. The state machine choices, soft-delete semantics, and confidence model are designed to exercise noorm features, not to be deployed.
- A general-purpose memory store for an LLM agent. The shape is shaped to surface noorm's edge cases, not to optimize for retrieval speed or cardinality.
- Stable. The schema may evolve via the change system; future changes will land in `changes/` and be applied with `noorm change ff`.
