/**
 * Layer 3 — impersonation integration tests.
 *
 * Exercises ctx.impersonate() against MSSQL. The dialect strategy
 * issues `EXECUTE AS USER = '<name>'` and `REVERT`, scoped to a
 * dedicated connection that auto-reverts when the callback returns.
 *
 * The limited principal is a database user WITHOUT LOGIN — this is
 * the recommended pattern for application-side impersonation in MSSQL
 * because it does not require sysadmin to create a server-level login
 * and works inside any database the SA user can already CREATE USER in.
 */
import { afterAll, beforeAll, describe, expect, it } from 'bun:test';
import { attempt } from '@logosdx/utils';
import { sql } from 'kysely';

import { bootstrap } from '../helpers/test-context';

// The suite shares a single context across files via the memoised
// bootstrap helper; do not call ctx.disconnect() in afterAll.
//
// Skip semantics: `it.skipIf(condition)` reads its condition at
// registration time (before `beforeAll` runs), so we cannot use the
// outcome of an async DB call to gate skip vs. run. Instead we honor
// `NOORM_SKIP_IMPERSONATION_TESTS=1` for environments where the SA
// user lacks CREATE USER rights. When the env var is unset, beforeAll
// throws on a real DB-side setup failure — that is the correct shape:
// "SA cannot create a contained user" is a real env regression, not
// something to silently skip past as green.
const SKIP_IMPERSONATION = process.env.NOORM_SKIP_IMPERSONATION_TESTS === '1';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

const TEST_USER = 'noorm_limited_reader';

beforeAll(async () => {

    ({ ctx } = await bootstrap());

    if (SKIP_IMPERSONATION) return;

    // Drop any leftover from a prior crashed run, then create a
    // contained user with read-only access to MemoryDomain only.
    // CREATE USER ... WITHOUT LOGIN avoids needing master-level rights.
    await sql.raw(`
        IF EXISTS (SELECT 1 FROM sys.database_principals WHERE name = '${TEST_USER}')
        BEGIN
            REVOKE SELECT ON OBJECT::dbo.MemoryDomain FROM [${TEST_USER}];
            DROP USER [${TEST_USER}];
        END
    `).execute(ctx.kysely);

    await sql.raw(`CREATE USER [${TEST_USER}] WITHOUT LOGIN`).execute(ctx.kysely);
    await sql.raw(`GRANT SELECT ON OBJECT::dbo.MemoryDomain TO [${TEST_USER}]`).execute(ctx.kysely);

}, 30_000);

afterAll(async () => {

    if (SKIP_IMPERSONATION || !ctx) return;

    // Best-effort cleanup. Failures here should not mask test failures.
    await attempt(async () => {

        await sql.raw(`REVOKE SELECT ON OBJECT::dbo.MemoryDomain FROM [${TEST_USER}]`).execute(ctx.kysely);
        await sql.raw(`DROP USER [${TEST_USER}]`).execute(ctx.kysely);

    });

});

// NOTE: no per-test truncate. These tests are read-only against
// reference tables (MemoryDomain) and do a single Agent insert in
// the revert test. Running truncate between tests can deadlock with
// any pool connection that an impersonated scope is still releasing.
// The bootstrap already truncated and re-seeded once, so reference
// data is present when the suite starts.

describe('impersonate: SELECT on granted table succeeds', () => {

    it.skipIf(SKIP_IMPERSONATION)('reads MemoryDomain rows from inside the impersonated scope', async () => {

        const rows = await ctx.impersonate(TEST_USER, async (scope) => {

            return scope.kysely
                .selectFrom('MemoryDomain')
                .selectAll()
                .execute();

        });

        // Reference table is seeded by the schema build; expect at least one row.
        expect(Array.isArray(rows)).toBe(true);
        expect(rows.length).toBeGreaterThanOrEqual(1);

    });

});

describe('impersonate: SELECT on un-granted table is rejected', () => {

    it.skipIf(SKIP_IMPERSONATION)('rejects a SELECT on Memory because no SELECT grant was issued', async () => {

        await expect(
            ctx.impersonate(TEST_USER, async (scope) => {

                return scope.kysely
                    .selectFrom('Memory')
                    .selectAll()
                    .execute();

            }),
        ).rejects.toThrow();

    });

});

describe('impersonate: revert restores the original principal', () => {

    it.skipIf(SKIP_IMPERSONATION)('returns to the SA principal after the scope ends so privileged DML works again', async () => {

        // Run an impersonated read; callback mode auto-reverts on exit.
        await ctx.impersonate(TEST_USER, async (scope) => {

            await scope.kysely.selectFrom('MemoryDomain').selectAll().execute();

        });

        // After revert, the main pool's connection is back to SA. Insert into
        // Agent (which the limited reader has zero rights on) to prove it.
        const inserted = await ctx.kysely
            .insertInto('Agent')
            .values({ name: `post-impersonation-${Date.now()}`, description: 'still SA' })
            .output('inserted.agent_id as agent_id')
            .executeTakeFirst();

        if (!inserted) throw new Error('insert returned no row');
        expect(typeof inserted.agent_id).toBe('number');

    });

});
