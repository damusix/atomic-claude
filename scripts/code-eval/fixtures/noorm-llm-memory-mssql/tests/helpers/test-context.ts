/**
 * Shared test bootstrap. Always uses the `test` config so the
 * `requireTest` guard refuses to run if anyone accidentally sets
 * NOORM_CONFIG=dev. Wipes data only — schema is built once via
 * `noorm run build` against the test DB before the suite starts.
 *
 * Memoised across test files so a single `bun test` process pays the
 * full setup cost exactly once. Subsequent test files share the same
 * context; per-test cleanliness is the responsibility of each suite's
 * `beforeEach` (typically `await ctx.noorm.db.truncate()` followed by
 * a re-seed of `sql/06_seeds/11_Sentinels.sql.tmpl`). Memoisation also
 * sidesteps cross-file deadlocks that occur on MSSQL when two short-lived
 * pools concurrently call `db.truncate()` (sp_MSforeach_worker fights
 * for schema locks across connections).
 *
 * Why not `db.reset()`: `db.reset()` works correctly now (teardown #36
 * fixed the drop ordering, and reset() no longer preserves the reference
 * vocabulary so the rebuild doesn't collide). We build once + `truncate()`
 * per test purely for speed — a full DDL rebuild on every `beforeEach`
 * would be far slower than wiping rows.
 *
 * @example
 * let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
 * let db: Awaited<ReturnType<typeof bootstrap>>['db'];
 *
 * beforeAll(async () => { ({ ctx, db } = await bootstrap()); });
 * beforeEach(async () => {
 *     await ctx.noorm.db.truncate();
 *     await ctx.noorm.run.file('sql/06_seeds/11_Sentinels.sql.tmpl');
 * });
 *
 * Do NOT call `ctx.disconnect()` in `afterAll`. The context is shared
 * across files; disconnecting it in one file breaks the others.
 */
import { createContext } from '@noormdev/sdk';

import { LlmMemoryDb } from '../../src';

import type { DB, Funcs, Procs, Tvfs } from '../../src';

type Bundle = {
    ctx: Awaited<ReturnType<typeof createContext<DB, Procs, Funcs, Tvfs>>>;
    db: LlmMemoryDb;
};

/**
 * Re-execute the Sentinel seed template even when its checksum matches
 * the prior run. `noorm.run.file` skips by default — required after a
 * truncate that wiped the rows but left the checksum intact in the
 * `__noorm_*` tracking tables.
 *
 * Exported so individual test files can call it inside `beforeEach`.
 */
export async function reseedSentinels(ctx: Bundle['ctx']): Promise<void> {

    await ctx.noorm.run.file('sql/06_seeds/11_Sentinels.sql.tmpl', { force: true });

}

/**
 * Per-test data wipe. Replaces `ctx.noorm.db.truncate()` because the
 * noorm built-in uses `sp_MSforeachtable` under the hood, which spawns
 * parallel DELETEs and deadlocks against itself on this schema (38
 * tables, many composite FKs). This helper deletes in explicit FK
 * dependency order over a single connection.
 *
 * Reference tables are preserved by this routine the same way they
 * are by `settings.yml > teardown.preserveTables`. Sentinel rows are
 * re-seeded at the end.
 */
const APPLICATION_TABLES_DELETE_ORDER = [
    'Task_Dependency',
    'Task_Artifact',
    'Milestone_Artifact',
    'Related_Memory',
    'Project_Milestone',
    'Project_Memory',
    'Task_Tag',
    'Milestone_Tag',
    'Artifact_Tag',
    'Memory_Tag',
    'Project_Tag',
    'Artifact_StateTransition',
    'Note_StateTransition',
    'Memory_StateTransition',
    'Task_StateTransition',
    'Milestone_StateTransition',
    'Task_Note',
    'Milestone_Note',
    'Project_Note',
    'Task',
    'StateTransition',
    'Milestone',
    'Artifact',
    'Memory',
    'Tag',
    'Note',
    'Project',
    'Agent',
] as const;

export async function resetApplicationData(ctx: Bundle['ctx']): Promise<void> {

    const { sql } = await import('kysely');

    for (const table of APPLICATION_TABLES_DELETE_ORDER) {

        await sql.raw(`DELETE FROM [dbo].[${table}]`).execute(ctx.kysely);

    }

    await reseedSentinels(ctx);

}

let cachedBundle: Promise<Bundle> | null = null;

export async function bootstrap(): Promise<Bundle> {

    if (cachedBundle) return cachedBundle;

    cachedBundle = (async (): Promise<Bundle> => {

        const ctx = await createContext<DB, Procs, Funcs, Tvfs>({
            config: 'test',
            requireTest: true,
        });

        await ctx.connect();

        // Use the same FK-ordered DELETE helper that test files use in
        // `beforeEach`. Avoids `db.truncate()` which deadlocks with
        // itself on this 38-table schema.
        await resetApplicationData(ctx);

        const db = new LlmMemoryDb(ctx);

        return { ctx, db };

    })();

    return cachedBundle;

}
