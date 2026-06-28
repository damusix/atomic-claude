# llm-memory-db-mssql


An end-to-end noorm example: an LLM-memory schema running on SQL Server 2022, exercising the SDK against MSSQL-specific surfaces (table-valued parameters, schema-bound validators, role-scoped views, named-procedure calls).


## What this project demonstrates


- A non-trivial T-SQL schema: 38 tables, 18 views, 72 stored procedures, 14 functions, 4 user-defined TVP table types.
- A domain-driven SDK (`src/<domain>/`) layered on top of the schema, with Zod input validation per command.
- Three test layers: SQL-surface tests (`tests/sql/`), domain SDK tests (`tests/domain/`), and integration tests (`tests/integration/`) covering observer events, locks, vault, impersonation, and MCP discovery.
- TVP-driven bulk operations on real data — three bulk procedures and one inline TVF that take table-typed parameters end-to-end through `noorm`'s SDK.


## Prerequisites


- Bun ≥ 1.3.
- Docker (for the MSSQL 2022 container).
- An existing noorm identity at `~/.noorm/identity.{key,pub,json}`. If you don't have one yet, run `noorm init` from an interactive terminal once (see `mssql-problems.md` gap #1 — `init` requires a TTY).

The MSSQL container is declared in `docker-compose.yml` at the monorepo root and listens on port `11433`. From the monorepo root:

    docker compose up -d


## Setup


The actual order matters — `noorm db create` requires an active named config, so configs must be imported before the database can be created (see `mssql-problems.md` gap #9). From this directory:

    bun install
    noorm config import dev.json
    noorm config import test.json
    noorm db create -c dev
    noorm db create -c test
    noorm config use dev
    noorm run build

`dev.json` / `test.json` are the per-config payloads matching the `Config` shape (`name`, `connection.{dialect,host,port,database,user,password}`, `isTest`, `protected`). The connection defaults are declared in `.noorm/settings.yml` under `stages.dev` and `stages.test`:

    host:     localhost
    port:     11433
    user:     SA
    password: NoOrm_Test123!
    dev DB:   noorm_llm_dev
    test DB:  noorm_llm_test  (isTest: true)


## Project layout


    sql/                      Numbered build buckets (built in order)
        00_types/             4 TVP table types
        01_reference/         Lookup tables (status, category, verb, etc.)
        02_tables/            Core entity tables (Memory, Tag, Project, ...)
        03_validators/        Schema-bound CHECK-constraint UDFs
        04_subtypes/          Subtype tables with inline named CHECKs
        05_binary/            Binary blob storage
        06_seeds/             Reference data + sentinel rows (test stage)
        07_functions/         Business UDFs and 1 TVF
        08_views/             18 role-scoped / projection views
        09_procedures/        73 .sql files (one CREATE per file — see gap #3)

    src/                      Per-domain SDK code
        agent/ artifact/ audit/ core/ memory/ milestone/
        note/ project/ tag/ task/
        index.ts              LlmMemoryDb facade

    tests/
        sql/                  SQL-surface tests (procs, views, functions, TVF, TVP edges)
        domain/               SDK / Zod / business-logic tests
        integration/          Observer, lock, vault, impersonation, MCP discovery
        helpers/              Shared bootstrap, resetApplicationData

    changes/                  Versioned change scripts (1 applied: 2026-05-10-add-memory-tag-color)


## Running tests


    bun test

The full suite is 215+ tests across the three layers. The bootstrap in `tests/helpers/test-context.ts` is memoised across files — `createContext` runs once per process. Per-test cleanup uses a hand-rolled `resetApplicationData(ctx)` that issues FK-ordered `DELETE` statements, **not** `db.truncate()` (which deadlocks on `sp_MSforeachtable` against this 38-table schema — see `mssql-problems.md` gap #6). `db.reset()` is also avoided because schema-bound validator UDFs block table drops (gap #5).


## Architectural decisions worth knowing


- **No triggers.** Subtype exclusivity (one parent row per subtype) and the `StateTransition` discriminator rules are enforced by inline named `CHECK` constraints in `sql/04_subtypes/` that call validator UDFs from `sql/03_validators/`. The same UDFs are reused defensively inside the bulk procedures.
- **Schema-bound validators reference only `01_reference` and `02_tables`.** They cannot reference subtype tables — those don't exist when the validators are created. Subtype membership is enforced by the discriminator `CHECK` alone, since each subtype's PK is the parent's FK.
- **No `GO` statements anywhere in `sql/`.** The noorm runner does not split MSSQL `GO` batches (gap #3). Several T-SQL DDL statements (`CREATE PROCEDURE`, `CREATE FUNCTION`, `CREATE VIEW`, `CREATE TYPE`) must be the only statement in their batch, so the procedure bucket is split into 73 one-statement files instead of being grouped by domain.
- **Tests don't call `db.reset()` or `db.truncate()`.** `resetApplicationData(ctx)` is the workaround for gaps #5 and #6.
- **Zod uses `.default(false)` for BIT flags, not `.optional()`.** When a Zod-parsed `undefined` reaches the SDK's named-parameter EXEC builder, it is sent as explicit `NULL` — which `NOT NULL` columns reject even when the proc has a `= 0` default (gap #8). Defaulting in Zod keeps the proc default knowledge in sync.
- **`noorm.run.file()` calls in tests pass `{ force: true }`.** Without it, the runner silently skips re-execution by checksum (gap #7) — a problem when an explicit re-seed is the whole point of the call.


## Bulk TVP usage


This example was built specifically to exercise MSSQL's table-valued parameters end-to-end through noorm's SDK.

Bulk procedures (`sql/09_procedures/12_bulk_tvp_*.sql`):

- `sp_Tag_Bulk_Attach_Memory` — attach many `(tag_id, memory_id)` pairs in one call.
- `sp_Memory_Bulk_Touch` — refresh `last_accessed_at` for many memory IDs.
- `sp_Task_Bulk_Depend` — declare many task-dependency edges in one call.

Inline table-valued function:

- `tvf_FilterMemoriesByTags` — given a `TagIdSet` TVP, return memories matching any of the supplied tags.

TVP edge cases — empty input, single-row input, and rows approaching SQL Server's ~2100-parameter limit — live in `tests/sql/tag-tvp-edge-cases.test.ts`. The 200-row vs. 500-row trade-off is documented inline.


## Known gaps and links


- `mssql-problems.md` — the 11 noorm gaps surfaced by Phase 1 and Phase 2 of this exercise (TTY-only `init`, missing `GO` splitting, silent build failures, teardown ordering, deadlocking truncate, checksum-skip on `run.file`, undefined-as-NULL, etc.).
- `REPORT.md` — post-Phase-2 summary of what was built, coverage, TVP findings, and T-SQL specifics.
