# code-eval fixtures

Index-only fixtures for the `atomic code` engine end-to-end tests. None of these
projects are run or compiled; they exist so the extractor and resolution pipeline
can be exercised against realistic real-world SQL schemas.


## Fixtures


### `noorm-llm-memory-postgres/`

A full-surface noorm project targeting PostgreSQL. Schema: `Memory` table plus views,
procedures, functions, triggers, subtype tables, and junction tables that form a
complete LLM-memory + task-tracker backend.

### `noorm-llm-memory-mssql/`

The same schema targeting SQL Server (T-SQL). Structurally identical to the Postgres
variant — same object names, same FK graph — allowing both dialects to be compared
under a single set of assertions.


## What they demonstrate

- **Multi-dialect SQL lineage.** Two dialects, one schema — the extractor must correctly
  parse both PG `CREATE OR REPLACE PROCEDURE` bodies and T-SQL `CREATE OR ALTER PROCEDURE`
  bodies and resolve references to the same table names.
- **Blast radius.** The `Memory` table is referenced by objects of every SQL kind:
  a view (`vw_Memory`), a procedure (`sp_Memory_Update`), a scalar function
  (`fn_MemoryConfidence`), and a junction table (`Memory_Tag` via FK). The impact
  radius of `Memory` therefore spans four object kinds, making it a good regression
  target.


## Exploring blast radius by hand

Index one fixture (writes to `.claude/.atomic-index/atomic.db` inside the fixture dir).
Run from the repo root, since `--repo` takes a path relative to the current directory
(pass an absolute path to run from anywhere):

```sh
atomic --repo scripts/code-eval/fixtures/noorm-llm-memory-postgres code index
```

Then query the blast radius of the `Memory` table:

```sh
atomic --repo scripts/code-eval/fixtures/noorm-llm-memory-postgres code impact Memory
```

The output should include `vw_Memory`, `sp_Memory_Update`, `fn_MemoryConfidence`, and
`Memory_Tag` in the impact set.

Use the same commands with `noorm-llm-memory-mssql` to compare results across dialects.


## Go tests

The blast-radius assertions are encoded as Go e2e tests in:

    atomic/internal/codeintel/engine/blast_radius_e2e_test.go

Run them with:

```sh
cd atomic
go test ./internal/codeintel/engine/ -run BlastRadius -v
```

Each test indexes only the `sql/` subtree of its fixture into a `t.TempDir()` database
(no fixture files are ever written), resolves all references, then asserts:

1. The impact graph of `Memory` contains at least 10 nodes (regression guard).
2. `vw_Memory`, `fn_MemoryConfidence`, `sp_Memory_Update`, and `Memory_Tag` are all
   present in the impact set — one representative per SQL object kind.


## Other fixtures

- `embedded-sql-multilang/` — multi-language embedded SQL extraction corpus (C, Go,
  Java, Python, TypeScript, etc.). See its own directory for details.
- `tsql-lineage/` — T-SQL lineage edge fixtures (proc-scoped temp tables, OUTPUT INTO,
  PIVOT/UNPIVOT, column-level alias resolution). See `sales_ops.sql` for the 29-edge
  verification corpus.
