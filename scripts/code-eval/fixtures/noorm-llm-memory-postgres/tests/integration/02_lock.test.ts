import { afterEach, beforeAll, beforeEach, describe, it, expect } from 'bun:test';
import { sql } from 'kysely';
import { attempt } from '@logosdx/utils';

import { LockAcquireError } from '@noormdev/sdk';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

    // Defensive: another test may have left a lock behind.
    const status = await ctx.noorm.lock.status();
    if (status.isLocked) await ctx.noorm.lock.forceRelease();

});

afterEach(async () => {

    // Belt-and-braces: never let a test leak a lock to the next file.
    const status = await ctx.noorm.lock.status();
    if (status.isLocked) await ctx.noorm.lock.forceRelease();

});

describe('ctx.noorm.lock.acquire / status / release', () => {

    it('acquires the lock, status reflects it, and release clears it', async () => {

        const lock = await ctx.noorm.lock.acquire({ timeout: 5_000 });
        if (!lock) throw new Error('acquire returned no lock');

        expect(typeof lock.lockedBy).toBe('string');
        expect(lock.lockedAt).toBeInstanceOf(Date);
        expect(lock.expiresAt).toBeInstanceOf(Date);

        const held = await ctx.noorm.lock.status();
        expect(held.isLocked).toBe(true);
        if (!held.lock) throw new Error('status reported isLocked but lock is null');
        expect(held.lock.lockedBy).toBe(lock.lockedBy);

        await ctx.noorm.lock.release();

        const free = await ctx.noorm.lock.status();
        expect(free.isLocked).toBe(false);
        expect(free.lock).toBeNull();

    });

});

describe('ctx.noorm.lock.withLock', () => {

    it('runs the callback while the lock is held, then auto-releases', async () => {

        const result = await ctx.noorm.lock.withLock(async () => {

            const inner = await ctx.noorm.lock.status();
            return { value: 42, wasLocked: inner.isLocked };

        }, { timeout: 10_000 });

        expect(result.value).toBe(42);
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

describe('ctx.noorm.lock.acquire concurrent', () => {

    it('is re-entrant for the same identity — second acquire succeeds while first is held', async () => {

        const first = await ctx.noorm.lock.acquire({ timeout: 5_000 });
        if (!first) throw new Error('first acquire returned no lock');

        // noorm locks are scoped per-identity. A second acquire from the
        // same context's identity does NOT reject — it returns successfully
        // and the existing hold is extended/refreshed. This is the
        // re-entrant pattern that lets nested SDK calls share one lock.
        const second = await ctx.noorm.lock.acquire({ timeout: 5_000, wait: false });
        expect(second).toBeDefined();

        // status() reports the lock as held.
        const status = await ctx.noorm.lock.status();
        expect(status.isLocked).toBe(true);

        await ctx.noorm.lock.release();

        // After release we should be able to acquire again cleanly.
        const third = await ctx.noorm.lock.acquire({ timeout: 5_000 });
        if (!third) throw new Error('third acquire returned no lock');
        await ctx.noorm.lock.release();

    });

    it('throws LockAcquireError when another identity already holds the lock and wait=false', async () => {

        // Simulate cross-identity contention by inserting a fresh lock row
        // owned by a DIFFERENT identity directly into noorm.lock (the noorm
        // lock table for postgres lives in the `noorm` schema, not the
        // application's public schema — see getNoormTables('postgres') in
        // the SDK). The SDK's lock namespace doesn't expose an identity
        // override, but the table IS the source of truth — so a row with a
        // foreign locked_by is functionally indistinguishable from a real
        // second context.
        const otherIdentity = 'other-user@somewhere.test|host:other-host';
        const now = new Date();
        const expiresAt = new Date(now.getTime() + 60_000);

        await sql`
            INSERT INTO noorm.lock (config_name, locked_by, locked_at, expires_at, reason)
            VALUES (${'test'}, ${otherIdentity}, ${now}, ${expiresAt}, ${'cross-identity contention test'})
        `.execute(ctx.kysely);

        // Sanity check — status() reflects the foreign hold.
        const held = await ctx.noorm.lock.status();
        expect(held.isLocked).toBe(true);
        if (!held.lock) throw new Error('status reported isLocked but lock is null');
        expect(held.lock.lockedBy).toBe(otherIdentity);

        // Now attempt to acquire from ctx (different identity) with wait=false.
        // This must surface LockAcquireError, not silently succeed and not
        // swallow the foreign hold via the re-entrant path.
        const [acquired, err] = await attempt(
            () => ctx.noorm.lock.acquire({ wait: false, timeout: 5_000 }),
        );

        // attempt() returns [null, error] on failure (Go-style tuple).
        expect(acquired).toBeNull();
        expect(err).toBeInstanceOf(LockAcquireError);

        // The error must carry the foreign holder so callers can render
        // useful diagnostics. Assert both the structured field (the public
        // contract) and the rendered message (the human-readable surface).
        if (err instanceof LockAcquireError) {

            expect(err.holder).toBe(otherIdentity);
            expect(err.message).toContain(otherIdentity);

        }

        // Lock state is unchanged — the foreign row is still there, our
        // failed acquire didn't mutate anything.
        const stillHeld = await ctx.noorm.lock.status();
        expect(stillHeld.isLocked).toBe(true);
        if (!stillHeld.lock) throw new Error('foreign lock vanished after failed acquire');
        expect(stillHeld.lock.lockedBy).toBe(otherIdentity);

        // Cleanup happens via afterEach forceRelease(); no manual cleanup needed.

    });

});
