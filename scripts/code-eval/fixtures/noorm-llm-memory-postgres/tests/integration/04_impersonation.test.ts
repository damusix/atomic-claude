import { afterAll, beforeAll, beforeEach, describe, it, expect } from 'bun:test';
import { sql } from 'kysely';
import { attempt } from '@logosdx/utils';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

const TEST_ROLE = 'test_reader';

beforeAll(async () => {

    ({ ctx } = await bootstrap());

    // Find the connected user so we can grant the role to it. PG requires
    // GRANT <role> TO <login_user> WITH SET true before SET ROLE will work.
    const currentUserResult = await sql<{ current_user: string }>`SELECT current_user`.execute(ctx.kysely);
    const [currentUserRow] = currentUserResult.rows;
    if (!currentUserRow) throw new Error('SELECT current_user returned no rows');
    const connectedUser = currentUserRow.current_user;

    // Drop any prior leftover from a failed run, then create a minimal role.
    await sql`DROP ROLE IF EXISTS ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);
    await sql`CREATE ROLE ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);
    await sql`GRANT ${sql.raw(TEST_ROLE)} TO ${sql.raw(connectedUser)} WITH SET true`.execute(ctx.kysely);

    await sql`GRANT USAGE ON SCHEMA public TO ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);
    await sql`GRANT SELECT ON ALL TABLES IN SCHEMA public TO ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);
    await sql`GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);

}, 30_000);

beforeEach(async () => {

    await truncateAll(ctx);

});

afterAll(async () => {

    // Make sure we're not still wearing the test role on the main pool.
    await sql`RESET ROLE`.execute(ctx.kysely);

    // Revoke before drop to avoid "role X cannot be dropped because some
    // objects depend on it" warnings on PG.
    await sql`REVOKE ALL ON ALL TABLES IN SCHEMA public FROM ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);
    await sql`REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);
    await sql`REVOKE USAGE ON SCHEMA public FROM ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);
    await sql`DROP ROLE IF EXISTS ${sql.raw(TEST_ROLE)}`.execute(ctx.kysely);

});

describe('ctx.impersonate read-only role', () => {

    it('allows SELECT on Agent inside the impersonated scope', async () => {

        const rows = await ctx.impersonate(TEST_ROLE, async (scope) => {

            return scope.kysely.selectFrom('Agent').selectAll().execute();

        });

        // Sentinel Agent(0) is re-seeded by truncateAll, so we expect at least one row.
        expect(Array.isArray(rows)).toBe(true);
        expect(rows.length).toBeGreaterThanOrEqual(1);

    });

    it('control: the SAME INSERT succeeds when NOT impersonating (proves rejection is role-driven)', async () => {

        // If this insert fails, the next test's rejection could be a schema
        // problem rather than a permissions problem. Run it first so we know
        // the failure in the impersonated case is specifically about the
        // missing INSERT grant on test_reader.
        const inserted = await ctx.kysely
            .insertInto('Agent')
            .values({ name: 'control-insert', description: 'baseline before impersonated reject' })
            .returning(['agent_id', 'name'])
            .executeTakeFirst();

        if (!inserted) throw new Error('control insert returned no row');
        expect(typeof inserted.agent_id).toBe('number');
        expect(inserted.name).toBe('control-insert');

    });

    it('rejects INSERT into Agent inside the impersonated scope with PG code 42501 / permission denied', async () => {

        const [result, err] = await attempt(
            () => ctx.impersonate(TEST_ROLE, async (scope) => {

                return scope.kysely
                    .insertInto('Agent')
                    .values({ name: 'should-not-land', description: 'restricted role insert' })
                    .execute();

            }),
        );

        // attempt() returns [null, error] on failure (Go-style tuple). The
        // result is null, not undefined.
        expect(result).toBeNull();
        if (!err) throw new Error('expected INSERT under test_reader to reject — got success');

        // pg-driver surfaces SQLSTATE on the error object. The shape comes
        // from the untyped pg DatabaseError, which Kysely re-throws verbatim.
        // We accept either the structured `code` field OR the human-readable
        // 'permission denied' substring — PG guarantees at least one.
        const message = err instanceof Error ? err.message : String(err);
        // cast-justified: pg DatabaseError is exposed via @types/pg only at the
        // `pg` import surface; errors that bubble through Kysely arrive as
        // plain `Error` to the consumer's type-system view. The `code` field
        // is part of the documented runtime contract (see pg-protocol's
        // NoticeOrError interface). Casting to a narrow shape is the
        // minimum-surface way to read it without pulling pg-protocol types
        // into the example's test deps.
        const errWithCode = err as { code?: string }; // cast-justified: see comment above
        const code = errWithCode.code;

        const matchesCode    = code === '42501';
        const matchesMessage = /permission denied/i.test(message);

        if (!matchesCode && !matchesMessage) {

            throw new Error(
                `expected SQLSTATE 42501 or /permission denied/ — got code=${code ?? 'undefined'} message=${message}`,
            );

        }

        expect(matchesCode || matchesMessage).toBe(true);

        // Make sure no row landed despite the rejection — defence-in-depth
        // against an INSERT that throws AFTER committing (it shouldn't, but
        // assert observable state).
        const stragglers = await ctx.kysely
            .selectFrom('Agent')
            .select('agent_id')
            .where('name', '=', 'should-not-land')
            .execute();

        expect(stragglers).toHaveLength(0);

    });

});

describe('ctx.impersonate revert behavior', () => {

    it('reverts cleanly so the original user can INSERT after the scope ends', async () => {

        // Run an impersonated SELECT — callback mode auto-reverts on exit.
        await ctx.impersonate(TEST_ROLE, async (scope) => {

            await scope.kysely.selectFrom('Agent').selectAll().execute();

        });

        // Verify the main pool's connection is back to the original user
        // by performing an INSERT that the test_reader role would NOT be
        // allowed to do. If revert failed, this would also reject.
        const result = await ctx.kysely
            .insertInto('Agent')
            .values({ name: 'post-impersonation', description: 'still original user' })
            .returning('agent_id')
            .executeTakeFirst();

        if (!result) throw new Error('insert returned no row');
        expect(typeof result.agent_id).toBe('number');

    });

});
