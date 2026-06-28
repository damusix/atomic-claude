import { createContext, LlmMemoryDb } from '../../src/index';

import type { Context, DB, Procs, Funcs, Tvfs } from '../../src/index';

export type TestBundle = {
    ctx: Context<DB, Procs, Funcs, Tvfs>;
    db: LlmMemoryDb;
};

let cachedBundle: Promise<TestBundle> | null = null;

/**
 * Returns a connected, schema-rebuilt SDK context + facade against the test DB.
 *
 * Memoised across test files so a single `bun test` process pays the full
 * teardown + build cost exactly once. Subsequent test files share the same
 * context; per-test cleanliness is the responsibility of `truncateAll()`
 * called from each suite's `beforeEach`.
 *
 * Steps (only on first call):
 *   1. createContext({ config: 'test', requireTest: true }) — refuses to run
 *      against a non-test config (safety guard).
 *   2. ctx.connect() — opens the connection pool.
 *   3. ctx.noorm.db.reset() — teardown + build for a deterministic schema.
 *   4. Re-seed the reference values + sentinel rows by re-running the seed
 *      templates against the freshly built schema. db.reset() rebuilds the
 *      schema from sql/ but doesn't re-run seeds for the test config since
 *      ctx.noorm.run.build() relies on settings.rules to include sql/05_seeds
 *      based on isTest, which IS true for this config.
 *
 * @example
 * ```typescript
 * import { beforeAll, afterAll, beforeEach, it, expect } from 'bun:test';
 * import { bootstrap, truncateAll } from '../helpers/test-context';
 *
 * let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
 * let db:  Awaited<ReturnType<typeof bootstrap>>['db'];
 *
 * beforeAll(async () => { ({ ctx, db } = await bootstrap()); });
 * beforeEach(async () => { await truncateAll(ctx); });
 * ```
 */
export async function bootstrap(): Promise<TestBundle> {

    if (cachedBundle) return cachedBundle;

    cachedBundle = (async () => {

        const ctx = await createContext({ config: 'test', requireTest: true });

        await ctx.connect();
        await ctx.noorm.db.reset();
        await ctx.noorm.changes.ff();

        return { ctx, db: new LlmMemoryDb(ctx) };

    })();

    return cachedBundle;

}

/**
 * Truncates every application table (preserves schema + reference seed data).
 *
 * Test isolation between `it()` blocks. Re-runs the seed templates after
 * truncating because `ctx.noorm.db.truncate()` wipes the reference-table
 * rows along with the application data — and the procs need those rows.
 */
export async function truncateAll(ctx: Context<DB, Procs, Funcs, Tvfs>): Promise<void> {

    await ctx.noorm.db.truncate();
    await ctx.noorm.run.dir('sql/05_seeds');

}
