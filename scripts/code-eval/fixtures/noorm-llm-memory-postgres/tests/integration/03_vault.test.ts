import { homedir } from 'node:os';
import { join } from 'node:path';

import { afterAll, beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
let privateKey: string;

const TEST_KEY = 'TEST_KEY';

beforeAll(async () => {

    ({ ctx } = await bootstrap());

    // Load the user's private identity key from ~/.noorm/identity.key.
    // The vault encrypts/decrypts secrets with the per-user vault key, which
    // is unwrapped using the user's private key.
    const keyPath = join(homedir(), '.noorm', 'identity.key');
    privateKey = await Bun.file(keyPath).text();

    // First-time vault init may take longer than a regular call.
    const status = await ctx.noorm.vault.status();
    if (!status.isInitialized) {

        const [, err] = await ctx.noorm.vault.init();
        if (err) throw err;

    }

}, 10_000);

beforeEach(async () => {

    await truncateAll(ctx);

    // Make sure no stale TEST_KEY survives between tests.
    const exists = await ctx.noorm.vault.exists(TEST_KEY);
    if (exists) await ctx.noorm.vault.delete(TEST_KEY);

});

afterAll(async () => {

    const exists = await ctx.noorm.vault.exists(TEST_KEY);
    if (exists) await ctx.noorm.vault.delete(TEST_KEY);

});

describe('ctx.noorm.vault.init', () => {

    it('reports already-initialized via the [result, err] tuple on repeat init', async () => {

        // First call happened in beforeAll. A repeat init() returns a
        // non-null Error in the tuple's second slot ("Vault already
        // initialized") rather than throwing — callers can safely call
        // it without a try/catch and just check the tuple.
        const [, err] = await ctx.noorm.vault.init();
        expect(err).not.toBeNull();
        expect(err?.message).toContain('already initialized');

        const status = await ctx.noorm.vault.status();
        expect(status.isInitialized).toBe(true);
        expect(status.hasAccess).toBe(true);

    });

});

describe('ctx.noorm.vault.set / get', () => {

    it('stores a value and returns it via get()', async () => {

        const [, setErr] = await ctx.noorm.vault.set(TEST_KEY, 'test-value', privateKey);
        expect(setErr).toBeNull();

        const value = await ctx.noorm.vault.get(TEST_KEY, privateKey);
        expect(value).toBe('test-value');

    });

});

describe('ctx.noorm.vault.list', () => {

    it('includes a key after it is set', async () => {

        const [, setErr] = await ctx.noorm.vault.set(TEST_KEY, 'list-me', privateKey);
        expect(setErr).toBeNull();

        const keys = await ctx.noorm.vault.list();
        expect(Array.isArray(keys)).toBe(true);
        expect(keys).toContain(TEST_KEY);

    });

});

describe('ctx.noorm.vault.delete', () => {

    it('returns [true, null] and exists() reports false afterwards', async () => {

        const [, setErr] = await ctx.noorm.vault.set(TEST_KEY, 'delete-me', privateKey);
        expect(setErr).toBeNull();

        const [deleted, delErr] = await ctx.noorm.vault.delete(TEST_KEY);
        expect(delErr).toBeNull();
        expect(deleted).toBe(true);

        const stillThere = await ctx.noorm.vault.exists(TEST_KEY);
        expect(stillThere).toBe(false);

    });

});
