/**
 * Layer 3 — vault integration tests.
 *
 * Round-trips set/get/delete/list against ctx.noorm.vault using the
 * caller's identity private key from ~/.noorm/identity.key. The vault
 * encrypts secrets with the per-user vault key, which is unwrapped via
 * the X25519 private key.
 */
import { existsSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import { attempt } from '@logosdx/utils';

import { bootstrap } from '../helpers/test-context';

// Tests are tolerant of missing identity / encrypted-state mismatch in
// CI/dev environments — the suite skips per-test rather than failing
// when the vault cannot be initialized for the connected user.
//
// Skip evaluation note: `it.skipIf(condition)` reads `condition` at test
// registration time (when this file is evaluated), BEFORE `beforeAll`
// runs. We must therefore decide skippability synchronously, here, at
// the top of the module — `existsSync` on the identity key file is the
// signal we have. If the key is present but vault init fails for a
// deeper reason (state.enc mismatch, etc.), the assertion in `beforeAll`
// throws and the suite reports a real failure — that is the correct
// behavior because "key on disk but vault broken" is a regression worth
// surfacing, not silently skipping.
const KEY_PATH = join(homedir(), '.noorm', 'identity.key');
const HAS_IDENTITY_KEY = existsSync(KEY_PATH);

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
let privateKey: string;

const TEST_KEY = `LLM_MEM_MSSQL_TEST_${Date.now().toString(36)}`;

beforeAll(async () => {

    ({ ctx } = await bootstrap());

    if (!HAS_IDENTITY_KEY) {

        privateKey = '';
        return;

    }

    privateKey = (await Bun.file(KEY_PATH).text()).trim();

    // Initialize the vault for this database. init() is documented as
    // returning [null, Error('Vault already initialized')] on a second
    // call (see mssql-problems.md gap #11) — that's fine, treat it as
    // idempotent. We wrap in attempt() because some failure paths
    // (sync DER parse errors on exotic identity setups) throw rather
    // than returning a tuple.
    const [initResult, initThrew] = await attempt(() => ctx.noorm.vault.init());
    if (initThrew) throw initThrew;

    const [, initErr] = initResult;
    if (initErr && !/already initialized/i.test(initErr.message)) {

        throw initErr;

    }

    // Probe: round-trip set/delete to surface state.enc-vs-key mismatch
    // immediately rather than during the first assertion.
    const [, probeErr] = await attempt(
        () => ctx.noorm.vault.set('__vault_probe__', 'ok', privateKey),
    );
    if (probeErr) throw probeErr;

    await ctx.noorm.vault.delete('__vault_probe__');

}, 15_000);

afterAll(async () => {

    if (HAS_IDENTITY_KEY && ctx) {

        const exists = await ctx.noorm.vault.exists(TEST_KEY);
        if (exists) await ctx.noorm.vault.delete(TEST_KEY);

    }

});

beforeEach(async () => {

    if (!HAS_IDENTITY_KEY) return;

    // Per-test cleanliness for the unique key. We do not truncate
    // because vault rows live in noorm tracking tables that truncate
    // intentionally preserves.
    const exists = await ctx.noorm.vault.exists(TEST_KEY);
    if (exists) await ctx.noorm.vault.delete(TEST_KEY);

});

describe('vault: init is idempotent', () => {

    it.skipIf(!HAS_IDENTITY_KEY)('a second init() on an already-initialized vault returns the already-initialized error and leaves status intact', async () => {

        const [result, err] = await ctx.noorm.vault.init();
        expect(result).toBeNull();
        expect(err).toBeInstanceOf(Error);
        expect(err?.message).toMatch(/already initialized/i);

        const status = await ctx.noorm.vault.status();
        expect(status.isInitialized).toBe(true);
        expect(status.hasAccess).toBe(true);

    });

});

describe('vault: set / get round-trip', () => {

    it.skipIf(!HAS_IDENTITY_KEY)('stores an encrypted value and decrypts it back via get()', async () => {

        const [, setErr] = await ctx.noorm.vault.set(TEST_KEY, 'mssql-secret-value', privateKey);
        expect(setErr).toBeNull();

        const value = await ctx.noorm.vault.get(TEST_KEY, privateKey);
        expect(value).toBe('mssql-secret-value');

    });

    it.skipIf(!HAS_IDENTITY_KEY)('list() reflects a key after set() and exists() agrees', async () => {

        const [, setErr] = await ctx.noorm.vault.set(TEST_KEY, 'list-me', privateKey);
        expect(setErr).toBeNull();

        const keys = await ctx.noorm.vault.list();
        expect(Array.isArray(keys)).toBe(true);
        expect(keys).toContain(TEST_KEY);

        const exists = await ctx.noorm.vault.exists(TEST_KEY);
        expect(exists).toBe(true);

    });

});

describe('vault: delete removes the secret', () => {

    it.skipIf(!HAS_IDENTITY_KEY)('returns [true, null] and exists() reports false after delete', async () => {

        const [, setErr] = await ctx.noorm.vault.set(TEST_KEY, 'delete-me', privateKey);
        expect(setErr).toBeNull();

        const [deleted, delErr] = await ctx.noorm.vault.delete(TEST_KEY);
        expect(delErr).toBeNull();
        expect(deleted).toBe(true);

        const stillThere = await ctx.noorm.vault.exists(TEST_KEY);
        expect(stillThere).toBe(false);

    });

});

// Negative path: requesting a key that was never set should not return
// stale data or a usable string. We assert the actual SDK contract by
// observing what `get` does for a missing key — empirically this
// resolves to `null` (the encrypted-state row simply is not there).
describe('vault: get of an unknown key', () => {

    it.skipIf(!HAS_IDENTITY_KEY)('returns null for a key that was never set', async () => {

        const NEVER_SET = `NEVER_SET_KEY_LLM_MEM_${Date.now().toString(36)}`;

        // Defensive: ensure the key really doesn't exist before we probe.
        const exists = await ctx.noorm.vault.exists(NEVER_SET);
        expect(exists).toBe(false);

        const [value, err] = await attempt(() => ctx.noorm.vault.get(NEVER_SET, privateKey));

        // Either the SDK rejects with a clear "not found" error or it
        // resolves to a falsy value. We accept either contract — but it
        // must NOT be a non-empty string (which would imply leakage).
        if (err) {

            expect(err).toBeInstanceOf(Error);
            return;

        }

        // Resolved without error — value must be empty / null / undefined.
        const isEmpty = value === null || value === undefined || value === '';
        expect(isEmpty).toBe(true);

    });

});
