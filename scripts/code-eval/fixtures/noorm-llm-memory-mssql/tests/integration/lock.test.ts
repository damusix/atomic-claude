/**
 * Layer 3 — lock integration tests.
 *
 * Asserts that the noorm tool-level lock is acquired, surfaced via
 * status(), released by withLock() on success, and released even when
 * the wrapped callback throws.
 */
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import { attempt } from '@logosdx/utils';
import { sql } from 'kysely';

import { LockAcquireError } from '@noormdev/sdk';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await resetApplicationData(ctx);

    // Defensive: a prior test may have left a lock behind despite afterEach.
    const status = await ctx.noorm.lock.status();
    if (status.isLocked) await ctx.noorm.lock.forceRelease();

});

afterEach(async () => {

    // Belt-and-braces: never let a leaked lock contaminate other test files.
    const status = await ctx.noorm.lock.status();
    if (status.isLocked) await ctx.noorm.lock.forceRelease();

});

describe('lock: acquire / status / release lifecycle', () => {

    it('acquires, exposes the lock via status, and clears it on release', async () => {

        const lock = await ctx.noorm.lock.acquire({ timeout: 5_000 });
        if (!lock) throw new Error('acquire returned no lock');

        expect(typeof lock.lockedBy).toBe('string');
        expect(lock.lockedAt).toBeInstanceOf(Date);
        expect(lock.expiresAt).toBeInstanceOf(Date);

        const held = await ctx.noorm.lock.status();
        expect(held.isLocked).toBe(true);
        if (!held.lock) throw new Error('status reported isLocked=true but lock is null');
        expect(held.lock.lockedBy).toBe(lock.lockedBy);

        await ctx.noorm.lock.release();

        const free = await ctx.noorm.lock.status();
        expect(free.isLocked).toBe(false);
        expect(free.lock).toBeNull();

    });

});

describe('lock: withLock callback execution', () => {

    it('runs the callback while the lock is held and releases on success', async () => {

        const before = await ctx.noorm.lock.status();
        expect(before.isLocked).toBe(false);

        const result = await ctx.noorm.lock.withLock(async () => {

            const inner = await ctx.noorm.lock.status();
            return { value: 7, wasLocked: inner.isLocked };

        }, { timeout: 10_000 });

        expect(result.value).toBe(7);
        expect(result.wasLocked).toBe(true);

        const after = await ctx.noorm.lock.status();
        expect(after.isLocked).toBe(false);

    });

    it('still releases the lock if the callback throws', async () => {

        await expect(
            ctx.noorm.lock.withLock(async () => {

                throw new Error('boom');

            }, { timeout: 10_000 }),
        ).rejects.toThrow('boom');

        const after = await ctx.noorm.lock.status();
        expect(after.isLocked).toBe(false);

    });

});

describe('lock: same-identity reentrancy', () => {

    it('extends the existing lock when acquired again by the same identity', async () => {

        // The lock is identity-keyed: a second acquire from the same identity
        // does not throw — it extends the lease. This is the documented
        // behavior in src/core/lock/manager.ts (extendLock branch).
        const first = await ctx.noorm.lock.acquire({ timeout: 5_000 });
        if (!first) throw new Error('first acquire returned no lock');

        const second = await ctx.noorm.lock.acquire({ timeout: 10_000, wait: false });
        if (!second) throw new Error('second acquire returned no lock');

        // Same holder identity, refreshed expiry.
        expect(second.lockedBy).toBe(first.lockedBy);
        expect(second.expiresAt.getTime()).toBeGreaterThanOrEqual(first.expiresAt.getTime());

        await ctx.noorm.lock.release();

        const after = await ctx.noorm.lock.status();
        expect(after.isLocked).toBe(false);

    });

});

describe('lock: contention with a different identity', () => {

    // The SDK's lock namespace is bound to the bootstrap identity, so the
    // public API alone cannot simulate "another agent holds the lock."
    // We seed the lock tracking table directly with a foreign holder
    // (under the `noorm` schema on MSSQL — see src/core/shared/tables.ts)
    // and then assert that `acquire({ wait: false })` raises
    // LockAcquireError. This is the same lock state a real second agent
    // would produce; the runtime treatment by the manager is identical.
    //
    // After releasing the foreign lock, the bootstrap identity must be
    // able to acquire normally — proves the contention failure is purely
    // due to ownership, not a leftover row.

    const FOREIGN_IDENTITY = 'other-agent@contention.test';

    // The bootstrap helper creates the context with `config: 'test'`,
    // and the lock manager scopes by that exact config name. Hard-coding
    // it here keeps the contention setup deterministic and avoids extra
    // DB roundtrips just to learn the value.
    const CONFIG_NAME = 'test';

    it('rejects acquire({ wait: false }) when another identity already holds the lock', async () => {

        // Seed a foreign-held lock that won't expire during this test.
        // MSSQL/Postgres put the lock table under the `noorm` schema as
        // `noorm.lock` (see src/core/shared/tables.ts:74-81); only mysql
        // and sqlite use the prefixed `__noorm_lock__` form.
        const farFuture = new Date(Date.now() + 60_000);
        await sql`
            INSERT INTO noorm.[lock] (config_name, locked_by, expires_at, reason)
            VALUES (${CONFIG_NAME}, ${FOREIGN_IDENTITY}, ${farFuture}, '')
        `.execute(ctx.kysely);

        // From the bootstrap identity, this should fail without waiting.
        const [acquired, err] = await attempt(
            () => ctx.noorm.lock.acquire({ wait: false, timeout: 1_000 }),
        );

        // Cleanup BEFORE assertions so a failure here doesn't leave a
        // stuck row that breaks downstream tests in the file.
        await sql`
            DELETE FROM noorm.[lock]
            WHERE config_name = ${CONFIG_NAME} AND locked_by = ${FOREIGN_IDENTITY}
        `.execute(ctx.kysely);

        // The brief asks for either a thrown LockAcquireError or a
        // tuple-style failure. The SDK throws — `attempt()` translates
        // that to `[null, LockAcquireError]`.
        expect(err).toBeInstanceOf(Error);
        expect(err).toBeInstanceOf(LockAcquireError);
        expect(acquired).toBeNull();

        // After cleanup the bootstrap identity must again be able to acquire.
        const lock = await ctx.noorm.lock.acquire({ wait: false, timeout: 1_000 });
        if (!lock) throw new Error('post-cleanup acquire returned no lock');
        await ctx.noorm.lock.release();

        const final = await ctx.noorm.lock.status();
        expect(final.isLocked).toBe(false);

    });

});
